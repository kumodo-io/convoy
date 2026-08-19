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
	"errors"
	"testing"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	fakeclient "github.com/kumodo-io/convoy/api/generated/clientset/versioned/fake"
	"github.com/kumodo-io/convoy/internal/artifact"
	"github.com/kumodo-io/convoy/internal/blobstore"
	"github.com/kumodo-io/convoy/internal/fetch"
	"github.com/kumodo-io/convoy/internal/render"
	"github.com/kumodo-io/convoy/internal/renderstore"
)

const (
	lastHealthyRef = "sha256:lasthealthy"
	lhCMName       = "lh-cm"
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

// rolloutTestController builds a controller whose render store holds a seeded
// last-healthy render (a ConfigMap named lhName), with the Release's
// status.lastHealthy pointing at it and a Progressing rollout for the current
// render started `age` ago. It returns the controller, client, deployer, and the
// current render ref (discovered by a probe reconcile).
func rolloutWithLastHealthy(t *testing.T, rollback corev1alpha1.RollbackPolicy, deadline, age time.Duration, lhName string) (*Controller, *fakeclient.Clientset, *fakeDeployer, string) {
	t.Helper()
	// Probe the render ref this spec produces.
	probe := manifestsRelease()
	pc, pclient := newTestController(t, probe, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	if err := pc.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("probe: %v", err)
	}
	renderRef := statusUpdate(pclient.Actions()).Status.LastAttempted.RenderRef

	rel := manifestsRelease()
	withFailurePolicy(rel, rollback, deadline)
	seedConditions(rel, metav1.ConditionFalse, metav1.ConditionFalse) // never healthy
	rel.Status.LastHealthy = &corev1alpha1.LastHealthy{RenderRef: lastHealthyRef}
	started := metav1.NewTime(time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC).Add(-age))
	rel.Status.Rollout = &corev1alpha1.RolloutStatus{
		State: corev1alpha1.RolloutStateProgressing, RenderRef: renderRef, StartedAt: started,
	}
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)

	// Seed the last-healthy render in the store.
	lh := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": configMapKind,
		"metadata": map[string]any{"name": lhName, "namespace": "ns"},
	}}
	manifest, err := render.MarshalManifest([]*unstructured.Unstructured{lh})
	if err != nil {
		t.Fatalf("MarshalManifest: %v", err)
	}
	c.renderStore = renderstore.New(blobstore.NewMemory())
	if err := c.renderStore.PutRender(context.Background(), "ns", "r1", lastHealthyRef, manifest); err != nil {
		t.Fatalf("PutRender: %v", err)
	}
	return c, client, deployer, renderRef
}

// TestRollout_ReleaseFailureRollsBack: past the deadline & unhealthy, OnReleaseFailure
// applies the last-healthy render and transitions to RolledBack.
func TestRollout_ReleaseFailureRollsBack(t *testing.T) {
	c, client, deployer, failedRef := rolloutWithLastHealthy(t, corev1alpha1.RollbackPolicyOnReleaseFailure, time.Minute, 2*time.Minute, lhCMName)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	got := statusUpdate(client.Actions())
	if got.Status.Rollout.State != corev1alpha1.RolloutStateRolledBack {
		t.Fatalf("state = %s, want RolledBack", got.Status.Rollout.State)
	}
	if got.Status.Rollout.RenderRef != failedRef {
		t.Errorf("rollout.renderRef = %s, want the failed render %s", got.Status.Rollout.RenderRef, failedRef)
	}
	// The last-healthy render (lh-cm) was applied, not the failed source render.
	if !deployer.called || deployer.got[0].GetName() != lhCMName {
		t.Errorf("rollback did not apply the last-healthy render; deployed %v", deployer.got)
	}
}

// TestRollout_OnReleaseFailureIgnoresWaveFailure: a wave failure under
// OnReleaseFailure does NOT roll back — it stays Failed.
func TestRollout_OnReleaseFailureIgnoresWaveFailure(t *testing.T) {
	// A wave failure needs a multi-wave, health-gated, bounded release. Build one
	// whose wave 1 waits on wave 0's health with a timeout, and wave 0 never healthy.
	early := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: w0\n  annotations:\n    convoy.kumodo.io/sync-wave: \"0\"\n    convoy.kumodo.io/sync-strategy: \"waitForHealthyWithTimeout\"\n    convoy.kumodo.io/sync-timeout: \"1s\"\n"
	late := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: w1\n  annotations:\n    convoy.kumodo.io/sync-wave: \"1\"\n    convoy.kumodo.io/sync-strategy: \"waitForHealthyWithTimeout\"\n    convoy.kumodo.io/sync-timeout: \"1s\"\n"
	coord := &fakeCoordinator{result: fetch.Result{
		Ready: true, Revision: waveRevision,
		Artifact: artifact.Artifact{Files: map[string][]byte{w0File: []byte(early), w1File: []byte(late)}},
	}}
	rel := manifestsRelease()
	rel.Spec.Content = manifestsContent(w0File, w1File)
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure, time.Minute)
	rel.Spec.HealthAssessment = &corev1alpha1.ReleaseHealthAssessment{} // health-gated
	rel.Status.LastHealthy = &corev1alpha1.LastHealthy{RenderRef: lastHealthyRef}
	// Wave 0 applied 2s ago (past its 1s timeout), never healthy → wave 1 fails.
	started := metav1.NewTime(time.Date(2026, 6, 25, 11, 59, 58, 0, time.UTC))
	rel.Status.Waves = []corev1alpha1.ResourceWaveStatus{{Number: 0, StartedAt: &started}}
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), coord, deployer)
	c.renderStore = renderstore.New(blobstore.NewMemory())

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	got := statusUpdate(client.Actions())
	if got.Status.Rollout == nil || got.Status.Rollout.State != corev1alpha1.RolloutStateFailed {
		t.Errorf("state = %v, want Failed (OnReleaseFailure must not roll back a wave failure)", got.Status.Rollout)
	}
}

