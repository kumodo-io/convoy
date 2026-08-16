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

package renderstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kumodo-io/convoy/internal/blobstore"
	"github.com/kumodo-io/convoy/internal/renderstore"
)

const (
	ns   = "default"
	name = "podinfo"
	refA = "sha256:a"
	refB = "sha256:b"
	refC = "sha256:c"
)

func at(sec int) metav1.Time {
	return metav1.NewTime(time.Date(2026, 8, 15, 12, 0, sec, 0, time.UTC))
}

func newStore() *renderstore.RenderStore {
	return renderstore.New(blobstore.NewMemory())
}

// TestPutGetRenderRoundTrip also confirms a real "sha256:" renderRef (with its
// colon) is a safe object key.
func TestPutGetRenderRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	ref := "sha256:deadbeef"
	content := []byte("apiVersion: v1\nkind: ConfigMap\n")

	if err := s.PutRender(ctx, ns, name, ref, content); err != nil {
		t.Fatalf("PutRender: %v", err)
	}
	got, err := s.GetRender(ctx, ns, name, ref)
	if err != nil {
		t.Fatalf("GetRender: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("GetRender = %q, want %q", got, content)
	}
	if ok, _ := s.HasRender(ctx, ns, name, ref); !ok {
		t.Errorf("HasRender = false, want true")
	}
}

func TestGetRenderMissingIsErrNotFound(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	if _, err := s.GetRender(ctx, ns, name, "sha256:absent"); !errors.Is(err, blobstore.ErrNotFound) {
		t.Errorf("GetRender(absent) err = %v, want ErrNotFound", err)
	}
	if ok, _ := s.HasRender(ctx, ns, name, "sha256:absent"); ok {
		t.Errorf("HasRender(absent) = true, want false")
	}
}

func TestPutRenderEmptyRefRejected(t *testing.T) {
	if err := newStore().PutRender(context.Background(), ns, name, "", []byte("x")); err == nil {
		t.Errorf("PutRender with empty renderRef: want error, got nil")
	}
}

// TestHistoryOrderedChronologically appends out of order and expects oldest-first.
func TestHistoryOrderedChronologically(t *testing.T) {
	ctx := context.Background()
	s := newStore()

	// Append at t=30, t=10, t=20 (out of order); include a repeated renderRef to
	// confirm the same render deployed twice yields two distinct entries.
	for _, e := range []renderstore.HistoryEntry{
		{RenderRef: "sha256:c", SyncedAt: at(30), Summary: "third"},
		{RenderRef: refA, SyncedAt: at(10), Summary: "first"},
		{RenderRef: refA, SyncedAt: at(20), Summary: "second (rollback to a)"},
	} {
		if err := s.AppendHistory(ctx, ns, name, e); err != nil {
			t.Fatalf("AppendHistory: %v", err)
		}
	}

	hist, err := s.History(ctx, ns, name)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) != 3 {
		t.Fatalf("History len = %d, want 3", len(hist))
	}
	wantOrder := []string{"first", "second (rollback to a)", "third"}
	for i, w := range wantOrder {
		if hist[i].Summary != w {
			t.Errorf("history[%d].Summary = %q, want %q (chronological)", i, hist[i].Summary, w)
		}
	}
}

// TestLastHistoryRef backs the controller's duplicate-suppression: it must report
// the most recent entry's renderRef (parsed from the key), or "" for an empty log.
func TestLastHistoryRef(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	if ref, err := s.LastHistoryRef(ctx, ns, name); err != nil || ref != "" {
		t.Fatalf("LastHistoryRef on empty log = %q,%v; want \"\",nil", ref, err)
	}
	_ = s.AppendHistory(ctx, ns, name, renderstore.HistoryEntry{RenderRef: refA, SyncedAt: at(10)})
	_ = s.AppendHistory(ctx, ns, name, renderstore.HistoryEntry{RenderRef: "sha256:b", SyncedAt: at(20)})
	ref, err := s.LastHistoryRef(ctx, ns, name)
	if err != nil {
		t.Fatalf("LastHistoryRef: %v", err)
	}
	if ref != "sha256:b" {
		t.Errorf("LastHistoryRef = %q, want sha256:b (most recent)", ref)
	}
}

func TestHistoryEmpty(t *testing.T) {
	hist, err := newStore().History(context.Background(), ns, name)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) != 0 {
		t.Errorf("History on empty store = %d entries, want 0", len(hist))
	}
}

func TestAppendHistoryValidation(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	if err := s.AppendHistory(ctx, ns, name, renderstore.HistoryEntry{SyncedAt: at(1)}); err == nil {
		t.Errorf("AppendHistory with empty renderRef: want error")
	}
	if err := s.AppendHistory(ctx, ns, name, renderstore.HistoryEntry{RenderRef: refA}); err == nil {
		t.Errorf("AppendHistory with zero syncedAt: want error")
	}
}

