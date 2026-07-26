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
	"strings"
	"testing"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clienttesting "k8s.io/client-go/testing"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	fakeclient "github.com/kumodo-io/convoy/api/generated/clientset/versioned/fake"
	informers "github.com/kumodo-io/convoy/api/generated/informers/externalversions"
	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
	"github.com/kumodo-io/convoy/internal/artifact"
	"github.com/kumodo-io/convoy/internal/auth"
	"github.com/kumodo-io/convoy/internal/fetch"
	"github.com/kumodo-io/convoy/internal/inventory"
)

const (
	dummyFile      = "app.yaml"
	configMapKind  = "ConfigMap"
	dummyRevision  = "abc123"
	dummyURL       = "https://example.com/repo.git"
	dummyCluster   = "target"
	dummyRepoName  = "repo"
	dummyFrozenSha = "sha256:frozen"
)

type fakeDeployer struct {
	got       []*unstructured.Unstructured
	waveCalls [][]*unstructured.Unstructured
	called    bool
	err       error
	pruned    []inventory.Ref
	pruneErr  error
}

func (d *fakeDeployer) Deploy(_ context.Context, _ *corev1alpha1.Cluster, objects []*unstructured.Unstructured) ([]inventory.Ref, error) {
	d.called = true
	d.got = objects
	d.waveCalls = append(d.waveCalls, objects)
	if d.err != nil {
		return nil, d.err
	}
	return refsOf(objects), nil
}

func (d *fakeDeployer) Prune(_ context.Context, _ *corev1alpha1.Cluster, refs []inventory.Ref) error {
	d.pruned = refs
	return d.pruneErr
}

func refsOf(objs []*unstructured.Unstructured) []inventory.Ref {
	out := make([]inventory.Ref, 0, len(objs))
	for _, o := range objs {
		gvk := o.GroupVersionKind()
		out = append(out, inventory.Ref{Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind, Namespace: o.GetNamespace(), Name: o.GetName()})
	}
	return out
}

// fakeCoordinator stands in for the fetch coordinator: it returns a canned
// Result/error without touching a real GitRepository or store.
type fakeCoordinator struct {
	result fetch.Result
	err    error
}

func (f *fakeCoordinator) Ensure(context.Context, corev1alpha1.ReleaseSourceSpec, string) (fetch.Result, error) {
	return f.result, f.err
}

// readyCoordinator returns a coordinator that resolves to a single-ConfigMap artifact.
func readyCoordinator() *fakeCoordinator {
	return &fakeCoordinator{result: fetch.Result{
		Ready:    true,
		Revision: dummyRevision,
		URL:      dummyURL,
		Artifact: artifact.Artifact{
			Revision: dummyRevision,
			Files:    map[string][]byte{dummyFile: []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n")},
		},
	}}
}

func manifestsRelease() *corev1alpha1.Release {
	return &corev1alpha1.Release{
		ObjectMeta: metav1.ObjectMeta{Name: "r1", Namespace: "ns", Generation: 1},
		Spec: corev1alpha1.ReleaseSpec{
			Source: corev1alpha1.ReleaseSourceSpec{
				Type: corev1alpha1.ReleaseSourceTypeGitRepository,
				GitRepository: &corev1alpha1.ReleaseGitRepositorySpec{
					Name: dummyRepoName,
					Ref:  &corev1alpha1.ReleaseGitRepositoryRefSpec{Branch: new("main")},
				},
			},
			Content:    manifestsContent(dummyFile),
			ClusterRef: corev1alpha1.ReleaseClusterRefSpec{Name: dummyCluster},
			// Automatic by default so these tests exercise the apply pipeline;
			// manual-mode gating is covered by the manual-sync tests below.
			SyncPolicy: corev1alpha1.ReleaseSyncPolicySpec{Automatic: true},
		},
	}
}

func reachableCluster() *corev1alpha1.Cluster {
	return &corev1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: dummyCluster, Namespace: "ns"},
		Status:     corev1alpha1.ClusterStatus{Reachable: true},
	}
}

func newTestController(t *testing.T, rel *corev1alpha1.Release, cluster *corev1alpha1.Cluster, coordinator fetch.Coordinator, deployer Deployer) (*Controller, *fakeclient.Clientset) {
	return newTestControllerWithResolver(t, rel, cluster, coordinator, fakeResolver{}, deployer)
}

func newTestControllerWithResolver(t *testing.T, rel *corev1alpha1.Release, cluster *corev1alpha1.Cluster, coordinator fetch.Coordinator, resolver auth.SecretResolver, deployer Deployer) (*Controller, *fakeclient.Clientset) {
	t.Helper()
	objs := []runtime.Object{rel}
	if cluster != nil {
		objs = append(objs, cluster)
	}
	client := fakeclient.NewSimpleClientset(objs...)
	factory := informers.NewSharedInformerFactory(client, 0)
	relInformer := factory.Core().V1alpha1().Releases()
	clInformer := factory.Core().V1alpha1().Clusters()
	if err := relInformer.Informer().GetStore().Add(rel); err != nil {
		t.Fatalf("seeding release store: %v", err)
	}
	if cluster != nil {
		if err := clInformer.Informer().GetStore().Add(cluster); err != nil {
			t.Fatalf("seeding cluster store: %v", err)
		}
	}
	// releasesForCluster is nil here: Axis B cluster-wave ordering is off by default
	// in unit tests, and the cluster-wave tests set c.releasesForCluster explicitly.
	c := NewController(client, relInformer, clInformer.Lister(), coordinator, resolver, inventory.NewMemoryStore(), deployer, nil)
	fixed := metav1.NewTime(time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC))
	c.now = func() metav1.Time { return fixed }
	return c, client
}

