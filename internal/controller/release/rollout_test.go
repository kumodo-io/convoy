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
	"github.com/kumodo-io/convoy/internal/inventory"
	"github.com/kumodo-io/convoy/internal/render"
	"github.com/kumodo-io/convoy/internal/renderstore"
)

const (
	lastHealthyRef = "sha256:lasthealthy"
	lhCMName       = "lh-cm"
)

// pastStart is an arbitrary time before the controller's fixed now; it anchors a
// prior rollout's StartedAt so preservation across reconciles can be observed.
var pastStart = metav1.NewTime(time.Date(2026, 6, 25, 11, 0, 0, 0, time.UTC))

func withFailurePolicy(rel *corev1alpha1.Release, rollback corev1alpha1.RollbackPolicy) {
	rel.Spec.FailurePolicy = &corev1alpha1.ReleaseFailurePolicy{Rollback: rollback}
}

// setVerdict overrides the controller's verdict seam so the rollout state-machine tests
// can drive Healthy/Failed/Pending directly, without depending on the in-memory inventory
// double (which doesn't model the apiStore's SSA co-ownership) or on the controller-owned
// deadline (the seam ignores startedAt).
func setVerdict(c *Controller, v renderVerdict) {
	c.verdictFn = func(context.Context, *corev1alpha1.Release, string, metav1.Time) renderVerdict { return v }
}

// setHealth is the common bool shorthand: true → Healthy, false → Pending (not-yet).
// Failed (a definitive, deadline-elapsed failure) is set explicitly via setVerdict.
func setHealth(c *Controller, healthy bool) {
	if healthy {
		setVerdict(c, renderVerdictHealthy)
	} else {
		setVerdict(c, renderVerdictPending)
	}
}

// probeRenderRef discovers the render ref manifestsRelease() produces, by running one
// reconcile against a throwaway controller.
func probeRenderRef(t *testing.T) string {
	t.Helper()
	probe := manifestsRelease()
	pc, pclient := newTestController(t, probe, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	if err := pc.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("probe syncHandler: %v", err)
	}
	return statusUpdate(pclient.Actions()).Status.LastAttempted.RenderRef
}

// seedLastHealthy stores a single-ConfigMap render (named lhName) under lastHealthyRef
// in a fresh in-memory render store attached to c.
func seedLastHealthy(t *testing.T, c *Controller, lhName string) {
	t.Helper()
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
}