// TestRollout_OnWaveFailureRollsBackOnWaveFailure: a wave failure under
// OnWaveFailure rolls back to last-healthy.
func TestRollout_OnWaveFailureRollsBackOnWaveFailure(t *testing.T) {
	early := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: w0\n  annotations:\n    convoy.kumodo.io/sync-wave: \"0\"\n    convoy.kumodo.io/sync-strategy: \"waitForHealthyWithTimeout\"\n    convoy.kumodo.io/sync-timeout: \"1s\"\n"
	late := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: w1\n  annotations:\n    convoy.kumodo.io/sync-wave: \"1\"\n    convoy.kumodo.io/sync-strategy: \"waitForHealthyWithTimeout\"\n    convoy.kumodo.io/sync-timeout: \"1s\"\n"
	coord := &fakeCoordinator{result: fetch.Result{
		Ready: true, Revision: waveRevision,
		Artifact: artifact.Artifact{Files: map[string][]byte{w0File: []byte(early), w1File: []byte(late)}},
	}}
	rel := manifestsRelease()
	rel.Spec.Content = manifestsContent(w0File, w1File)
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnWaveFailure, time.Minute)
	rel.Spec.HealthAssessment = &corev1alpha1.ReleaseHealthAssessment{}
	rel.Status.LastHealthy = &corev1alpha1.LastHealthy{RenderRef: lastHealthyRef}
	started := metav1.NewTime(time.Date(2026, 6, 25, 11, 59, 58, 0, time.UTC))
	rel.Status.Waves = []corev1alpha1.ResourceWaveStatus{{Number: 0, StartedAt: &started}}
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), coord, deployer)

	// Seed the last-healthy render.
	lh := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": configMapKind,
		"metadata": map[string]any{"name": lhCMName, "namespace": "ns"},
	}}
	manifest, _ := render.MarshalManifest([]*unstructured.Unstructured{lh})
	c.renderStore = renderstore.New(blobstore.NewMemory())
	_ = c.renderStore.PutRender(context.Background(), "ns", "r1", lastHealthyRef, manifest)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	got := statusUpdate(client.Actions())
	if got.Status.Rollout == nil || got.Status.Rollout.State != corev1alpha1.RolloutStateRolledBack {
		t.Fatalf("state = %v, want RolledBack (OnWaveFailure rolls back a wave failure)", got.Status.Rollout)
	}
	if got := deployer.got; len(got) == 0 || got[len(got)-1].GetName() != lhCMName {
		t.Errorf("last apply was not the last-healthy render: %v", deployer.got)
	}
}

// TestRollout_NeverStaysFailed: rollback=Never never rolls back, even with a target.
func TestRollout_NeverStaysFailed(t *testing.T) {
	c, client, deployer, _ := rolloutWithLastHealthy(t, corev1alpha1.RollbackPolicyNever, time.Minute, 2*time.Minute, lhCMName)
	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	got := statusUpdate(client.Actions())
	if got.Status.Rollout.State != corev1alpha1.RolloutStateFailed {
		t.Errorf("state = %s, want Failed (rollback=Never)", got.Status.Rollout.State)
	}
	if deployer.called {
		t.Error("rollback applied despite rollback=Never")
	}
}

// TestRollout_ForceClearsHeldRenderAndBypassesDryRun: /sync --force re-drives a held
// Failed render — applying it despite a failing dry-run and resetting the rollout to
// Progressing.
func TestRollout_ForceClearsHeldRenderAndBypassesDryRun(t *testing.T) {
	// Discover the render this spec produces.
	probe := manifestsRelease()
	pc, pclient := newTestController(t, probe, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	if err := pc.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("probe: %v", err)
	}
	renderRef := statusUpdate(pclient.Actions()).Status.LastAttempted.RenderRef

	rel := manifestsRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure, time.Minute)
	// Held Failed for the current render.
	rel.Status.Rollout = &corev1alpha1.RolloutStatus{
		State: corev1alpha1.RolloutStateFailed, RenderRef: renderRef,
		StartedAt: metav1.NewTime(time.Date(2026, 6, 25, 11, 0, 0, 0, time.UTC)),
	}
	// A newer forced-sync request than any handled one.
	rel.Annotations = map[string]string{
		corev1alpha1.SyncForceRequestedAtAnnotation: "2026-06-25T12:00:00Z",
	}
	// dry-run would fail — force must bypass it.
	deployer := &fakeDeployer{dryRunErr: errors.New("field is immutable")}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if !deployer.called {
		t.Error("force did not re-apply the held render")
	}
	if deployer.dryRunCalled {
		t.Error("force did not bypass the dry-run")
	}
	got := statusUpdate(client.Actions())
	if got.Status.Rollout == nil || got.Status.Rollout.State != corev1alpha1.RolloutStateProgressing {
		t.Errorf("rollout state = %v, want Progressing (force reset)", got.Status.Rollout)
	}
	if got.Status.LastHandledForceSyncAt == nil {
		t.Error("lastHandledForceSyncAt not recorded (force must be one-shot)")
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
