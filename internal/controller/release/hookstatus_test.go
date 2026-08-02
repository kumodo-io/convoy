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
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

func preSyncItem(name string, state *corev1alpha1.LifecycleHookState) corev1alpha1.ReleaseLifecycleHookItem {
	it := armedItem(name, hookRenderRef, state)
	return it
}

func TestBuildLifecycleStatus_ProjectsEntries(t *testing.T) {
	rel := relWithPreSync(podHookSpec(hookNameMigrate, 0, "", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupNever))
	created := metav1.NewTime(time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC))
	it := preSyncItem(hookNameMigrate, new(corev1alpha1.LifecycleHookStateFailed))
	it.CreatedAt = &created
	it.Message = "boom"

	ls := buildLifecycleStatus(rel, []corev1alpha1.ReleaseLifecycleHookItem{it})
	if len(ls.PreSync) != 1 {
		t.Fatalf("PreSync rollup = %d entries, want 1", len(ls.PreSync))
	}
	hs := ls.PreSync[0]
	if hs.Name != hookNameMigrate || hs.Source != corev1alpha1.LifecycleHookSourceLifecycle {
		t.Errorf("projected = %+v, want migrate/lifecycle", hs)
	}
	if hs.ApiVersion != "v1" || hs.Kind != kindPod || hs.ResourceName != it.WorkloadName {
		t.Errorf("workload ref = %s/%s/%s, want v1/Pod/%s", hs.ApiVersion, hs.Kind, hs.ResourceName, it.WorkloadName)
	}
	if hs.State != corev1alpha1.LifecycleHookStateFailed || hs.Message != "boom" {
		t.Errorf("state/message = %s/%q, want Failed/boom", hs.State, hs.Message)
	}
	if hs.FollowUpAction == nil || *hs.FollowUpAction != corev1alpha1.LifecycleHookOnFailureActionFail {
		t.Errorf("followUpAction = %v, want Fail (from spec onFailure)", hs.FollowUpAction)
	}
	if hs.StartedAt == nil {
		t.Error("startedAt not projected from createdAt")
	}
}

func TestBuildLifecycleStatus_NilStateIsRunning(t *testing.T) {
	rel := relWithPreSync(podHookSpec(hookNameMigrate, 0, "", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupNever))
	ls := buildLifecycleStatus(rel, []corev1alpha1.ReleaseLifecycleHookItem{preSyncItem(hookNameMigrate, nil)})
	if ls.PreSync[0].State != corev1alpha1.LifecycleHookStateRunning {
		t.Errorf("nil state projected as %s, want Running", ls.PreSync[0].State)
	}
}

func TestLifecycleCondition(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC))
	rel := relWithPreSync(podHookSpec(hookNameMigrate, 0, "", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupNever))

	// No hooks → nil (absent → column N/A).
	if c := lifecycleCondition(1, now, corev1alpha1.LifecycleStatus{}); c != nil {
		t.Errorf("no hooks: condition = %+v, want nil", c)
	}

	build := func(states ...corev1alpha1.LifecycleHookState) corev1alpha1.LifecycleStatus {
		items := make([]corev1alpha1.ReleaseLifecycleHookItem, 0, len(states))
		for i, s := range states {
			items = append(items, preSyncItem(string(rune('a'+i)), new(s)))
		}
		return buildLifecycleStatus(rel, items)
	}

	// All succeeded → True/AllSucceeded.
	c := lifecycleCondition(1, now, build(corev1alpha1.LifecycleHookStateSucceeded, corev1alpha1.LifecycleHookStateSkipped))
	if c == nil || c.Status != metav1.ConditionTrue || c.Reason != reasonHooksAllSucceeded {
		t.Errorf("all-ok condition = %+v, want True/AllSucceeded", c)
	}

	// Some running → Unknown/Running.
	c = lifecycleCondition(1, now, build(corev1alpha1.LifecycleHookStateSucceeded, corev1alpha1.LifecycleHookStateRunning))
	if c == nil || c.Status != metav1.ConditionUnknown || c.Reason != reasonHooksRunning {
		t.Errorf("running condition = %+v, want Unknown/Running", c)
	}

	// Any failed → False/HookFailed (wins over running).
	c = lifecycleCondition(1, now, build(corev1alpha1.LifecycleHookStateRunning, corev1alpha1.LifecycleHookStateFailed))
	if c == nil || c.Status != metav1.ConditionFalse || c.Reason != conditionReasonHookFailed {
		t.Errorf("failed condition = %+v, want False/HookFailed", c)
	}
}