// TestVerdict_GatesOnAssessedRenderRefAndDeadline covers the verdict function: it must
// require the observer to have assessed THIS render (AssessedRenderRef == renderRef),
// and Failed is gated on the CONTROLLER-owned deadline measured from startedAt.
func TestVerdict_GatesOnAssessedRenderRefAndDeadline(t *testing.T) {
	ctx := context.Background()
	rel := manifestsRelease()
	rel.Spec.HealthAssessment = &corev1alpha1.ReleaseHealthAssessment{Deadline: &metav1.Duration{Duration: time.Minute}}
	c, _ := newTestController(t, rel, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	now := c.now() // fixed 2026-06-25 12:00:00
	withinDeadline := metav1.NewTime(now.Add(-30 * time.Second))
	pastDeadline := metav1.NewTime(now.Add(-2 * time.Minute))

	healthy := corev1alpha1.ResourceStateHealthy
	newRef, oldRef := "sha256:new", "sha256:old"
	seed := func(state *corev1alpha1.ResourceState, assessedRef string) {
		_ = c.inventory.Put(ctx, "ns", "r1", []inventory.Ref{
			{Version: "v1", Kind: configMapKind, Name: "cm", State: state, AssessedRenderRef: assessedRef},
		})
	}

	// Healthy but assessed for the OLD render → no verdict yet for the new one.
	seed(&healthy, oldRef)
	if got := c.verdict(ctx, rel, newRef, pastDeadline); got != renderVerdictPending {
		t.Errorf("stale assessment: verdict = %v, want Pending", got)
	}
	// Healthy AND assessed for the new render → Healthy (deadline-independent).
	seed(&healthy, newRef)
	if got := c.verdict(ctx, rel, newRef, pastDeadline); got != renderVerdictHealthy {
		t.Errorf("verdict = %v, want Healthy", got)
	}
	// Not passing (Degraded) for the new render, WITHIN the deadline → Pending (wait).
	degraded := corev1alpha1.ResourceStateDegraded
	seed(&degraded, newRef)
	if got := c.verdict(ctx, rel, newRef, withinDeadline); got != renderVerdictPending {
		t.Errorf("within deadline: verdict = %v, want Pending", got)
	}
	// Not passing PAST the deadline (measured from startedAt) → Failed.
	if got := c.verdict(ctx, rel, newRef, pastDeadline); got != renderVerdictFailed {
		t.Errorf("past deadline: verdict = %v, want Failed", got)
	}
	// No health assessment → Healthy (inert).
	relNoHealth := manifestsRelease()
	if got := c.verdict(ctx, relNoHealth, newRef, pastDeadline); got != renderVerdictHealthy {
		t.Errorf("no healthAssessment: verdict = %v, want Healthy (inert)", got)
	}
}

// TestRollout_ProgressingThenSucceeded: a fresh apply that isn't yet observer-confirmed
// healthy is Progressing; once the verdict is Healthy, it is Succeeded and lastHealthy
// advances.
func TestRollout_ProgressingThenSucceeded(t *testing.T) {
	rel := manifestsRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure)
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	setHealth(c, false) // not yet confirmed healthy

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
	if cond := apimeta.FindStatusCondition(got.Status.Conditions, "RolloutSucceeded"); cond == nil || cond.Status != metav1.ConditionUnknown {
		t.Errorf("RolloutSucceeded condition = %+v, want Unknown", cond)
	}

	// Next reconcile: verdict is Healthy → Succeeded.
	rel2 := got.DeepCopy()
	rel2.Spec = rel.Spec
	c2, client2 := newTestController(t, rel2, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	setHealth(c2, true)
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

// TestRollout_FailsOnDegradedVerdict: a Progressing render whose observer verdict is
// Degraded fails now (no rollback target → rests at Failed).
func TestRollout_FailsOnDegradedVerdict(t *testing.T) {
	renderRef := probeRenderRef(t)
	rel := manifestsRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure) // no lastHealthy → can't roll back
	rel.Status.Rollout = &corev1alpha1.RolloutStatus{
		State: corev1alpha1.RolloutStateProgressing, RenderRef: renderRef, StartedAt: pastStart,
	}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	setVerdict(c, renderVerdictFailed)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	got := statusUpdate(client.Actions())
	if got.Status.Rollout.State != corev1alpha1.RolloutStateFailed {
		t.Fatalf("state = %s, want Failed (Degraded verdict, no rollback target)", got.Status.Rollout.State)
	}
	if cond := apimeta.FindStatusCondition(got.Status.Conditions, "RolloutSucceeded"); cond == nil || cond.Status != metav1.ConditionFalse {
		t.Errorf("RolloutSucceeded = %+v, want False", cond)
	}
}

// TestRollout_OutageStaysProgressing is the regression guard for the false-rollback
// bug: a Progressing render with NO fresh verdict (Pending — e.g. an apiserver/observer
// outage) must never roll back, even with a rollback target available. It waits.
func TestRollout_OutageStaysProgressing(t *testing.T) {
	renderRef := probeRenderRef(t)
	rel := manifestsRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure)
	rel.Status.LastHealthy = &corev1alpha1.LastHealthy{RenderRef: lastHealthyRef} // target EXISTS
	rel.Status.Rollout = &corev1alpha1.RolloutStatus{
		State: corev1alpha1.RolloutStateProgressing, RenderRef: renderRef, StartedAt: pastStart,
	}
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	seedLastHealthy(t, c, lhCMName)     // rollback would be possible…
	setVerdict(c, renderVerdictPending) // …but there is no verdict: observer down

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	got := statusUpdate(client.Actions())
	if got.Status.Rollout.State != corev1alpha1.RolloutStateProgressing {
		t.Fatalf("state = %s, want Progressing (a Pending verdict must not roll back)", got.Status.Rollout.State)
	}
	for _, o := range deployer.got {
		if o.GetName() == lhCMName {
			t.Error("rolled back on a Pending verdict (outage) — must wait, not fail")
		}
	}
}

