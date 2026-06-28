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

package ociregistry

import (
	"errors"
	"testing"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
)

func regWithStatus(generation int64, status sourcev1alpha1.OCIRegistryStatus) *sourcev1alpha1.OCIRegistry {
	return &sourcev1alpha1.OCIRegistry{
		ObjectMeta: metav1.ObjectMeta{Name: "o1", Namespace: "ns", Generation: generation},
		Status:     status,
	}
}

func TestComputeStatus_Verified(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC))
	got := computeStatus(regWithStatus(5, sourcev1alpha1.OCIRegistryStatus{}), nil, now)

	if got.ObservedGeneration != 5 {
		t.Errorf("ObservedGeneration = %d, want 5", got.ObservedGeneration)
	}
	cond := apimeta.FindStatusCondition(got.Conditions, conditionReady)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != conditionReasonVerified {
		t.Errorf("Ready condition = %+v, want True/Verified", cond)
	}
}

func TestComputeStatus_VerifyFailed(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC))
	got := computeStatus(regWithStatus(3, sourcev1alpha1.OCIRegistryStatus{}), errors.Join(ErrFetchFailed, errors.New("connection refused")), now)

	cond := apimeta.FindStatusCondition(got.Conditions, conditionReady)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != conditionReasonVerifyFailed {
		t.Errorf("Ready condition = %+v, want False/VerifyFailed", cond)
	}
}

func TestComputeStatus_Unsupported(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC))
	got := computeStatus(regWithStatus(1, sourcev1alpha1.OCIRegistryStatus{}), errors.Join(ErrUnsupported, errors.New("basic auth")), now)

	cond := apimeta.FindStatusCondition(got.Conditions, conditionReady)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != conditionReasonUnsupported {
		t.Errorf("Ready condition = %+v, want False/Unsupported", cond)
	}
}

func TestStatusChanged(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC))
	reg := regWithStatus(2, sourcev1alpha1.OCIRegistryStatus{})

	first := computeStatus(reg, nil, now)
	if !statusChanged(reg, first) {
		t.Fatalf("statusChanged = false on first reconcile, want true")
	}

	settled := regWithStatus(2, first)
	again := computeStatus(settled, nil, now)
	if statusChanged(settled, again) {
		t.Errorf("statusChanged = true for identical verify, want false")
	}
}