// seedDeploys stores three renders (a,b,c) with blobs and history at t=10,20,30.
func seedDeploys(t *testing.T, s *renderstore.RenderStore) {
	t.Helper()
	ctx := context.Background()
	for i, ref := range []string{refA, refB, refC} {
		if err := s.PutRender(ctx, ns, name, ref, []byte(ref)); err != nil {
			t.Fatalf("PutRender %s: %v", ref, err)
		}
		if err := s.AppendHistory(ctx, ns, name, renderstore.HistoryEntry{RenderRef: ref, SyncedAt: at(10 * (i + 1))}); err != nil {
			t.Fatalf("AppendHistory %s: %v", ref, err)
		}
	}
}

func has(t *testing.T, s *renderstore.RenderStore, ref string) bool {
	t.Helper()
	ok, err := s.HasRender(context.Background(), ns, name, ref)
	if err != nil {
		t.Fatalf("HasRender %s: %v", ref, err)
	}
	return ok
}

// TestHistoryCount reports the number of deploy-log entries.
func TestHistoryCount(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	if n, err := s.HistoryCount(ctx, ns, name); err != nil || n != 0 {
		t.Fatalf("HistoryCount empty = %d,%v; want 0,nil", n, err)
	}
	seedDeploys(t, s)
	if n, err := s.HistoryCount(ctx, ns, name); err != nil || n != 3 {
		t.Errorf("HistoryCount = %d,%v; want 3,nil", n, err)
	}
}

// TestGC prunes history to the newest keepHistory entries and reclaims the blobs
// no longer referenced.
func TestGC(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	seedDeploys(t, s)

	if err := s.GC(ctx, ns, name, 2); err != nil {
		t.Fatalf("GC: %v", err)
	}
	hist, _ := s.History(ctx, ns, name)
	if len(hist) != 2 || hist[0].RenderRef != refB || hist[1].RenderRef != refC {
		t.Fatalf("history after GC = %v, want [b c]", hist)
	}
	if has(t, s, refA) {
		t.Error("blob a should be reclaimed (pruned from history, not protected)")
	}
	if !has(t, s, refB) || !has(t, s, refC) {
		t.Error("blobs b and c should survive (still in history)")
	}
}

// TestGCProtectsRefs keeps a protected render's blob even when its history entry
// is pruned — e.g. the current render or an active rollback pin.
func TestGCProtectsRefs(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	seedDeploys(t, s)

	// Keep only the newest (c); protect a (pruned from history) explicitly.
	if err := s.GC(ctx, ns, name, 1, refA); err != nil {
		t.Fatalf("GC: %v", err)
	}
	if !has(t, s, refA) {
		t.Error("protected blob a should survive despite history prune")
	}
	if has(t, s, refB) {
		t.Error("blob b should be reclaimed (pruned and unprotected)")
	}
	if !has(t, s, refC) {
		t.Error("blob c should survive (newest history entry)")
	}
}

// TestGCKeepZeroRetainsAll confirms keepHistory<=0 disables GC entirely.
func TestGCKeepZeroRetainsAll(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	seedDeploys(t, s)

	if err := s.GC(ctx, ns, name, 0); err != nil {
		t.Fatalf("GC: %v", err)
	}
	if hist, _ := s.History(ctx, ns, name); len(hist) != 3 {
		t.Errorf("history = %d entries, want 3 (GC disabled)", len(hist))
	}
	if !has(t, s, refA) || !has(t, s, refB) || !has(t, s, refC) {
		t.Error("all blobs should survive when GC is disabled")
	}
}

// TestDeleteReleaseDropsPrefix confirms delete removes this release's blobs and
// history but leaves another release untouched.
func TestDeleteReleaseDropsPrefix(t *testing.T) {
	ctx := context.Background()
	s := newStore()

	_ = s.PutRender(ctx, ns, name, refA, []byte("a"))
	_ = s.AppendHistory(ctx, ns, name, renderstore.HistoryEntry{RenderRef: refA, SyncedAt: at(1)})
	// A different release that must survive.
	_ = s.PutRender(ctx, ns, "other", "sha256:z", []byte("z"))

	if err := s.DeleteRelease(ctx, ns, name); err != nil {
		t.Fatalf("DeleteRelease: %v", err)
	}
	if ok, _ := s.HasRender(ctx, ns, name, refA); ok {
		t.Errorf("blob survived DeleteRelease")
	}
	if hist, _ := s.History(ctx, ns, name); len(hist) != 0 {
		t.Errorf("history survived DeleteRelease: %d entries", len(hist))
	}
	if ok, _ := s.HasRender(ctx, ns, "other", "sha256:z"); !ok {
		t.Errorf("DeleteRelease removed a different release's blob")
	}
	// Idempotent.
	if err := s.DeleteRelease(ctx, ns, name); err != nil {
		t.Errorf("second DeleteRelease err = %v, want nil", err)
	}
}
