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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/health"
)

func relWithWaves(waves ...corev1alpha1.ResourceWaveStatus) *corev1alpha1.Release {
	return &corev1alpha1.Release{
		ObjectMeta: metav1.ObjectMeta{Name: "r1", Namespace: "ns"},
		Status:     corev1alpha1.ReleaseStatus{Waves: waves},
	}
}

func appliedWave(n int64) corev1alpha1.ResourceWaveStatus {
	return corev1alpha1.ResourceWaveStatus{Number: n, Applied: new(true)}
}

func entry(wave int, state health.State) healthEntry {
	return healthEntry{wave: wave, state: state}
}

func TestAppliedWaves(t *testing.T) {
	rel := relWithWaves(appliedWave(0), appliedWave(1), corev1alpha1.ResourceWaveStatus{Number: 2}) // wave 2 not applied
	got := appliedWaves(rel)
	if len(got) != 2 || !got[0] || !got[1] || got[2] {
		t.Errorf("appliedWaves = %v, want {0,1}", got)
	}
}

func TestWaveHealthRollup(t *testing.T) {
	// Waves 0 and 1 applied; wave 2 not applied.
	rel := relWithWaves(appliedWave(0), appliedWave(1), corev1alpha1.ResourceWaveStatus{Number: 2})
	entries := []healthEntry{
		entry(0, health.StateHealthy),  // wave 0: all healthy
		entry(0, health.StateHealthy),  //
		entry(1, health.StateHealthy),  // wave 1: one degraded → not healthy
		entry(1, health.StateDegraded), //
		entry(2, health.StateUnknown),  // wave 2: not applied → excluded
	}
	got := waveHealthRollup(rel, entries)

	if len(got) != 2 {
		t.Fatalf("rollup has %d waves, want 2 (only applied)", len(got))
	}
	if !got[0] {
		t.Error("wave 0 = false, want true (all healthy)")
	}
	if got[1] {
		t.Error("wave 1 = true, want false (one degraded)")
	}
	if _, ok := got[2]; ok {
		t.Error("wave 2 present, want excluded (not applied)")
	}
}

func TestWaveHealthRollup_AppliedWaveWithNoObjectsIsHealthy(t *testing.T) {
	// An applied wave with no assessed objects (e.g. no rules) is healthy by
	// default — nothing gates it.
	rel := relWithWaves(appliedWave(0))
	got := waveHealthRollup(rel, nil)
	if !got[0] {
		t.Error("applied wave with no objects = false, want true (nothing unhealthy)")
	}
}
