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
	"testing"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	fakeclient "github.com/kumodo-io/convoy/api/generated/clientset/versioned/fake"
	"github.com/kumodo-io/convoy/internal/inventory"
)

// deleteHookItem is a recorded delete-phase hook entry, keyed to the delete arming
// render (deleteRenderRefNone, since the test release never rendered).
func deleteHookItem(phase corev1alpha1.LifecycleHookPhase, name string, state *corev1alpha1.LifecycleHookState) corev1alpha1.ReleaseLifecycleHookItem {
	return corev1alpha1.ReleaseLifecycleHookItem{
		Name: name, Phase: phase, Version: "v1", Kind: kindPod,
		WorkloadName: string(phase) + "-" + name, WorkloadNamespace: appName,
		RenderRef: deleteRenderRefNone, State: state,
	}
}

// newDeleteHookController wires a deleting Release with the given delete-phase hooks,
// a fake hook runner, and a fake hook store (seeded with resources + hook entries).
func newDeleteHookController(t *testing.T, keep bool, lc *corev1alpha1.ReleaseLifecycle) (*Controller, *fakeclient.Clientset, *fakeDeployer, *fakeHookRunner, *fakeHookStore) {
	t.Helper()
	rel := deletingRelease(keep, false)
	rel.Lifecycle = lc
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	fr := &fakeHookRunner{resolveNS: appName}
	fs := &fakeHookStore{resources: []inventory.Ref{cmRef("cm")}}
	c.hookRunner = fr
	c.inventory = fs
	return c, client, deployer, fr, fs
}

func TestDeleteRenderRef(t *testing.T) {
	rel := manifestsRelease()
	if got := deleteRenderRef(rel); got != deleteRenderRefNone {
		t.Errorf("no LastAttempted → %q, want %q", got, deleteRenderRefNone)
	}
	rel.Status.LastAttempted = &corev1alpha1.ReleaseAttemptedRenderStatus{RenderRef: "sha256:x"}
	if got := deleteRenderRef(rel); got != "sha256:x" {
		t.Errorf("LastAttempted → %q, want sha256:x", got)
	}
}

func TestFinalize_PreDeleteArmsAndHolds(t *testing.T) {
	lc := &corev1alpha1.ReleaseLifecycle{PreDelete: []corev1alpha1.LifecycleHookSpec{
		podHookSpec("drain", 0, "", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupNever),
	}}
	c, _, deployer, fr, _ := newDeleteHookController(t, false, lc)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if len(fr.created) != 1 {
		t.Errorf("preDelete armed %d workload(s), want 1", len(fr.created))
	}
	if deployer.pruned != nil {
		t.Errorf("pruned while preDelete still running: %+v", deployer.pruned)
	}
}

func TestFinalize_PreDeleteFailBlocks(t *testing.T) {
	failed := corev1alpha1.LifecycleHookStateFailed
	lc := &corev1alpha1.ReleaseLifecycle{PreDelete: []corev1alpha1.LifecycleHookSpec{
		podHookSpec("drain", 0, "", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupNever),
	}}
	c, _, deployer, _, fs := newDeleteHookController(t, false, lc)
	fs.hooks = []corev1alpha1.ReleaseLifecycleHookItem{deleteHookItem(corev1alpha1.LifecycleHookPhasePreDelete, "drain", &failed)}

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if deployer.pruned != nil {
		t.Errorf("pruned despite a blocking preDelete failure: %+v", deployer.pruned)
	}
}

func TestFinalize_PreDeleteDone_PrunesThenArmsPostDelete(t *testing.T) {
	succeeded := corev1alpha1.LifecycleHookStateSucceeded
	lc := &corev1alpha1.ReleaseLifecycle{
		PreDelete: []corev1alpha1.LifecycleHookSpec{
			podHookSpec("drain", 0, "", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupNever),
		},
		PostDelete: []corev1alpha1.LifecycleHookSpec{
			podHookSpec("notify", 0, "", corev1alpha1.LifecycleHookOnFailureActionContinue, corev1alpha1.LifecycleHookCleanupNever),
		},
	}
	c, _, deployer, fr, fs := newDeleteHookController(t, false, lc)
	// preDelete already succeeded → advance → prune → postDelete arms + holds.
	fs.hooks = []corev1alpha1.ReleaseLifecycleHookItem{deleteHookItem(corev1alpha1.LifecycleHookPhasePreDelete, "drain", &succeeded)}

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if len(deployer.pruned) != 1 || deployer.pruned[0].Name != "cm" {
		t.Errorf("pruned = %+v, want [cm] after preDelete done", deployer.pruned)
	}
	if len(fr.created) != 1 {
		t.Errorf("postDelete armed %d workload(s), want 1", len(fr.created))
	}
	if fs.putLast == nil || fs.putLast[len(fs.putLast)-1].Phase != corev1alpha1.LifecycleHookPhasePostDelete {
		t.Errorf("postDelete identity not recorded: %+v", fs.putLast)
	}
}

func TestFinalize_KeepWorkloads_StillRunsDeleteHooks(t *testing.T) {
	// KeepWorkloads=true: workloads are kept (no prune) but delete-phase hooks still
	// run — the user gates them off via `if` if they don't want that (decision D2).
	lc := &corev1alpha1.ReleaseLifecycle{PreDelete: []corev1alpha1.LifecycleHookSpec{
		podHookSpec("drain", 0, "", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupNever),
	}}
	c, _, deployer, fr, _ := newDeleteHookController(t, true, lc)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if len(fr.created) != 1 {
		t.Errorf("preDelete armed %d workload(s) under keepWorkloads, want 1", len(fr.created))
	}
	if deployer.pruned != nil {
		t.Errorf("pruned under keepWorkloads: %+v, want none", deployer.pruned)
	}
}

func TestFinalize_AllDeleteHooksDone_RemovesFinalizer(t *testing.T) {
	succeeded := corev1alpha1.LifecycleHookStateSucceeded
	lc := &corev1alpha1.ReleaseLifecycle{
		PreDelete:  []corev1alpha1.LifecycleHookSpec{podHookSpec("drain", 0, "", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupNever)},
		PostDelete: []corev1alpha1.LifecycleHookSpec{podHookSpec("notify", 0, "", corev1alpha1.LifecycleHookOnFailureActionContinue, corev1alpha1.LifecycleHookCleanupNever)},
	}
	c, client, deployer, _, fs := newDeleteHookController(t, false, lc)
	fs.hooks = []corev1alpha1.ReleaseLifecycleHookItem{
		deleteHookItem(corev1alpha1.LifecycleHookPhasePreDelete, "drain", &succeeded),
		deleteHookItem(corev1alpha1.LifecycleHookPhasePostDelete, "notify", &succeeded),
	}

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if len(deployer.pruned) != 1 {
		t.Errorf("pruned = %+v, want [cm]", deployer.pruned)
	}
	// Both delete phases done → finalizer removed (deletion completes).
	upd := releaseUpdate(client.Actions())
	if upd == nil || hasReleaseFinalizer(upd.Finalizers) {
		t.Errorf("finalizer not removed after delete hooks completed: %+v", upd)
	}
}
