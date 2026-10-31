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

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

func failedOutcome(stage corev1alpha1.ReleaseRenderAttemptStage) reconcileOutcome {
	return reconcileOutcome{attempt: corev1alpha1.ReleaseAttemptedRenderStatus{
		Phase:   corev1alpha1.ReleaseRenderAttemptPhaseFailed,
		Failure: &corev1alpha1.ReleaseAttemptedRenderFailureStatus{Stage: stage},
	}}
}

func TestIsSyncingFailure(t *testing.T) {
	if !isSyncingFailure(failedOutcome(corev1alpha1.ReleaseRenderAttemptStageSyncing)) {
		t.Error("syncing-stage failure not detected")
	}
	// Other failure stages do not trigger SyncFailed.
	for _, stage := range []corev1alpha1.ReleaseRenderAttemptStage{
		corev1alpha1.ReleaseRenderAttemptStageRendering,
		corev1alpha1.ReleaseRenderAttemptStageClusterResolution,
		corev1alpha1.ReleaseRenderAttemptStageHookFailed,
	} {
		if isSyncingFailure(failedOutcome(stage)) {
			t.Errorf("%s failure wrongly detected as syncing failure", stage)
		}
	}
	// A non-failed (hold/success) outcome never triggers it.
	if isSyncingFailure(reconcileOutcome{}) {
		t.Error("empty outcome wrongly detected as syncing failure")
	}
	if isSyncingFailure(reconcileOutcome{ready: true, reason: conditionReasonApplied}) {
		t.Error("success outcome wrongly detected as syncing failure")
	}
}

func relWithSyncFailed(hooks ...corev1alpha1.LifecycleHookSpec) *corev1alpha1.Release {
	return &corev1alpha1.Release{
		Name: appName, Namespace: "ns", UID: hookRelUID,
		Lifecycle: &corev1alpha1.ReleaseLifecycle{SyncFailed: hooks},
	}
}

func TestRunSyncFailedHooks_ArmsHooks(t *testing.T) {
	fr := &fakeHookRunner{resolveNS: appName}
	fs := &fakeHookStore{}
	c := newHookController(fr, fs)
	rel := relWithSyncFailed(podHookSpec("notify", 0, "", corev1alpha1.LifecycleHookOnFailureActionContinue, corev1alpha1.LifecycleHookCleanupNever))

	c.runSyncFailedHooks(context.Background(), rel, hookCluster, hookRenderRef)
	if len(fr.created) != 1 {
		t.Errorf("syncFailed armed %d workload(s), want 1", len(fr.created))
	}
	if len(fs.putLast) != 1 || fs.putLast[0].Phase != corev1alpha1.LifecycleHookPhaseSyncFailed {
		t.Errorf("recorded hook = %+v, want one syncFailed entry", fs.putLast)
	}
}

func TestRunSyncFailedHooks_NoClusterOrRenderRefIsNoop(t *testing.T) {
	fr := &fakeHookRunner{resolveNS: appName}
	c := newHookController(fr, &fakeHookStore{})
	rel := relWithSyncFailed(podHookSpec("notify", 0, "", corev1alpha1.LifecycleHookOnFailureActionContinue, corev1alpha1.LifecycleHookCleanupNever))

	c.runSyncFailedHooks(context.Background(), rel, nil, hookRenderRef) // no cluster
	c.runSyncFailedHooks(context.Background(), rel, hookCluster, "")    // no renderRef
	if len(fr.created) != 0 {
		t.Errorf("armed %d workload(s) without cluster/renderRef, want 0", len(fr.created))
	}
}