// statusUpdate returns the Release from the most recent status write. Status is
// now written via Server-Side Apply, so the recorded action is an apply patch on
// the status subresource; its patch body is the apply configuration JSON, which
// mirrors the Release shape and decodes straight into one.
func statusUpdate(actions []clienttesting.Action) *corev1alpha1.Release {
	var found *corev1alpha1.Release
	for _, a := range actions {
		p, ok := a.(clienttesting.PatchAction)
		if !ok || !a.Matches("patch", "releases") || a.GetSubresource() != "status" || p.GetPatchType() != types.ApplyPatchType {
			continue
		}
		rel := &corev1alpha1.Release{}
		if err := json.Unmarshal(p.GetPatch(), rel); err != nil {
			return nil
		}
		found = rel // keep the last one
	}
	return found
}

func readyReason(rel *corev1alpha1.Release) (metav1.ConditionStatus, string) {
	c := apimeta.FindStatusCondition(rel.Status.Conditions, conditionApplied)
	if c == nil {
		return "", ""
	}
	return c.Status, c.Reason
}

func TestSyncRequeueAfter(t *testing.T) {
	rel := manifestsRelease()
	rel.Spec.SyncPolicy.Interval = metav1.Duration{Duration: 90 * time.Second}
	if got := syncRequeueAfter(rel); got != 90*time.Second {
		t.Errorf("syncRequeueAfter = %v, want 90s", got)
	}

	rel.Spec.SyncPolicy.Interval = metav1.Duration{}
	if got := syncRequeueAfter(rel); got != 0 {
		t.Errorf("syncRequeueAfter = %v, want 0 when interval unset", got)
	}
}

func cmRef(name string) inventory.Ref {
	return inventory.Ref{Version: "v1", Kind: configMapKind, Name: name}
}

// putInv seeds the controller's inventory for release ns/r1 (the fixture used
// across these tests). The store is in-memory in tests, so writes never fail.
func putInv(t *testing.T, c *Controller, refs ...inventory.Ref) {
	t.Helper()
	if err := c.inventory.Put(context.Background(), "ns", "r1", refs); err != nil {
		t.Fatalf("seeding inventory: %v", err)
	}
}

// getInv reads the controller's inventory for release ns/r1.
func getInv(t *testing.T, c *Controller) []inventory.Ref {
	t.Helper()
	refs, _, err := c.inventory.Get(context.Background(), "ns", "r1")
	if err != nil {
		t.Fatalf("reading inventory: %v", err)
	}
	return refs
}

func TestUnionByID(t *testing.T) {
	// base carries a prior-generation-only ref ("old") plus a "shared" ref that the
	// current apply re-covers at a newer wave. applied wins on identity collision.
	base := []inventory.Ref{
		{Version: "v1", Kind: configMapKind, Name: "old", Wave: 0},
		{Version: "v1", Kind: configMapKind, Name: "shared", Wave: 0},
	}
	applied := []inventory.Ref{
		{Version: "v1", Kind: configMapKind, Name: "shared", Wave: 3},
		{Version: "v1", Kind: configMapKind, Name: "new", Wave: 3},
	}

	got := unionByID(base, applied)
	byName := map[string]inventory.Ref{}
	for _, r := range got {
		byName[r.Name] = r
	}
	if len(got) != 3 {
		t.Fatalf("union = %+v, want 3 refs (shared deduped)", got)
	}
	if byName["shared"].Wave != 3 {
		t.Errorf("shared wave = %d, want 3 (applied wins on collision)", byName["shared"].Wave)
	}
	if _, ok := byName["old"]; !ok {
		t.Error("prior-only ref 'old' dropped, want carried through the rollout")
	}
	if _, ok := byName["new"]; !ok {
		t.Error("current ref 'new' missing")
	}
}

func TestProtectedFilter(t *testing.T) {
	orphans := []inventory.Ref{
		cmRef("cm"),
		{Group: CustomResourceDefinitionGroup, Version: "v1", Kind: CustomResourceDefinitionKind, Name: "widgets.example.com"},
		{Version: "v1", Kind: "Namespace", Name: "team-a"},
	}

	kept := protectedFilter(append([]inventory.Ref(nil), orphans...), corev1alpha1.PruneSpec{Orphaned: true})
	if len(kept) != 1 || kept[0].Kind != configMapKind {
		t.Errorf("kept = %+v, want only ConfigMap (CRD + Namespace protected by default)", kept)
	}

	all := protectedFilter(append([]inventory.Ref(nil), orphans...),
		corev1alpha1.PruneSpec{Orphaned: true, IncludeCRDs: true, IncludeNamespaces: true})
	if len(all) != 3 {
		t.Errorf("kept = %+v, want all 3 when opted in", all)
	}
}

func TestSyncHandler_PrunesOrphan(t *testing.T) {
	rel := manifestsRelease()
	rel.Spec.SyncPolicy.Prune.Orphaned = true
	deployer := &fakeDeployer{}
	c, _ := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)

	// Prior render managed cm (still rendered) + old (now orphaned).
	putInv(t, c, cmRef("cm"), cmRef("old"))

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	if len(deployer.pruned) != 1 || deployer.pruned[0].Name != "old" {
		t.Errorf("pruned = %+v, want [old]", deployer.pruned)
	}
	if got := getInv(t, c); len(got) != 1 || got[0].Name != "cm" {
		t.Errorf("inventory = %+v, want [cm]", got)
	}
}

