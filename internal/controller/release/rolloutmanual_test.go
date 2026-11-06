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

const supersededRef = "sha256:superseded"

var manualSyncAt = metav1.NewTime(time.Date(2026, 6, 25, 11, 59, 0, 0, time.UTC))

// renderRefOf discovers the render ref rel produces, by running one reconcile of a
// copy without a failurePolicy against a throwaway controller.
func renderRefOf(t *testing.T, rel *corev1alpha1.Release) string {
	t.Helper()
	probe := rel.DeepCopy()
	probe.Spec.FailurePolicy = nil
	pc, pclient := newTestController(t, probe, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	if err := pc.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("probe syncHandler: %v", err)
	}
	return statusUpdate(pclient.Actions()).Status.LastAttempted.RenderRef
}

func requestSync(rel *corev1alpha1.Release, at metav1.Time) {
	rel.Annotations = map[string]string{corev1alpha1.SyncRequestedAtAnnotation: at.UTC().Format(time.RFC3339Nano)}
}

// manualRolledBack is a manual-mode Release whose rollout is RolledBack for its current
// render, with lastSuccessful at the given render.
func manualRolledBack(t *testing.T, lastSuccessful func(renderRef string) string) *corev1alpha1.Release {
	t.Helper()
	rel := manualRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure)
	renderRef := renderRefOf(t, rel)
	rel.Status.LastHealthy = &corev1alpha1.LastHealthy{RenderRef: lastHealthyRef}
	rel.Status.LastSuccessful = &corev1alpha1.ReleaseAttemptedRenderStatus{RenderRef: lastSuccessful(renderRef)}
	rel.Status.Rollout = &corev1alpha1.RolloutStatus{
		State: corev1alpha1.RolloutStateRolledBack, RenderRef: renderRef, StartedAt: pastStart, Message: "rb",
	}
	return rel
}

func appliedLastHealthy(d *fakeDeployer) bool {
	for _, o := range d.got {
		if o.GetName() == lhCMName {
			return true
		}
	}
	return false
}

// TestManualRollout_NewRenderWithoutSyncOpensNoRollout: in manual mode a new render
// that waits for /sync is not applied, so it must not open a rollout (KB-4).
func TestManualRollout_NewRenderWithoutSyncOpensNoRollout(t *testing.T) {
	rel := manualRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure)
	rel.Status.Rollout = &corev1alpha1.RolloutStatus{State: corev1alpha1.RolloutStateSucceeded, RenderRef: supersededRef, StartedAt: pastStart}
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	setVerdict(c, renderVerdictPending)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if deployer.called {
		t.Error("applied without a /sync in manual mode")
	}
	got := statusUpdate(client.Actions())
	if r := got.Status.Rollout; r == nil || r.State != corev1alpha1.RolloutStateSucceeded || r.RenderRef != supersededRef {
		t.Errorf("rollout = %+v, want Succeeded{%s} until a /sync applies the new render", r, supersededRef)
	}
	if s, r := readyReason(got); s != metav1.ConditionFalse || r != conditionReasonWaitingForSync {
		t.Errorf("Applied = %s/%s, want False/WaitingForSync", s, r)
	}
}

// TestManualRollout_DeadlineStartsAtTheSyncThatApplies is the KB-4 regression for a
// healthy render rolled back right after its /sync: the deadline counted from the spec
// edit, not from the apply.
func TestManualRollout_DeadlineStartsAtTheSyncThatApplies(t *testing.T) {
	rel := manualRelease()
	withFailurePolicy(rel, corev1alpha1.RollbackPolicyOnReleaseFailure)
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), &fakeDeployer{})
	setVerdict(c, renderVerdictPending)
	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler #1: %v", err)
	}

	syncAt := metav1.NewTime(time.Date(2026, 6, 25, 12, 10, 0, 0, time.UTC))
	rel2 := statusUpdate(client.Actions()).DeepCopy()
	rel2.Spec = rel.Spec
	requestSync(rel2, syncAt)
	deployer := &fakeDeployer{}
	c2, client2 := newTestController(t, rel2, reachableCluster(), readyCoordinator(), deployer)
	c2.now = func() metav1.Time { return syncAt }
	setVerdict(c2, renderVerdictPending)
	if err := c2.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler #2: %v", err)
	}
	if !deployer.called {
		t.Fatal("the /sync did not apply the render")
	}
	got := statusUpdate(client2.Actions())
	if r := got.Status.Rollout; r == nil || r.State != corev1alpha1.RolloutStateProgressing || !r.StartedAt.Equal(&syncAt) {
		t.Errorf("rollout = %+v, want Progressing with startedAt %s (the apply)", r, syncAt)
	}
}

