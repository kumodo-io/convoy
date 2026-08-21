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

package inventory

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	fakeclient "github.com/kumodo-io/convoy/api/generated/clientset/versioned/fake"
	informers "github.com/kumodo-io/convoy/api/generated/informers/externalversions"
	"github.com/kumodo-io/convoy/internal/sharding"
)

func newAPIStore(t *testing.T, objs ...*corev1alpha1.ReleaseInventory) (Store, *fakeclient.Clientset) {
	t.Helper()
	runtimeObjs := make([]runtime.Object, 0, len(objs))
	for _, o := range objs {
		runtimeObjs = append(runtimeObjs, o)
	}
	client := fakeclient.NewSimpleClientset(runtimeObjs...)
	factory := informers.NewSharedInformerFactory(client, 0)
	informer := factory.Core().V1alpha1().ReleaseInventories()
	for _, o := range objs {
		if err := informer.Informer().GetStore().Add(o); err != nil {
			t.Fatalf("seeding inventory store: %v", err)
		}
	}
	releaseInformer := factory.Core().V1alpha1().Releases()
	return NewAPIStore(client, informer.Lister(), releaseInformer.Lister()), client
}

func TestAPIStore_CopiesShardLabelFromRelease(t *testing.T) {
	ctx := context.Background()
	const ns, name = "ns", "app"
	// Seed an empty RI (the fake Apply updates rather than create-on-applies) and
	// the owning Release carrying its apiserver-stamped shard label.
	ri := &corev1alpha1.ReleaseInventory{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
	rel := &corev1alpha1.Release{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: map[string]string{sharding.ShardLabel: "7"}}}
	client := fakeclient.NewSimpleClientset(ri)
	factory := informers.NewSharedInformerFactory(client, 0)
	riInformer := factory.Core().V1alpha1().ReleaseInventories()
	relInformer := factory.Core().V1alpha1().Releases()
	if err := riInformer.Informer().GetStore().Add(ri); err != nil {
		t.Fatalf("seeding RI: %v", err)
	}
	if err := relInformer.Informer().GetStore().Add(rel); err != nil {
		t.Fatalf("seeding release: %v", err)
	}
	store := NewAPIStore(client, riInformer.Lister(), relInformer.Lister())

	if err := store.Put(ctx, ns, name, []Ref{{Group: groupApps, Version: "v1", Kind: kindDeployment, Namespace: ns, Name: "d"}}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := client.CoreV1alpha1().ReleaseInventories(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get RI: %v", err)
	}
	if got.Labels[sharding.ShardLabel] != "7" {
		t.Fatalf("RI shard label = %q, want 7 (copied from its Release)", got.Labels[sharding.ShardLabel])
	}
}

// TestAPIStore_SurvivesRestart is the regression guard for the reported bug: a
// freshly-constructed store (as after a controller restart, in-memory state
// gone) still finds a Release's applied resources from the persisted
// ReleaseInventory, so the delete path can prune them.
func TestAPIStore_SurvivesRestart(t *testing.T) {
	ctx := context.Background()
	wave := 1
	existing := &corev1alpha1.ReleaseInventory{
		ObjectMeta: metav1.ObjectMeta{Name: "r1", Namespace: "ns"},
		Inventory: []corev1alpha1.ResourceInventoryItem{
			{Group: groupApps, Version: "v1", Kind: kindDeployment, Namespace: "ns", Name: nameAPI, Wave: &wave},
			{Version: "v1", Kind: kindConfigMap, Namespace: "ns", Name: nameCfg},
		},
	}
	store, _ := newAPIStore(t, existing)

	refs, ok, err := store.Get(ctx, "ns", "r1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || len(refs) != 2 {
		t.Fatalf("Get = %+v ok=%v, want 2 refs", refs, ok)
	}
	if refs[0].Name != nameAPI || refs[0].Wave != 1 {
		t.Errorf("ref[0] = %+v, want api at wave 1", refs[0])
	}
}

func TestAPIStore_PutAndDelete(t *testing.T) {
	ctx := context.Background()
	// Seed an existing (empty) inventory: the fake clientset's Apply does not
	// create-on-apply the way the real apiserver does (our strategy allows it), so
	// this exercises the update path over an existing object.
	store, client := newAPIStore(t, &corev1alpha1.ReleaseInventory{
		ObjectMeta: metav1.ObjectMeta{Name: "r1", Namespace: "ns"},
	})

	refs := []Ref{{Group: groupApps, Version: "v1", Kind: kindDeployment, Namespace: "ns", Name: nameAPI, Wave: 2}}
	if err := store.Put(ctx, "ns", "r1", refs); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := client.CoreV1alpha1().ReleaseInventories("ns").Get(ctx, "r1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("reading applied inventory: %v", err)
	}
	if len(got.Inventory) != 1 || got.Inventory[0].Name != nameAPI || got.Inventory[0].Wave == nil || *got.Inventory[0].Wave != 2 {
		t.Errorf("applied inventory = %+v, want one api entry at wave 2", got.Inventory)
	}

	if err := store.Delete(ctx, "ns", "r1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := client.CoreV1alpha1().ReleaseInventories("ns").Get(ctx, "r1", metav1.GetOptions{}); err == nil {
		t.Errorf("inventory still present after Delete")
	}
}

// TestAPIStore_DeleteMissingIsNoError confirms deleting an absent inventory (e.g.
// a Release that never applied) is treated as success.
func TestAPIStore_DeleteMissingIsNoError(t *testing.T) {
	store, _ := newAPIStore(t)
	if err := store.Delete(context.Background(), "ns", "gone"); err != nil {
		t.Errorf("Delete of missing inventory = %v, want nil", err)
	}
}

func TestMembershipChanged(t *testing.T) {
	healthy := corev1alpha1.ResourceStateHealthy
	degraded := corev1alpha1.ResourceStateDegraded
	dep := corev1alpha1.ResourceInventoryItem{Group: groupApps, Kind: kindDeployment, Namespace: "ns", Name: nameAPI}
	cfg := corev1alpha1.ResourceInventoryItem{Version: "v1", Kind: kindConfigMap, Namespace: "ns", Name: nameCfg}

	inv := func(items ...corev1alpha1.ResourceInventoryItem) *corev1alpha1.ReleaseInventory {
		return &corev1alpha1.ReleaseInventory{Inventory: items}
	}

	tests := []struct {
		name     string
		old, new *corev1alpha1.ReleaseInventory
		want     bool
	}{
		{caseIdentical, inv(dep), inv(dep), false},
		{"item added", inv(dep), inv(dep, cfg), true},
		{"item removed", inv(dep, cfg), inv(dep), true},
		{"identity swapped, same count", inv(dep), inv(cfg), true},
		{
			name: "health-only change is not membership",
			old:  inv(func() corev1alpha1.ResourceInventoryItem { d := dep; d.State = &degraded; return d }()),
			new:  inv(func() corev1alpha1.ResourceInventoryItem { d := dep; d.State = &healthy; return d }()),
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := MembershipChanged(tc.old, tc.new); got != tc.want {
				t.Errorf("MembershipChanged = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHealthChanged(t *testing.T) {
	healthy := corev1alpha1.ResourceStateHealthy
	progressing := corev1alpha1.ResourceStateProgressing
	dep := corev1alpha1.ResourceInventoryItem{Group: groupApps, Kind: kindDeployment, Namespace: "ns", Name: nameAPI}
	withState := func(s *corev1alpha1.ResourceState, assessed string) corev1alpha1.ResourceInventoryItem {
		d := dep
		d.State, d.AssessedRenderRef = s, assessed
		return d
	}
	inv := func(items ...corev1alpha1.ResourceInventoryItem) *corev1alpha1.ReleaseInventory {
		return &corev1alpha1.ReleaseInventory{Inventory: items}
	}

	tests := []struct {
		name     string
		old, new *corev1alpha1.ReleaseInventory
		want     bool
	}{
		{caseIdentical, inv(withState(&healthy, "r1")), inv(withState(&healthy, "r1")), false},
		{"state changed", inv(withState(&progressing, "r1")), inv(withState(&healthy, "r1")), true},
		{"assessedRenderRef changed", inv(withState(&healthy, "r1")), inv(withState(&healthy, "r2")), true},
		{"controller-only field (renderRef) ignored", func() *corev1alpha1.ReleaseInventory {
			d := withState(&healthy, "r1")
			d.RenderRef = "r1"
			return inv(d)
		}(), func() *corev1alpha1.ReleaseInventory {
			d := withState(&healthy, "r1")
			d.RenderRef = "r2" // controller-owned; must not count as a health change
			return inv(d)
		}(), false},
		{"item added", inv(withState(&healthy, "r1")), inv(withState(&healthy, "r1"),
			corev1alpha1.ResourceInventoryItem{Version: "v1", Kind: kindConfigMap, Namespace: "ns", Name: nameCfg}), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HealthChanged(tc.old, tc.new); got != tc.want {
				t.Errorf("HealthChanged = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRenderChanged(t *testing.T) {
	healthy := corev1alpha1.ResourceStateHealthy
	dep := corev1alpha1.ResourceInventoryItem{Group: groupApps, Kind: kindDeployment, Namespace: "ns", Name: nameAPI}
	withRender := func(renderRef string, s *corev1alpha1.ResourceState, assessed string) corev1alpha1.ResourceInventoryItem {
		d := dep
		d.RenderRef, d.State, d.AssessedRenderRef = renderRef, s, assessed
		return d
	}
	inv := func(items ...corev1alpha1.ResourceInventoryItem) *corev1alpha1.ReleaseInventory {
		return &corev1alpha1.ReleaseInventory{Inventory: items}
	}

	tests := []struct {
		name     string
		old, new *corev1alpha1.ReleaseInventory
		want     bool
	}{
		{caseIdentical, inv(withRender("r1", &healthy, "r1")), inv(withRender("r1", &healthy, "r1")), false},
		{"renderRef changed", inv(withRender("r1", &healthy, "r1")), inv(withRender("r2", &healthy, "r1")), true},
		{
			"observer-only field (state/assessed) ignored", inv(withRender("r1", &healthy, "r1")),
			inv(withRender("r1", nil, "r2")), false,
		},
		{"resource added", inv(withRender("r1", &healthy, "r1")), inv(withRender("r1", &healthy, "r1"),
			corev1alpha1.ResourceInventoryItem{Version: "v1", Kind: kindConfigMap, Namespace: "ns", Name: nameCfg, RenderRef: "r1"}), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := RenderChanged(tc.old, tc.new); got != tc.want {
				t.Errorf("RenderChanged = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHooksArmChanged(t *testing.T) {
	created := metav1.NewTime(time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC))
	later := metav1.NewTime(created.Add(time.Minute))
	running := corev1alpha1.LifecycleHookStateRunning
	succeeded := corev1alpha1.LifecycleHookStateSucceeded

	hook := func(mut func(*corev1alpha1.ReleaseLifecycleHookItem)) corev1alpha1.ReleaseLifecycleHookItem {
		h := corev1alpha1.ReleaseLifecycleHookItem{
			Name: hookNameMigrate, Phase: corev1alpha1.LifecycleHookPhasePreSync,
			WorkloadName: "migrate-1", CreatedAt: &created, RenderRef: "sha256:a",
		}
		if mut != nil {
			mut(&h)
		}
		return h
	}
	inv := func(hs ...corev1alpha1.ReleaseLifecycleHookItem) *corev1alpha1.ReleaseInventory {
		return &corev1alpha1.ReleaseInventory{Hooks: hs}
	}

	base := hook(nil)
	tests := []struct {
		name     string
		old, new *corev1alpha1.ReleaseInventory
		want     bool
	}{
		{caseIdentical, inv(base), inv(base), false},
		{"hook added", inv(), inv(base), true},
		{
			name: "re-armed: workloadName changed",
			old:  inv(base),
			new:  inv(hook(func(h *corev1alpha1.ReleaseLifecycleHookItem) { h.WorkloadName = "migrate-2" })),
			want: true,
		},
		{
			name: "re-armed: createdAt bumped",
			old:  inv(base),
			new:  inv(hook(func(h *corev1alpha1.ReleaseLifecycleHookItem) { h.CreatedAt = &later })),
			want: true,
		},
		{
			name: "re-armed: renderRef changed (same workloadName+createdAt)",
			old:  inv(base),
			new:  inv(hook(func(h *corev1alpha1.ReleaseLifecycleHookItem) { h.RenderRef = "sha256:b" })),
			want: true,
		},
		{
			name: "observer status write is not arming",
			old:  inv(hook(func(h *corev1alpha1.ReleaseLifecycleHookItem) { h.State = &running })),
			new:  inv(hook(func(h *corev1alpha1.ReleaseLifecycleHookItem) { h.State = &succeeded; h.FinishedAt = &later })),
			want: false,
		},
		{
			name: "pure deletion does not wake",
			old:  inv(base, hook(func(h *corev1alpha1.ReleaseLifecycleHookItem) { h.Name = "seed" })),
			new:  inv(base),
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HooksArmChanged(tc.old, tc.new); got != tc.want {
				t.Errorf("HooksArmChanged = %v, want %v", got, tc.want)
			}
		})
	}
}
