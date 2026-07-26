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
	"time"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

// Axis B — Release-within-Cluster ordering. Unlike Axis A (which orders a single
// Release's rendered objects via per-object annotations), Axis B orders whole
// Releases sharing a target cluster, using each Release's spec.syncPolicy.
// waveStrategy. These are pure selectors over the Release spec; the controller
// reads predecessor conditions (Applied/Healthy) itself and decides hold/fail.

// ClusterWaveOf returns a Release's cluster-wave number (spec.syncPolicy.
// waveStrategy.wave); default 0.
func ClusterWaveOf(rel *corev1alpha1.Release) int {
	return rel.Spec.SyncPolicy.WaveStrategy.Wave
}

// ClusterStrategyOf returns a Release's cluster-wave readiness strategy, defaulting
// to waitForApplied when unset or unrecognized.
func ClusterStrategyOf(rel *corev1alpha1.Release) corev1alpha1.ReleaseWaveStrategy {
	switch rel.Spec.SyncPolicy.WaveStrategy.Strategy {
	case corev1alpha1.ReleaseWaveStrategyWaitForHealthy:
		return corev1alpha1.ReleaseWaveStrategyWaitForHealthy
	case corev1alpha1.ReleaseWaveStrategyWaitForHealthyWithTimeout:
		return corev1alpha1.ReleaseWaveStrategyWaitForHealthyWithTimeout
	default:
		return corev1alpha1.ReleaseWaveStrategyWaitForApplied
	}
}

// ClusterWaveBound returns the bounded-wait timeout for a Release's cluster wave
// and whether the wait is bounded at all. The wait is bounded only for
// waitForHealthyWithTimeout with an explicit timeout set — matching the API
// contract that an unset timeout waits indefinitely.
func ClusterWaveBound(rel *corev1alpha1.Release) (time.Duration, bool) {
	if ClusterStrategyOf(rel) != corev1alpha1.ReleaseWaveStrategyWaitForHealthyWithTimeout {
		return 0, false
	}
	if t := rel.Spec.SyncPolicy.WaveStrategy.Timeout; t != nil {
		return t.Duration, true
	}
	return 0, false
}

// ClusterPredecessors returns the Releases in siblings that must clear their bar
// before rel may apply: those on the same cluster (siblings are already scoped to
// it) with a strictly lower cluster wave, excluding rel itself. Same-wave Releases
// do not gate each other, so ordering is acyclic.
func ClusterPredecessors(rel *corev1alpha1.Release, siblings []*corev1alpha1.Release) []*corev1alpha1.Release {
	myWave := ClusterWaveOf(rel)
	var preds []*corev1alpha1.Release
	for _, s := range siblings {
		if s.Namespace == rel.Namespace && s.Name == rel.Name {
			continue // self
		}
		if ClusterWaveOf(s) < myWave {
			preds = append(preds, s)
		}
	}
	return preds
}

// NeedsHealthBar reports whether a cluster-wave strategy gates on predecessors
// being Healthy (vs merely Applied).
func NeedsHealthBar(strategy corev1alpha1.ReleaseWaveStrategy) bool {
	return strategy == corev1alpha1.ReleaseWaveStrategyWaitForHealthy ||
		strategy == corev1alpha1.ReleaseWaveStrategyWaitForHealthyWithTimeout
}