// TestRollout_NoPolicyNoRolloutStatus: without a failurePolicy, no rollout status is
// written (behaviour unchanged from before the feature).
func TestRollout_NoPolicyNoRolloutStatus(t *testing.T) {
	rel := manifestsRelease() // no failurePolicy, no healthAssessment
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

// TestRollout_NoHealthConfiguredProgressingThenSucceeds: a fresh rollout always passes
// through Progressing once (uniform, observable), then the inert Healthy verdict (no
// health assessment) settles it to Succeeded on the next reconcile.
func TestRollout_NoHealthConfiguredProgressingThenSucceeds(t *testing.T) {
	rel := manifestsRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure)
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	got := statusUpdate(client.Actions())
	if got.Status.Rollout == nil || got.Status.Rollout.State != corev1alpha1.RolloutStateProgressing {
		t.Fatalf("first reconcile state = %v, want Progressing (fresh rollout starts Progressing)", got.Status.Rollout)
	}

	// Next reconcile: same render, inert Healthy verdict → Succeeded.
	rel2 := got.DeepCopy()
	rel2.Spec = rel.Spec
	c2, client2 := newTestController(t, rel2, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	if err := c2.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler #2: %v", err)
	}
	got2 := statusUpdate(client2.Actions())
	if got2.Status.Rollout.State != corev1alpha1.RolloutStateSucceeded {
		t.Errorf("second reconcile state = %s, want Succeeded (no health → inert Healthy)", got2.Status.Rollout.State)
	}
}

// rolloutWithLastHealthy builds a controller whose render store holds a seeded
// last-healthy render (a ConfigMap named lhName), with the Release's status.lastHealthy
// pointing at it and a Progressing rollout for the current render. The verdict seam is
// pre-set to Failed (a definitive, deadline-elapsed failure). It returns the controller,
// client, deployer, and the current (failed) render ref.
func rolloutWithLastHealthy(t *testing.T, rollback corev1alpha1.RollbackPolicy, lhName string) (*Controller, *fakeclient.Clientset, *fakeDeployer, string) {
	t.Helper()
	renderRef := probeRenderRef(t)

	rel := manifestsRelease()
	withFailurePolicy(rel, rollback)
	rel.Status.LastHealthy = &corev1alpha1.LastHealthy{RenderRef: lastHealthyRef}
	rel.Status.Rollout = &corev1alpha1.RolloutStatus{
		State: corev1alpha1.RolloutStateProgressing, RenderRef: renderRef, StartedAt: pastStart,
	}
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	setVerdict(c, renderVerdictFailed) // definitive failure
	seedLastHealthy(t, c, lhName)
	return c, client, deployer, renderRef
}

// TestRollout_ReleaseFailureRollsBack: a Degraded verdict under OnReleaseFailure applies
// the last-healthy render and transitions to RolledBack.
func TestRollout_ReleaseFailureRollsBack(t *testing.T) {
	c, client, deployer, failedRef := rolloutWithLastHealthy(t, corev1alpha1.RollbackPolicyOnReleaseFailure, lhCMName)

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
	// A successful rollback reads as applied — Applied stays True.
	if cond := apimeta.FindStatusCondition(got.Status.Conditions, "Applied"); cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("Applied = %+v, want True (last-healthy is applied)", cond)
	}
	// lastSuccessful reflects the render actually running (last-healthy).
	if got.Status.LastSuccessful == nil || got.Status.LastSuccessful.RenderRef != lastHealthyRef {
		t.Errorf("lastSuccessful = %+v, want the last-healthy render %s", got.Status.LastSuccessful, lastHealthyRef)
	}
}

