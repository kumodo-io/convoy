/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package renderstore layers convoy's render/history semantics onto a
// blobstore.BlobStore. It implements the durable, self-contained render history
// designed in docs/architecture/03-persistent-storage.md:
//
//	<namespace>/<name>/blobs/<renderRef>                  immutable, content-addressed render
//	<namespace>/<name>/history/<sortable-ts>-<renderRef>.json  ordered deploy log
//
// etcd holds only the *current* pointer (status.lastSuccessful.renderRef); this
// store is the authoritative ordered history that answers "all renders for a
// release, in order" for history, rollback, and historical diff. A single writer
// owns each release (see the sharding model), so appends never race.
package renderstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kumodo-io/convoy/internal/blobstore"
)

// historyTimeLayout is a fixed-width, UTC, nanosecond-precision timestamp whose
// lexical order equals chronological order — so a plain prefix List of history/
// returns the deploy log in order.
const historyTimeLayout = "20060102T150405.000000000Z"

// HistoryEntry is one record in a release's ordered deploy log: which render was
// synced, when, and a short human summary of the resolved source. It is
// deliberately decoupled from the API status types so the store carries no
// dependency on them.
type HistoryEntry struct {
	// RenderRef is the content hash of the render deployed by this entry; it keys
	// the blob under blobs/.
	RenderRef string `json:"renderRef"`
	// SyncedAt is when this render was applied. It orders the log and must be set.
	SyncedAt metav1.Time `json:"syncedAt"`
	// Summary is a short resolved-source description (e.g. "gitRepository/podinfo@<sha>").
	Summary string `json:"summary,omitempty"`
}

// RenderStore is a render/history store over a BlobStore. A nil *RenderStore is
// not usable; callers that run without durable storage hold a nil and guard on it.
type RenderStore struct {
	bs blobstore.BlobStore
}

// New returns a RenderStore backed by bs.
func New(bs blobstore.BlobStore) *RenderStore {
	return &RenderStore{bs: bs}
}

func blobKey(namespace, name, renderRef string) string {
	return namespace + "/" + name + "/blobs/" + renderRef
}

func historyDir(namespace, name string) string {
	return namespace + "/" + name + "/history/"
}

func blobsDir(namespace, name string) string {
	return namespace + "/" + name + "/blobs/"
}

func historyKey(namespace, name, renderRef string, syncedAt metav1.Time) string {
	ts := syncedAt.UTC().Format(historyTimeLayout)
	return historyDir(namespace, name) + ts + "-" + renderRef + ".json"
}

func releasePrefix(namespace, name string) string {
	return namespace + "/" + name + "/"
}

// PutRender stores a render's content addressed by renderRef. Blobs are
// **write-once and immutable**: because the key is the content hash, an existing
// blob already holds identical bytes, so PutRender leaves it untouched (an Exists
// check short-circuits the write). A stored render is therefore never rewritten
// after creation — only read or deleted — so it is tamper-evident and its mtime is
// stable across re-applies/rollbacks. It does not touch the history log — that is a
// separate commit step (AppendHistory).
func (s *RenderStore) PutRender(ctx context.Context, namespace, name, renderRef string, content []byte) error {
	if renderRef == "" {
		return fmt.Errorf("renderstore: empty renderRef")
	}
	key := blobKey(namespace, name, renderRef)
	if ok, err := s.bs.Exists(ctx, key); err != nil {
		return fmt.Errorf("renderstore: checking blob %s: %w", renderRef, err)
	} else if ok {
		return nil // already stored; immutable, do not rewrite
	}
	return s.bs.Put(ctx, key, content)
}

// GetRender returns a stored render's content, or blobstore.ErrNotFound if the
// renderRef was never persisted (e.g. a pre-storage render, or GC'd).
func (s *RenderStore) GetRender(ctx context.Context, namespace, name, renderRef string) ([]byte, error) {
	return s.bs.Get(ctx, blobKey(namespace, name, renderRef))
}

// HasRender reports whether a render blob exists. It backs /pin rollback
// validation: a rollback target is only valid if its render is still stored.
func (s *RenderStore) HasRender(ctx context.Context, namespace, name, renderRef string) (bool, error) {
	return s.bs.Exists(ctx, blobKey(namespace, name, renderRef))
}

// AppendHistory records a deploy in the ordered log. Callers append only when the
// render actually changed (a real deploy), not on every reconcile. The key
// encodes the sync time, so entries sort chronologically.
func (s *RenderStore) AppendHistory(ctx context.Context, namespace, name string, entry HistoryEntry) error {
	if entry.RenderRef == "" {
		return fmt.Errorf("renderstore: history entry has empty renderRef")
	}
	if entry.SyncedAt.IsZero() {
		return fmt.Errorf("renderstore: history entry has zero syncedAt")
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("renderstore: marshaling history entry: %w", err)
	}
	return s.bs.Put(ctx, historyKey(namespace, name, entry.RenderRef, entry.SyncedAt), data)
}

// HistoryCount returns the number of deploy-log entries currently stored for a
// release. It lists keys only (no content reads), so it is cheap to call on
// reconcile to populate status.resolvedRetentionPolicy.historyCount.
func (s *RenderStore) HistoryCount(ctx context.Context, namespace, name string) (int, error) {
	keys, err := s.bs.List(ctx, historyDir(namespace, name))
	if err != nil {
		return 0, fmt.Errorf("renderstore: counting history for %s/%s: %w", namespace, name, err)
	}
	return len(keys), nil
}

