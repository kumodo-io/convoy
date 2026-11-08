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
	"slices"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/inventory"
)

const (
	gizmoCRD   = "gizmos.kb12.example.com"
	gizmoGroup = "kb12.example.com"
	teamA      = "team-a"
	orphanCM   = "old"
)

func liveObj(apiVersion, kind, namespace, name, uid string, owners ...string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetAPIVersion(apiVersion)
	u.SetKind(kind)
	u.SetNamespace(namespace)
	u.SetName(name)
	u.SetUID(types.UID(uid))
	refs := make([]metav1.OwnerReference, 0, len(owners))
	for _, o := range owners {
		refs = append(refs, metav1.OwnerReference{UID: types.UID(o)})
	}
	u.SetOwnerReferences(refs)
	return u
}

var teamANS = inventory.Ref{Version: "v1", Kind: "Namespace", Name: teamA}

func refNames(refs []inventory.Ref) []string {
	names := make([]string, 0, len(refs))
	for _, r := range refs {
		names = append(names, r.Name)
	}
	slices.Sort(names)
	return names
}

func TestUnmanaged(t *testing.T) {
	managed := []inventory.Ref{
		{Group: "apps", Version: "v1", Kind: "Deployment", Namespace: teamA, Name: "web"},
		{Version: "v1", Kind: "Service", Namespace: teamA, Name: "web", UID: "svc"},
	}
	objs := []unstructured.Unstructured{
		liveObj("v1", "Pod", teamA, "web-1-x", "pod", "rs"),
		liveObj("apps/v1", "ReplicaSet", teamA, "web-1", "rs", "dep"),
		liveObj("apps/v1", "Deployment", teamA, "web", "dep"),
		liveObj("discovery.k8s.io/v1", "EndpointSlice", teamA, "web-abc", "eps", "svc"),
		liveObj("v1", "Endpoints", teamA, "web", "ep"),
		liveObj("v1", "ServiceAccount", teamA, "default", "sa"),
		liveObj("v1", configMapKind, teamA, "kube-root-ca.crt", "root"),
		liveObj("v1", configMapKind, teamA, "foreign", "cm"),
		liveObj("v1", "Pod", teamA, "stray", "stray", "unknown"),
		liveObj(gizmoGroup+"/v1", "Gizmo", "default", "not-convoy", "gz"),
	}

	got := unmanaged(objs, managed)
	want := []string{"ConfigMap team-a/foreign", "Gizmo default/not-convoy", "Pod team-a/stray"}
	if !slices.Equal(got, want) {
		t.Errorf("unmanaged = %v, want %v", got, want)
	}
}

func TestListNamespaced(t *testing.T) {
	cms := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	events := schema.GroupVersionResource{Version: "v1", Resource: "events"}
	leases := schema.GroupVersionResource{Group: "coordination.k8s.io", Version: "v1", Resource: "leases"}
	in := liveObj("v1", configMapKind, teamA, "a", "a")
	out := liveObj("v1", configMapKind, "other", "b", "b")
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{cms: "ConfigMapList", events: "EventList", leases: "LeaseList"}, &in, &out)
	dyn.PrependReactor(verbList, "leases", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(leases.GroupResource(), "", errors.New("rbac"))
	})
	dyn.PrependReactor(verbList, "events", func(clienttesting.Action) (bool, runtime.Object, error) {
		t.Error("events were listed")
		return false, nil, nil
	})
	lists := []*metav1.APIResourceList{
		{GroupVersion: "v1", APIResources: []metav1.APIResource{
			{Name: cms.Resource, Namespaced: true, Kind: configMapKind, Verbs: []string{"get", verbList}},
			{Name: "events", Namespaced: true, Kind: "Event", Verbs: []string{verbList}},
			{Name: "pods/log", Namespaced: true, Kind: "Pod", Verbs: []string{"get"}},
			{Name: "bindings", Namespaced: true, Kind: "Binding", Verbs: []string{"create"}},
		}},
		{GroupVersion: "coordination.k8s.io/v1", APIResources: []metav1.APIResource{
			{Name: "leases", Namespaced: true, Kind: "Lease", Verbs: []string{verbList}},
		}},
	}

	objs, failures := listNamespaced(context.Background(), dyn, lists, teamA)
	if len(objs) != 1 || objs[0].GetName() != "a" || objs[0].GetKind() != configMapKind {
		t.Errorf("objects = %v, want [ConfigMap a]", objs)
	}
	if len(failures) != 1 || !strings.Contains(failures[0], "leases.coordination.k8s.io") {
		t.Errorf("failures = %v, want one for leases.coordination.k8s.io", failures)
	}
}

