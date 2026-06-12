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

package cluster

import (
	"errors"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

// conditionReady is the condition type summarizing whether the controller can
// reach the target cluster.
// todo: move to API package
const (
	conditionReady             = "Ready"
	conditionReasonReachable   = "Reachable"
	conditionReasonUnsupported = "Unsupported"
	conditionReasonProbeFailed = "ProbeFailed"
)

// computeStatus derives the desired ClusterStatus from the current object plus
// the outcome of a probe. It is pure (no I/O, no clocks beyond the injected
// now) so it can be exhaustively unit-tested. On a probe error the previously
// discovered facts (version, API versions, LastSeen) are intentionally retained
// — they describe the last time the cluster was actually reachable.
func computeStatus(cluster *corev1alpha1.Cluster, result ProbeResult, probeErr error, now metav1.Time) corev1alpha1.ClusterStatus {
	status := *cluster.Status.DeepCopy()
	status.ObservedGeneration = cluster.Generation

	cond := metav1.Condition{
		Type:               conditionReady,
		ObservedGeneration: cluster.Generation,
		LastTransitionTime: now,
	}

	if probeErr != nil {
		status.Reachable = false
		cond.Status = metav1.ConditionFalse
		cond.Message = probeErr.Error()
		switch {
		case errors.Is(probeErr, ErrUnsupported):
			cond.Reason = conditionReasonUnsupported
		case errors.Is(probeErr, ErrProbeFailed):
			cond.Reason = conditionReasonProbeFailed
		default:
			cond.Reason = conditionReasonProbeFailed
		}
	} else {
		status.Reachable = true
		status.LastSeen = now
		status.DiscoveredVersion = result.Version
		status.DiscoveredAPIVersions = result.APIVersions
		cond.Status = metav1.ConditionTrue
		cond.Reason = conditionReasonReachable
		cond.Message = "cluster is reachable"
	}

	apimeta.SetStatusCondition(&status.Conditions, cond)
	return status
}

// statusChanged reports whether newStatus differs from the cluster's current
// status in a way that warrants a status write. LastTransitionTime is managed
// by SetStatusCondition (it only changes when the condition's Status flips), so
// a semantic deep-equal is sufficient to suppress no-op updates.
func statusChanged(cluster *corev1alpha1.Cluster, newStatus corev1alpha1.ClusterStatus) bool {
	return !apiequality.Semantic.DeepEqual(cluster.Status, newStatus)
}
