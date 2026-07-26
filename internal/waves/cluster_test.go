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

package waves

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

func clusterRel(namespace, name string, wave int, strategy corev1alpha1.ReleaseWaveStrategy, timeout *metav1.Duration) *corev1alpha1.Release {
	return &corev1alpha1.Release{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: corev1alpha1.ReleaseSpec{
			SyncPolicy: corev1alpha1.ReleaseSyncPolicySpec{
				WaveStrategy: corev1alpha1.ReleaseWaveStrategySpec{Strategy: strategy, Wave: wave, Timeout: timeout},
			},
		},
	}
}

func TestClusterStrategyOf(t *testing.T) {
	if got := ClusterStrategyOf(clusterRel("ns", "r", 0, "", nil)); got != corev1alpha1.ReleaseWaveStrategyWaitForApplied {
		t.Errorf("empty strategy = %q, want waitForApplied (default)", got)
	}
	if got := ClusterStrategyOf(clusterRel("ns", "r", 0, corev1alpha1.ReleaseWaveStrategyWaitForHealthy, nil)); got != corev1alpha1.ReleaseWaveStrategyWaitForHealthy {
		t.Errorf("strategy = %q, want waitForHealthy", got)
	}
}

func TestClusterWaveBound(t *testing.T) {
	d := metav1.Duration{Duration: 45 * time.Second}

	// waitForApplied / waitForHealthy are never bounded.
	if _, bounded := ClusterWaveBound(clusterRel("ns", "r", 0, corev1alpha1.ReleaseWaveStrategyWaitForHealthy, &d)); bounded {
		t.Error("waitForHealthy reported bounded, want unbounded")
	}
	// waitForHealthyWithTimeout with an explicit timeout is bounded.
	got, bounded := ClusterWaveBound(clusterRel("ns", "r", 0, corev1alpha1.ReleaseWaveStrategyWaitForHealthyWithTimeout, &d))
	if !bounded || got != 45*time.Second {
		t.Errorf("bound = (%s,%v), want (45s,true)", got, bounded)
	}
	// waitForHealthyWithTimeout without a timeout waits indefinitely (unbounded).
	if _, bounded := ClusterWaveBound(clusterRel("ns", "r", 0, corev1alpha1.ReleaseWaveStrategyWaitForHealthyWithTimeout, nil)); bounded {
		t.Error("WithTimeout + nil timeout reported bounded, want unbounded (waits indefinitely)")
	}
}

func TestClusterPredecessors(t *testing.T) {
	me := clusterRel("ns", "app", 5, corev1alpha1.ReleaseWaveStrategyWaitForApplied, nil)
	siblings := []*corev1alpha1.Release{
		clusterRel("infra", "db", 0, "", nil),      // lower wave, other ns → predecessor
		clusterRel("ns", "cache", 3, "", nil),       // lower wave → predecessor
		clusterRel("ns", "app", 5, "", nil),         // self → excluded
		clusterRel("ns", "peer", 5, "", nil),        // same wave → not a predecessor
		clusterRel("ns", "frontend", 9, "", nil),    // higher wave → not a predecessor
	}
	preds := ClusterPredecessors(me, siblings)
	if len(preds) != 2 {
		t.Fatalf("predecessors = %d, want 2 (db, cache)", len(preds))
	}
	names := map[string]bool{}
	for _, p := range preds {
		names[p.Name] = true
	}
	if !names["db"] || !names["cache"] {
		t.Errorf("predecessors = %v, want {db, cache}", names)
	}
}
