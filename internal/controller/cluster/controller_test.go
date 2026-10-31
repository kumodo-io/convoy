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
	"errors"
	"testing"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	fakeclient "github.com/kumodo-io/convoy/api/generated/clientset/versioned/fake"
	informers "github.com/kumodo-io/convoy/api/generated/informers/externalversions"
	"github.com/kumodo-io/convoy/internal/clusterconn"
)

const (
	sampleKubeVersion = "v1.32.0"
)

type fakeProber struct {
	result ProbeResult
	err    error
}

func (f fakeProber) Probe(context.Context, *corev1alpha1.Cluster) (ProbeResult, error) {
	return f.result, f.err
}

func clusterWithInClusterConnectionMode() *corev1alpha1.Cluster {
	return &corev1alpha1.Cluster{
		Name: "c1", Namespace: "ns", Generation: 1,
		Spec: corev1alpha1.ClusterSpec{
			ConnectionMode: corev1alpha1.ClusterConnectionModeInCluster,
		},
	}
}

// newTestController builds a Controller over a fake clientset seeded with the
// given cluster, preloads it into the informer store, and pins the clock. It
// returns the controller, the fake clientset (to inspect actions), and the
// informer store (to mutate observed state between syncs).
func newTestController(t *testing.T, cluster *corev1alpha1.Cluster, prober Prober) (*Controller, *fakeclient.Clientset, cache.Store) {
	noReleases := func(string, string) ([]*corev1alpha1.Release, error) { return nil, nil }
	return newTestControllerWithReleases(t, cluster, prober, noReleases)
}

func newTestControllerWithReleases(t *testing.T, cluster *corev1alpha1.Cluster, prober Prober, releasesForCluster ReleasesForClusterFunc) (*Controller, *fakeclient.Clientset, cache.Store) {
	t.Helper()
	client := fakeclient.NewSimpleClientset(cluster)
	factory := informers.NewSharedInformerFactory(client, 0)
	clusterInformer := factory.Core().V1alpha1().Clusters()
	store := clusterInformer.Informer().GetStore()
	if err := store.Add(cluster); err != nil {
		t.Fatalf("seeding informer store: %v", err)
	}
	c := NewController(client, clusterInformer, prober, releasesForCluster)
	fixed := metav1.NewTime(time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC))
	c.now = func() metav1.Time { return fixed }
	return c, client, store
}

func statusUpdate(actions []clienttesting.Action) *corev1alpha1.Cluster {
	for _, a := range actions {
		if a.Matches("update", "clusters") && a.GetSubresource() == "status" {
			return a.(clienttesting.UpdateAction).GetObject().(*corev1alpha1.Cluster)
		}
	}
	return nil
}

func TestSyncHandler_ReachableWritesStatus(t *testing.T) {
	c, client, _ := newTestController(t, clusterWithInClusterConnectionMode(),
		fakeProber{result: ProbeResult{Version: sampleKubeVersion, APIVersions: []string{"v1"}}})

	if err := c.syncHandler(context.Background(), "ns/c1"); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}

	got := statusUpdate(client.Actions())
	if got == nil {
		t.Fatalf("expected an UpdateStatus action, got none; actions=%v", client.Actions())
	}
	if !got.Status.Reachable || got.Status.DiscoveredVersion != sampleKubeVersion {
		t.Errorf("persisted status = %+v, want Reachable + v1.32.0", got.Status)
	}
}

func TestSyncHandler_UnsupportedDoesNotRequeue(t *testing.T) {
	c, client, _ := newTestController(t, clusterWithInClusterConnectionMode(),
		fakeProber{err: errors.Join(clusterconn.ErrUnsupported, errors.New("auth not supported"))})

	// ErrUnsupported is terminal: status is written but syncHandler must not
	// return an error (which would hot-loop the rate-limited queue).
	if err := c.syncHandler(context.Background(), "ns/c1"); err != nil {
		t.Fatalf("syncHandler returned error for unsupported cluster: %v", err)
	}

	got := statusUpdate(client.Actions())
	if got == nil {
		t.Fatalf("expected an UpdateStatus action, got none")
	}
	cond := apimeta.FindStatusCondition(got.Status.Conditions, conditionReady)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != conditionReasonUnsupported {
		t.Errorf("Ready condition = %+v, want False/Unsupported", cond)
	}
}

func TestSyncHandler_ProbeErrorRequeues(t *testing.T) {
	c, _, _ := newTestController(t, clusterWithInClusterConnectionMode(),
		fakeProber{err: errors.New("connection refused")})

	if err := c.syncHandler(context.Background(), "ns/c1"); err == nil {
		t.Fatalf("syncHandler returned nil, want error so the queue retries")
	}
}

func TestSyncHandler_NoOpWhenStatusUnchanged(t *testing.T) {
	prober := fakeProber{result: ProbeResult{Version: sampleKubeVersion, APIVersions: []string{"v1"}}}
	c, client, store := newTestController(t, clusterWithInClusterConnectionMode(), prober)

	// First sync writes status.
	if err := c.syncHandler(context.Background(), "ns/c1"); err != nil {
		t.Fatalf("first syncHandler: %v", err)
	}
	first := statusUpdate(client.Actions())
	if first == nil {
		t.Fatalf("expected status write on first sync")
	}

	// Reflect the written status back into the informer store and re-sync; the
	// computed status is identical, so no second write should occur.
	settled := clusterWithInClusterConnectionMode()
	settled.Status = first.Status
	if err := store.Update(settled); err != nil {
		t.Fatalf("updating informer store: %v", err)
	}
	client.ClearActions()

	if err := c.syncHandler(context.Background(), "ns/c1"); err != nil {
		t.Fatalf("second syncHandler: %v", err)
	}
	if got := statusUpdate(client.Actions()); got != nil {
		t.Errorf("unexpected second status write: %+v", got.Status)
	}
}
