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
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

// predecessor builds a Release on the same cluster (ns/dummyCluster) at the given
// wave with the given Applied/Healthy condition statuses.
func predecessor(name string, wave int, applied, healthy metav1.ConditionStatus, appliedAt metav1.Time) *corev1alpha1.Release {
	conds := []metav1.Condition{{Type: conditionApplied, Status: applied, LastTransitionTime: appliedAt, Reason: "x"}}
	if healthy != "" {
		conds = append(conds, metav1.Condition{Type: conditionHealthy, Status: healthy, Reason: "x"})
	}
	return &corev1alpha1.Release{
		Name: name, Namespace: "ns",
		Spec: corev1alpha1.ReleaseSpec{
			ClusterRef: corev1alpha1.ReleaseClusterRefSpec{Name: dummyCluster},
			SyncPolicy: corev1alpha1.ReleaseSyncPolicySpec{
				WaveStrategy: corev1alpha1.ReleaseWaveStrategySpec{Wave: wave},
			},
		},
		Status: corev1alpha1.ReleaseStatus{Conditions: conds},
	}
}

// clusterWaveRelease returns the release-under-test at the given wave/strategy.
func clusterWaveRelease(wave int, strategy corev1alpha1.ReleaseWaveStrategy, timeout *metav1.Duration) *corev1alpha1.Release {
	rel := manifestsRelease()
	rel.Spec.SyncPolicy.WaveStrategy = corev1alpha1.ReleaseWaveStrategySpec{Wave: wave, Strategy: strategy, Timeout: timeout}
	return rel
}

func TestClusterWave_HoldsForUnhealthyPredecessor(t *testing.T) {
	rel := clusterWaveRelease(1, corev1alpha1.ReleaseWaveStrategyWaitForHealthy, nil)
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	// Predecessor at wave 0: applied, but not yet healthy → gate holds.
	c.releasesForCluster = func(string, string) ([]*corev1alpha1.Release, error) {
		return []*corev1alpha1.Release{predecessor("db", 0, metav1.ConditionTrue, metav1.ConditionFalse, metav1.Now())}, nil
	}

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if deployer.called {
		t.Error("deployed while holding for cluster wave, want no apply")
	}
	if s, r := readyReason(statusUpdate(client.Actions())); s != metav1.ConditionFalse || r != conditionReasonWaitingForClusterWave {
		t.Errorf("condition = %v/%s, want False/WaitingForClusterWave", s, r)
	}
}

func TestClusterWave_AdvancesWhenPredecessorHealthy(t *testing.T) {
	rel := clusterWaveRelease(1, corev1alpha1.ReleaseWaveStrategyWaitForHealthy, nil)
	deployer := &fakeDeployer{}
	c, _ := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	c.releasesForCluster = func(string, string) ([]*corev1alpha1.Release, error) {
		return []*corev1alpha1.Release{predecessor("db", 0, metav1.ConditionTrue, metav1.ConditionTrue, metav1.Now())}, nil
	}

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if !deployer.called {
		t.Error("did not apply though the predecessor is healthy, want apply")
	}
}

func TestClusterWave_WaitForAppliedIgnoresHealth(t *testing.T) {
	rel := clusterWaveRelease(1, corev1alpha1.ReleaseWaveStrategyWaitForApplied, nil)
	deployer := &fakeDeployer{}
	c, _ := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	// Predecessor applied but NOT healthy — waitForApplied only needs Applied.
	c.releasesForCluster = func(string, string) ([]*corev1alpha1.Release, error) {
		return []*corev1alpha1.Release{predecessor("db", 0, metav1.ConditionTrue, metav1.ConditionFalse, metav1.Now())}, nil
	}

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if !deployer.called {
		t.Error("waitForApplied did not advance on an applied (unhealthy) predecessor, want apply")
	}
}

