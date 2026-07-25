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

// Package inventory records the set of resources each Release currently manages,
// so the release controller can prune resources dropped from the rendered set.
// The production Store is backed by the ReleaseInventory aggregated API object
// (see apistore.go), so the record survives a controller restart — the delete
// path can still find and prune a Release's resources after the process bounced.
// The in-memory Store (NewMemoryStore) is retained for unit tests. Losing an
// inventory can only under-prune (leave orphans), never over-delete, because an
// empty inventory yields an empty orphan set.
package inventory

import (
	"context"
	"sync"

	"k8s.io/apimachinery/pkg/types"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

// Ref identifies a managed resource. Identity for diffing is (Group, Kind,
// Namespace, Name) — Version is carried for reference only and deliberately
// excluded from identity, so a resource whose apiVersion bumps between renders
// is not treated as a delete-and-recreate. Wave records the sync wave the
// resource was applied in, and UID the applied object's cluster UID; both are
// persisted for observability and are not part of identity. State is the
// observer-assessed health, read back so the controller can roll up per-wave
// health for the sync-wave gate; nil means not yet assessed.
type Ref struct {
	Group     string
	Version   string
	Kind      string
	Namespace string
	Name      string
	Wave      int
	UID       types.UID
	State     *corev1alpha1.ResourceState
}

// ID is the identity key used to compare refs across renders (Version-independent).
func (r Ref) ID() string {
	return r.Group + "|" + r.Kind + "|" + r.Namespace + "|" + r.Name
}

// Store holds the managed-resource inventory for each Release, keyed by the
// Release's namespace and name. Operations are context-aware and may fail
// (the durable store talks to the API server).
type Store interface {
	// Get returns the recorded refs for a Release and whether a record exists.
	Get(ctx context.Context, namespace, name string) ([]Ref, bool, error)
	// Put records the full managed set for a Release, replacing any prior record.
	Put(ctx context.Context, namespace, name string, refs []Ref) error
	// Delete removes the Release's inventory record.
	Delete(ctx context.Context, namespace, name string) error
}

type memoryStore struct {
	mu    sync.RWMutex
	items map[string][]Ref
}

// NewMemoryStore returns an in-memory, concurrency-safe Store, used in tests.
func NewMemoryStore() Store {
	return &memoryStore{items: make(map[string][]Ref)}
}

func (s *memoryStore) Get(_ context.Context, namespace, name string) ([]Ref, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	refs, ok := s.items[namespace+"/"+name]
	return refs, ok, nil
}

func (s *memoryStore) Put(_ context.Context, namespace, name string, refs []Ref) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[namespace+"/"+name] = refs
	return nil
}

func (s *memoryStore) Delete(_ context.Context, namespace, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, namespace+"/"+name)
	return nil
}

// Diff returns the refs in old whose identity is absent from current — i.e. the
// orphans that a prior render managed but the current render no longer does.
func Diff(old, current []Ref) []Ref {
	live := make(map[string]struct{}, len(current))
	for _, r := range current {
		live[r.ID()] = struct{}{}
	}
	var orphans []Ref
	for _, r := range old {
		if _, ok := live[r.ID()]; !ok {
			orphans = append(orphans, r)
		}
	}
	return orphans
}
