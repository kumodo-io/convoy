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

func TestAllHealthy(t *testing.T) {
	healthy := []healthEntry{
		waveEntry("a", 0, corev1alpha1.ResourceStateHealthy),
		waveEntry("b", 1, corev1alpha1.ResourceStateHealthy),
	}
	if !allHealthy(healthy) {
		t.Error("allHealthy = false, want true (all healthy)")
	}
	if !allHealthy(nil) {
		t.Error("allHealthy(nil) = false, want true (nothing unhealthy)")
	}
	mixed := []healthEntry{
		waveEntry("a", 0, corev1alpha1.ResourceStateHealthy),
		waveEntry("c", 1, corev1alpha1.ResourceStateProgressing),
	}
	if allHealthy(mixed) {
		t.Error("allHealthy = true, want false (one progressing)")
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
	const cap = 30 * time.Second
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 2 * time.Second},  // clamped to 1
		{1, 2 * time.Second},  // base
		{2, 4 * time.Second},  // ×2
		{3, 8 * time.Second},  // ×2
		{4, 16 * time.Second}, // ×2
		{5, cap},              // 32s → capped at 30s
		{99, cap},             // far past cap (shift guard)
	}
	for _, tc := range tests {
		if got := progressBackoff(tc.attempt, cap); got != tc.want {
			t.Errorf("progressBackoff(%d) = %s, want %s", tc.attempt, got, tc.want)
		}
	}
}
