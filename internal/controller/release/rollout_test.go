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

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

func withFailurePolicy(rel *corev1alpha1.Release, rollback corev1alpha1.RollbackPolicy, deadline time.Duration) {
	rel.Spec.FailurePolicy = &corev1alpha1.ReleaseFailurePolicy{
		Rollback: rollback,
		Deadline: &metav1.Duration{Duration: deadline},
	}
}

// seedConditions sets observer-owned InSync/Healthy on a Release.
func seedConditions(rel *corev1alpha1.Release, inSync, healthy metav1.ConditionStatus) {
	rel.Status.Conditions = []metav1.Condition{
		{Type: "InSync", Status: inSync, Reason: "InSync"},
		{Type: "Healthy", Status: healthy, Reason: "Healthy"},
	}
}

// TestRollout_ProgressingThenSucceeded: a fresh apply that isn't yet observer-
// confirmed healthy is Progressing; once InSync+Healthy, it is Succeeded and
// lastHealthy advances.
func TestRollout_ProgressingThenSucceeded(t *testing.T) {
	rel := manifestsRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure, 5*time.Minute)
	// Not yet confirmed healthy (observer hasn't re-assessed the fresh apply).
	seedConditions(rel, metav1.ConditionFalse, metav1.ConditionTrue)
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), &fakeDeployer{})

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	got := statusUpdate(client.Actions())
	if got == nil || got.Status.Rollout == nil {
		t.Fatalf("no rollout status written")
	}
	if got.Status.Rollout.State != corev1alpha1.RolloutStateProgressing {
		t.Errorf("state = %s, want Progressing", got.Status.Rollout.State)
	}
	if got.Status.LastHealthy != nil {
		t.Errorf("lastHealthy set while only Progressing: %+v", got.Status.LastHealthy)
	}
	// RolloutSucceeded condition mirrors state = Unknown while Progressing.
	if cond := apimeta.FindStatusCondition(got.Status.Conditions, "RolloutSucceeded"); cond == nil || cond.Status != metav1.ConditionUnknown {
		t.Errorf("RolloutSucceeded condition = %+v, want Unknown", cond)
	}

	// Next reconcile: observer has confirmed InSync+Healthy → Succeeded.
	rel2 := got.DeepCopy()
	rel2.Spec = rel.Spec
	seedConditions(rel2, metav1.ConditionTrue, metav1.ConditionTrue)
	c2, client2 := newTestController(t, rel2, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	if err := c2.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler #2: %v", err)
	}
	got2 := statusUpdate(client2.Actions())
	if got2.Status.Rollout.State != corev1alpha1.RolloutStateSucceeded {
		t.Errorf("state = %s, want Succeeded", got2.Status.Rollout.State)
	}
	if got2.Status.LastHealthy == nil || got2.Status.LastHealthy.RenderRef != got2.Status.Rollout.RenderRef {
		t.Errorf("lastHealthy not advanced to the succeeded render: %+v", got2.Status.LastHealthy)
	}
	if cond := apimeta.FindStatusCondition(got2.Status.Conditions, "RolloutSucceeded"); cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("RolloutSucceeded = %+v, want True", cond)
	}
}