// TestRollout_RolledBackKeepsLastHealthyApplied: on a later reconcile of a render whose
// rollout is already RolledBack, the controller re-asserts the last-healthy render
// (Applied stays True) and the rollout stays RolledBack keyed by the failed render.
func TestRollout_RolledBackKeepsLastHealthyApplied(t *testing.T) {
	renderRef := probeRenderRef(t)
	rel := manifestsRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure)
	rel.Status.LastHealthy = &corev1alpha1.LastHealthy{RenderRef: lastHealthyRef}
	rel.Status.Rollout = &corev1alpha1.RolloutStatus{
		State: corev1alpha1.RolloutStateRolledBack, RenderRef: renderRef, StartedAt: pastStart, Message: "prior rollback",
	}
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	seedLastHealthy(t, c, lhCMName)
	setVerdict(c, renderVerdictPending) // last-healthy not yet re-assessed

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if !deployer.called || deployer.got[0].GetName() != lhCMName {
		t.Errorf("RolledBack steady state did not re-apply the last-healthy render; deployed %v", deployer.got)
	}
	got := statusUpdate(client.Actions())
	if got.Status.Rollout.State != corev1alpha1.RolloutStateRolledBack {
		t.Errorf("state = %s, want RolledBack (sticky)", got.Status.Rollout.State)
	}
	if got.Status.Rollout.RenderRef != renderRef {
		t.Errorf("rollout.renderRef = %s, want the failed render %s (key preserved)", got.Status.Rollout.RenderRef, renderRef)
	}
	if cond := apimeta.FindStatusCondition(got.Status.Conditions, "Applied"); cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("Applied = %+v, want True (last-healthy stays applied)", cond)
	}
	if got.Status.LastSuccessful == nil || got.Status.LastSuccessful.RenderRef != lastHealthyRef {
		t.Errorf("lastSuccessful = %+v, want the last-healthy render", got.Status.LastSuccessful)
	}
}

// TestRollout_RenderChangeFlushesRolledBackEvenWhenHolding is the regression for the
// bug where editing the Release (a new render) while RolledBack left the rollout stuck
// at RolledBack until the new render's apply happened to complete (~pod Ready), because
// evaluateRollout carried the prior terminal state forward on any hold. A render change
// must flip the rollout to Progressing immediately — even while the new render's apply
// is still holding — since the prior RolledBack is about a superseded render.
func TestRollout_RenderChangeFlushesRolledBackEvenWhenHolding(t *testing.T) {
	rel := manifestsRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure)
	// Prior rollout is RolledBack for a DIFFERENT (old, superseded) render than the one
	// this spec now renders.
	rel.Status.Rollout = &corev1alpha1.RolloutStatus{
		State: corev1alpha1.RolloutStateRolledBack, RenderRef: "sha256:oldfailed", StartedAt: pastStart,
		Message: "old rollback",
	}
	rel.Status.LastHealthy = &corev1alpha1.LastHealthy{RenderRef: lastHealthyRef}
	// Deployer errors so the new render's apply holds (outcome.applied stays false) —
	// the rollout must still flip to Progressing for the new render.
	deployer := &fakeDeployer{err: errors.New("apply held")}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	seedLastHealthy(t, c, lhCMName)
	setVerdict(c, renderVerdictPending)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Logf("syncHandler returned (expected on a held apply): %v", err)
	}
	got := statusUpdate(client.Actions())
	if got == nil || got.Status.Rollout == nil {
		t.Fatalf("no rollout status written")
	}
	if got.Status.Rollout.State != corev1alpha1.RolloutStateProgressing {
		t.Errorf("state = %s, want Progressing (a render change must flush the stale RolledBack even mid-hold)", got.Status.Rollout.State)
	}
	// The new render is the failed source render (not last-healthy): reassertRollback
	// must NOT re-apply last-healthy, since the render changed.
	for _, o := range deployer.got {
		if o.GetName() == lhCMName {
			t.Error("re-applied last-healthy after a render change; the rollout should have moved to the new render")
		}
	}
}

// TestRollout_RolledBackHoldsWhenLastHealthyUnloadable is the regression for the
// flip-flop bug: while re-asserting a RolledBack rollout, if last-healthy can't be
// loaded (a transient store error — e.g. a context cancellation under load), the
// controller must HOLD, NOT fall through and re-apply the FAILED source render (which
// flipped the workload back to the bad render and churned).
func TestRollout_RolledBackHoldsWhenLastHealthyUnloadable(t *testing.T) {
	renderRef := probeRenderRef(t)
	rel := manifestsRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure)
	rel.Status.LastHealthy = &corev1alpha1.LastHealthy{RenderRef: lastHealthyRef}
	rel.Status.Rollout = &corev1alpha1.RolloutStatus{
		State: corev1alpha1.RolloutStateRolledBack, RenderRef: renderRef, StartedAt: pastStart, Message: "rb",
	}
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	// Store present but the last-healthy render is NOT seeded → GetRender fails.
	c.renderStore = renderstore.New(blobstore.NewMemory())
	setVerdict(c, renderVerdictPending)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if deployer.called {
		t.Error("re-applied a render while last-healthy was unloadable; must hold, not re-apply the failed source render")
	}
	got := statusUpdate(client.Actions())
	if got.Status.Rollout == nil || got.Status.Rollout.State != corev1alpha1.RolloutStateRolledBack {
		t.Errorf("rollout = %v, want RolledBack (held)", got.Status.Rollout)
	}
	if cond := apimeta.FindStatusCondition(got.Status.Conditions, "Applied"); cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("Applied = %+v, want True (holding on last-healthy)", cond)
	}
}

