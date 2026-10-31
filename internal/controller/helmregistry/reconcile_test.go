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
	"testing"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
)

func helmReg(generation int64, status sourcev1alpha1.HelmRegistryStatus) *sourcev1alpha1.HelmRegistry {
	return &sourcev1alpha1.HelmRegistry{
		Name: "h1", Namespace: "ns", Generation: generation,
		Status: status,
	}
}

func TestComputeStatus_Verified(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC))
	got := computeStatus(helmReg(5, sourcev1alpha1.HelmRegistryStatus{}), nil, now)

	if got.ObservedGeneration != 5 {
		t.Errorf("ObservedGeneration = %d, want 5", got.ObservedGeneration)
	}
	cond := apimeta.FindStatusCondition(got.Conditions, conditionReady)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != conditionReasonVerified {
		t.Errorf("Ready condition = %+v, want True/Verified", cond)
	}
	if got.LastSuccessfulProbeAt == nil || !got.LastSuccessfulProbeAt.Equal(&now) {
		t.Errorf("LastSuccessfulProbeAt = %v, want %v", got.LastSuccessfulProbeAt, now)
	}
}

func TestComputeStatus_VerifyFailed(t *testing.T) {
	earlier := metav1.NewTime(time.Date(2026, 6, 28, 11, 0, 0, 0, time.UTC))
	now := metav1.NewTime(time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC))
	got := computeStatus(helmReg(3, sourcev1alpha1.HelmRegistryStatus{LastSuccessfulProbeAt: &earlier}), errors.Join(ErrFetchFailed, errors.New("connection refused")), now)

	cond := apimeta.FindStatusCondition(got.Conditions, conditionReady)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != conditionReasonVerifyFailed {
		t.Errorf("Ready condition = %+v, want False/VerifyFailed", cond)
	}
	if got.LastSuccessfulProbeAt == nil || !got.LastSuccessfulProbeAt.Equal(&earlier) {
		t.Errorf("LastSuccessfulProbeAt = %v, want retained %v (not bumped on failure)", got.LastSuccessfulProbeAt, earlier)
	}
}

func TestComputeStatus_Unsupported(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC))
	got := computeStatus(helmReg(1, sourcev1alpha1.HelmRegistryStatus{}), errors.Join(ErrUnsupported, errors.New("basic auth")), now)

	cond := apimeta.FindStatusCondition(got.Conditions, conditionReady)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != conditionReasonUnsupported {
		t.Errorf("Ready condition = %+v, want False/Unsupported", cond)
	}
}

func TestStatusChanged(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC))
	reg := helmReg(2, sourcev1alpha1.HelmRegistryStatus{})

	first := computeStatus(reg, nil, now)
	if !statusChanged(reg, first) {
		t.Fatalf("statusChanged = false on first reconcile, want true")
	}

	settled := helmReg(2, first)
	again := computeStatus(settled, nil, now)
	if statusChanged(settled, again) {
		t.Errorf("statusChanged = true for identical verify, want false")
	}
}
