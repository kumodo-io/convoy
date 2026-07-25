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

// TestAppliedWaves covers which waves the observer treats as applied — only
// those the controller marked applied in status.waves. The observer persists
// per-resource health onto the ReleaseInventory only for these waves (an
// unapplied wave's objects aren't in the cluster yet).
func TestAppliedWaves(t *testing.T) {
	rel := relWithWaves(appliedWave(0), appliedWave(1), corev1alpha1.ResourceWaveStatus{Number: 2}) // wave 2 not applied
	got := appliedWaves(rel)
	if len(got) != 2 || !got[0] || !got[1] || got[2] {
		t.Errorf("appliedWaves = %v, want {0,1}", got)
	}
}
