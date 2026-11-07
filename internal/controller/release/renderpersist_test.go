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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/artifact"
	"github.com/kumodo-io/convoy/internal/blobstore"
	"github.com/kumodo-io/convoy/internal/fetch"
	"github.com/kumodo-io/convoy/internal/render"
	"github.com/kumodo-io/convoy/internal/renderstore"
)

const (
	cmFile    = "cm.yaml"
	cmYAML    = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n"
	oldCMName = "old-cm"
)

// TestSyncHandler_PersistsRenderAndHistory verifies persist-then-apply: a
// successful reconcile stores the render blob and records one history entry, and
// the blob is a reparseable manifest keyed by the release's renderRef.
func TestSyncHandler_PersistsRenderAndHistory(t *testing.T) {
	cm := cmYAML
	coord := &fakeCoordinator{result: fetch.Result{
		Ready:    true,
		Revision: waveRevision,
		Artifact: artifact.Artifact{Files: map[string][]byte{cmFile: []byte(cm)}},
	}}
	rel := manifestsRelease()
	rel.Spec.Content = manifestsContent(cmFile)
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
	if len(objs) != 1 || objs[0]["kind"] != configMapKind {
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

// TestSyncHandler_HistoryDedupsAcrossReconciles guards against duplicate deploy-log
// entries: several reconciles of one deploy (lifecycle-hook phases) run before the
// status write lands in the informer cache, so each still sees an empty
// lastSuccessful. The test seeds no status and reconciles twice — the cache never
// updates between calls, reproducing that window — and asserts a single entry.
func TestSyncHandler_HistoryDedupsAcrossReconciles(t *testing.T) {
	cm := cmYAML
	coord := &fakeCoordinator{result: fetch.Result{
		Ready:    true,
		Revision: waveRevision,
		Artifact: artifact.Artifact{Files: map[string][]byte{cmFile: []byte(cm)}},
	}}
	rel := manifestsRelease()
	rel.Spec.Content = manifestsContent(cmFile)
	c, _ := newTestController(t, rel, reachableCluster(), coord, &fakeDeployer{})
	c.renderStore = renderstore.New(blobstore.NewMemory())

	ctx := context.Background()
	for i := range 3 {
		if err := c.syncHandler(ctx, "ns/r1"); err != nil {
			t.Fatalf("syncHandler #%d: %v", i, err)
		}
	}
	hist, err := c.renderStore.History(ctx, "ns", "r1")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) != 1 {
		t.Fatalf("history has %d entries after 3 reconciles of one render, want 1 (deduped)", len(hist))
	}
}

// TestSyncHandler_PersistFailureBlocksApply verifies the persist-then-apply
// invariant: if the render can't be persisted, nothing is applied to the cluster.
func TestSyncHandler_PersistFailureBlocksApply(t *testing.T) {
	cm := cmYAML
	coord := &fakeCoordinator{result: fetch.Result{
		Ready:    true,
		Revision: waveRevision,
		Artifact: artifact.Artifact{Files: map[string][]byte{cmFile: []byte(cm)}},
	}}
	rel := manifestsRelease()
	rel.Spec.Content = manifestsContent(cmFile)
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

// TestSyncHandler_RollbackAppliesStoredRender verifies a rollback pin: pinned to a
// past render present in the store, the controller applies that stored render's
// objects — not what source renders to now.
func TestSyncHandler_RollbackAppliesStoredRender(t *testing.T) {
	coord := &fakeCoordinator{result: fetch.Result{
		Ready:    true,
		Revision: waveRevision,
		Artifact: artifact.Artifact{Files: map[string][]byte{cmFile: []byte(cmYAML)}}, // current source renders "cm"
	}}
	rel := manifestsRelease()
	rel.Spec.Content = manifestsContent(cmFile)
	const rollbackRef = "sha256:rollback"
	rel.Pin = new(corev1alpha1.ReleasePin{RenderRef: rollbackRef})
	deployer := &fakeDeployer{}
	c, _ := newTestController(t, rel, reachableCluster(), coord, deployer)

	// Seed the store with a *different* past render (a ConfigMap named "old-cm").
	oldCM := &unstructured.Unstructured{Object: map[string]any{
		apiVersionKey: "v1", kindKey: configMapKind,
		metadataKey: map[string]any{nameKey: oldCMName, namespaceKey: "ns"},
	}}
	manifest, err := render.MarshalManifest([]*unstructured.Unstructured{oldCM})
	if err != nil {
		t.Fatalf("MarshalManifest: %v", err)
	}
	c.renderStore = renderstore.New(blobstore.NewMemory())
	if err := c.renderStore.PutRender(context.Background(), "ns", "r1", rollbackRef, manifest); err != nil {
		t.Fatalf("PutRender: %v", err)
	}

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if len(deployer.waveCalls) != 1 || len(deployer.waveCalls[0]) != 1 {
		t.Fatalf("expected one applied object, got waves %v", deployer.waveCalls)
	}
	if got := deployer.waveCalls[0][0].GetName(); got != oldCMName {
		t.Errorf("applied object = %q, want the stored render's %q (rollback)", got, oldCMName)
	}
}

// TestSyncHandler_RollbackDoesNotDuplicateHistory verifies that rolling back to a
// render already in the deploy log does not append a second entry for it — history
// is a set of distinct renders.
func TestSyncHandler_RollbackDoesNotDuplicateHistory(t *testing.T) {
	coord := &fakeCoordinator{result: fetch.Result{
		Ready:    true,
		Revision: waveRevision,
		Artifact: artifact.Artifact{Files: map[string][]byte{cmFile: []byte(cmYAML)}},
	}}
	rel := manifestsRelease()
	rel.Spec.Content = manifestsContent(cmFile)
	const rollbackRef = "sha256:rollback"
	rel.Pin = new(corev1alpha1.ReleasePin{RenderRef: rollbackRef})
	c, _ := newTestController(t, rel, reachableCluster(), coord, &fakeDeployer{})
	c.renderStore = renderstore.New(blobstore.NewMemory())

	ctx := context.Background()
	oldCM := &unstructured.Unstructured{Object: map[string]any{
		apiVersionKey: "v1", kindKey: configMapKind,
		metadataKey: map[string]any{nameKey: oldCMName, namespaceKey: "ns"},
	}}
	manifest, _ := render.MarshalManifest([]*unstructured.Unstructured{oldCM})
	_ = c.renderStore.PutRender(ctx, "ns", "r1", rollbackRef, manifest)
	// The rollback target was deployed before, so it already has a history entry.
	_ = c.renderStore.AppendHistory(ctx, "ns", "r1",
		renderstore.HistoryEntry{RenderRef: rollbackRef, SyncedAt: metav1.NewTime(time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC))})

	if err := c.syncHandler(ctx, "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	hist, _ := c.renderStore.History(ctx, "ns", "r1")
	if len(hist) != 1 {
		t.Fatalf("history has %d entries after rollback, want 1 (no duplicate for a re-deployed render)", len(hist))
	}
	if hist[0].RenderRef != rollbackRef {
		t.Errorf("history entry = %s, want the rollback ref %s", hist[0].RenderRef, rollbackRef)
	}
}

// TestSyncHandler_RollbackToMissingRenderHolds verifies a pin to a render absent
// from the store holds (does not apply) and reports Pinned.
func TestSyncHandler_RollbackToMissingRenderHolds(t *testing.T) {
	rel := manifestsRelease()
	rel.Pin = new(corev1alpha1.ReleasePin{RenderRef: "sha256:ghost"})
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	c.renderStore = renderstore.New(blobstore.NewMemory()) // empty store

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if deployer.called {
		t.Error("Deploy called despite the pinned render being absent from the store")
	}
	if got := statusUpdate(client.Actions()); got == nil {
		t.Fatalf("expected status write")
	} else if s, r := readyReason(got); s != metav1.ConditionFalse || r != conditionReasonPinned {
		t.Errorf("Ready = %s/%s, want False/Pinned", s, r)
	}
}

// TestFinalize_DeletesRenderStore verifies finalizing a Release drops its render
// store prefix (blobs + history).
func TestFinalize_DeletesRenderStore(t *testing.T) {
	rel := deletingRelease(true, false) // keepWorkloads: skip cluster prune
	c, _ := newTestController(t, rel, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	c.renderStore = renderstore.New(blobstore.NewMemory())

	ctx := context.Background()
	_ = c.renderStore.PutRender(ctx, "ns", "r1", "sha256:kept", []byte("x"))
	_ = c.renderStore.AppendHistory(ctx, "ns", "r1", renderstore.HistoryEntry{RenderRef: "sha256:kept", SyncedAt: metav1.Now()})

	if err := c.syncHandler(ctx, "ns/r1"); err != nil {
		t.Fatalf("syncHandler (finalize): %v", err)
	}
	if ok, _ := c.renderStore.HasRender(ctx, "ns", "r1", "sha256:kept"); ok {
		t.Error("render blob survived finalize")
	}
	if hist, _ := c.renderStore.History(ctx, "ns", "r1"); len(hist) != 0 {
		t.Errorf("history survived finalize: %d entries", len(hist))
	}
}

// TestSyncHandler_GCPrunesHistoryOnReconcile verifies a successful reconcile
// applies the retention policy: older history entries beyond keepHistory (and
// their blobs) are reclaimed.
func TestSyncHandler_GCPrunesHistoryOnReconcile(t *testing.T) {
	coord := &fakeCoordinator{result: fetch.Result{
		Ready:    true,
		Revision: waveRevision,
		Artifact: artifact.Artifact{Files: map[string][]byte{cmFile: []byte(cmYAML)}},
	}}
	rel := manifestsRelease()
	rel.Spec.Content = manifestsContent(cmFile)
	rel.Spec.RetentionPolicy.KeepHistory = new(1)
	c, _ := newTestController(t, rel, reachableCluster(), coord, &fakeDeployer{})
	c.renderStore = renderstore.New(blobstore.NewMemory())

	ctx := context.Background()
	// Two older deploys already in the log (before the fixed reconcile time).
	old := metav1.NewTime(time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC))
	for _, ref := range []string{"sha256:old1", "sha256:old2"} {
		_ = c.renderStore.PutRender(ctx, "ns", "r1", ref, []byte(ref))
		_ = c.renderStore.AppendHistory(ctx, "ns", "r1", renderstore.HistoryEntry{RenderRef: ref, SyncedAt: old})
	}

	if err := c.syncHandler(ctx, "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}

	// keepHistory=1 → only the newest entry (this reconcile's render) survives.
	hist, _ := c.renderStore.History(ctx, "ns", "r1")
	if len(hist) != 1 {
		t.Fatalf("history = %d entries after GC, want 1", len(hist))
	}
	if ok, _ := c.renderStore.HasRender(ctx, "ns", "r1", "sha256:old1"); ok {
		t.Error("pruned blob old1 should be reclaimed")
	}
}

// TestEffectiveKeepHistory covers the retention resolution: nil ⇒ default,
// explicit 0 ⇒ unlimited, explicit N ⇒ N.
func TestEffectiveKeepHistory(t *testing.T) {
	rel := manifestsRelease()
	rel.Spec.RetentionPolicy.KeepHistory = nil
	if got := effectiveKeepHistory(rel); got != defaultKeepHistory {
		t.Errorf("nil keepHistory = %d, want default %d", got, defaultKeepHistory)
	}
	rel.Spec.RetentionPolicy.KeepHistory = new(0)
	if got := effectiveKeepHistory(rel); got != 0 {
		t.Errorf("explicit 0 keepHistory = %d, want 0 (unlimited)", got)
	}
	rel.Spec.RetentionPolicy.KeepHistory = new(5)
	if got := effectiveKeepHistory(rel); got != 5 {
		t.Errorf("explicit 5 keepHistory = %d, want 5", got)
	}
}

// TestSyncHandler_PopulatesResolvedRetentionStatus verifies a reconcile reports
// the effective retention and the actual stored history count — the status bug
// where resolvedRetentionPolicy was never written.
func TestSyncHandler_PopulatesResolvedRetentionStatus(t *testing.T) {
	coord := &fakeCoordinator{result: fetch.Result{
		Ready:    true,
		Revision: waveRevision,
		Artifact: artifact.Artifact{Files: map[string][]byte{cmFile: []byte(cmYAML)}},
	}}
	rel := manifestsRelease()
	rel.Spec.Content = manifestsContent(cmFile)
	rel.Spec.RetentionPolicy.KeepHistory = new(0) // unlimited: nothing pruned
	c, client := newTestController(t, rel, reachableCluster(), coord, &fakeDeployer{})
	c.renderStore = renderstore.New(blobstore.NewMemory())

	// A pre-existing older deploy, plus this reconcile's — count should be 2.
	_ = c.renderStore.PutRender(context.Background(), "ns", "r1", "sha256:old", []byte("old"))
	_ = c.renderStore.AppendHistory(context.Background(), "ns", "r1",
		renderstore.HistoryEntry{RenderRef: "sha256:old", SyncedAt: metav1.NewTime(time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC))})

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	got := statusUpdate(client.Actions())
	if got == nil {
		t.Fatalf("expected status write")
	}
	rp := got.Status.ResolvedRetentionPolicy
	if rp.KeepHistory != 0 {
		t.Errorf("resolved keepHistory = %d, want 0 (unlimited)", rp.KeepHistory)
	}
	if rp.HistoryCount != 2 {
		t.Errorf("resolved historyCount = %d, want 2 (old + this reconcile)", rp.HistoryCount)
	}
}

// TestSyncHandler_DryRunFailureBlocksPersistAndApply verifies the pre-apply
// dry-run: when it fails, the render is neither persisted nor applied.
func TestSyncHandler_DryRunFailureBlocksPersistAndApply(t *testing.T) {
	coord := &fakeCoordinator{result: fetch.Result{
		Ready:    true,
		Revision: waveRevision,
		Artifact: artifact.Artifact{Files: map[string][]byte{cmFile: []byte(cmYAML)}},
	}}
	rel := manifestsRelease()
	rel.Spec.Content = manifestsContent(cmFile)
	deployer := &fakeDeployer{dryRunErr: errors.New("field is immutable")}
	c, client := newTestController(t, rel, reachableCluster(), coord, deployer)
	bs := blobstore.NewMemory()
	c.renderStore = renderstore.New(bs)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if !deployer.dryRunCalled {
		t.Error("dry-run was not called")
	}
	if deployer.called {
		t.Error("real apply happened despite a failing dry-run")
	}
	if blobs, _ := bs.List(context.Background(), "ns/r1/blobs/"); len(blobs) != 0 {
		t.Errorf("render was persisted despite a failing dry-run: %v", blobs)
	}
	if got := statusUpdate(client.Actions()); got == nil {
		t.Fatal("expected status write")
	} else if _, r := readyReason(got); r != conditionReasonUnappliable {
		t.Errorf("reason = %s, want Unappliable", r)
	}
}

// TestSyncHandler_LastHealthySetWhenRenderHealthy verifies status.lastHealthy is
// written to the current render only when that render is confirmed healthy (the
// observer's per-render assessment, via the health seam), and NOT otherwise.
func TestSyncHandler_LastHealthySetWhenRenderHealthy(t *testing.T) {
	run := func(healthy bool) *corev1alpha1.Release {
		rel := manifestsRelease()
		c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), &fakeDeployer{})
		setHealth(c, healthy)
		if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
			t.Fatalf("syncHandler: %v", err)
		}
		return statusUpdate(client.Actions())
	}

	t.Run("render healthy sets lastHealthy", func(t *testing.T) {
		got := run(true)
		if got == nil || got.Status.LastHealthy == nil {
			t.Fatalf("lastHealthy not set when the render is healthy: %+v", got.Status.LastHealthy)
		}
		if got.Status.LastHealthy.RenderRef != got.Status.LastAttempted.RenderRef {
			t.Errorf("lastHealthy.renderRef = %q, want current render %q",
				got.Status.LastHealthy.RenderRef, got.Status.LastAttempted.RenderRef)
		}
	})

	t.Run("render not healthy does not set lastHealthy", func(t *testing.T) {
		if got := run(false); got != nil && got.Status.LastHealthy != nil {
			t.Errorf("lastHealthy set despite the render not being healthy: %+v", got.Status.LastHealthy)
		}
	})
}

