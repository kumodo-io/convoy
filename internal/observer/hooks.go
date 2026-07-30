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

package observer

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	corev1alpha1ac "github.com/kumodo-io/convoy/api/generated/applyconfiguration/core/v1alpha1"
)

// hookStatusFieldManager is the Server-Side Apply field manager the observer owns
// on a ReleaseInventory hook entry. It writes only the runtime fields (state,
// finishedAt, message); the release controller owns the identity/creation fields
// under its own manager, so the two co-own each entry without clobbering — the
// same split used for per-resource health.
const hookStatusFieldManager = "convoy-hook-status"

const (
	workloadKindJob = "Job"
	workloadKindPod = "Pod"

	// Job status.conditions[].type values that terminate a hook.
	jobConditionComplete = "Complete"
	// terminalFailed is both a Job condition type and a Pod phase.
	terminalFailed = "Failed"
	// podPhaseSucceeded is the Pod terminal-success phase.
	podPhaseSucceeded = "Succeeded"

	fieldConditions = "conditions"
	fieldPhase      = "phase"
)

// hookPollResult summarizes one lifecycle-hook polling pass.
type hookPollResult struct {
	// batchCompleted is true when at least one polled hook reached a terminal state
	// this pass — the edge trigger to wake the controller so it advances the phase
	// (or arms the next weight group).
	batchCompleted bool
	// allSettled is true when no hook is still running — nothing left to wait for.
	allSettled bool
}

// pollHooks reads the lifecycle-hook workloads recorded on the Release's
// ReleaseInventory, polls each armed-but-unfinished one on the target cluster, and
// records its state/finishedAt/message back onto the inventory. The controller
// owns whether a hook is armed (it creates the workload + entry); the observer only
// reports the workload's outcome. It does not render — the inventory is the source
// of truth for which hooks exist.
func (c *HealthController) pollHooks(ctx context.Context, rel *corev1alpha1.Release) (*hookPollResult, error) {
	inv, err := c.inventoryLister.ReleaseInventories(rel.Namespace).Get(rel.Name)
	if apierrors.IsNotFound(err) {
		return &hookPollResult{allSettled: true}, nil // nothing armed yet
	}
	if err != nil {
		return nil, fmt.Errorf("reading release inventory: %w", err)
	}

	var toPoll []corev1alpha1.ReleaseLifecycleHookItem
	for i := range inv.Hooks {
		if needsHookPoll(inv.Hooks[i]) {
			toPoll = append(toPoll, inv.Hooks[i])
		}
	}
	if len(toPoll) == 0 {
		return &hookPollResult{allSettled: true}, nil
	}

	cluster, err := c.targetCluster(rel)
	if err != nil {
		return nil, err
	}
	dyn, mapper, err := c.connector.DynamicClients(ctx, cluster)
	if err != nil {
		return nil, err
	}
	now := c.now().Time

	entries := make([]hookStatusEntry, 0, len(toPoll))
	batchCompleted, stillRunning := false, false
	for i := range toPoll {
		item := toPoll[i]
		state, message := c.pollHookWorkload(ctx, dyn, mapper, item, now)
		terminal := isTerminalHookState(state)
		if terminal {
			batchCompleted = true
		} else {
			stillRunning = true
		}
		entries = append(entries, hookStatusEntry{
			name:       item.Name,
			phase:      item.Phase,
			state:      state,
			message:    message,
			finishedAt: terminalTime(terminal, now),
		})
	}
	if err := c.writeHookStatus(ctx, rel, entries); err != nil {
		return nil, err
	}
	return &hookPollResult{batchCompleted: batchCompleted, allSettled: !stillRunning}, nil
}

// pollHookWorkload reads a hook workload live from the target and maps it to a
// lifecycle-hook state. A missing workload (armed but gone) is a Failure; a
// transient read error keeps it Running so a blip doesn't latch a false failure.
func (c *HealthController) pollHookWorkload(ctx context.Context, dyn dynamic.Interface, mapper meta.RESTMapper, item corev1alpha1.ReleaseLifecycleHookItem, now time.Time) (corev1alpha1.LifecycleHookState, string) {
	gvk := schema.GroupVersionKind{Group: item.Group, Version: item.Version, Kind: item.Kind}
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return corev1alpha1.LifecycleHookStateFailed, fmt.Sprintf("cannot resolve hook workload %s: %v", gvk, err)
	}
	live, err := resourceClient(dyn, mapping, item.WorkloadNamespace).Get(ctx, item.WorkloadName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return corev1alpha1.LifecycleHookStateFailed, "hook workload not found in cluster"
	}
	if err != nil {
		return corev1alpha1.LifecycleHookStateRunning, fmt.Sprintf("reading hook workload: %v", err)
	}
	return hookWorkloadState(item, live, now)
}

// writeHookStatus applies the observed hook runtime fields onto the
// ReleaseInventory via Server-Side Apply under the hook field manager. Each entry
// carries its identity keys (name, phase) plus the runtime fields; identity and
// creation stay the controller's. A non-terminal hook is written without
// finishedAt, which clears any stale value from a prior run (SSA drops owned
// fields absent from the apply).
func (c *HealthController) writeHookStatus(ctx context.Context, rel *corev1alpha1.Release, entries []hookStatusEntry) error {
	if len(entries) == 0 {
		return nil
	}
	ac := corev1alpha1ac.ReleaseInventory(rel.Name, rel.Namespace)
	for _, e := range entries {
		item := corev1alpha1ac.ReleaseLifecycleHookItem().
			WithName(e.name).
			WithPhase(e.phase).
			WithState(e.state)
		if e.message != "" {
			item.WithMessage(e.message)
		}
		if e.finishedAt != nil {
			item.WithFinishedAt(*e.finishedAt)
		}
		ac.WithHooks(item)
	}
	if _, err := c.client.CoreV1alpha1().ReleaseInventories(rel.Namespace).
		Apply(ctx, ac, metav1.ApplyOptions{FieldManager: hookStatusFieldManager, Force: true}); err != nil {
		return fmt.Errorf("applying hook status for %s/%s: %w", rel.Namespace, rel.Name, err)
	}
	return nil
}