// TestRollout_FailsAfterDeadline: still unhealthy past the deadline → Failed.
func TestRollout_FailsAfterDeadline(t *testing.T) {
	rel := manifestsRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure, time.Minute)
	seedConditions(rel, metav1.ConditionFalse, metav1.ConditionFalse) // never healthy
	// Prior rollout started 2 minutes ago (past the 1m deadline).
	started := metav1.NewTime(time.Date(2026, 6, 25, 11, 58, 0, 0, time.UTC))
	rel.Status.Rollout = &corev1alpha1.RolloutStatus{
		State: corev1alpha1.RolloutStateProgressing, StartedAt: started,
	}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	// The prior rollout's renderRef must match this reconcile's render; fill it after
	// we know the render. Simplest: run once to discover the render, then re-run.
	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	got := statusUpdate(client.Actions())
	// The prior rollout had an empty renderRef (≠ current), so this reconcile opens a
	// fresh rollout at now → Progressing, not Failed. Re-run with the matching ref +
	// an old startedAt to exercise the deadline path.
	renderRef := got.Status.Rollout.RenderRef

	rel2 := manifestsRelease()
	withFailurePolicy(rel2, corev1alpha1.RollbackPolicyOnReleaseFailure, time.Minute)
	seedConditions(rel2, metav1.ConditionFalse, metav1.ConditionFalse)
	rel2.Status.Rollout = &corev1alpha1.RolloutStatus{
		State: corev1alpha1.RolloutStateProgressing, RenderRef: renderRef, StartedAt: started,
	}
	c2, client2 := newTestController(t, rel2, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	if err := c2.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler #2: %v", err)
	}
	got2 := statusUpdate(client2.Actions())
	if got2.Status.Rollout.State != corev1alpha1.RolloutStateFailed {
		t.Fatalf("state = %s, want Failed (past deadline, unhealthy)", got2.Status.Rollout.State)
	}
	if cond := apimeta.FindStatusCondition(got2.Status.Conditions, "RolloutSucceeded"); cond == nil || cond.Status != metav1.ConditionFalse {
		t.Errorf("RolloutSucceeded = %+v, want False", cond)
	}
}

// TestRollout_NoPolicyNoRolloutStatus: without a failurePolicy, no rollout status
// is written (behaviour is unchanged from before the feature).
func TestRollout_NoPolicyNoRolloutStatus(t *testing.T) {
	rel := manifestsRelease() // no failurePolicy
	seedConditions(rel, metav1.ConditionTrue, metav1.ConditionTrue)
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	got := statusUpdate(client.Actions())
	if got != nil && got.Status.Rollout != nil {
		t.Errorf("rollout status written without a failurePolicy: %+v", got.Status.Rollout)
	}
	if cond := apimeta.FindStatusCondition(got.Status.Conditions, "RolloutSucceeded"); cond != nil {
		t.Errorf("RolloutSucceeded condition set without a failurePolicy: %+v", cond)
	}
}

// TestRollout_NoHealthConfiguredSucceedsImmediately: with no health rules the
// observer reports Healthy=True; combined with InSync, the gate passes at once.
func TestRollout_NoHealthConfiguredSucceedsImmediately(t *testing.T) {
	rel := manifestsRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure, time.Minute)
	// No spec.healthAssessment → observer writes Healthy=True (NotConfigured). Model
	// the converged steady state: InSync=True, Healthy=True.
	seedConditions(rel, metav1.ConditionTrue, metav1.ConditionTrue)
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	got := statusUpdate(client.Actions())
	if got.Status.Rollout == nil || got.Status.Rollout.State != corev1alpha1.RolloutStateSucceeded {
		t.Errorf("state = %v, want Succeeded (no health rules → inert-by-passing)", got.Status.Rollout)
	}
}

// TestRollout_HeldFailedRenderNotReapplied: a render whose rollout Failed is not
// re-applied on the next reconcile.
func TestRollout_HeldFailedRenderNotReapplied(t *testing.T) {
	// First, discover the render this spec produces.
	probe := manifestsRelease()
	withFailurePolicy(probe, corev1alpha1.RollbackPolicyNever, 0)
	pc, pclient := newTestController(t, probe, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	if err := pc.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("probe syncHandler: %v", err)
	}
	renderRef := statusUpdate(pclient.Actions()).Status.LastAttempted.RenderRef

	rel := manifestsRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure, time.Minute)
	rel.Status.Rollout = &corev1alpha1.RolloutStatus{
		State: corev1alpha1.RolloutStateFailed, RenderRef: renderRef,
		StartedAt: metav1.NewTime(time.Date(2026, 6, 25, 11, 0, 0, 0, time.UTC)),
	}
	deployer := &fakeDeployer{}
	c, _ := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if deployer.called {
		t.Error("held Failed render was re-applied; the controller must hold")
	}
}
