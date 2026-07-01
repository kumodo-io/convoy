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
	"errors"
	"testing"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	fakeclient "github.com/kumodo-io/convoy/api/generated/clientset/versioned/fake"
	informers "github.com/kumodo-io/convoy/api/generated/informers/externalversions"
	"github.com/kumodo-io/convoy/internal/artifact"
	"github.com/kumodo-io/convoy/internal/auth"
	"github.com/kumodo-io/convoy/internal/fetch"
)

const (
	dummyFile     = "app.yaml"
	configMapKind = "ConfigMap"
	dummyRevision = "abc123"
)

type fakeDeployer struct {
	got    []*unstructured.Unstructured
	called bool
	err    error
}

func (d *fakeDeployer) Deploy(_ context.Context, _ *corev1alpha1.Cluster, objects []*unstructured.Unstructured) error {
	d.called = true
	d.got = objects
	return d.err
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
					Name: "repo",
					Ref:  &corev1alpha1.ReleaseGitRepositoryRefSpec{Branch: new("main")},
				},
			},
			Content:    manifestsContent(dummyFile),
			ClusterRef: corev1alpha1.ReleaseClusterRefSpec{Name: "target"},
		},
	}
}

func reachableCluster() *corev1alpha1.Cluster {
	return &corev1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: "ns"},
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
	c := NewController(client, relInformer, clInformer.Lister(), coordinator, resolver, deployer)
	fixed := metav1.NewTime(time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC))
	c.now = func() metav1.Time { return fixed }
	return c, client
}

func statusUpdate(actions []clienttesting.Action) *corev1alpha1.Release {
	for _, a := range actions {
		if a.Matches("update", "releases") && a.GetSubresource() == "status" {
			return a.(clienttesting.UpdateAction).GetObject().(*corev1alpha1.Release)
		}
	}
	return nil
}

func readyReason(rel *corev1alpha1.Release) (metav1.ConditionStatus, string) {
	c := apimeta.FindStatusCondition(rel.Status.Conditions, conditionReady)
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
		t.Fatalf("expected an UpdateStatus action, got none")
	}
	if s, r := readyReason(got); s != metav1.ConditionTrue || r != conditionReasonApplied {
		t.Errorf("Ready = %s/%s, want True/Applied", s, r)
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