func TestListCustomResources(t *testing.T) {
	crds := schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
	gizmos := schema.GroupVersionResource{Group: gizmoGroup, Version: "v1", Resource: "gizmos"}
	crd := unstructured.Unstructured{Object: map[string]any{
		apiVersionKey: "apiextensions.k8s.io/v1", kindKey: "CustomResourceDefinition",
		metadataKey: map[string]any{nameKey: gizmoCRD},
		"spec": map[string]any{
			"group": gizmoGroup, "scope": "Namespaced",
			"names": map[string]any{"plural": "gizmos", "kind": "Gizmo"},
			"versions": []any{
				map[string]any{nameKey: "v1alpha1", "served": true, "storage": false},
				map[string]any{nameKey: "v1", "served": true, "storage": true},
			},
		},
	}}
	a := liveObj(gizmoGroup+"/v1", "Gizmo", "default", "not-convoy", "a")
	b := liveObj(gizmoGroup+"/v1", "Gizmo", "team-b", "other", "b")
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{crds: "CustomResourceDefinitionList", gizmos: "GizmoList"}, &crd, &a, &b)

	objs, failures := listCustomResources(context.Background(), dyn, gizmoCRD)
	names := make([]string, 0, len(objs))
	for _, o := range objs {
		names = append(names, o.GetName())
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"not-convoy", "other"}) || len(failures) != 0 {
		t.Errorf("objects = %v, failures = %v, want [not-convoy other] and none", names, failures)
	}

	if objs, failures := listCustomResources(context.Background(), dyn, "absent.example.com"); len(objs) != 0 || len(failures) != 0 {
		t.Errorf("absent CRD: objects = %v, failures = %v, want none", objs, failures)
	}
}

func TestBlockedMessage(t *testing.T) {
	ns := teamANS
	if got, want := blockedMessage(ns, []string{"a"}), "Namespace team-a not deleted, blocked by: a"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
	got := blockedMessage(ns, []string{"a", "b", "c", "d", "e", "f", "g"})
	if want := "Namespace team-a not deleted, blocked by: a, b, c, d, e and 2 more"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

func TestSyncHandler_BlockedOrphanNamespaceStaysInInventory(t *testing.T) {
	rel := manifestsRelease()
	rel.Spec.SyncPolicy.Prune = corev1alpha1.PruneSpec{Orphaned: true, IncludeNamespaces: true}
	deployer := &fakeDeployer{blockers: map[string][]string{teamA: {"ConfigMap team-a/live"}}}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	putInv(t, c, cmRef("cm"), cmRef(orphanCM), teamANS)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if got := refNames(deployer.pruned); !slices.Equal(got, []string{orphanCM}) {
		t.Errorf("pruned = %v, want [old]", got)
	}
	if got := refNames(deployer.managedSeen); !slices.Equal(got, []string{orphanCM, teamA}) {
		t.Errorf("managed = %v, want only the orphans [old team-a]", got)
	}
	got := statusUpdate(client.Actions())
	var kept *inventory.Ref
	for _, r := range getInv(t, c) {
		if r.Name == teamA {
			kept = &r
		}
	}
	if kept == nil || kept.RenderRef != got.Status.LastAttempted.RenderRef {
		t.Errorf("kept namespace = %+v, want it in the inventory at render %s", kept, got.Status.LastAttempted.RenderRef)
	}
	if s, r := readyReason(got); s != metav1.ConditionTrue || r != conditionReasonApplied {
		t.Errorf("Applied = %s/%s, want True/Applied", s, r)
	}
	if cond := apimeta.FindStatusCondition(got.Status.Conditions, conditionApplied); cond == nil || !strings.Contains(cond.Message, "ConfigMap team-a/live") {
		t.Errorf("Applied message = %+v, want the blocker named", cond)
	}
}