// TestRollout_RenderChangeToHealthyStartsProgressing is the regression for the
// transient-Succeeded bug: editing the spec to a render whose resources are ALREADY
// healthy (e.g. it equals the running rollback target) must start a fresh rollout at
// Progressing and only Succeed once the observer re-confirms THIS rollout — not flash
// Succeeded instantly off the carried-over verdict.
func TestRollout_RenderChangeToHealthyStartsProgressing(t *testing.T) {
	renderRef := probeRenderRef(t)
	rel := manifestsRelease()
	rel.Spec.HealthAssessment = &corev1alpha1.ReleaseHealthAssessment{} // health configured
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure)
	// Prior rollout is RolledBack for a DIFFERENT (old) render; the new render's verdict
	// is already Healthy (carried over from being the running rollback target).
	rel.Status.Rollout = &corev1alpha1.RolloutStatus{
		State: corev1alpha1.RolloutStateRolledBack, RenderRef: "sha256:oldfailed", StartedAt: pastStart,
	}
	rel.Status.LastHealthy = &corev1alpha1.LastHealthy{RenderRef: renderRef}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	setVerdict(c, renderVerdictHealthy) // already Healthy for the new render

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	got := statusUpdate(client.Actions())
	if got.Status.Rollout == nil || got.Status.Rollout.State != corev1alpha1.RolloutStateProgressing {
		t.Fatalf("fresh rollout to an already-healthy render = %v, want Progressing (must be re-confirmed, not instant Succeeded)", got.Status.Rollout)
	}

	// Next reconcile: the render is now the active rollout (sameRenderAsPrior) and the
	// verdict is Healthy → Succeeded. So it does not get stuck Progressing.
	rel2 := got.DeepCopy()
	rel2.Spec = rel.Spec
	c2, client2 := newTestController(t, rel2, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	setVerdict(c2, renderVerdictHealthy)
	if err := c2.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler #2: %v", err)
	}
	got2 := statusUpdate(client2.Actions())
	if got2.Status.Rollout.State != corev1alpha1.RolloutStateSucceeded {
		t.Errorf("second reconcile = %s, want Succeeded (confirmed)", got2.Status.Rollout.State)
	}
}

// TestRollout_TransientSourceFailureKeepsRolledBack is the regression for the
// wake-from-sleep bug: a transient source failure (e.g. git fetch failed) bails out of
// reconcile BEFORE the rollout state machine runs, so the outcome carries no rollout.
// syncHandler must PRESERVE the persisted RolledBack state, not wipe it to nil — a wipe
// makes the next reconcile see no prior rollout, open a fresh Progressing, and re-apply
// the failed source render (spontaneous RolledBack→Progressing→RolledBack).
func TestRollout_TransientSourceFailureKeepsRolledBack(t *testing.T) {
	rel := manifestsRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure)
	rel.Status.LastHealthy = &corev1alpha1.LastHealthy{RenderRef: lastHealthyRef}
	rel.Status.Rollout = &corev1alpha1.RolloutStatus{
		State: corev1alpha1.RolloutStateRolledBack, RenderRef: "sha256:failed", StartedAt: pastStart, Message: "rb",
	}
	// Coordinator errors → stageRender fails before planRollout runs.
	coord := &fakeCoordinator{err: errors.New("git fetch failed")}
	c, client := newTestController(t, rel, reachableCluster(), coord, &fakeDeployer{})

	// A transient source failure requeues (non-nil error); the status write still happens.
	_ = c.syncHandler(context.Background(), "ns/r1")
	got := statusUpdate(client.Actions())
	if got == nil || got.Status.Rollout == nil {
		t.Fatalf("rollout was WIPED by a transient source failure; want RolledBack preserved")
	}
	if got.Status.Rollout.State != corev1alpha1.RolloutStateRolledBack || got.Status.Rollout.RenderRef != "sha256:failed" {
		t.Errorf("rollout = %+v, want RolledBack{sha256:failed} preserved", got.Status.Rollout)
	}
}

