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

// Package artifact holds fetched source content in-process, handed off from the
// fetch coordinator (producer) to the release controller (consumer) without
// re-fetching. Content is addressed by (URL, revision): identical content is
// held once and shared, regardless of which source referenced it. The store is
// in-memory and lost on restart; it is repopulated on the next fetch.
package artifact

import (
	"fmt"
	"strings"
	"sync"
)

// Artifact is a snapshot of a source's content at a resolved revision.
type Artifact struct {
	// Revision is the resolved source revision (e.g. a git commit SHA).
	Revision string

	// Files maps a repo-relative path to its content.
	Files map[string][]byte
}

// Store holds artifacts keyed by content identity.
type Store interface {
	Put(key string, a Artifact)
	Get(key string) (Artifact, bool)
}

// Key builds the content-addressed store key for a fetched revision. Content is
// identified by (normalized URL, revision) — not by the referencing source — so
// the same bytes are stored once and shared.
func Key(url, revision string) string {
	return fmt.Sprintf("%s@%s", normalizeURL(url), revision)
}

// normalizeURL canonicalizes a repo URL so equivalent forms collapse to one key.
func normalizeURL(url string) string {
	u := strings.TrimSpace(url)
	u = strings.TrimSuffix(u, "/")
	u = strings.TrimSuffix(u, ".git")
	return strings.ToLower(u)
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