func TestSyncHandler_AppliesInWaveOrder(t *testing.T) {
	// Two manifests with sync-wave annotations: "late" at wave 10, "early" at -5.
	// They must apply early-before-late regardless of file order.
	early := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: early\n  annotations:\n    convoy.kumodo.io/sync-wave: \"-5\"\n"
	late := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: late\n  annotations:\n    convoy.kumodo.io/sync-wave: \"10\"\n"
	coord := &fakeCoordinator{result: fetch.Result{
		Ready:    true,
		Revision: "rev1",
		Artifact: artifact.Artifact{Files: map[string][]byte{"late.yaml": []byte(late), "early.yaml": []byte(early)}},
	}}
	rel := manifestsRelease()
	rel.Spec.Content = manifestsContent("late.yaml", "early.yaml")
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), coord, deployer)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}

	// One Deploy call per wave, in ascending order: -5 (early) then 10 (late).
	if len(deployer.waveCalls) != 2 {
		t.Fatalf("Deploy called %d times, want 2 (one per wave)", len(deployer.waveCalls))
	}
	if deployer.waveCalls[0][0].GetName() != "early" || deployer.waveCalls[1][0].GetName() != "late" {
		t.Errorf("wave order = [%s, %s], want [early, late]", deployer.waveCalls[0][0].GetName(), deployer.waveCalls[1][0].GetName())
	}

	// status.waves reflects both waves, applied.
	waves := statusUpdate(client.Actions()).Status.Waves
	if len(waves) != 2 {
		t.Fatalf("status.waves = %d entries, want 2", len(waves))
	}
	byNum := map[int64]corev1alpha1.ResourceWaveStatus{}
	for _, w := range waves {
		byNum[w.Number] = w
	}
	for _, n := range []int64{-5, 10} {
		w, ok := byNum[n]
		if !ok || w.Applied == nil || !*w.Applied {
			t.Errorf("wave %d = %+v, want applied=true", n, w)
		}
		// This release has no health assessment, so every wave is healthy by
		// default and the controller (now the sole writer of the field) records it.
		if w.Healthy == nil || !*w.Healthy {
			t.Errorf("wave %d healthy = %v, want true (no health assessment → healthy)", n, w.Healthy)
		}
	}
}

// TestSyncHandler_StatusWriteOnlyAssertsAppliedCondition guards the SSA
// field-manager boundary: the controller owns only the Applied condition, so its
// status write must not carry the observer-owned InSync / Healthy conditions —
// even though computeStatus deep-copies them from the current status. Asserting
// them under the controller's field manager (with Force) would clobber the
// observer's values.
func TestSyncHandler_StatusWriteOnlyAssertsAppliedCondition(t *testing.T) {
	const condInSync, condHealthy = "InSync", "Healthy"
	rel := manifestsRelease()
	// Seed observer-owned conditions as if the observer had already written them.
	rel.Status.Conditions = []metav1.Condition{
		{Type: conditionApplied, Status: metav1.ConditionFalse, Reason: "Stale"},
		{Type: condInSync, Status: metav1.ConditionTrue, Reason: condInSync},
		{Type: condHealthy, Status: metav1.ConditionTrue, Reason: condHealthy},
	}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), &fakeDeployer{})

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}

	got := statusUpdate(client.Actions())
	if got == nil {
		t.Fatal("no status write recorded")
	}
	if apimeta.FindStatusCondition(got.Status.Conditions, conditionApplied) == nil {
		t.Error("status write missing the Applied condition (controller must assert it)")
	}
	for _, notOwned := range []string{condInSync, condHealthy} {
		if c := apimeta.FindStatusCondition(got.Status.Conditions, notOwned); c != nil {
			t.Errorf("status write asserts %s condition = %+v, want absent (observer-owned)", notOwned, c)
		}
	}
}

// gatedCoordinator renders wave 0 (default, "gate") and wave 1 ("after") where
// wave 1 carries the given strategy/timeout annotation block (indented 4 spaces
// per line). Wave 1's strategy makes it wait for prior waves (wave 0) to be
// healthy before it applies.
func gatedCoordinator(wave1Ann string) *fakeCoordinator {
	w0 := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: gate\n  annotations:\n    convoy.kumodo.io/sync-wave: \"0\"\n"
	w1 := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: after\n  annotations:\n    convoy.kumodo.io/sync-wave: \"1\"\n" + wave1Ann
	return &fakeCoordinator{result: fetch.Result{
		Ready: true, Revision: "rev1",
		Artifact: artifact.Artifact{Files: map[string][]byte{"w0.yaml": []byte(w0), "w1.yaml": []byte(w1)}},
	}}
}

func gatedRelease() *corev1alpha1.Release {
	rel := manifestsRelease()
	rel.Spec.Content = manifestsContent("w0.yaml", "w1.yaml")
	// A health bar only applies when the release has a health assessment; without
	// one there is nothing to wait for, so the controller advances immediately.
	rel.Spec.HealthAssessment = &corev1alpha1.ReleaseHealthAssessment{}
	return rel
}

// healthyRef is a ConfigMap ref pre-marked Healthy, for seeding the inventory
// store so the wave gate sees a predecessor wave as healthy.
func healthyRef(name string) inventory.Ref {
	r := cmRef(name)
	healthy := corev1alpha1.ResourceStateHealthy
	r.State = &healthy
	return r
}

