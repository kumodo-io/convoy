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
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	corev1alpha1ac "github.com/kumodo-io/convoy/api/generated/applyconfiguration/core/v1alpha1"
	clientset "github.com/kumodo-io/convoy/api/generated/clientset/versioned"
	corelisters "github.com/kumodo-io/convoy/api/generated/listers/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/sharding"
)

// ControllerFieldManager is the Server-Side Apply field manager the release
// controller owns on a ReleaseInventory. It asserts the inventory membership and
// each entry's identity/wave/uid; the observer's health fields are owned by a
// separate manager, so the two writers co-own each entry without clobbering.
const ControllerFieldManager = "convoy-release-inventory"

// HookFieldManager is the Server-Side Apply field manager the release controller
// owns for lifecycle-hook *identity* on a ReleaseInventory. It is deliberately
// disjoint from ControllerFieldManager: the resource-inventory Put asserts
// `inventory` without `hooks`, so sharing a manager would SSA-clobber the hook
// entries. The observer's hook-status manager (convoy-hook-status) owns the
// runtime fields, so all three co-own each hook entry by field.
const HookFieldManager = "convoy-release-hooks"

// apiStore is the durable Store backed by the ReleaseInventory aggregated API
// object (one per Release, colocated in the Release's namespace, named after it).
// Reads come from the informer cache; writes go straight to the API server via
// Server-Side Apply. Because the record lives in the cluster, it survives a
// controller restart — the delete path can still enumerate and prune a Release's
// resources after the process bounced.
type apiStore struct {
	client        clientset.Interface
	lister        corelisters.ReleaseInventoryLister
	releaseLister corelisters.ReleaseLister
}

// NewAPIStore returns a Store backed by ReleaseInventory objects. releaseLister
// lets it copy each Release's shard label onto its inventory (the RI shards with
// its Release; see docs/architecture/05-shard-scoped-caches.md).
func NewAPIStore(client clientset.Interface, lister corelisters.ReleaseInventoryLister, releaseLister corelisters.ReleaseLister) Store {
	return &apiStore{client: client, lister: lister, releaseLister: releaseLister}
}

// shardLabels returns the shard label to stamp on a Release's inventory, copied
// from the Release's own (apiserver-stamped) label — so the RI is guaranteed to
// share its Release's shard. Returns nil if the Release or its label is absent;
// the RI is then left unlabeled until the Release is labeled.
func (s *apiStore) shardLabels(namespace, name string) map[string]string {
	if s.releaseLister == nil {
		return nil
	}
	rel, err := s.releaseLister.Releases(namespace).Get(name)
	if err != nil {
		return nil
	}
	if v, ok := rel.Labels[sharding.ShardLabel]; ok {
		return map[string]string{sharding.ShardLabel: v}
	}
	return nil
}

func (s *apiStore) Get(_ context.Context, namespace, name string) ([]Ref, bool, error) {
	inv, err := s.lister.ReleaseInventories(namespace).Get(name)
	if apierrors.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("reading release inventory %s/%s: %w", namespace, name, err)
	}
	return refsFromItems(inv.Inventory), true, nil
}

func (s *apiStore) Put(ctx context.Context, namespace, name string, refs []Ref) error {
	ac := corev1alpha1ac.ReleaseInventory(name, namespace)
	if ls := s.shardLabels(namespace, name); ls != nil {
		ac.WithLabels(ls)
	}
	for _, r := range refs {
		item := corev1alpha1ac.ResourceInventoryItem().
			WithGroup(r.Group).
			WithVersion(r.Version).
			WithKind(r.Kind).
			WithNamespace(r.Namespace).
			WithName(r.Name).
			WithWave(r.Wave)
		if r.UID != "" {
			item.WithUID(r.UID)
		}
		if r.RenderRef != "" {
			item.WithRenderRef(r.RenderRef)
		}
		ac.WithInventory(item)
	}
	if _, err := s.client.CoreV1alpha1().ReleaseInventories(namespace).
		Apply(ctx, ac, metav1.ApplyOptions{FieldManager: ControllerFieldManager, Force: true}); err != nil {
		return fmt.Errorf("applying release inventory %s/%s: %w", namespace, name, err)
	}
	return nil
}

func (s *apiStore) Delete(ctx context.Context, namespace, name string) error {
	err := s.client.CoreV1alpha1().ReleaseInventories(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting release inventory %s/%s: %w", namespace, name, err)
	}
	return nil
}

func (s *apiStore) PutHooks(ctx context.Context, namespace, name string, hooks []HookRef) error {
	ac := corev1alpha1ac.ReleaseInventory(name, namespace)
	if ls := s.shardLabels(namespace, name); ls != nil {
		ac.WithLabels(ls)
	}
	for _, h := range hooks {
		item := corev1alpha1ac.ReleaseLifecycleHookItem().
			WithName(h.Name).
			WithPhase(h.Phase).
			WithWeight(h.Weight).
			WithGroup(h.Group).
			WithVersion(h.Version).
			WithKind(h.Kind).
			WithWorkloadName(h.WorkloadName).
			WithRenderRef(h.RenderRef)
		if h.WorkloadNamespace != "" {
			item.WithWorkloadNamespace(h.WorkloadNamespace)
		}
		if h.CreatedAt != nil {
			item.WithCreatedAt(*h.CreatedAt)
		}
		if h.Timeout != nil {
			item.WithTimeout(*h.Timeout)
		}
		ac.WithHooks(item)
	}
	if _, err := s.client.CoreV1alpha1().ReleaseInventories(namespace).
		Apply(ctx, ac, metav1.ApplyOptions{FieldManager: HookFieldManager, Force: true}); err != nil {
		return fmt.Errorf("applying release inventory hooks %s/%s: %w", namespace, name, err)
	}
	return nil
}

