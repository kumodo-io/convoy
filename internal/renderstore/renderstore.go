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

func historyKey(namespace, name, renderRef string, syncedAt metav1.Time) string {
	ts := syncedAt.UTC().Format(historyTimeLayout)
	return historyDir(namespace, name) + ts + "-" + renderRef + ".json"
}

func releasePrefix(namespace, name string) string {
	return namespace + "/" + name + "/"
}

// PutRender stores a render's content addressed by renderRef. It is idempotent:
// the same renderRef always carries the same bytes, so a repeat Put (e.g. a
// reconcile retry) re-stores identical content and dedups. It does not touch the
// history log — that is a separate commit step (AppendHistory).
func (s *RenderStore) PutRender(ctx context.Context, namespace, name, renderRef string, content []byte) error {
	if renderRef == "" {
		return fmt.Errorf("renderstore: empty renderRef")
	}
	return s.bs.Put(ctx, blobKey(namespace, name, renderRef), content)
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