// TestSyncHandler_LastHealthyKeptWhenRenderNotApplied is the KB-11 regression: a
// Healthy verdict (always so without healthAssessment) must not advance lastHealthy
// to a render that was never applied, or a rollback targets a render not in the store.
func TestSyncHandler_LastHealthyKeptWhenRenderNotApplied(t *testing.T) {
	const priorRef = "sha256:prior-applied"
	for _, tc := range []struct {
		name     string
		rel      *corev1alpha1.Release
		deployer *fakeDeployer
	}{
		{name: "dry-run rejects the render", rel: manifestsRelease(), deployer: &fakeDeployer{dryRunErr: errors.New("field is immutable")}},
		{name: "manual mode waits for sync", rel: manualRelease(), deployer: &fakeDeployer{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.rel.Status.LastSuccessful = &corev1alpha1.ReleaseAttemptedRenderStatus{RenderRef: priorRef}
			tc.rel.Status.LastHealthy = &corev1alpha1.LastHealthy{RenderRef: priorRef}
			c, client := newTestController(t, tc.rel, reachableCluster(), readyCoordinator(), tc.deployer)
			setHealth(c, true)

			if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
				t.Fatalf("syncHandler: %v", err)
			}
			if tc.deployer.called {
				t.Fatal("the render was applied")
			}
			got := statusUpdate(client.Actions())
			if got == nil || got.Status.LastHealthy == nil || got.Status.LastHealthy.RenderRef != priorRef {
				t.Errorf("lastHealthy = %+v, want %s", got.Status.LastHealthy, priorRef)
			}
		})
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
