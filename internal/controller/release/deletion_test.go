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
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clienttesting "k8s.io/client-go/testing"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

// deletingRelease is a Release mid-deletion (finalizer + deletionTimestamp set),
// targeting the "target" cluster used by reachableCluster().
func deletingRelease(keepWorkloads, blockUnreachable bool) *corev1alpha1.Release {
	rel := manifestsRelease()
	now := metav1.Now()
	rel.DeletionTimestamp = &now
	rel.Finalizers = []string{releaseFinalizer}
	rel.Spec.DeletionPolicy = corev1alpha1.ReleaseDeletionPolicy{
		KeepWorkloads:             keepWorkloads,
		BlockOnClusterUnreachable: blockUnreachable,
	}
	return rel
}

func unreachableCluster() *corev1alpha1.Cluster {
	c := reachableCluster()
	c.Status.Reachable = false
	return c
}

// releaseUpdate returns the object from a main-resource (non-status) update —
// i.e. a finalizer add/remove.
func releaseUpdate(actions []clienttesting.Action) *corev1alpha1.Release {
	for _, a := range actions {
		if a.Matches("update", "releases") && a.GetSubresource() == "" {
			return a.(clienttesting.UpdateAction).GetObject().(*corev1alpha1.Release)
		}
	}
	return nil
}

func hasReleaseFinalizer(finalizers []string) bool {
	return slices.Contains(finalizers, releaseFinalizer)
}

func TestFinalize_PrunesWorkloadsAndRemovesFinalizer(t *testing.T) {
	deployer := &fakeDeployer{}
	c, client := newTestController(t, deletingRelease(false, false), reachableCluster(), readyCoordinator(), deployer)
	putInv(t, c, cmRef("cm"))

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	if len(deployer.pruned) != 1 || deployer.pruned[0].Name != "cm" {
		t.Errorf("pruned = %+v, want [cm]", deployer.pruned)
	}
	upd := releaseUpdate(client.Actions())
	if upd == nil || hasReleaseFinalizer(upd.Finalizers) {
		t.Errorf("finalizer not removed after cleanup: %+v", upd)
	}
}

func TestFinalize_KeepWorkloads_RemovesFinalizerWithoutPruning(t *testing.T) {
	deployer := &fakeDeployer{}
	c, client := newTestController(t, deletingRelease(true, false), reachableCluster(), readyCoordinator(), deployer)
	putInv(t, c, cmRef("cm"))

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	if deployer.pruned != nil {
		t.Errorf("pruned = %+v, want none (keepWorkloads)", deployer.pruned)
	}
	upd := releaseUpdate(client.Actions())
	if upd == nil || hasReleaseFinalizer(upd.Finalizers) {
		t.Errorf("finalizer not removed: %+v", upd)
	}
}

func TestFinalize_UnreachableCluster_BlocksWhenConfigured(t *testing.T) {
	deployer := &fakeDeployer{}
	c, client := newTestController(t, deletingRelease(false, true), unreachableCluster(), readyCoordinator(), deployer)
	putInv(t, c, cmRef("cm"))

	if err := c.syncHandler(context.Background(), "ns/r1"); err == nil {
		t.Fatalf("syncHandler returned nil, want a blocking error (cluster unreachable)")
	}
	if deployer.pruned != nil {
		t.Errorf("Prune attempted against an unreachable cluster: %+v", deployer.pruned)
	}
	if releaseUpdate(client.Actions()) != nil {
		t.Errorf("finalizer removed despite blocking; want it kept")
	}
}

func TestFinalize_UnreachableCluster_GivesUpWhenNotBlocking(t *testing.T) {
	deployer := &fakeDeployer{}
	c, client := newTestController(t, deletingRelease(false, false), unreachableCluster(), readyCoordinator(), deployer)
	putInv(t, c, cmRef("cm"))

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	if deployer.pruned != nil {
		t.Errorf("Prune attempted against an unreachable cluster: %+v", deployer.pruned)
	}
	upd := releaseUpdate(client.Actions())
	if upd == nil || hasReleaseFinalizer(upd.Finalizers) {
		t.Errorf("finalizer not removed (best-effort give-up): %+v", upd)
	}
}

func TestFinalize_ClusterGone_GivesUp(t *testing.T) {
	deployer := &fakeDeployer{}
	// No cluster seeded → clusterLister returns NotFound.
	c, client := newTestController(t, deletingRelease(false, true), nil, readyCoordinator(), deployer)
	putInv(t, c, cmRef("cm"))

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	if deployer.pruned != nil {
		t.Errorf("Prune attempted with no target cluster: %+v", deployer.pruned)
	}
	upd := releaseUpdate(client.Actions())
	if upd == nil || hasReleaseFinalizer(upd.Finalizers) {
		t.Errorf("finalizer not removed when cluster is gone: %+v", upd)
	}
}

func TestFinalize_NoFinalizer_IsNoOp(t *testing.T) {
	rel := deletingRelease(false, false)
	rel.Finalizers = nil
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), &fakeDeployer{})

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	if len(client.Actions()) != 0 {
		t.Errorf("expected no actions for a delete with no matching finalizer, got %d", len(client.Actions()))
	}
}

func TestSyncHandler_AddsFinalizerWhenNotDeleting(t *testing.T) {
	c, client := newTestController(t, manifestsRelease(), reachableCluster(), readyCoordinator(), &fakeDeployer{})

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	upd := releaseUpdate(client.Actions())
	if upd == nil || !hasReleaseFinalizer(upd.Finalizers) {
		t.Errorf("finalizer not added on a live release: %+v", upd)
	}
}
