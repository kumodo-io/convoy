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
	"fmt"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

const (
	// conditionLifecycleSucceeded is the controller-owned aggregate over a Release's
	// lifecycle hooks. Absent when the Release has no hooks; otherwise True (all
	// succeeded/skipped), Unknown (some still running), or False (a hook failed —
	// regardless of its onFailure, which only governs whether the sync proceeds).
	conditionLifecycleSucceeded = "LifecycleSucceeded"

	reasonHooksAllSucceeded = "AllSucceeded"
	reasonHooksRunning      = "Running"
)

// applyLifecycleRollup projects the Release's ReleaseInventory hook entries into the
// controller-owned status.lifecycle rollup and the aggregate LifecycleSucceeded
// condition (parity with status.waves). It is a no-op when hooks are disabled (nil
// runner). It overwrites newStatus.Lifecycle and sets/removes the condition.
func (c *Controller) applyLifecycleRollup(ctx context.Context, rel *corev1alpha1.Release, newStatus *corev1alpha1.ReleaseStatus, now metav1.Time) error {
	if c.hookRunner == nil {
		return nil // hooks disabled: leave lifecycle status untouched
	}
	items, err := c.inventory.GetHooks(ctx, rel.Namespace, rel.Name)
	if err != nil {
		return fmt.Errorf("reading hook inventory for status: %w", err)
	}
	newStatus.Lifecycle = buildLifecycleStatus(rel, items)
	if cond := lifecycleCondition(rel.Generation, now, newStatus.Lifecycle); cond != nil {
		apimeta.SetStatusCondition(&newStatus.Conditions, *cond)
	} else {
		apimeta.RemoveStatusCondition(&newStatus.Conditions, conditionLifecycleSucceeded)
	}
	return nil
}

// buildLifecycleStatus projects the recorded hook entries into the per-phase
// status.lifecycle rollup, mirroring each entry's observer-written runtime state.
func buildLifecycleStatus(rel *corev1alpha1.Release, items []corev1alpha1.ReleaseLifecycleHookItem) corev1alpha1.LifecycleStatus {
	var ls corev1alpha1.LifecycleStatus
	for i := range items {
		hs := hookItemToStatus(rel, items[i])
		appendHookStatus(&ls, items[i].Phase, hs)
	}
	return ls
}

// hookItemToStatus maps one inventory hook entry to its status projection. A
// not-yet-assessed entry (nil state) reads as Running — it is armed but the
// observer has not reported an outcome. FollowUpAction records the configured
// onFailure only when the hook actually failed.
func hookItemToStatus(rel *corev1alpha1.Release, it corev1alpha1.ReleaseLifecycleHookItem) corev1alpha1.LifecycleHookStatus {
	state := corev1alpha1.LifecycleHookStateRunning
	if it.State != nil {
		state = *it.State
	}
	hs := corev1alpha1.LifecycleHookStatus{
		Name:              it.Name,
		Source:            corev1alpha1.LifecycleHookSourceLifecycle,
		Kind:              it.Kind,
		ApiVersion:        apiVersionOf(it.Group, it.Version),
		ResourceName:      it.WorkloadName,
		ResourceNamespace: it.WorkloadNamespace,
		State:             state,
		Weight:            it.Weight,
		RenderRef:         it.RenderRef,
		StartedAt:         it.CreatedAt,
		CompletedAt:       it.FinishedAt,
		Message:           it.Message,
	}
	if state == corev1alpha1.LifecycleHookStateFailed || state == corev1alpha1.LifecycleHookStateTimedOut {
		if spec := findHookSpec(rel, it.Phase, it.Name); spec != nil {
			fa := spec.OnFailure
			hs.FollowUpAction = &fa
		}
	}
	return hs
}

