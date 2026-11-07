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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/fetch"
)

const boundedWaveAnn = "    convoy.kumodo.io/sync-strategy: \"waitForHealthyWithTimeout\"\n    convoy.kumodo.io/sync-timeout: \"1m\"\n"

// renderRefFor discovers the render ref rel produces with coord, by running one
// reconcile of a copy without a failurePolicy against a throwaway controller.
func renderRefFor(t *testing.T, rel *corev1alpha1.Release, coord fetch.Coordinator) string {
	t.Helper()
	probe := rel.DeepCopy()
	probe.Spec.FailurePolicy = nil
	pc, pclient := newTestController(t, probe, reachableCluster(), coord, &fakeDeployer{})
	if err := pc.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("probe syncHandler: %v", err)
	}
	return statusUpdate(pclient.Actions()).Status.LastAttempted.RenderRef
}

func waveStartedAt(rel *corev1alpha1.Release, number int64) *metav1.Time {
	for _, w := range rel.Status.Waves {
		if w.Number == number {
			return w.StartedAt
		}
	}
	return nil
}

// TestWaveGate_TimeoutCountsFromThisRender is the KB-7 regression for a new render:
// wave 0's startedAt from an earlier render must not time out the gate at once.
func TestWaveGate_TimeoutCountsFromThisRender(t *testing.T) {
	rel := gatedRelease()
	rel.Status.Waves = []corev1alpha1.ResourceWaveStatus{{Number: 0, Applied: new(true), StartedAt: &pastStart}}
	rel.Status.LastAttempted = &corev1alpha1.ReleaseAttemptedRenderStatus{RenderRef: supersededRef}
	c, client := newTestController(t, rel, reachableCluster(), gatedCoordinator(boundedWaveAnn), &fakeDeployer{})

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	got := statusUpdate(client.Actions())
	if s, r := readyReason(got); s != metav1.ConditionFalse || r != conditionReasonWaitingForWaveHealth {
		t.Errorf("Applied = %s/%s, want False/WaitingForWaveHealth", s, r)
	}
	now := c.now()
	if started := waveStartedAt(got, 0); started == nil || !started.Equal(&now) {
		t.Errorf("wave 0 startedAt = %v, want %s (this render's apply)", started, now)
	}
}

// TestWaveGate_CompletedRenderHolds is the KB-7 regression for a settled Release: once
// the render has fully applied, an unhealthy earlier wave holds the later waves and
// never fails the wave or rolls back.
func TestWaveGate_CompletedRenderHolds(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rollback corev1alpha1.RollbackPolicy
	}{
		{name: "no failure policy"},
		{name: "OnWaveFailure", rollback: corev1alpha1.RollbackPolicyOnWaveFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rel := gatedRelease()
			coord := gatedCoordinator(boundedWaveAnn)
			renderRef := renderRefFor(t, rel, coord)
			rel.Status.Waves = []corev1alpha1.ResourceWaveStatus{{Number: 0, Applied: new(true), StartedAt: &pastStart}}
			rel.Status.LastAttempted = &corev1alpha1.ReleaseAttemptedRenderStatus{RenderRef: renderRef}
			rel.Status.LastSuccessful = &corev1alpha1.ReleaseAttemptedRenderStatus{RenderRef: renderRef}
			if tc.rollback != "" {
				withFailurePolicy(rel, tc.rollback)
				rel.Status.LastHealthy = &corev1alpha1.LastHealthy{RenderRef: lastHealthyRef}
				rel.Status.Rollout = succeededRollout(renderRef, pastStart)
			}
			deployer := &fakeDeployer{}
			c, client := newTestController(t, rel, reachableCluster(), coord, deployer)
			seedLastHealthy(t, c, lhCMName)
			setVerdict(c, renderVerdictPending)

			if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
				t.Fatalf("syncHandler: %v", err)
			}
			got := statusUpdate(client.Actions())
			if s, r := readyReason(got); s != metav1.ConditionFalse || r != conditionReasonWaitingForWaveHealth {
				t.Errorf("Applied = %s/%s, want False/WaitingForWaveHealth", s, r)
			}
			if appliedLastHealthy(deployer) {
				t.Error("rolled back a render that already applied fully")
			}
			if tc.rollback != "" && (got.Status.Rollout == nil || got.Status.Rollout.State != corev1alpha1.RolloutStateSucceeded) {
				t.Errorf("rollout = %+v, want Succeeded", got.Status.Rollout)
			}
		})
	}
}
