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

package release

import (
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

const (
	conditionReady = "Ready"

	conditionReasonApplied         = "Applied"
	conditionReasonSourceNotReady  = "SourceNotReady"
	conditionReasonValuesNotReady  = "ValuesNotReady"
	conditionReasonClusterNotReady = "ClusterNotReady"
	conditionReasonUnsupported     = "Unsupported"
	conditionReasonDeployFailed    = "DeployFailed"
	conditionReasonPruneFailed     = "PruneFailed"
	conditionReasonPinned          = "Pinned"
	conditionReasonWaitingForSync  = "WaitingForSync"
)

// computeStatus derives the desired ReleaseStatus from the current object and a
// reconcile outcome. It is pure; ReconciledAt is stamped by the controller only
// when it actually writes, so a steady Release produces no status churn.
func computeStatus(release *corev1alpha1.Release, now metav1.Time, ready bool, reason, message string) corev1alpha1.ReleaseStatus {
	status := *release.Status.DeepCopy()
	status.ObservedGeneration = release.Generation

	cond := metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: release.Generation,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}
	if ready {
		cond.Status = metav1.ConditionTrue
	}

	apimeta.SetStatusCondition(&status.Conditions, cond)
	return status
}

// statusChanged reports whether newStatus differs from the release's current
// status in a way that warrants a write.
func statusChanged(release *corev1alpha1.Release, newStatus corev1alpha1.ReleaseStatus) bool {
	return !apiequality.Semantic.DeepEqual(release.Status, newStatus)
}
