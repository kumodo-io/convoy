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
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kumodo-io/convoy/internal/health"
	"github.com/kumodo-io/convoy/internal/healthresult"
)

// objectRef identifies a managed object for the health store and /health payload.
type objectRef struct {
	gvk       schema.GroupVersionKind
	namespace string
	name      string
}

// key is the stable per-object map key (GVK + namespace/name).
func (r objectRef) key() string {
	return r.gvk.String() + "|" + r.namespace + "/" + r.name
}

// healthEntry is a single managed object's last assessed health, held in memory.
// FirstUnhealthy anchors the progress deadline (when the object first failed in
// the current unhealthy streak); zero when Healthy.
type healthEntry struct {
	ref            objectRef
	wave           int
	state          health.State
	message        string
	firstUnhealthy time.Time
}

// healthStore is the observer's in-memory, health-extended inventory: per Release
// key, the last assessed health of each managed object. It is the anchor for the
// progress deadline (Progressing vs. Degraded) and the source for the /health
// subresource. Ephemeral — lost on restart, which just restarts the progress
// clocks.
type healthStore struct {
	mu       sync.RWMutex
	releases map[string]map[string]healthEntry // releaseKey → objectKey → entry
}

func newHealthStore() *healthStore {
	return &healthStore{releases: make(map[string]map[string]healthEntry)}
}

// anchor returns the stored first-unhealthy time for an object, or zero if none.
func (s *healthStore) anchor(releaseKey, objectKey string) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.releases[releaseKey][objectKey].firstUnhealthy
}

// replace swaps in a Release's freshly-assessed entries, discarding entries for
// objects no longer managed (GC). An empty set removes the Release.
func (s *healthStore) replace(releaseKey string, entries map[string]healthEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(entries) == 0 {
		delete(s.releases, releaseKey)
		return
	}
	s.releases[releaseKey] = entries
}

// delete removes a Release's entries entirely (on deletion or when assessment is
// disabled).
func (s *healthStore) delete(releaseKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.releases, releaseKey)
}

// entries returns a copy of a Release's stored per-object health entries, for
// rolling up per-wave health.
func (s *healthStore) entries(releaseKey string) []healthEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	objs := s.releases[releaseKey]
	out := make([]healthEntry, 0, len(objs))
	for _, e := range objs {
		out = append(out, e)
	}
	return out
}

// result builds the /health payload for a Release from the stored entries, or nil
// if the Release has not been assessed.
func (s *healthStore) result(releaseKey string) *healthresult.Result {
	s.mu.RLock()
	defer s.mu.RUnlock()
	objs, ok := s.releases[releaseKey]
	if !ok {
		return nil
	}
	res := &healthresult.Result{Objects: make([]healthresult.ObjectHealth, 0, len(objs))}
	for _, e := range objs {
		res.Objects = append(res.Objects, healthresult.ObjectHealth{
			Group:     e.ref.gvk.Group,
			Version:   e.ref.gvk.Version,
			Kind:      e.ref.gvk.Kind,
			Namespace: e.ref.namespace,
			Name:      e.ref.name,
			State:     healthresult.State(e.state),
			Message:   e.message,
		})
	}
	return res
}
