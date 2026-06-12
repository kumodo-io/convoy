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
	"testing"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

func testCluster(generation int64, status corev1alpha1.ClusterStatus) *corev1alpha1.Cluster {
	return &corev1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: "ns", Generation: generation},
		Status:     status,
	}
}

func TestComputeStatus_Reachable(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC))
	cluster := testCluster(7, corev1alpha1.ClusterStatus{})
	result := ProbeResult{Version: sampleKubeVersion, APIVersions: []string{"v1", "apps/v1"}}

	got := computeStatus(cluster, result, nil, now)

	if !got.Reachable {
		t.Errorf("Reachable = false, want true")
	}
	if got.ObservedGeneration != 7 {
		t.Errorf("ObservedGeneration = %d, want 7", got.ObservedGeneration)
	}
	if got.DiscoveredVersion != sampleKubeVersion {
		t.Errorf("DiscoveredVersion = %q, want v1.32.0", got.DiscoveredVersion)
	}
	if !got.LastSeen.Equal(&now) {
		t.Errorf("LastSeen = %v, want %v", got.LastSeen, now)
	}
	cond := apimeta.FindStatusCondition(got.Conditions, conditionReady)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != "Reachable" {
		t.Errorf("Ready condition = %+v, want True/Reachable", cond)
	}
}

func TestComputeStatus_ProbeFailedRetainsLastKnown(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC))
	lastSeen := metav1.NewTime(time.Date(2026, 6, 11, 11, 0, 0, 0, time.UTC))
	cluster := testCluster(3, corev1alpha1.ClusterStatus{
		Reachable:         true,
		LastSeen:          lastSeen,
		DiscoveredVersion: "v1.31.0",
	})

	got := computeStatus(cluster, ProbeResult{}, errors.Join(ErrProbeFailed, errors.New("connection refused")), now)

	if got.Reachable {
		t.Errorf("Reachable = true, want false")
	}
	// Last-known facts must be retained on failure.
	if got.DiscoveredVersion != "v1.31.0" {
		t.Errorf("DiscoveredVersion = %q, want retained v1.31.0", got.DiscoveredVersion)
	}
	if !got.LastSeen.Equal(&lastSeen) {
		t.Errorf("LastSeen = %v, want retained %v", got.LastSeen, lastSeen)
	}
	cond := apimeta.FindStatusCondition(got.Conditions, conditionReady)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "ProbeFailed" {
		t.Errorf("Ready condition = %+v, want False/ProbeFailed", cond)
	}
}

func TestComputeStatus_UnsupportedReason(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC))
	cluster := testCluster(1, corev1alpha1.ClusterStatus{})

	got := computeStatus(cluster, ProbeResult{}, errors.Join(ErrUnsupported, errors.New("nope")), now)

	cond := apimeta.FindStatusCondition(got.Conditions, conditionReady)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != conditionReasonUnsupported {
		t.Errorf("Ready condition = %+v, want False/Unsupported", cond)
	}
}

func TestStatusChanged(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC))
	cluster := testCluster(2, corev1alpha1.ClusterStatus{})

	// First computation must be a change relative to the empty status.
	first := computeStatus(cluster, ProbeResult{Version: sampleKubeVersion}, nil, now)
	if !statusChanged(cluster, first) {
		t.Fatalf("statusChanged = false on first reconcile, want true")
	}

	// Recomputing against an object already carrying that status is a no-op.
	settled := testCluster(2, first)
	again := computeStatus(settled, ProbeResult{Version: sampleKubeVersion}, nil, now)
	if statusChanged(settled, again) {
		t.Errorf("statusChanged = true for identical status, want false")
	}
}
