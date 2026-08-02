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

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

// objectRef identifies a managed object for the /health payload and for matching
// its ReleaseInventory entry.
type objectRef struct {
	gvk       schema.GroupVersionKind
	namespace string
	name      string
}

// identityID is the version-independent identity key (group|kind|namespace|name),
// matching inventory.Ref.ID() so a health entry lines up with the controller's
// ReleaseInventory entry for the same object regardless of apiVersion.
func (r objectRef) identityID() string {
	return r.gvk.Group + "|" + r.gvk.Kind + "|" + r.namespace + "|" + r.name
}

// healthEntry is a single managed object's assessed health. FirstUnhealthy
// anchors the progress deadline (when the object first failed in the current
// unhealthy streak); zero when Healthy. wave is the sync wave the resource was
// applied in (from its ReleaseInventory entry), used to roll up per-wave
// completion for the gate-advance sync request.
type healthEntry struct {
	ref            objectRef
	wave           int
	state          corev1alpha1.ResourceState
	message        string
	firstUnhealthy time.Time
}

// backoffCounts drives the health loop's progress backoff: fast re-checks while a
// target (a resource or a lifecycle hook) is still pending. Each release tracks a
// per-target consecutive-attempt count, so a freshly-pending target backs off from
// scratch (2s) while a chronically-slow one stays at its climbed cadence — and the
// release polls as fast as its most-recently-active target needs. Concurrency-safe;
// in-memory (a lost count on restart only resets the cadence).
type backoffCounts struct {
	mu sync.Mutex
	n  map[string]map[string]int // releaseKey → targetID → attempts
}

func newBackoffCounts() *backoffCounts {
	return &backoffCounts{n: make(map[string]map[string]int)}
}

// advance increments the attempt count of every currently-pending target, drops the
// counts of targets that are no longer pending (settled or vanished), and returns
// the minimum staircase delay across the pending targets — so a target at attempt 1
// (2s) pulls the whole release to a fast re-check regardless of what else is slow.
// A pending set that is empty clears the release's counts and returns 0 (the caller
// falls back to the steady interval).
func (b *backoffCounts) advance(releaseKey string, targets []string, capDelay time.Duration) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(targets) == 0 {
		delete(b.n, releaseKey)
		return 0
	}
	prev := b.n[releaseKey]
	next := make(map[string]int, len(targets))
	var minDelay time.Duration
	for i, t := range targets {
		c := prev[t] + 1 // prev[t] is 0 for a target seen the first time
		next[t] = c
		if d := progressBackoff(c, capDelay); i == 0 || d < minDelay {
			minDelay = d
		}
	}
	b.n[releaseKey] = next
	return minDelay
}

// clear drops all per-target counts for a release (called once it is fully settled).
func (b *backoffCounts) clear(releaseKey string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.n, releaseKey)
}