// TestRollout_OnReleaseFailureIgnoresWaveFailure: a wave failure under OnReleaseFailure
// does NOT roll back — it stays Failed.
func TestRollout_OnReleaseFailureIgnoresWaveFailure(t *testing.T) {
	early := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: w0\n  annotations:\n    convoy.kumodo.io/sync-wave: \"0\"\n    convoy.kumodo.io/sync-strategy: \"waitForHealthyWithTimeout\"\n    convoy.kumodo.io/sync-timeout: \"1s\"\n"
	late := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: w1\n  annotations:\n    convoy.kumodo.io/sync-wave: \"1\"\n    convoy.kumodo.io/sync-strategy: \"waitForHealthyWithTimeout\"\n    convoy.kumodo.io/sync-timeout: \"1s\"\n"
	coord := &fakeCoordinator{result: fetch.Result{
		Ready: true, Revision: waveRevision,
		Artifact: artifact.Artifact{Files: map[string][]byte{w0File: []byte(early), w1File: []byte(late)}},
	}}
	rel := manifestsRelease()
	rel.Spec.Content = manifestsContent(w0File, w1File)
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure)
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

// TestRollout_OnWaveFailureRollsBackOnWaveFailure: a wave failure under OnWaveFailure
// rolls back to last-healthy.
func TestRollout_OnWaveFailureRollsBackOnWaveFailure(t *testing.T) {
	early := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: w0\n  annotations:\n    convoy.kumodo.io/sync-wave: \"0\"\n    convoy.kumodo.io/sync-strategy: \"waitForHealthyWithTimeout\"\n    convoy.kumodo.io/sync-timeout: \"1s\"\n"
	late := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: w1\n  annotations:\n    convoy.kumodo.io/sync-wave: \"1\"\n    convoy.kumodo.io/sync-strategy: \"waitForHealthyWithTimeout\"\n    convoy.kumodo.io/sync-timeout: \"1s\"\n"
	coord := &fakeCoordinator{result: fetch.Result{
		Ready: true, Revision: waveRevision,
		Artifact: artifact.Artifact{Files: map[string][]byte{w0File: []byte(early), w1File: []byte(late)}},
	}}
	rel := manifestsRelease()
	rel.Spec.Content = manifestsContent(w0File, w1File)
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnWaveFailure)
	rel.Spec.HealthAssessment = &corev1alpha1.ReleaseHealthAssessment{}
	rel.Status.LastHealthy = &corev1alpha1.LastHealthy{RenderRef: lastHealthyRef}
	started := metav1.NewTime(time.Date(2026, 6, 25, 11, 59, 58, 0, time.UTC))
	rel.Status.Waves = []corev1alpha1.ResourceWaveStatus{{Number: 0, StartedAt: &started}}
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), coord, deployer)
	seedLastHealthy(t, c, lhCMName)

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

// TestRollout_NeverStaysFailed: rollback=Never never rolls back, even with a target and
// a Degraded verdict. The failed render stays applied (re-applied), and the rollout is
// Failed — but the last-healthy render is never applied.
func TestRollout_NeverStaysFailed(t *testing.T) {
	c, client, deployer, _ := rolloutWithLastHealthy(t, corev1alpha1.RollbackPolicyNever, lhCMName)
	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	got := statusUpdate(client.Actions())
	if got.Status.Rollout.State != corev1alpha1.RolloutStateFailed {
		t.Errorf("state = %s, want Failed (rollback=Never)", got.Status.Rollout.State)
	}
	for _, o := range deployer.got {
		if o.GetName() == lhCMName {
			t.Error("rolled back to last-healthy despite rollback=Never")
		}
	}
}

