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

package helmregistry

import (
	"errors"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
)

const (
	conditionReady              = "Ready"
	conditionReasonVerified     = "Verified"
	conditionReasonUnsupported  = "Unsupported"
	conditionReasonVerifyFailed = "VerifyFailed"
)

// computeStatus derives the desired HelmRegistryStatus from the current object
// plus the outcome of a reachability verification. The controller does not fetch
// content (the coordinate lives on the Release); it only confirms the registry is
// reachable. Pure, so it's unit-testable.
func computeStatus(reg *sourcev1alpha1.HelmRegistry, verifyErr error, now metav1.Time) sourcev1alpha1.HelmRegistryStatus {
	status := *reg.Status.DeepCopy()
	status.ObservedGeneration = reg.Generation

	cond := metav1.Condition{
		Type:               conditionReady,
		ObservedGeneration: reg.Generation,
		LastTransitionTime: now,
	}

	if verifyErr != nil {
		cond.Status = metav1.ConditionFalse
		cond.Message = verifyErr.Error()
		switch {
		case errors.Is(verifyErr, ErrUnsupported):
			cond.Reason = conditionReasonUnsupported
		default:
			cond.Reason = conditionReasonVerifyFailed
		}
	} else {
		cond.Status = metav1.ConditionTrue
		cond.Reason = conditionReasonVerified
		cond.Message = "registry is reachable"
	}

	apimeta.SetStatusCondition(&status.Conditions, cond)
	return status
}

// statusChanged reports whether newStatus differs from the registry's current
// status in a way that warrants a status write.
func statusChanged(reg *sourcev1alpha1.HelmRegistry, newStatus sourcev1alpha1.HelmRegistryStatus) bool {
	return !apiequality.Semantic.DeepEqual(reg.Status, newStatus)
}