func TestSyncHandler_HoldsAtHealthGate(t *testing.T) {
	// Wave 0 is waitForHealthy; its healthy is unset → gate blocks wave 1.
	rel := gatedRelease()
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(),
		gatedCoordinator("    convoy.kumodo.io/sync-strategy: \"waitForHealthy\"\n"), deployer)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if len(deployer.waveCalls) != 1 || deployer.waveCalls[0][0].GetName() != "gate" {
		t.Fatalf("waveCalls = %v, want just [gate]", waveCallNames(deployer))
	}
	if s, r := readyReason(statusUpdate(client.Actions())); s != metav1.ConditionFalse || r != conditionReasonWaitingForWaveHealth {
		t.Errorf("condition = %v/%s, want False/WaitingForWaveHealth", s, r)
	}
	// The inventory is written incrementally: wave 0's resource is recorded even
	// though the rollout is holding at the gate (not only on full success), so the
	// observer can assess it and a partial rollout has a prune record.
	inv := getInv(t, c)
	if len(inv) != 1 || inv[0].Name != "gate" {
		t.Errorf("inventory = %+v, want [gate] recorded despite the hold", inv)
	}
}

func TestSyncHandler_AdvancesWhenPredecessorHealthy(t *testing.T) {
	rel := gatedRelease()
	// Wave 0 applied earlier, timestamps preserved in status.waves.
	firstApply := metav1.NewTime(time.Date(2026, 6, 25, 11, 30, 0, 0, time.UTC))
	rel.Status.Waves = []corev1alpha1.ResourceWaveStatus{
		{Number: 0, Applied: new(true), StartedAt: &firstApply, CompletedAt: &firstApply},
	}
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(),
		gatedCoordinator("    convoy.kumodo.io/sync-strategy: \"waitForHealthy\"\n"), deployer)
	// The observer marked wave 0's resource ("gate") Healthy on the ReleaseInventory;
	// the gate rolls that up to advance wave 1.
	putInv(t, c, healthyRef("gate"))

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if len(deployer.waveCalls) != 2 || deployer.waveCalls[1][0].GetName() != "after" {
		t.Errorf("waveCalls = %v, want [gate, after]", waveCallNames(deployer))
	}
	// Wave 0's original timestamps are preserved across reconciles, not reset to now.
	for _, w := range statusUpdate(client.Actions()).Status.Waves {
		if w.Number == 0 {
			if w.StartedAt == nil || !w.StartedAt.Equal(&firstApply) || w.CompletedAt == nil || !w.CompletedAt.Equal(&firstApply) {
				t.Errorf("wave 0 timestamps = started %v completed %v, want both %v (preserved)", w.StartedAt, w.CompletedAt, firstApply)
			}
		}
	}
}

func TestSyncHandler_HealthGateTimesOut(t *testing.T) {
	rel := gatedRelease()
	// Wave 0 applied an hour ago, still not healthy → bounded wait (1ms) has expired.
	past := metav1.NewTime(time.Date(2026, 6, 25, 11, 0, 0, 0, time.UTC))
	rel.Status.Waves = []corev1alpha1.ResourceWaveStatus{{Number: 0, Applied: new(true), StartedAt: &past}}
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(),
		gatedCoordinator("    convoy.kumodo.io/sync-strategy: \"waitForHealthyWithTimeout\"\n    convoy.kumodo.io/sync-timeout: \"1ms\"\n"), deployer)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if s, r := readyReason(statusUpdate(client.Actions())); s != metav1.ConditionFalse || r != conditionReasonWaveFailed {
		t.Errorf("condition = %v/%s, want False/WaveFailed", s, r)
	}
}

func waveCallNames(d *fakeDeployer) [][]string {
	out := make([][]string, len(d.waveCalls))
	for i, call := range d.waveCalls {
		for _, o := range call {
			out[i] = append(out[i], o.GetName())
		}
	}
	return out
}

func TestSyncHandler_CRDProtectedFromPrune(t *testing.T) {
	rel := manifestsRelease()
	rel.Spec.SyncPolicy.Prune.Orphaned = true // IncludeCRDs stays false
	deployer := &fakeDeployer{}
	c, _ := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)

	crd := inventory.Ref{Group: CustomResourceDefinitionGroup, Version: "v1", Kind: CustomResourceDefinitionKind, Name: "widgets.example.com"}
	putInv(t, c, cmRef("cm"), crd)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	if len(deployer.pruned) != 0 {
		t.Errorf("pruned = %+v, want none (CRD protected)", deployer.pruned)
	}
}

func TestSyncHandler_PruneDisabledUntracks(t *testing.T) {
	rel := manifestsRelease() // Prune.Orphaned defaults false
	deployer := &fakeDeployer{}
	c, _ := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)

	putInv(t, c, cmRef("cm"), cmRef("old"))

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	if deployer.pruned != nil {
		t.Errorf("pruned = %+v, want no prune when disabled", deployer.pruned)
	}
	// Orphan is untracked: inventory advances to the current render.
	if got := getInv(t, c); len(got) != 1 || got[0].Name != "cm" {
		t.Errorf("inventory = %+v, want [cm] (orphan untracked)", got)
	}
}