// lifecycleCondition computes the aggregate LifecycleSucceeded condition over the
// rollup, or nil when the Release has no hooks (condition absent → column N/A).
func lifecycleCondition(generation int64, now metav1.Time, ls corev1alpha1.LifecycleStatus) *metav1.Condition {
	total, running, failed := 0, 0, 0
	var failMessage string
	forEachHookStatus(ls, func(phase corev1alpha1.LifecycleHookPhase, hs corev1alpha1.LifecycleHookStatus) {
		total++
		switch hs.State {
		case corev1alpha1.LifecycleHookStateFailed, corev1alpha1.LifecycleHookStateTimedOut:
			failed++
			if failMessage == "" {
				failMessage = fmt.Sprintf("%s/%s: %s", phase, hs.Name, orDefault(hs.Message, string(hs.State)))
			}
		case corev1alpha1.LifecycleHookStateSucceeded, corev1alpha1.LifecycleHookStateSkipped:
			// terminal-ok
		default:
			running++
		}
	})
	if total == 0 {
		return nil
	}
	cond := &metav1.Condition{
		Type:               conditionLifecycleSucceeded,
		ObservedGeneration: generation,
		LastTransitionTime: now,
	}
	switch {
	case failed > 0:
		cond.Status = metav1.ConditionFalse
		cond.Reason = conditionReasonHookFailed
		cond.Message = failMessage
	case running > 0:
		cond.Status = metav1.ConditionUnknown
		cond.Reason = reasonHooksRunning
		cond.Message = "lifecycle hooks in progress"
	default:
		cond.Status = metav1.ConditionTrue
		cond.Reason = reasonHooksAllSucceeded
		cond.Message = "all lifecycle hooks succeeded"
	}
	return cond
}

// findHookSpec returns the spec for a hook by (phase, name), or nil.
func findHookSpec(rel *corev1alpha1.Release, phase corev1alpha1.LifecycleHookPhase, name string) *corev1alpha1.LifecycleHookSpec {
	for i := range hooksForPhase(rel, phase) {
		if h := hooksForPhase(rel, phase)[i]; h.Name == name {
			return &h
		}
	}
	return nil
}

// apiVersionOf assembles an apiVersion string from a GVK's group and version.
func apiVersionOf(group, version string) string {
	if group == "" {
		return version
	}
	return group + "/" + version
}

// appendHookStatus appends a hook status to the slice for its phase.
func appendHookStatus(ls *corev1alpha1.LifecycleStatus, phase corev1alpha1.LifecycleHookPhase, hs corev1alpha1.LifecycleHookStatus) {
	switch phase {
	case corev1alpha1.LifecycleHookPhasePreSync:
		ls.PreSync = append(ls.PreSync, hs)
	case corev1alpha1.LifecycleHookPhaseSyncFailed:
		ls.SyncFailed = append(ls.SyncFailed, hs)
	case corev1alpha1.LifecycleHookPhasePostSync:
		ls.PostSync = append(ls.PostSync, hs)
	case corev1alpha1.LifecycleHookPhasePreDelete:
		ls.PreDelete = append(ls.PreDelete, hs)
	case corev1alpha1.LifecycleHookPhasePostDelete:
		ls.PostDelete = append(ls.PostDelete, hs)
	}
}

// lifecycleStatusEmpty reports whether the rollup has no hook entries in any phase.
func lifecycleStatusEmpty(ls corev1alpha1.LifecycleStatus) bool {
	return len(ls.PreSync) == 0 && len(ls.SyncFailed) == 0 &&
		len(ls.PostSync) == 0 && len(ls.PreDelete) == 0 && len(ls.PostDelete) == 0
}

// forEachHookStatus visits every hook status in the rollup with its phase, in
// pipeline order.
func forEachHookStatus(ls corev1alpha1.LifecycleStatus, fn func(corev1alpha1.LifecycleHookPhase, corev1alpha1.LifecycleHookStatus)) {
	visit := func(phase corev1alpha1.LifecycleHookPhase, hss []corev1alpha1.LifecycleHookStatus) {
		for i := range hss {
			fn(phase, hss[i])
		}
	}
	visit(corev1alpha1.LifecycleHookPhasePreSync, ls.PreSync)
	visit(corev1alpha1.LifecycleHookPhaseSyncFailed, ls.SyncFailed)
	visit(corev1alpha1.LifecycleHookPhasePostSync, ls.PostSync)
	visit(corev1alpha1.LifecycleHookPhasePreDelete, ls.PreDelete)
	visit(corev1alpha1.LifecycleHookPhasePostDelete, ls.PostDelete)
}
