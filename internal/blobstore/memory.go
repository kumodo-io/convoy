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

package blobstore

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// memory is an in-process BlobStore. It backs dev/single-node runs and is the
// test double for everything layered above the interface (no cloud, no network).
type memory struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewMemory returns an empty in-memory BlobStore.
func NewMemory() BlobStore {
	return &memory{data: make(map[string][]byte)}
}

func (m *memory) Put(_ context.Context, key string, data []byte) error {
	// Copy so a later mutation of the caller's slice can't corrupt the store.
	buf := make([]byte, len(data))
	copy(buf, data)
	m.mu.Lock()
	m.data[key] = buf
	m.mu.Unlock()
	return nil
}

func (m *memory) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	buf, ok := m.data[key]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	out := make([]byte, len(buf))
	copy(out, buf)
	return out, nil
}

func (m *memory) Exists(_ context.Context, key string) (bool, error) {
	m.mu.RLock()
	_, ok := m.data[key]
	m.mu.RUnlock()
	return ok, nil
}

func (m *memory) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.RLock()
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	m.mu.RUnlock()
	sort.Strings(keys)
	return keys, nil
}

func (m *memory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.data, key)
	m.mu.Unlock()
	return nil
}