func TestSyncHandler_PruneFailureRequeuesAndKeepsInventory(t *testing.T) {
	rel := manifestsRelease()
	rel.Spec.SyncPolicy.Prune.Orphaned = true
	deployer := &fakeDeployer{pruneErr: errors.New("delete forbidden")}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)

	putInv(t, c, cmRef("cm"), cmRef("old"))

	if err := c.syncHandler(context.Background(), "ns/r1"); err == nil {
		t.Fatalf("syncHandler returned nil, want error so it requeues")
	}
	if got := getInv(t, c); len(got) != 2 {
		t.Errorf("inventory = %+v, want unchanged (2 refs) on prune failure", got)
	}
	if s, r := readyReason(statusUpdate(client.Actions())); s != metav1.ConditionFalse || r != conditionReasonPruneFailed {
		t.Errorf("Ready = %s/%s, want False/PruneFailed", s, r)
	}
}

func TestSyncHandler_NoPruneOnDeployFailure(t *testing.T) {
	rel := manifestsRelease()
	rel.Spec.SyncPolicy.Prune.Orphaned = true
	deployer := &fakeDeployer{err: errors.New("apply failed")}
	c, _ := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	putInv(t, c, cmRef("old"))

	if err := c.syncHandler(context.Background(), "ns/r1"); err == nil {
		t.Fatalf("syncHandler returned nil, want error from failed deploy")
	}
	if deployer.pruned != nil {
		t.Errorf("pruned despite deploy failure: %+v", deployer.pruned)
	}
}

func TestBackoffDelay(t *testing.T) {
	spec := corev1alpha1.ReleaseRetryBackoffSpec{
		Initial: metav1.Duration{Duration: time.Second},
		Max:     metav1.Duration{Duration: 30 * time.Second},
		Factor:  2,
	}
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, time.Second},       // Initial
		{2, 2 * time.Second},   // Initial * 2
		{3, 4 * time.Second},   // Initial * 4
		{10, 30 * time.Second}, // capped at Max
	}
	for _, c := range cases {
		if got := backoffDelay(spec, c.attempt); got != c.want {
			t.Errorf("backoffDelay(attempt=%d) = %v, want %v", c.attempt, got, c.want)
		}
	}

	// Factor < 1 falls back to the default (2), so attempt 2 doubles.
	noFactor := corev1alpha1.ReleaseRetryBackoffSpec{Initial: metav1.Duration{Duration: time.Second}}
	if got := backoffDelay(noFactor, 2); got != 2*time.Second {
		t.Errorf("backoffDelay with unset factor = %v, want 2s", got)
	}
	// A huge attempt is capped, not overflowed.
	if got := backoffDelay(spec, 1000); got != 30*time.Second {
		t.Errorf("backoffDelay(attempt=1000) = %v, want cap 30s", got)
	}
}

func TestFailureCounts(t *testing.T) {
	f := newFailureCounts()
	if n := f.inc("ns/a"); n != 1 {
		t.Errorf("first inc = %d, want 1", n)
	}
	if n := f.inc("ns/a"); n != 2 {
		t.Errorf("second inc = %d, want 2", n)
	}
	f.reset("ns/a")
	if n := f.inc("ns/a"); n != 1 {
		t.Errorf("inc after reset = %d, want 1", n)
	}
}

func backoffRelease() *corev1alpha1.Release {
	rel := manifestsRelease()
	rel.Spec.SyncPolicy.RetryBackoff = corev1alpha1.ReleaseRetryBackoffSpec{
		Initial: metav1.Duration{Duration: time.Second},
		Max:     metav1.Duration{Duration: time.Minute},
		Factor:  2,
	}
	return rel
}

func TestSyncHandler_CustomBackoffHandlesRetry(t *testing.T) {
	coordinator := &fakeCoordinator{err: errors.New("GitRepository not ready")}
	c, _ := newTestController(t, backoffRelease(), reachableCluster(), coordinator, &fakeDeployer{})

	// Transient failure with a configured backoff is handled internally → nil,
	// and the consecutive-failure count climbs.
	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned %v, want nil (custom backoff handled)", err)
	}
	if c.failures.n["ns/r1"] != 1 {
		t.Errorf("failure count = %d, want 1 after one failure", c.failures.n["ns/r1"])
	}
	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("second syncHandler returned %v, want nil", err)
	}
	if c.failures.n["ns/r1"] != 2 {
		t.Errorf("failure count = %d, want 2 after two failures", c.failures.n["ns/r1"])
	}
}

func TestSyncHandler_DefaultRateLimitWhenNoBackoff(t *testing.T) {
	coordinator := &fakeCoordinator{err: errors.New("GitRepository not ready")}
	// manifestsRelease() has no RetryBackoff → default path returns the error.
	c, _ := newTestController(t, manifestsRelease(), reachableCluster(), coordinator, &fakeDeployer{})

	if err := c.syncHandler(context.Background(), "ns/r1"); err == nil {
		t.Fatalf("syncHandler returned nil, want error (default rate-limited retry)")
	}
}

func TestSyncHandler_SuccessResetsFailures(t *testing.T) {
	c, _ := newTestController(t, backoffRelease(), reachableCluster(), readyCoordinator(), &fakeDeployer{})
	c.failures.inc("ns/r1") // pretend a prior failure streak

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	if c.failures.n["ns/r1"] != 0 {
		t.Errorf("failure count = %d, want 0 (reset on success)", c.failures.n["ns/r1"])
	}
}

