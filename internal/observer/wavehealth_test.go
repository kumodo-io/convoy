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
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

// waveEntry builds a healthEntry at a given wave/state with an identity derived
// from its name (group apps, kind Deployment) for prior-state lookups.
func waveEntry(name string, wave int, state corev1alpha1.ResourceState) healthEntry {
	return healthEntry{
		ref:   objectRef{gvk: schema.GroupVersionKind{Group: "apps", Kind: deploymentKind}, namespace: "ns", name: name},
		wave:  wave,
		state: state,
	}
}

func TestUnhealthyResourceTargets(t *testing.T) {
	healthy := []healthEntry{
		waveEntry("a", 0, corev1alpha1.ResourceStateHealthy),
		waveEntry("b", 1, corev1alpha1.ResourceStateHealthy),
	}
	if got := unhealthyResourceTargets(healthy); len(got) != 0 {
		t.Errorf("all healthy → %v, want none pending", got)
	}
	if got := unhealthyResourceTargets(nil); len(got) != 0 {
		t.Errorf("empty → %v, want none pending", got)
	}
	mixed := []healthEntry{
		waveEntry("a", 0, corev1alpha1.ResourceStateHealthy),
		waveEntry("c", 1, corev1alpha1.ResourceStateProgressing),
	}
	got := unhealthyResourceTargets(mixed)
	if len(got) != 1 || got[0] != "res|apps|Deployment|ns|c" {
		t.Errorf("mixed → %v, want [res|apps|Deployment|ns|c]", got)
	}
}

func TestFrontierWaveCompleted(t *testing.T) {
	id := func(name string) string {
		return (objectRef{gvk: schema.GroupVersionKind{Group: "apps", Kind: deploymentKind}, namespace: "ns", name: name}).identityID()
	}
	healthy := corev1alpha1.ResourceStateHealthy
	progressing := corev1alpha1.ResourceStateProgressing

	tests := []struct {
		name    string
		entries []healthEntry
		prior   map[string]corev1alpha1.ResourceState
		want    bool
	}{
		{
			name:    "frontier just became healthy",
			entries: []healthEntry{waveEntry("gate", 0, healthy), waveEntry("svc", 1, healthy)},
			prior:   map[string]corev1alpha1.ResourceState{id("gate"): healthy, id("svc"): progressing},
			want:    true, // svc (wave 1, the frontier) transitioned to healthy
		},
		{
			name:    "frontier not fully healthy",
			entries: []healthEntry{waveEntry("gate", 0, healthy), waveEntry("svc", 1, progressing)},
			prior:   map[string]corev1alpha1.ResourceState{id("gate"): healthy},
			want:    false,
		},
		{
			name:    "frontier steady healthy, no transition",
			entries: []healthEntry{waveEntry("svc", 1, healthy)},
			prior:   map[string]corev1alpha1.ResourceState{id("svc"): healthy},
			want:    false, // already healthy last pass — don't re-fire
		},
		{
			name:    "newly-appeared already-healthy frontier",
			entries: []healthEntry{waveEntry("gate", 0, healthy), waveEntry("svc", 1, healthy)},
			prior:   map[string]corev1alpha1.ResourceState{id("gate"): healthy}, // svc absent before
			want:    true,
		},
		{
			name:    "lower wave healthy but frontier progressing",
			entries: []healthEntry{waveEntry("gate", 0, healthy), waveEntry("svc", 1, progressing)},
			prior:   map[string]corev1alpha1.ResourceState{id("gate"): progressing},
			want:    false, // gate transitioned but it's not the frontier
		},
		{
			name:    "empty",
			entries: nil,
			want:    false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := frontierWaveCompleted(tc.entries, tc.prior); got != tc.want {
				t.Errorf("frontierWaveCompleted = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProgressBackoff(t *testing.T) {
	const cap = time.Minute // large enough not to clamp the staircase
	// Staircase: 2s×5, 4s×4, 8s×3, 16s×2, 32s×∞ (attempts are 1-based, cumulative).
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 2 * time.Second},  // clamped to 1
		{1, 2 * time.Second},  // level 0 (2s ×5): attempts 1..5
		{5, 2 * time.Second},  // still level 0
		{6, 4 * time.Second},  // level 1 (4s ×4): attempts 6..9
		{9, 4 * time.Second},  // still level 1
		{10, 8 * time.Second}, // level 2 (8s ×3): attempts 10..12
		{12, 8 * time.Second},
		{13, 16 * time.Second}, // level 3 (16s ×2): attempts 13..14
		{14, 16 * time.Second},
		{15, 32 * time.Second},  // level 4 (32s ×∞)
		{999, 32 * time.Second}, // far tail
	}
	for _, tc := range tests {
		if got := progressBackoff(tc.attempt, cap); got != tc.want {
			t.Errorf("progressBackoff(%d) = %s, want %s", tc.attempt, got, tc.want)
		}
	}
}

func TestProgressBackoff_CappedAtInterval(t *testing.T) {
	// A short interval caps the staircase so it never polls slower than steady state.
	const cap = 10 * time.Second
	if got := progressBackoff(1, cap); got != 2*time.Second {
		t.Errorf("early attempt = %s, want 2s (under cap)", got)
	}
	if got := progressBackoff(99, cap); got != cap {
		t.Errorf("tail attempt = %s, want %s (capped at interval)", got, cap)
	}
}

func TestBackoffCounts_PerTarget(t *testing.T) {
	const cap = time.Minute
	const slow = "res|a" // a chronically-slow resource target
	const relKey = "ns/r"
	b := newBackoffCounts()

	// One slow target climbs its own staircase across passes.
	for i := 1; i <= 6; i++ {
		b.advance(relKey, []string{slow}, cap)
	}
	// After 6 passes, slow is at attempt 6 → 4s. A freshly-added target starts at
	// attempt 1 → 2s, and the release re-checks at the MIN (2s), not the slow 4s.
	if d := b.advance(relKey, []string{slow, "hook|x"}, cap); d != 2*time.Second {
		t.Errorf("min delay with a fresh target = %s, want 2s (fresh pulls the cadence)", d)
	}

	// The slow target keeps its climbed count: alone again, it is back to its own pace.
	// (slow is now at attempt 8 → 4s; hook|x dropped since it's no longer pending.)
	if d := b.advance(relKey, []string{slow}, cap); d != 4*time.Second {
		t.Errorf("slow target alone = %s, want 4s (kept its climbed count)", d)
	}

	// A target that stops being pending is dropped, so it restarts fresh next time.
	b.advance(relKey, []string{slow}, cap) // slow alone, still climbing
	if d := b.advance(relKey, []string{"hook|x"}, cap); d != 2*time.Second {
		t.Errorf("re-added target = %s, want 2s (dropped when not pending)", d)
	}

	// Nothing pending clears the release and returns 0.
	if d := b.advance(relKey, nil, cap); d != 0 {
		t.Errorf("empty pending = %s, want 0", d)
	}
	if _, ok := b.n[relKey]; ok {
		t.Error("release counts not cleared when nothing pending")
	}
}