// GetHooks reads live from the API server, not the informer cache: the controller
// writes hook identity via PutHooks and then reads it back in the SAME reconcile
// (to gate and to roll up status). A cache read would return the pre-write revision,
// so a just-armed hook would be invisible until the cache caught up — leaving status
// a step behind (never showing a hook as Running) and the gate reading stale state.
// This matches the in-memory store, which always returns the latest Put.
func (s *apiStore) GetHooks(ctx context.Context, namespace, name string) ([]corev1alpha1.ReleaseLifecycleHookItem, error) {
	inv, err := s.client.CoreV1alpha1().ReleaseInventories(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading release inventory %s/%s: %w", namespace, name, err)
	}
	return inv.Hooks, nil
}

// MembershipChanged reports whether the controller-owned membership of a
// ReleaseInventory changed between two revisions — i.e. the set of entry
// identities (group|kind|namespace|name) differs. It ignores the observer's
// health-field updates on existing entries, so the observer's health loop can
// watch the inventory for "a resource was applied" and wake on that, without
// re-waking on its own health writes.
func MembershipChanged(oldInv, newInv *corev1alpha1.ReleaseInventory) bool {
	if len(oldInv.Inventory) != len(newInv.Inventory) {
		return true
	}
	seen := make(map[string]struct{}, len(oldInv.Inventory))
	for i := range oldInv.Inventory {
		seen[itemIdentityID(oldInv.Inventory[i])] = struct{}{}
	}
	for i := range newInv.Inventory {
		if _, ok := seen[itemIdentityID(newInv.Inventory[i])]; !ok {
			return true
		}
	}
	return false
}

// itemIdentityID is the version-independent identity key (group|kind|namespace|name)
// of an inventory entry, matching Ref.ID().
func itemIdentityID(it corev1alpha1.ResourceInventoryItem) string {
	return it.Group + "|" + it.Kind + "|" + it.Namespace + "|" + it.Name
}

// HooksArmChanged reports whether the controller (re-)armed any lifecycle hook
// between two ReleaseInventory revisions — i.e. a hook entry was added, or an
// existing entry's arming (workloadName, createdAt, or renderRef) changed for a
// new run. It ignores the observer's own state/finishedAt/message writes, so the
// observer wakes on "a hook was armed" without re-waking on its own status writes
// — the hook analog of MembershipChanged.
//
// A pure deletion does NOT wake: only new hooks are checked against the old set,
// so a removed entry (nothing left to poll) is silent; the observer's existing
// backoff tick reconciles any hook that vanished mid-run.
func HooksArmChanged(oldInv, newInv *corev1alpha1.ReleaseInventory) bool {
	old := make(map[string]string, len(oldInv.Hooks))
	for i := range oldInv.Hooks {
		old[hookKey(oldInv.Hooks[i])] = hookArmToken(oldInv.Hooks[i])
	}
	for i := range newInv.Hooks {
		tok, ok := old[hookKey(newInv.Hooks[i])]
		if !ok || tok != hookArmToken(newInv.Hooks[i]) {
			return true
		}
	}
	return false
}

// hookKey is the composite identity of a hook entry (phase, name).
func hookKey(h corev1alpha1.ReleaseLifecycleHookItem) string {
	return string(h.Phase) + "|" + h.Name
}

// hookArmToken captures the controller-owned arming state of a hook: its workload
// name, creation time, and render. A change to any means a fresh run the observer
// must (re)poll.
func hookArmToken(h corev1alpha1.ReleaseLifecycleHookItem) string {
	created := ""
	if h.CreatedAt != nil {
		created = h.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return h.WorkloadName + "@" + created + "@" + h.RenderRef
}

// refsFromItems projects the persisted inventory entries back to refs. The
// observer-owned State is carried through so the controller can roll up per-wave
// health for the sync-wave gate; the other health fields (firstUnhealthy,
// message) are not needed by the controller and are dropped here.
func refsFromItems(items []corev1alpha1.ResourceInventoryItem) []Ref {
	refs := make([]Ref, 0, len(items))
	for i := range items {
		it := items[i]
		ref := Ref{
			Group:             it.Group,
			Version:           it.Version,
			Kind:              it.Kind,
			Namespace:         it.Namespace,
			Name:              it.Name,
			State:             it.State,
			RenderRef:         it.RenderRef,
			AssessedRenderRef: it.AssessedRenderRef,
		}
		if it.Wave != nil {
			ref.Wave = *it.Wave
		}
		if it.UID != nil {
			ref.UID = *it.UID
		}
		refs = append(refs, ref)
	}
	return refs
}