func TestSyncHandler_RecordsAttemptOnSuccess(t *testing.T) {
	c, client := newTestController(t, manifestsRelease(), reachableCluster(), readyCoordinator(), &fakeDeployer{})

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	got := statusUpdate(client.Actions())
	if got == nil {
		t.Fatalf("expected a status update")
	}

	la := got.Status.LastAttempted
	if la == nil {
		t.Fatalf("LastAttempted is nil")
	}
	if la.Phase != corev1alpha1.ReleaseRenderAttemptPhaseSucceeded {
		t.Errorf("phase = %q, want Succeeded", la.Phase)
	}
	if la.ResolvedSource.Revision != dummyRevision || la.ResolvedSource.URL != dummyURL {
		t.Errorf("resolvedSource = %+v, want revision %s / url %s", la.ResolvedSource, dummyRevision, dummyURL)
	}
	if la.ResolvedSource.Kind != sourcev1alpha1.GitRepositoryKind || la.ResolvedSource.Ref != "branch:main" {
		t.Errorf("resolvedSource kind/ref = %q/%q, want GitRepository/branch:main", la.ResolvedSource.Kind, la.ResolvedSource.Ref)
	}
	if la.ResolvedCluster.Name != dummyCluster {
		t.Errorf("resolvedCluster.Name = %q, want target", la.ResolvedCluster.Name)
	}
	if la.SyncedResources != 1 {
		t.Errorf("syncedResources = %d, want 1", la.SyncedResources)
	}
	if !strings.HasPrefix(la.RenderRef, "sha256:") {
		t.Errorf("renderRef = %q, want a sha256: hash", la.RenderRef)
	}
	if got.Status.LastSuccessful == nil || got.Status.LastSuccessful.Phase != corev1alpha1.ReleaseRenderAttemptPhaseSucceeded {
		t.Errorf("LastSuccessful = %+v, want a Succeeded record", got.Status.LastSuccessful)
	}
}

func TestSyncHandler_RecordsFailureAndPreservesLastSuccessful(t *testing.T) {
	rel := manifestsRelease()
	rel.Status.LastSuccessful = &corev1alpha1.ReleaseAttemptedRenderStatus{
		Phase:          corev1alpha1.ReleaseRenderAttemptPhaseSucceeded,
		ResolvedSource: corev1alpha1.ResolvedSource{Revision: "good"},
	}
	coordinator := &fakeCoordinator{err: errors.New("source down")}
	c, client := newTestController(t, rel, reachableCluster(), coordinator, &fakeDeployer{})

	if err := c.syncHandler(context.Background(), "ns/r1"); err == nil {
		t.Fatalf("syncHandler returned nil, want transient error")
	}
	got := statusUpdate(client.Actions())
	if got == nil {
		t.Fatalf("expected a status update")
	}
	if la := got.Status.LastAttempted; la == nil || la.Phase != corev1alpha1.ReleaseRenderAttemptPhaseFailed ||
		la.Failure == nil || la.Failure.Stage != corev1alpha1.ReleaseRenderAttemptStageSourceResolution {
		t.Errorf("LastAttempted = %+v, want Failed at SourceResolution", la)
	}
	if ls := got.Status.LastSuccessful; ls == nil || ls.ResolvedSource.Revision != "good" {
		t.Errorf("LastSuccessful = %+v, want preserved revision good", ls)
	}
}

func TestSyncHandler_AppliesEvenWhenStatusUnchanged(t *testing.T) {
	deployer := &fakeDeployer{}
	c, _ := newTestController(t, manifestsRelease(), reachableCluster(), readyCoordinator(), deployer)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("first syncHandler: %v", err)
	}
	if !deployer.called {
		t.Fatalf("Deploy not called on first reconcile")
	}

	// Re-reconcile in steady state: no status change, but we must still apply to
	// correct drift.
	deployer.called = false
	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("second syncHandler: %v", err)
	}
	if !deployer.called {
		t.Errorf("Deploy not called on re-reconcile — drift correction broken")
	}
}

func TestSyncHandler_AppliesAndMarksReady(t *testing.T) {
	deployer := &fakeDeployer{}
	c, client := newTestController(t, manifestsRelease(), reachableCluster(), readyCoordinator(), deployer)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	if !deployer.called || len(deployer.got) != 1 || deployer.got[0].GetKind() != configMapKind {
		t.Errorf("deployer got %+v (called=%v), want one ConfigMap", deployer.got, deployer.called)
	}
	got := statusUpdate(client.Actions())
	if got == nil {
		t.Fatalf("expected a status apply action, got none")
	}
	if s, r := readyReason(got); s != metav1.ConditionTrue || r != conditionReasonApplied {
		t.Errorf("Applied = %s/%s, want True/Applied", s, r)
	}
}

// TestSyncHandler_StatusWrittenViaSSA verifies the controller writes status with
// Server-Side Apply under its own field manager, and sends only the fields it
// owns (so other writers' fields — e.g. health — are never clobbered).
func TestSyncHandler_StatusWrittenViaSSA(t *testing.T) {
	c, client := newTestController(t, manifestsRelease(), reachableCluster(), readyCoordinator(), &fakeDeployer{})

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}

	var applied clienttesting.PatchAction
	for _, a := range client.Actions() {
		p, ok := a.(clienttesting.PatchAction)
		if ok && a.Matches("patch", "releases") && a.GetSubresource() == "status" && p.GetPatchType() == types.ApplyPatchType {
			applied = p
		}
	}
	if applied == nil {
		t.Fatalf("expected a status apply (SSA) action")
	}
	if got := applied.GetName(); got != "r1" {
		t.Errorf("apply target = %q, want r1", got)
	}

	// The patch body carries the owned fields and omits fields owned by other
	// writers (healthSummary / health are the observer's).
	var body map[string]any
	if err := json.Unmarshal(applied.GetPatch(), &body); err != nil {
		t.Fatalf("apply patch is not valid JSON: %v", err)
	}
	status, _ := body["status"].(map[string]any)
	if status == nil {
		t.Fatalf("apply patch has no status: %s", applied.GetPatch())
	}
	if _, present := status["conditions"]; !present {
		t.Errorf("owned field 'conditions' missing from apply patch: %v", status)
	}
	if _, present := status["healthSummary"]; present {
		t.Errorf("unowned field 'healthSummary' must not be sent by the release controller: %v", status)
	}
}

