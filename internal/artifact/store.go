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

// Package artifact holds fetched source content in-process so a source
// controller (producer) and the release controller (consumer) can hand off
// rendered-ready files without re-fetching. The store is in-memory and lost on
// restart; producers repopulate it on their next reconcile.
package artifact

import (
	"fmt"
	"sync"
)

// Artifact is a snapshot of a source's content at a resolved revision.
type Artifact struct {
	// Revision is the resolved source revision (e.g. a git commit SHA).
	Revision string
	// Files maps a repo-relative path to its content.
	Files map[string][]byte
}

// Store holds the latest Artifact per source key.
type Store interface {
	Put(key string, a Artifact)
	Get(key string) (Artifact, bool)
}

// Key builds the store key for a source object.
func Key(kind, namespace, name string) string {
	return fmt.Sprintf("%s/%s/%s", kind, namespace, name)
}

type memoryStore struct {
	mu    sync.RWMutex
	items map[string]Artifact
}

// NewMemoryStore returns an in-memory, concurrency-safe Store.
func NewMemoryStore() Store {
	return &memoryStore{items: make(map[string]Artifact)}
}

func (s *memoryStore) Put(key string, a Artifact) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[key] = a
}

func (s *memoryStore) Get(key string) (Artifact, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.items[key]
	return a, ok
}
