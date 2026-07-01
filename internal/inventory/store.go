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
// The store is in-memory and lost on restart; losing it can only under-prune
// (leave orphans), never over-delete, because an empty inventory yields an empty
// orphan set. The interface is the seam for a durable (S3-backed) implementation
// later.
package inventory

import "sync"

// Ref identifies a managed resource. Identity for diffing is (Group, Kind,
// Namespace, Name) — Version is carried for reference only and deliberately
// excluded from identity, so a resource whose apiVersion bumps between renders
// is not treated as a delete-and-recreate.
type Ref struct {
	Group     string
	Version   string
	Kind      string
	Namespace string
	Name      string
}

// ID is the identity key used to compare refs across renders (Version-independent).
func (r Ref) ID() string {
	return r.Group + "|" + r.Kind + "|" + r.Namespace + "|" + r.Name
}

// Store holds the managed-resource inventory for each Release, keyed by the
// Release's namespace/name.
type Store interface {
	Get(releaseKey string) ([]Ref, bool)
	Put(releaseKey string, refs []Ref)
	Delete(releaseKey string)
}

type memoryStore struct {
	mu    sync.RWMutex
	items map[string][]Ref
}

// NewMemoryStore returns an in-memory, concurrency-safe Store.
func NewMemoryStore() Store {
	return &memoryStore{items: make(map[string][]Ref)}
}

func (s *memoryStore) Get(releaseKey string) ([]Ref, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	refs, ok := s.items[releaseKey]
	return refs, ok
}

func (s *memoryStore) Put(releaseKey string, refs []Ref) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[releaseKey] = refs
}

func (s *memoryStore) Delete(releaseKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, releaseKey)
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
