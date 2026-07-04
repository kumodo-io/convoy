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

package cluster

import (
	"context"
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clienttesting "k8s.io/client-go/testing"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

func deletingCluster(pruneReleases bool) *corev1alpha1.Cluster {
	now := metav1.Now()
	return &corev1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "c1",
			Namespace:         "ns",
			Finalizers:        []string{clusterFinalizer},
			DeletionTimestamp: &now,
		},
		Spec: corev1alpha1.ClusterSpec{
			ConnectionMode: corev1alpha1.ClusterConnectionModeInCluster,
			DeletionPolicy: corev1alpha1.ClusterDeletionPolicy{PruneReleases: pruneReleases},
		},
	}
}

// clusterUpdate returns the object from a main-resource (non-status) update.
func clusterUpdate(actions []clienttesting.Action) *corev1alpha1.Cluster {
	for _, a := range actions {
		if a.Matches("update", "clusters") && a.GetSubresource() == "" {
			return a.(clienttesting.UpdateAction).GetObject().(*corev1alpha1.Cluster)
		}
	}
	return nil
}

func releaseDeleteNames(actions []clienttesting.Action) []string {
	var names []string
	for _, a := range actions {
		if a.Matches("delete", "releases") {
			names = append(names, a.(clienttesting.DeleteAction).GetName())
		}
	}
	return names
}

func releaseRefs(names ...string) ReleasesForClusterFunc {
	return func(string, string) ([]*corev1alpha1.Release, error) {
		out := make([]*corev1alpha1.Release, 0, len(names))
		for _, n := range names {
			out = append(out, &corev1alpha1.Release{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: "ns"}})
		}
		return out, nil
	}
}

func hasClusterFinalizer(finalizers []string) bool {
	return slices.Contains(finalizers, clusterFinalizer)
}

func TestFinalize_PruneReleases_DeletesAndBlocksUntilGone(t *testing.T) {
	c, client, _ := newTestControllerWithReleases(t, deletingCluster(true), fakeProber{}, releaseRefs("r1", "r2"))

	err := c.syncHandler(context.Background(), "ns/c1")
	if err == nil {
		t.Fatalf("syncHandler returned nil, want a (blocking) error while releases remain")
	}
	if got := releaseDeleteNames(client.Actions()); !slices.Equal(got, []string{"r1", "r2"}) {
		t.Errorf("deleted releases = %v, want [r1 r2]", got)
	}
	if clusterUpdate(client.Actions()) != nil {
		t.Errorf("finalizer was removed while releases still exist; want it kept")
	}
}

func TestFinalize_PruneReleases_NoneLeft_RemovesFinalizer(t *testing.T) {
	c, client, _ := newTestControllerWithReleases(t, deletingCluster(true), fakeProber{}, releaseRefs())

	if err := c.syncHandler(context.Background(), "ns/c1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	if got := releaseDeleteNames(client.Actions()); len(got) != 0 {
		t.Errorf("deleted releases = %v, want none", got)
	}
	upd := clusterUpdate(client.Actions())
	if upd == nil || hasClusterFinalizer(upd.Finalizers) {
		t.Errorf("finalizer not removed once no releases remain: %+v", upd)
	}
}

func TestFinalize_PruneReleasesDisabled_OrphansAndRemovesFinalizer(t *testing.T) {
	// releasesForCluster should never be consulted when pruneReleases is false.
	called := false
	fn := func(string, string) ([]*corev1alpha1.Release, error) { called = true; return nil, nil }
	c, client, _ := newTestControllerWithReleases(t, deletingCluster(false), fakeProber{}, fn)

	if err := c.syncHandler(context.Background(), "ns/c1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	if called {
		t.Errorf("releasesForCluster consulted despite pruneReleases=false")
	}
	if len(releaseDeleteNames(client.Actions())) != 0 {
		t.Errorf("releases deleted despite pruneReleases=false")
	}
	upd := clusterUpdate(client.Actions())
	if upd == nil || hasClusterFinalizer(upd.Finalizers) {
		t.Errorf("finalizer not removed: %+v", upd)
	}
}

func TestFinalize_NoFinalizer_IsNoOp(t *testing.T) {
	cluster := deletingCluster(true)
	cluster.Finalizers = nil
	c, client, _ := newTestControllerWithReleases(t, cluster, fakeProber{}, releaseRefs("r1"))

	if err := c.syncHandler(context.Background(), "ns/c1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	if len(client.Actions()) != 0 {
		t.Errorf("expected no actions for a delete with no matching finalizer, got %d", len(client.Actions()))
	}
}

func TestSyncHandler_AddsFinalizerWhenNotDeleting(t *testing.T) {
	c, client, _ := newTestController(t, clusterWithInClusterConnectionMode(),
		fakeProber{result: ProbeResult{Version: sampleKubeVersion, APIVersions: []string{"v1"}}})

	if err := c.syncHandler(context.Background(), "ns/c1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}
	upd := clusterUpdate(client.Actions())
	if upd == nil || !hasClusterFinalizer(upd.Finalizers) {
		t.Errorf("finalizer not added on a live cluster: %+v", upd)
	}
}