// TestManualRollout_LandedRollbackHoldsWithoutSync: once the rollback applied the
// last-healthy render, manual mode re-applies it only on a /sync (KB-4).
func TestManualRollout_LandedRollbackHoldsWithoutSync(t *testing.T) {
	rel := manualRolledBack(t, func(string) string { return lastHealthyRef })
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	seedLastHealthy(t, c, lhCMName)
	setVerdict(c, renderVerdictPending)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if deployer.called {
		t.Errorf("re-applied without a /sync in manual mode; deployed %v", deployer.got)
	}
	got := statusUpdate(client.Actions())
	if got.Status.Rollout == nil || got.Status.Rollout.State != corev1alpha1.RolloutStateRolledBack {
		t.Errorf("rollout = %+v, want RolledBack", got.Status.Rollout)
	}
	if s, r := readyReason(got); s != metav1.ConditionTrue || r != conditionReasonRolledBack {
		t.Errorf("Applied = %s/%s, want True/RolledBack", s, r)
	}
}

// TestManualRollout_RollbackRunsOnceWithoutSync: the rollback answers the /sync that
// applied the failed render, so it applies without a new /sync until it lands, also
// when an earlier attempt failed.
func TestManualRollout_RollbackRunsOnceWithoutSync(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prior   corev1alpha1.RolloutState
		verdict renderVerdict
	}{
		{name: "failed verdict", prior: corev1alpha1.RolloutStateProgressing, verdict: renderVerdictFailed},
		{name: "rollback not landed", prior: corev1alpha1.RolloutStateRolledBack, verdict: renderVerdictPending},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rel := manualRolledBack(t, func(renderRef string) string { return renderRef })
			rel.Status.Rollout.State = tc.prior
			deployer := &fakeDeployer{}
			c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
			seedLastHealthy(t, c, lhCMName)
			setVerdict(c, tc.verdict)

			if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
				t.Fatalf("syncHandler: %v", err)
			}
			if !appliedLastHealthy(deployer) {
				t.Errorf("did not apply the last-healthy render; deployed %v", deployer.got)
			}
			got := statusUpdate(client.Actions())
			if got.Status.Rollout == nil || got.Status.Rollout.State != corev1alpha1.RolloutStateRolledBack {
				t.Errorf("rollout = %+v, want RolledBack", got.Status.Rollout)
			}
		})
	}
}

// TestManualRollout_RolledBackSyncIsHandled: a /sync that re-applies the last-healthy
// render is recorded as handled, or it stays pending and applies the next spec edit
// without a new /sync (KB-4).
func TestManualRollout_RolledBackSyncIsHandled(t *testing.T) {
	rel := manualRolledBack(t, func(string) string { return lastHealthyRef })
	requestSync(rel, manualSyncAt)
	deployer := &fakeDeployer{}
	c, client := newTestController(t, rel, reachableCluster(), readyCoordinator(), deployer)
	seedLastHealthy(t, c, lhCMName)
	setVerdict(c, renderVerdictPending)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if !appliedLastHealthy(deployer) {
		t.Errorf("the /sync did not re-apply the last-healthy render; deployed %v", deployer.got)
	}
	got := statusUpdate(client.Actions())
	if h := got.Status.LastHandledSyncAt; h == nil || !h.Equal(&manualSyncAt) {
		t.Errorf("lastHandledSyncAt = %v, want %s", h, manualSyncAt)
	}
}

// TestManualRollout_WaveRollbackHandlesSync: a wave-failure rollback during a /sync
// records the request as handled (KB-4).
func TestManualRollout_WaveRollbackHandlesSync(t *testing.T) {
	rel, coord := waveTimeoutRelease(corev1alpha1.RollbackPolicyOnWaveFailure)
	rel.Spec.SyncPolicy.Automatic = false
	requestSync(rel, manualSyncAt)
	c, client := newTestController(t, rel, reachableCluster(), coord, &fakeDeployer{})
	seedLastHealthy(t, c, lhCMName)

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	got := statusUpdate(client.Actions())
	if got.Status.Rollout == nil || got.Status.Rollout.State != corev1alpha1.RolloutStateRolledBack {
		t.Fatalf("rollout = %+v, want RolledBack", got.Status.Rollout)
	}
	if h := got.Status.LastHandledSyncAt; h == nil || !h.Equal(&manualSyncAt) {
		t.Errorf("lastHandledSyncAt = %v, want %s", h, manualSyncAt)
	}
}