func TestClusterWave_BoundedTimeoutFails(t *testing.T) {
	rel := clusterWaveRelease(1, corev1alpha1.ReleaseWaveStrategyWaitForHealthyWithTimeout, &metav1.Duration{Duration: time.Millisecond})
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	// Predecessor applied an hour ago, still not healthy → bounded wait (1ms) expired.
	appliedAt := metav1.NewTime(time.Date(2026, 6, 25, 11, 0, 0, 0, time.UTC))
	c.releasesForCluster = func(string, string) ([]*corev1alpha1.Release, error) {
		return []*corev1alpha1.Release{predecessor("db", 0, metav1.ConditionTrue, metav1.ConditionFalse, appliedAt)}, nil
	}

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if deployer.called {
		t.Error("deployed despite the bounded cluster-wave wait failing, want no apply")
	}
	if s, r := readyReason(statusUpdate(client.Actions())); s != metav1.ConditionFalse || r != conditionReasonClusterWaveFailed {
		t.Errorf("condition = %v/%s, want False/ClusterWaveFailed", s, r)
	}
}

func TestClusterWave_NoPredecessorsProceeds(t *testing.T) {
	rel := clusterWaveRelease(5, corev1alpha1.ReleaseWaveStrategyWaitForHealthy, nil)
	deployer := &fakeDeployer{}
	c, _ := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	// Only same-and-higher-wave siblings exist → nothing gates this release.
	c.releasesForCluster = func(string, string) ([]*corev1alpha1.Release, error) {
		return []*corev1alpha1.Release{
			predecessor("peer", 5, metav1.ConditionFalse, "", metav1.Now()),     // same wave
			predecessor("frontend", 9, metav1.ConditionFalse, "", metav1.Now()), // higher wave
		}, nil
	}

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if !deployer.called {
		t.Error("held despite no lower-wave predecessors, want apply")
	}
}

// TestClusterWave_HoldsRollback: a rollback applies like any other render, so it waits
// for the predecessors on the cluster too (KB-4).
func TestClusterWave_HoldsRollback(t *testing.T) {
	rel := clusterWaveRelease(1, corev1alpha1.ReleaseWaveStrategyWaitForHealthy, nil)
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure)
	rel.Status.LastHealthy = &corev1alpha1.LastHealthy{RenderRef: lastHealthyRef}
	rel.Status.Rollout = &corev1alpha1.RolloutStatus{
		State: corev1alpha1.RolloutStateRolledBack, RenderRef: renderRefOf(t, rel), StartedAt: pastStart, Message: "rb",
	}
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	seedLastHealthy(t, c, lhCMName)
	setVerdict(c, renderVerdictPending)
	c.releasesForCluster = func(string, string) ([]*corev1alpha1.Release, error) {
		return []*corev1alpha1.Release{predecessor("db", 0, metav1.ConditionTrue, metav1.ConditionFalse, metav1.Now())}, nil
	}

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if deployer.called {
		t.Errorf("rolled back while holding for cluster wave; deployed %v", deployer.got)
	}
	got := statusUpdate(client.Actions())
	if got.Status.Rollout == nil || got.Status.Rollout.State != corev1alpha1.RolloutStateRolledBack {
		t.Errorf("rollout = %+v, want RolledBack", got.Status.Rollout)
	}
	if s, r := readyReason(got); s != metav1.ConditionFalse || r != conditionReasonWaitingForClusterWave {
		t.Errorf("Applied = %s/%s, want False/WaitingForClusterWave", s, r)
	}
}

func TestClusterWaveSignalChanged(t *testing.T) {
	base := predecessor("db", 0, metav1.ConditionFalse, metav1.ConditionFalse, metav1.Now())

	appliedFlipped := base.DeepCopy()
	appliedFlipped.Status.Conditions[0].Status = metav1.ConditionTrue
	if !ClusterWaveSignalChanged(base, appliedFlipped) {
		t.Error("Applied flip not detected")
	}

	healthyFlipped := base.DeepCopy()
	healthyFlipped.Status.Conditions[1].Status = metav1.ConditionTrue
	if !ClusterWaveSignalChanged(base, healthyFlipped) {
		t.Error("Healthy flip not detected")
	}

	unrelated := base.DeepCopy()
	unrelated.Status.ObservedGeneration = 7
	if ClusterWaveSignalChanged(base, unrelated) {
		t.Error("non-condition change reported as a signal change")
	}
}
