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

package release

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/kumodo-io/convoy/internal/artifact"
	"github.com/kumodo-io/convoy/internal/blobstore"
	"github.com/kumodo-io/convoy/internal/fetch"
	"github.com/kumodo-io/convoy/internal/renderstore"
)

// TestSyncHandler_PersistsRenderAndHistory verifies persist-then-apply: a
// successful reconcile stores the render blob and records one history entry, and
// the blob is a reparseable manifest keyed by the release's renderRef.
func TestSyncHandler_PersistsRenderAndHistory(t *testing.T) {
	cm := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n"
	coord := &fakeCoordinator{result: fetch.Result{
		Ready:    true,
		Revision: waveRevision,
		Artifact: artifact.Artifact{Files: map[string][]byte{"cm.yaml": []byte(cm)}},
	}}
	rel := manifestsRelease()
	rel.Spec.Content = manifestsContent("cm.yaml")
	deployer := &fakeDeployer{}
	c, _ := newTestController(t, rel, reachableCluster(), coord, deployer)

	bs := blobstore.NewMemory()
	c.renderStore = renderstore.New(bs)

	ctx := context.Background()
	if err := c.syncHandler(ctx, "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}

	// Exactly one render blob was persisted under this release's prefix.
	blobs, err := bs.List(ctx, "ns/r1/blobs/")
	if err != nil {
		t.Fatalf("List blobs: %v", err)
	}
	if len(blobs) != 1 {
		t.Fatalf("persisted %d blobs, want 1: %v", len(blobs), blobs)
	}

	// The blob content is a reparseable JSON manifest.
	raw, err := bs.Get(ctx, blobs[0])
	if err != nil {
		t.Fatalf("Get blob: %v", err)
	}
	var objs []map[string]any
	if err := json.Unmarshal(raw, &objs); err != nil {
		t.Fatalf("blob is not a JSON manifest array: %v", err)
	}
	if len(objs) != 1 || objs[0]["kind"] != "ConfigMap" {
		t.Errorf("blob manifest = %v, want one ConfigMap", objs)
	}

	// Exactly one history entry, referencing a render whose blob exists.
	hist, err := c.renderStore.History(ctx, "ns", "r1")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) != 1 {
		t.Fatalf("history has %d entries, want 1", len(hist))
	}
	if hist[0].RenderRef == "" || hist[0].SyncedAt.IsZero() {
		t.Errorf("history entry missing renderRef/syncedAt: %+v", hist[0])
	}
	if ok, _ := c.renderStore.HasRender(ctx, "ns", "r1", hist[0].RenderRef); !ok {
		t.Errorf("history renderRef %q has no stored blob", hist[0].RenderRef)
	}
}

// TestSyncHandler_PersistFailureBlocksApply verifies the persist-then-apply
// invariant: if the render can't be persisted, nothing is applied to the cluster.
func TestSyncHandler_PersistFailureBlocksApply(t *testing.T) {
	cm := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n"
	coord := &fakeCoordinator{result: fetch.Result{
		Ready:    true,
		Revision: waveRevision,
		Artifact: artifact.Artifact{Files: map[string][]byte{"cm.yaml": []byte(cm)}},
	}}
	rel := manifestsRelease()
	rel.Spec.Content = manifestsContent("cm.yaml")
	deployer := &fakeDeployer{}
	c, _ := newTestController(t, rel, reachableCluster(), coord, deployer)
	c.renderStore = renderstore.New(failingBlobStore{})

	// The reconcile surfaces the persist error (retryable); crucially, nothing was
	// applied — persist precedes apply.
	err := c.syncHandler(context.Background(), "ns/r1")
	if err == nil {
		t.Log("syncHandler returned nil (failure recorded on status); checking no apply happened")
	}
	if len(deployer.waveCalls) != 0 {
		t.Errorf("deployer applied %d wave(s) despite a persist failure; persist must precede apply", len(deployer.waveCalls))
	}
}

// failingBlobStore fails every write, to exercise the persist-then-apply guard.
type failingBlobStore struct{}

func (failingBlobStore) Put(context.Context, string, []byte) error { return errors.New("store down") }
func (failingBlobStore) Get(context.Context, string) ([]byte, error) {
	return nil, blobstore.ErrNotFound
}
func (failingBlobStore) Exists(context.Context, string) (bool, error)   { return false, nil }
func (failingBlobStore) List(context.Context, string) ([]string, error) { return nil, nil }
func (failingBlobStore) Delete(context.Context, string) error           { return nil }