// HasHistoryRef reports whether the deploy log already contains an entry for
// renderRef. It reads only the key list (the renderRef is encoded in each key),
// so it is cheap enough to call on every deploy to keep history a set of *distinct*
// renders: a redeploy of an already-logged render (a rollback, or a re-reconcile
// whose view of status still lags in the informer cache) appends nothing. The
// store is read-after-write consistent, so it reflects an immediately-preceding
// AppendHistory.
func (s *RenderStore) HasHistoryRef(ctx context.Context, namespace, name, renderRef string) (bool, error) {
	refs, err := s.historyRefs(ctx, namespace, name)
	if err != nil {
		return false, err
	}
	for _, r := range refs {
		if r == renderRef {
			return true, nil
		}
	}
	return false, nil
}

// refFromHistoryKey extracts the renderRef from a history key of the form
// "…/history/<ts>-<renderRef>.json". The timestamp layout contains no "-" and the
// renderRef ("sha256:<hex>") contains none either, so the first "-" splits them.
func refFromHistoryKey(key string) string {
	base := key[strings.LastIndex(key, "/")+1:]
	base = strings.TrimSuffix(base, ".json")
	if i := strings.IndexByte(base, '-'); i >= 0 {
		return base[i+1:]
	}
	return ""
}

// History returns a release's deploy log in chronological order (oldest first).
// An empty log (no deploys, or a deleted release) returns an empty slice.
func (s *RenderStore) History(ctx context.Context, namespace, name string) ([]HistoryEntry, error) {
	keys, err := s.bs.List(ctx, historyDir(namespace, name))
	if err != nil {
		return nil, fmt.Errorf("renderstore: listing history for %s/%s: %w", namespace, name, err)
	}
	entries := make([]HistoryEntry, 0, len(keys))
	for _, key := range keys {
		data, err := s.bs.Get(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("renderstore: reading history entry %q: %w", key, err)
		}
		var e HistoryEntry
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, fmt.Errorf("renderstore: decoding history entry %q: %w", key, err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// GC applies retention to a release: it prunes the history log to the newest
// keepHistory entries, then deletes any blob no longer referenced by a surviving
// history entry or by one of protectedRefs. protectedRefs shields renders that
// aren't in the (pruned) log but must survive — the current/last-attempted
// render (covering the persist→commit window) and any active rollback pin.
//
// keepHistory <= 0 means "retain all history" (GC is effectively disabled); no
// blobs are reclaimed either, since every render stays referenced.
func (s *RenderStore) GC(ctx context.Context, namespace, name string, keepHistory int, protectedRefs ...string) error {
	if keepHistory <= 0 {
		return nil
	}
	if err := s.pruneHistory(ctx, namespace, name, keepHistory); err != nil {
		return err
	}
	// referenced = surviving history refs ∪ protectedRefs.
	refs, err := s.historyRefs(ctx, namespace, name)
	if err != nil {
		return err
	}
	referenced := make(map[string]bool, len(refs)+len(protectedRefs))
	for _, r := range refs {
		referenced[r] = true
	}
	for _, r := range protectedRefs {
		if r != "" {
			referenced[r] = true
		}
	}
	return s.gcBlobs(ctx, namespace, name, referenced)
}

// pruneHistory deletes all but the newest keep history entries (keys are ordered
// oldest-first, so the oldest len-keep are removed).
func (s *RenderStore) pruneHistory(ctx context.Context, namespace, name string, keep int) error {
	keys, err := s.bs.List(ctx, historyDir(namespace, name))
	if err != nil {
		return fmt.Errorf("renderstore: listing history for prune: %w", err)
	}
	if len(keys) <= keep {
		return nil
	}
	for _, key := range keys[:len(keys)-keep] {
		if err := s.bs.Delete(ctx, key); err != nil {
			return fmt.Errorf("renderstore: pruning history entry %q: %w", key, err)
		}
	}
	return nil
}

// historyRefs returns the renderRefs of the current history entries (parsed from
// the keys, no content reads).
func (s *RenderStore) historyRefs(ctx context.Context, namespace, name string) ([]string, error) {
	keys, err := s.bs.List(ctx, historyDir(namespace, name))
	if err != nil {
		return nil, fmt.Errorf("renderstore: listing history refs: %w", err)
	}
	refs := make([]string, 0, len(keys))
	for _, key := range keys {
		refs = append(refs, refFromHistoryKey(key))
	}
	return refs, nil
}

// gcBlobs deletes every blob whose renderRef is not in referenced.
func (s *RenderStore) gcBlobs(ctx context.Context, namespace, name string, referenced map[string]bool) error {
	keys, err := s.bs.List(ctx, blobsDir(namespace, name))
	if err != nil {
		return fmt.Errorf("renderstore: listing blobs for GC: %w", err)
	}
	prefix := blobsDir(namespace, name)
	for _, key := range keys {
		ref := strings.TrimPrefix(key, prefix)
		if referenced[ref] {
			continue
		}
		if err := s.bs.Delete(ctx, key); err != nil {
			return fmt.Errorf("renderstore: GC deleting blob %q: %w", key, err)
		}
	}
	return nil
}

// DeleteRelease drops a release's entire prefix (blobs + history). Used when a
// Release is finalized. It is idempotent.
func (s *RenderStore) DeleteRelease(ctx context.Context, namespace, name string) error {
	prefix := releasePrefix(namespace, name)
	keys, err := s.bs.List(ctx, prefix)
	if err != nil {
		return fmt.Errorf("renderstore: listing %q for delete: %w", prefix, err)
	}
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) { // defense-in-depth against a broad List
			continue
		}
		if err := s.bs.Delete(ctx, key); err != nil {
			return fmt.Errorf("renderstore: deleting %q: %w", key, err)
		}
	}
	return nil
}
