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

package observer

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	fakeclient "github.com/kumodo-io/convoy/api/generated/clientset/versioned/fake"
	informers "github.com/kumodo-io/convoy/api/generated/informers/externalversions"
)

const (
	groupApps     = "apps"
	kindConfigMap = "ConfigMap"
	depName       = "api"
)

func inventoryHealthController(t *testing.T, ris ...*corev1alpha1.ReleaseInventory) (*HealthController, *fakeclient.Clientset) {
	t.Helper()
	objs := make([]runtime.Object, 0, len(ris))
	for _, ri := range ris {
		objs = append(objs, ri)
	}
	client := fakeclient.NewSimpleClientset(objs...)
	factory := informers.NewSharedInformerFactory(client, 0)
	informer := factory.Core().V1alpha1().ReleaseInventories()
	for _, ri := range ris {
		if err := informer.Informer().GetStore().Add(ri); err != nil {
			t.Fatalf("seeding inventory informer: %v", err)
		}
	}
	return &HealthController{client: client, inventoryLister: informer.Lister(), now: metav1.Now}, client
}

func deploymentEntry(name string, state corev1alpha1.ResourceState, message string, firstUnhealthy time.Time) healthEntry {
	return healthEntry{
		ref: objectRef{
			gvk:       schema.GroupVersionKind{Group: groupApps, Version: "v1", Kind: deploymentKind},
			namespace: "ns",
			name:      name,
		},
		state:          state,
		message:        message,
		firstUnhealthy: firstUnhealthy,
	}
}

// TestWriteInventoryHealth verifies the observer persists only the health fields
// onto an inventory entry (state/firstUnhealthy/message), leaving wave/uid — the
// controller's — untouched.
func TestWriteInventoryHealth(t *testing.T) {
	ctx := context.Background()
	rel := &corev1alpha1.Release{ObjectMeta: metav1.ObjectMeta{Name: "r1", Namespace: "ns"}}
	// Seed an existing inventory (the fake Apply does not create-on-apply).
	c, client := inventoryHealthController(t, &corev1alpha1.ReleaseInventory{
		ObjectMeta: metav1.ObjectMeta{Name: "r1", Namespace: "ns"},
	})

	first := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	entries := []healthEntry{
		deploymentEntry(depName, corev1alpha1.ResourceStateDegraded, "not ready", first),
		deploymentEntry("cache", corev1alpha1.ResourceStateHealthy, "", time.Time{}),
	}
	if err := c.writeInventoryHealth(ctx, rel, entries); err != nil {
		t.Fatalf("writeInventoryHealth: %v", err)
	}

	got, err := client.CoreV1alpha1().ReleaseInventories("ns").Get(ctx, "r1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("reading inventory: %v", err)
	}
	byName := map[string]corev1alpha1.ResourceInventoryItem{}
	for _, it := range got.Inventory {
		byName[it.Name] = it
	}
	api := byName[depName]
	if api.State == nil || *api.State != corev1alpha1.ResourceStateDegraded {
		t.Errorf("api state = %v, want Degraded", api.State)
	}
	if api.Message != "not ready" {
		t.Errorf("api message = %q, want 'not ready'", api.Message)
	}
	if api.FirstUnhealthy == nil || !api.FirstUnhealthy.Time.Equal(first) {
		t.Errorf("api firstUnhealthy = %v, want %v", api.FirstUnhealthy, first)
	}
	if api.Wave != nil || api.UID != nil {
		t.Errorf("api wave/uid = %v/%v, want nil (controller-owned)", api.Wave, api.UID)
	}
	// A Healthy object clears its anchor (firstUnhealthy omitted).
	if cache := byName["cache"]; cache.FirstUnhealthy != nil {
		t.Errorf("cache firstUnhealthy = %v, want nil (healthy)", cache.FirstUnhealthy)
	}
}

// TestPriorAndAnchors reads back the persisted prior states + progress anchors
// keyed by version-independent identity, so an in-flight progress clock survives
// a restart (and the wave-complete transition can be detected).
func TestPriorAndAnchors(t *testing.T) {
	first := metav1.NewTime(time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC))
	degraded := corev1alpha1.ResourceStateDegraded
	inv := &corev1alpha1.ReleaseInventory{
		ObjectMeta: metav1.ObjectMeta{Name: "r1", Namespace: "ns"},
		Inventory: []corev1alpha1.ResourceInventoryItem{
			{Group: groupApps, Kind: deploymentKind, Namespace: "ns", Name: depName, State: &degraded, FirstUnhealthy: &first},
			{Version: "v1", Kind: kindConfigMap, Namespace: "ns", Name: "cfg"}, // no state, no anchor
		},
	}

	prior, anchors := priorAndAnchors(inv)
	if got, ok := anchors["apps|Deployment|ns|api"]; !ok || !got.Equal(first.Time) {
		t.Errorf("api anchor = %v ok=%v, want %v", got, ok, first.Time)
	}
	if prior["apps|Deployment|ns|api"] != degraded {
		t.Errorf("api prior state = %v, want Degraded", prior["apps|Deployment|ns|api"])
	}
	if _, ok := anchors["|ConfigMap|ns|cfg"]; ok {
		t.Errorf("cfg has an anchor, want none (never unhealthy)")
	}
	if _, ok := prior["|ConfigMap|ns|cfg"]; ok {
		t.Errorf("cfg has a prior state, want none (unassessed)")
	}
}

// TestResultFromInventory builds the /health payload from persisted entries.
func TestResultFromInventory(t *testing.T) {
	degraded := corev1alpha1.ResourceStateDegraded
	inv := &corev1alpha1.ReleaseInventory{
		Inventory: []corev1alpha1.ResourceInventoryItem{
			{Group: groupApps, Version: "v1", Kind: deploymentKind, Namespace: "ns", Name: depName, State: &degraded, Message: "x"},
			{Version: "v1", Kind: kindConfigMap, Namespace: "ns", Name: "cfg"}, // no state
		},
	}
	res := resultFromInventory(inv)
	if res == nil || len(res.Objects) != 2 {
		t.Fatalf("result = %+v, want 2 objects", res)
	}
	if res.Objects[0].State != "Degraded" || res.Objects[0].Name != depName {
		t.Errorf("object[0] = %+v, want api/Degraded", res.Objects[0])
	}
	if res.Objects[1].State != "" {
		t.Errorf("object[1] state = %q, want empty (unassessed)", res.Objects[1].State)
	}

	if resultFromInventory(&corev1alpha1.ReleaseInventory{}) != nil {
		t.Errorf("empty inventory result = non-nil, want nil")
	}
}
