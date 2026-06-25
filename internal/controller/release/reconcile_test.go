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
	"testing"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

func release(generation int64, status corev1alpha1.ReleaseStatus) *corev1alpha1.Release {
	return &corev1alpha1.Release{
		ObjectMeta: metav1.ObjectMeta{Name: "r1", Namespace: "ns", Generation: generation},
		Status:     status,
	}
}

func TestComputeStatus_ReadyAndNotReady(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC))

	ready := computeStatus(release(4, corev1alpha1.ReleaseStatus{}), now, true, conditionReasonApplied, "ok")
	if ready.ObservedGeneration != 4 {
		t.Errorf("ObservedGeneration = %d, want 4", ready.ObservedGeneration)
	}
	if c := apimeta.FindStatusCondition(ready.Conditions, conditionReady); c == nil || c.Status != metav1.ConditionTrue || c.Reason != conditionReasonApplied {
		t.Errorf("Ready condition = %+v, want True/Applied", c)
	}

	notReady := computeStatus(release(4, corev1alpha1.ReleaseStatus{}), now, false, conditionReasonSourceNotReady, "wait")
	if c := apimeta.FindStatusCondition(notReady.Conditions, conditionReady); c == nil || c.Status != metav1.ConditionFalse || c.Reason != conditionReasonSourceNotReady {
		t.Errorf("Ready condition = %+v, want False/SourceNotReady", c)
	}
}

func TestStatusChanged(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC))
	rel := release(1, corev1alpha1.ReleaseStatus{})

	first := computeStatus(rel, now, true, conditionReasonApplied, "applied 1 object(s)")
	if !statusChanged(rel, first) {
		t.Fatalf("statusChanged = false on first reconcile, want true")
	}

	settled := release(1, first)
	again := computeStatus(settled, now, true, conditionReasonApplied, "applied 1 object(s)")
	if statusChanged(settled, again) {
		t.Errorf("statusChanged = true for identical outcome, want false")
	}
}