// TestRollout_FailedRenderKeepsApplying: a render whose rollout Failed keeps being
// applied on later reconciles (Applied stays True) — it is not held.
func TestRollout_FailedRenderKeepsApplying(t *testing.T) {
	renderRef := probeRenderRef(t)
	rel := manifestsRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure)
	rel.Status.Rollout = &corev1alpha1.RolloutStatus{
		State: corev1alpha1.RolloutStateFailed, RenderRef: renderRef, StartedAt: pastStart,
	}
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	setVerdict(c, renderVerdictPending) // still not healthy

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if !deployer.called {
		t.Error("a Failed render must keep being applied (Applied stays True), not held")
	}
	got := statusUpdate(client.Actions())
	if cond := apimeta.FindStatusCondition(got.Status.Conditions, "Applied"); cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("Applied = %+v, want True (failed render stays applied)", cond)
	}
	if got.Status.Rollout.State != corev1alpha1.RolloutStateFailed {
		t.Errorf("state = %s, want Failed (one-shot: no re-rollback, no re-open)", got.Status.Rollout.State)
	}
}

// TestRollout_FailedRenderSelfHeals: a Failed render whose verdict later flips to
// Healthy advances to Succeeded (the gate reflects the current verdict).
func TestRollout_FailedRenderSelfHeals(t *testing.T) {
	renderRef := probeRenderRef(t)
	rel := manifestsRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure)
	rel.Status.Rollout = &corev1alpha1.RolloutStatus{
		State: corev1alpha1.RolloutStateFailed, RenderRef: renderRef, StartedAt: pastStart,
	}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	setHealth(c, true) // recovered

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	got := statusUpdate(client.Actions())
	if got.Status.Rollout.State != corev1alpha1.RolloutStateSucceeded {
		t.Errorf("state = %s, want Succeeded (Failed render self-healed)", got.Status.Rollout.State)
	}
}

// TestRollout_ForceReDrivesFailedAndBypassesDryRun: /sync --force re-drives a Failed
// render — applying it despite a failing dry-run and resetting the rollout to
// Progressing.
func TestRollout_ForceReDrivesFailedAndBypassesDryRun(t *testing.T) {
	renderRef := probeRenderRef(t)
	rel := manifestsRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure)
	rel.Status.Rollout = &corev1alpha1.RolloutStatus{
		State: corev1alpha1.RolloutStateFailed, RenderRef: renderRef, StartedAt: pastStart,
	}
	rel.Annotations = map[string]string{
		corev1alpha1.SyncForceRequestedAtAnnotation: "2026-06-25T12:00:00Z",
	}
	deployer := &fakeDeployer{dryRunErr: errors.New("field is immutable")} // dry-run would fail
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	setHealth(c, false) // re-driven render not yet healthy → resets to Progressing

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if !deployer.called {
		t.Error("force did not re-apply the render")
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

// TestRollout_ForceFlushesRolledBackEvenWhenHolding is the regression for the bug where
// /sync --force on a RolledBack render left the stale RolledBack in place when the
// re-drive held at a wave (outcome.applied=false). Force must flush it to a fresh
// Progressing regardless.
func TestRollout_ForceFlushesRolledBackEvenWhenHolding(t *testing.T) {
	renderRef := probeRenderRef(t)
	rel := manifestsRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure)
	rel.Status.Rollout = &corev1alpha1.RolloutStatus{
		State: corev1alpha1.RolloutStateRolledBack, RenderRef: renderRef, StartedAt: pastStart,
		Message: "old rollback",
	}
	rel.Annotations = map[string]string{
		corev1alpha1.SyncForceRequestedAtAnnotation: "2026-06-25T12:00:00Z",
	}
	// Deployer errors so the apply holds (outcome.applied stays false).
	deployer := &fakeDeployer{err: errors.New("apply held")}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	setHealth(c, false)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Logf("syncHandler returned (expected on a held/failed apply): %v", err)
	}
	got := statusUpdate(client.Actions())
	if got == nil || got.Status.Rollout == nil {
		t.Fatalf("no rollout status written")
	}
	if got.Status.Rollout.State != corev1alpha1.RolloutStateProgressing {
		t.Errorf("state = %s, want Progressing (force must flush the stale RolledBack)", got.Status.Rollout.State)
	}
	if !got.Status.Rollout.StartedAt.After(pastStart.Time) {
		t.Errorf("startedAt = %v, want a fresh time after the old %v", got.Status.Rollout.StartedAt, pastStart)
	}
}