func TestSyncHandler_SourceNotReadyRequeues(t *testing.T) {
	deployer := &fakeDeployer{}
	coordinator := &fakeCoordinator{err: errors.New("GitRepository not ready")}
	c, client := newTestController(t, manifestsRelease(), reachableCluster(), coordinator, deployer)

	if err := c.syncHandler(context.Background(), "ns/r1"); err == nil {
		t.Fatalf("syncHandler returned nil, want error so the queue retries")
	}
	if deployer.called {
		t.Errorf("deployer was called despite missing source artifact")
	}
	if got := statusUpdate(client.Actions()); got == nil {
		t.Fatalf("expected status write")
	} else if _, r := readyReason(got); r != conditionReasonSourceNotReady {
		t.Errorf("reason = %s, want SourceNotReady", r)
	}
}

func TestSyncHandler_ClusterNotReachableRequeues(t *testing.T) {
	cluster := reachableCluster()
	cluster.Status.Reachable = false
	deployer := &fakeDeployer{}
	c, client := newTestController(t, manifestsRelease(), cluster, readyCoordinator(), deployer)

	if err := c.syncHandler(context.Background(), "ns/r1"); err == nil {
		t.Fatalf("syncHandler returned nil, want error so the queue retries")
	}
	if deployer.called {
		t.Errorf("deployer was called despite unreachable cluster")
	}
	if got := statusUpdate(client.Actions()); got == nil {
		t.Fatalf("expected status write")
	} else if _, r := readyReason(got); r != conditionReasonClusterNotReady {
		t.Errorf("reason = %s, want ClusterNotReady", r)
	}
}

func TestSyncHandler_PinHoldsWhenDrifted(t *testing.T) {
	rel := manifestsRelease()
	// Pinned to a render that differs from what the current spec/source renders:
	// the controller must hold (not apply) and report the pin.
	rel.Pin = new(corev1alpha1.ReleasePin{RenderRef: "sha256:stale"})
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	if deployer.called {
		t.Error("Deploy called despite a pin holding a stale render")
	}
	if got := statusUpdate(client.Actions()); got == nil {
		t.Fatalf("expected status write")
	} else if s, r := readyReason(got); s != metav1.ConditionFalse || r != conditionReasonPinned {
		t.Errorf("Ready = %s/%s, want False/Pinned", s, r)
	}
}

func TestSyncHandler_PinAppliesCurrentRender(t *testing.T) {
	rel := manifestsRelease()
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)

	// First reconcile (unpinned) discovers the current render ref.
	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("first syncHandler: %v", err)
	}
	got := statusUpdate(client.Actions())
	if got == nil || got.Status.LastAttempted == nil {
		t.Fatalf("expected a recorded render attempt")
	}
	current := got.Status.LastAttempted.RenderRef

	// Freezing to the *current* render is idempotent: the controller still applies.
	rel.Pin = new(corev1alpha1.ReleasePin{RenderRef: current})
	deployer.called = false
	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("second syncHandler: %v", err)
	}
	if !deployer.called {
		t.Errorf("Deploy not called when pinned to the current render")
	}
	if s, r := readyReason(statusUpdate(client.Actions())); s != metav1.ConditionTrue || r != conditionReasonApplied {
		t.Errorf("Ready = %s/%s, want True/Applied", s, r)
	}
}

func TestSyncHandler_ReflectsPinIntoStatus(t *testing.T) {
	rel := manifestsRelease()
	// A prior successful render that the pin freezes; pinStatus snapshots its inputs.
	rel.Status.LastSuccessful = &corev1alpha1.ReleaseAttemptedRenderStatus{
		RenderRef: dummyFrozenSha,
		ResolvedSource: corev1alpha1.ResolvedSource{
			Kind:        sourcev1alpha1.GitRepositoryKind,
			Name:        dummyRepoName,
			Namespace:   "ns",
			Revision:    dummyRevision,
			ContentType: corev1alpha1.ReleaseContentTypeManifests,
			Path:        dummyFile,
		},
	}
	// Pinned to that render; the current render will differ, so the controller holds.
	rel.Pin = new(corev1alpha1.ReleasePin{RenderRef: dummyFrozenSha})
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	got := statusUpdate(client.Actions())
	if got == nil {
		t.Fatalf("expected a status write")
	}
	if got.Status.Pin.Intent != corev1alpha1.ReleasePinIntentFreeze {
		t.Errorf("status.pin.intent = %q, want freeze", got.Status.Pin.Intent)
	}
	ri := got.Status.Pin.RenderInputs
	if ri == nil {
		t.Fatalf("status.pin.renderInputs not populated")
	}
	if ri.SourceKind != sourcev1alpha1.GitRepositoryKind || ri.SourceName != dummyRepoName || ri.SourceRevision != dummyRevision ||
		ri.ContentType != corev1alpha1.ReleaseContentTypeManifests || ri.ContentPath != dummyFile {
		t.Errorf("renderInputs = %+v, want the frozen render's source/content inputs", ri)
	}

	// Unpinning clears the observed pin status.
	rel.Pin = nil
	client.ClearActions()
	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("second syncHandler: %v", err)
	}
	// The render now applies (no pin); its status write carries an empty pin.
	got = statusUpdate(client.Actions())
	if got.Status.Pin != nil {
		t.Errorf("status.pin = %+v, want cleared after unpin", got.Status.Pin)
	}
}

