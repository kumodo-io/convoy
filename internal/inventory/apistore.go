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
)

// ControllerFieldManager is the Server-Side Apply field manager the release
// controller owns on a ReleaseInventory. It asserts the inventory membership and
// each entry's identity/wave/uid; the observer's health fields are owned by a
// separate manager, so the two writers co-own each entry without clobbering.
const ControllerFieldManager = "convoy-release-inventory"

// apiStore is the durable Store backed by the ReleaseInventory aggregated API
// object (one per Release, colocated in the Release's namespace, named after it).
// Reads come from the informer cache; writes go straight to the API server via
// Server-Side Apply. Because the record lives in the cluster, it survives a
// controller restart — the delete path can still enumerate and prune a Release's
// resources after the process bounced.
type apiStore struct {
	client clientset.Interface
	lister corelisters.ReleaseInventoryLister
}

// NewAPIStore returns a Store backed by ReleaseInventory objects.
func NewAPIStore(client clientset.Interface, lister corelisters.ReleaseInventoryLister) Store {
	return &apiStore{client: client, lister: lister}
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

// refsFromItems projects the persisted inventory entries back to refs. The
// observer-owned State is carried through so the controller can roll up per-wave
// health for the sync-wave gate; the other health fields (firstUnhealthy,
// message) are not needed by the controller and are dropped here.
func refsFromItems(items []corev1alpha1.ResourceInventoryItem) []Ref {
	refs := make([]Ref, 0, len(items))
	for i := range items {
		it := items[i]
		ref := Ref{
			Group:     it.Group,
			Version:   it.Version,
			Kind:      it.Kind,
			Namespace: it.Namespace,
			Name:      it.Name,
			State:     it.State,
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