// hookStatusEntry is one hook's observed runtime state, to be written back.
type hookStatusEntry struct {
	name       string
	phase      corev1alpha1.LifecycleHookPhase
	state      corev1alpha1.LifecycleHookState
	message    string
	finishedAt *metav1.Time
}

// targetCluster resolves and validates the Release's target Cluster.
func (c *HealthController) targetCluster(rel *corev1alpha1.Release) (*corev1alpha1.Cluster, error) {
	ns := rel.Spec.ClusterRef.Namespace
	if ns == "" {
		ns = rel.Namespace
	}
	cluster, err := c.clusterLister.Clusters(ns).Get(rel.Spec.ClusterRef.Name)
	if err != nil {
		return nil, fmt.Errorf("resolving target cluster: %w", err)
	}
	if !cluster.Status.Reachable {
		return nil, fmt.Errorf("target cluster %s is not reachable", cluster.Name)
	}
	return cluster, nil
}

// needsHookPoll reports whether a hook entry has a workload to poll that has not
// yet been recorded as finished for its current arming. finishedAt==nil means it
// has never finished; createdAt.After(finishedAt) means the controller re-armed it
// (bumped createdAt) since it last finished — the timestamp relationship the
// observer uses to detect a re-run without the controller writing observer-owned
// fields.
func needsHookPoll(item corev1alpha1.ReleaseLifecycleHookItem) bool {
	if item.WorkloadName == "" {
		return false
	}
	if item.FinishedAt == nil {
		return true
	}
	return item.CreatedAt != nil && item.CreatedAt.After(item.FinishedAt.Time)
}

// hookWorkloadState maps a live hook workload to a lifecycle-hook state, applying
// the per-hook timeout: a still-running workload past its deadline (measured from
// createdAt) is TimedOut.
func hookWorkloadState(item corev1alpha1.ReleaseLifecycleHookItem, live *unstructured.Unstructured, now time.Time) (corev1alpha1.LifecycleHookState, string) {
	var state corev1alpha1.LifecycleHookState
	var message string
	switch item.Kind {
	case workloadKindPod:
		state, message = podHookState(live)
	default: // Job (the only other validated type)
		state, message = jobHookState(live)
	}
	if state == corev1alpha1.LifecycleHookStateRunning && item.Timeout != nil && item.CreatedAt != nil {
		if now.Sub(item.CreatedAt.Time) > item.Timeout.Duration {
			return corev1alpha1.LifecycleHookStateTimedOut, fmt.Sprintf("hook did not complete within %s", item.Timeout.Duration)
		}
	}
	return state, message
}

// jobHookState maps a batch/v1 Job's conditions to a hook state: the Complete
// condition → Succeeded, the Failed condition → Failed, otherwise Running.
func jobHookState(u *unstructured.Unstructured) (corev1alpha1.LifecycleHookState, string) {
	conds, found, _ := unstructured.NestedSlice(u.Object, statusFieldName, fieldConditions)
	if found {
		for _, c := range conds {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if s, _ := cm[statusFieldName].(string); s != string(metav1.ConditionTrue) {
				continue
			}
			switch t, _ := cm["type"].(string); t {
			case jobConditionComplete:
				return corev1alpha1.LifecycleHookStateSucceeded, ""
			case terminalFailed:
				msg, _ := cm["message"].(string)
				return corev1alpha1.LifecycleHookStateFailed, msg
			}
		}
	}
	return corev1alpha1.LifecycleHookStateRunning, ""
}

// podHookState maps a Pod's terminal phase to a hook state: Succeeded/Failed are
// terminal, anything else (Pending/Running/Unknown) is Running.
func podHookState(u *unstructured.Unstructured) (corev1alpha1.LifecycleHookState, string) {
	phase, _, _ := unstructured.NestedString(u.Object, statusFieldName, fieldPhase)
	switch phase {
	case podPhaseSucceeded:
		return corev1alpha1.LifecycleHookStateSucceeded, ""
	case terminalFailed:
		reason, _, _ := unstructured.NestedString(u.Object, statusFieldName, "reason")
		if reason == "" {
			reason, _, _ = unstructured.NestedString(u.Object, statusFieldName, "message")
		}
		return corev1alpha1.LifecycleHookStateFailed, reason
	default:
		return corev1alpha1.LifecycleHookStateRunning, ""
	}
}

func isTerminalHookState(s corev1alpha1.LifecycleHookState) bool {
	switch s {
	case corev1alpha1.LifecycleHookStateSucceeded,
		corev1alpha1.LifecycleHookStateFailed,
		corev1alpha1.LifecycleHookStateTimedOut:
		return true
	default:
		return false
	}
}

func terminalTime(terminal bool, now time.Time) *metav1.Time {
	if !terminal {
		return nil
	}
	t := metav1.NewTime(now)
	return &t
}