// manualRelease returns a Release in manual sync mode (automatic=false).
func manualRelease() *corev1alpha1.Release {
	rel := manifestsRelease()
	rel.Spec.SyncPolicy.Automatic = false
	return rel
}

// TestSyncHandler_ManualModeStillObservesPin verifies manual mode gates only the
// apply, not reconciliation: a pinned Release still gets status.pin projected (and
// is not applied) without a pending sync.
func TestSyncHandler_ManualModeStillObservesPin(t *testing.T) {
	rel := manualRelease()
	rel.Status.LastSuccessful = &corev1alpha1.ReleaseAttemptedRenderStatus{
		RenderRef: dummyFrozenSha,
		ResolvedSource: corev1alpha1.ResolvedSource{
			Kind: sourcev1alpha1.GitRepositoryKind, Name: dummyRepoName, Namespace: "ns",
			Revision: dummyRevision, ContentType: corev1alpha1.ReleaseContentTypeManifests, Path: dummyFile,
		},
	}
	rel.Pin = new(corev1alpha1.ReleasePin{RenderRef: dummyFrozenSha})
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	if deployer.called {
		t.Error("Deploy called in manual mode without a pending sync")
	}
	got := statusUpdate(client.Actions())
	if got == nil {
		t.Fatalf("expected a status write — manual mode must still observe/reconcile")
	}
	if got.Status.Pin.Intent != corev1alpha1.ReleasePinIntentFreeze || got.Status.Pin.RenderInputs == nil {
		t.Errorf("status.pin = %+v, want freeze intent + renderInputs even in manual mode", got.Status.Pin)
	}
}

func TestSyncHandler_ManualModeHoldsWithoutSyncRequest(t *testing.T) {
	deployer := &fakeDeployer{}
	c, client := newTestController(t, manualRelease(), reachableCluster(), readyCoordinator(), deployer)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	if deployer.called {
		t.Error("Deploy called in manual mode without a pending sync request")
	}
	// Never synced: an explanatory WaitingForSync condition is published.
	got := statusUpdate(client.Actions())
	if got == nil {
		t.Fatalf("expected a status write explaining the hold")
	}
	if s, r := readyReason(got); s != metav1.ConditionFalse || r != conditionReasonWaitingForSync {
		t.Errorf("Ready = %s/%s, want False/WaitingForSync", s, r)
	}
	if got.Status.LastHandledSyncAt != nil {
		t.Errorf("LastHandledSyncAt = %v, want nil (nothing handled)", got.Status.LastHandledSyncAt)
	}
}

func TestSyncHandler_ManualModeAppliesOnSyncRequest(t *testing.T) {
	rel := manualRelease()
	requestedAt := metav1.NewTime(time.Date(2026, 6, 25, 11, 59, 0, 0, time.UTC))
	rel.Annotations = map[string]string{
		corev1alpha1.SyncRequestedAtAnnotation: requestedAt.UTC().Format(time.RFC3339Nano),
	}
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	if !deployer.called {
		t.Error("Deploy not called despite a pending sync request")
	}
	got := statusUpdate(client.Actions())
	if got == nil {
		t.Fatalf("expected a status write")
	}
	if s, r := readyReason(got); s != metav1.ConditionTrue || r != conditionReasonApplied {
		t.Errorf("Ready = %s/%s, want True/Applied", s, r)
	}
	if got.Status.LastHandledSyncAt == nil || !got.Status.LastHandledSyncAt.Time.Equal(requestedAt.Time) {
		t.Errorf("LastHandledSyncAt = %v, want %v", got.Status.LastHandledSyncAt, requestedAt)
	}
}

func TestSyncHandler_ManualModeHoldsAfterHandled(t *testing.T) {
	rel := manualRelease()
	requestedAt := metav1.NewTime(time.Date(2026, 6, 25, 11, 59, 0, 0, time.UTC))
	rel.Annotations = map[string]string{
		corev1alpha1.SyncRequestedAtAnnotation: requestedAt.UTC().Format(time.RFC3339Nano),
	}
	deployer := &fakeDeployer{}
	c, _ := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)

	// First reconcile handles the pending request and applies.
	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("first syncHandler: %v", err)
	}
	if !deployer.called {
		t.Fatalf("Deploy not called on the pending request")
	}
	// Reflect the handled sync back onto the object the lister returns, as a real
	// status write would; the same stamp is now no longer pending.
	rel.Status.LastHandledSyncAt = &requestedAt

	deployer.called = false
	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("second syncHandler: %v", err)
	}
	if deployer.called {
		t.Error("Deploy called again for an already-handled sync request")
	}
}

func TestSyncHandler_UnsupportedContentIsTerminal(t *testing.T) {
	rel := manifestsRelease()
	rel.Spec.Content.Type = corev1alpha1.ReleaseContentTypeHelm
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error for unsupported content: %v", err)
	}
	if deployer.called {
		t.Errorf("deployer was called for unsupported content")
	}
	if got := statusUpdate(client.Actions()); got == nil {
		t.Fatalf("expected status write")
	} else if _, r := readyReason(got); r != conditionReasonUnsupported {
		t.Errorf("reason = %s, want Unsupported", r)
	}
}
