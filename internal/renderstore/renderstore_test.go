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
