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
	"sort"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/inventory"
	"github.com/kumodo-io/convoy/internal/render"
)

// hookPhaseState is the outcome of evaluating one lifecycle-hook phase.
type hookPhaseState int

const (
	// hookPhaseAdvance: every hook in the phase is done for this render (succeeded,
	// skipped, or a Continue-failure) — the caller proceeds.
	hookPhaseAdvance hookPhaseState = iota
	// hookPhaseHold: a hook is armed/running — the caller holds and requeues; the
	// observer polls the workload and pokes /sync when the batch completes.
	hookPhaseHold
	// hookPhaseFailed: a hook failed with onFailure=Fail — the caller aborts.
	hookPhaseFailed
)

// hookPhaseResult carries the phase evaluation plus the details a caller needs to
// build an outcome (which hook failed, a human message).
type hookPhaseResult struct {
	state      hookPhaseState
	failedHook string
	message    string
}

// runHookPhase drives one lifecycle-hook phase for the current render, weight-group
// by weight-group. For the lowest incomplete weight-group it: skips hooks whose
// `when` is false; arms (deleteBeforeCreation + create + record identity) hooks not
// yet armed for this render; holds while any armed hook is still running; and, once
// a group's hooks are all terminal, cleans them up and advances to the next group.
// It reads the observer-written runtime state from the ReleaseInventory and never
// polls the workloads itself. A phase with no hooks (or a nil hookRunner) advances
// immediately.
func (c *Controller) runHookPhase(ctx context.Context, rel *corev1alpha1.Release, cluster *corev1alpha1.Cluster, phase corev1alpha1.LifecycleHookPhase, renderRef string) (hookPhaseResult, error) {
	hooks := hooksForPhase(rel, phase)
	if len(hooks) == 0 || c.hookRunner == nil {
		return hookPhaseResult{state: hookPhaseAdvance}, nil
	}

	// Read the controller+observer co-owned hook entries and index by (phase, name).
	items, err := c.inventory.GetHooks(ctx, rel.Namespace, rel.Name)
	if err != nil {
		return hookPhaseResult{}, err
	}
	armed := make(map[string]corev1alpha1.ReleaseLifecycleHookItem, len(items))
	for i := range items {
		armed[hookItemKey(items[i].Phase, items[i].Name)] = items[i]
	}

	// The controller-owned identity set to (re)assert this pass — the previously
	// recorded entries for this phase, updated as we arm hooks in the active group.
	// Entries for *other* phases are preserved untouched (co-owned by name/phase).
	desired := hookRefsForPhaseExcept(items, phase)

	ctxVals := render.HookContext(rel, cluster)
	groups := weightGroups(hooks)
	for _, group := range groups {
		groupDone := true
		for _, hook := range group {
			run, err := render.EvalHookWhen(hook.When, ctxVals)
			if err != nil {
				// A bad `when` is a config error: skip the hook (do not block the phase),
				// mirroring celTransform's treat-as-skip. Surfaced as Skipped in status.
				klog.V(4).InfoS("hook `when` errored; skipping", "release", rel.Name, "phase", phase, "hook", hook.Name, "err", err.Error())
				run = false
			}
			if !run {
				continue // Skipped: no workload, no entry; the status rollup marks it.
			}

			key := hookItemKey(phase, hook.Name)
			entry, isArmed := armed[key]
			if !isArmed || entry.RenderRef != renderRef {
				// Not armed for this render: (re)arm it. Deletes the prior workload if
				// requested, creates the new one, records its identity. Holds afterward.
				ref, err := c.armHook(ctx, rel, cluster, phase, hook, renderRef)
				if err != nil {
					return hookPhaseResult{}, err
				}
				desired = append(desired, ref)
				if err := c.inventory.PutHooks(ctx, rel.Namespace, rel.Name, desired); err != nil {
					return hookPhaseResult{}, err
				}
				return hookPhaseResult{state: hookPhaseHold}, nil
			}

			// Armed for this render — carry its identity forward and inspect its state.
			desired = append(desired, hookRefFromItem(entry))
			switch {
			case entry.State == nil || !isTerminalHookItemState(*entry.State):
				groupDone = false // still running: hold below
			case *entry.State == corev1alpha1.LifecycleHookStateFailed || *entry.State == corev1alpha1.LifecycleHookStateTimedOut:
				if hook.OnFailure == corev1alpha1.LifecycleHookOnFailureActionFail {
					return hookPhaseResult{
						state:      hookPhaseFailed,
						failedHook: hook.Name,
						message:    fmt.Sprintf("%s/%s: %s", phase, hook.Name, orDefault(entry.Message, string(*entry.State))),
					}, nil
				}
				// Continue: the failure doesn't block; clean up and move on.
				if err := c.cleanupHook(ctx, cluster, hook, entry); err != nil {
					return hookPhaseResult{}, err
				}
			default: // Succeeded
				if err := c.cleanupHook(ctx, cluster, hook, entry); err != nil {
					return hookPhaseResult{}, err
				}
			}
		}
		if !groupDone {
			// Some hook in this weight-group is still running: hold. Lower groups are
			// already terminal (we'd have returned), higher groups are not yet armed.
			return hookPhaseResult{state: hookPhaseHold}, nil
		}
	}
	return hookPhaseResult{state: hookPhaseAdvance}, nil
}

// armHook (re)creates a hook's workload on the target cluster and returns the
// identity to record. deleteBeforeCreation removes any prior workloads for this
// hook (across renders) first. The created workload's server-resolved namespace is
// recorded so the observer polls the right object.
func (c *Controller) armHook(ctx context.Context, rel *corev1alpha1.Release, cluster *corev1alpha1.Cluster, phase corev1alpha1.LifecycleHookPhase, hook corev1alpha1.LifecycleHookSpec, renderRef string) (inventory.HookRef, error) {
	workload, err := buildHookWorkload(rel, cluster, phase, hook, renderRef)
	if err != nil {
		return inventory.HookRef{}, err
	}
	gvk := workload.GroupVersionKind()
	if hook.DeleteBeforeCreation {
		if err := c.hookRunner.DeleteMatching(ctx, cluster, gvk, workload.GetNamespace(), hookSelector(rel, phase, hook.Name)); err != nil {
			return inventory.HookRef{}, err
		}
	}
	created, err := c.hookRunner.Create(ctx, cluster, workload)
	if err != nil {
		return inventory.HookRef{}, err
	}
	now := c.now()
	return inventory.HookRef{
		Name:              hook.Name,
		Phase:             phase,
		Weight:            hook.Weight,
		Timeout:           hook.Timeout,
		Group:             gvk.Group,
		Version:           gvk.Version,
		Kind:              gvk.Kind,
		WorkloadNamespace: created.GetNamespace(),
		WorkloadName:      created.GetName(),
		CreatedAt:         &now,
		RenderRef:         renderRef,
	}, nil
}

// cleanupHook deletes a hook's workload per its cleanup policy once terminal.
func (c *Controller) cleanupHook(ctx context.Context, cluster *corev1alpha1.Cluster, hook corev1alpha1.LifecycleHookSpec, entry corev1alpha1.ReleaseLifecycleHookItem) error {
	del := false
	switch hook.Cleanup {
	case corev1alpha1.LifecycleHookCleanupOnSuccess:
		del = entry.State != nil && *entry.State == corev1alpha1.LifecycleHookStateSucceeded
	case corev1alpha1.LifecycleHookCleanupOnCompletion:
		del = true
	case corev1alpha1.LifecycleHookCleanupNever:
		del = false
	}
	if !del || entry.WorkloadName == "" {
		return nil
	}
	gvk := schema.GroupVersionKind{Group: entry.Group, Version: entry.Version, Kind: entry.Kind}
	return c.hookRunner.Delete(ctx, cluster, gvk, entry.WorkloadNamespace, entry.WorkloadName)
}

// hooksForPhase returns the spec hooks declared for a phase (nil-safe).
func hooksForPhase(rel *corev1alpha1.Release, phase corev1alpha1.LifecycleHookPhase) []corev1alpha1.LifecycleHookSpec {
	if rel.Lifecycle == nil {
		return nil
	}
	switch phase {
	case corev1alpha1.LifecycleHookPhasePreRender:
		return rel.Lifecycle.PreRender
	case corev1alpha1.LifecycleHookPhasePostRender:
		return rel.Lifecycle.PostRender
	case corev1alpha1.LifecycleHookPhasePreSync:
		return rel.Lifecycle.PreSync
	case corev1alpha1.LifecycleHookPhaseSyncFailed:
		return rel.Lifecycle.SyncFailed
	case corev1alpha1.LifecycleHookPhasePostSync:
		return rel.Lifecycle.PostSync
	case corev1alpha1.LifecycleHookPhasePreDelete:
		return rel.Lifecycle.PreDelete
	case corev1alpha1.LifecycleHookPhasePostDelete:
		return rel.Lifecycle.PostDelete
	default:
		return nil
	}
}

// weightGroups sorts hooks by ascending weight and groups consecutive equal
// weights: a group applies together, and the next group waits for it.
func weightGroups(hooks []corev1alpha1.LifecycleHookSpec) [][]corev1alpha1.LifecycleHookSpec {
	sorted := make([]corev1alpha1.LifecycleHookSpec, len(hooks))
	copy(sorted, hooks)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Weight < sorted[j].Weight })
	var groups [][]corev1alpha1.LifecycleHookSpec
	for i := 0; i < len(sorted); {
		j := i + 1
		for j < len(sorted) && sorted[j].Weight == sorted[i].Weight {
			j++
		}
		groups = append(groups, sorted[i:j])
		i = j
	}
	return groups
}

// hookItemKey is the composite identity of a hook (phase, name).
func hookItemKey(phase corev1alpha1.LifecycleHookPhase, name string) string {
	return string(phase) + "|" + name
}

// hookRefsForPhaseExcept projects the recorded hook items (other than the given
// phase) back to HookRefs, so a PutHooks for one phase re-asserts the identity of
// the others rather than dropping them.
func hookRefsForPhaseExcept(items []corev1alpha1.ReleaseLifecycleHookItem, phase corev1alpha1.LifecycleHookPhase) []inventory.HookRef {
	var refs []inventory.HookRef
	for i := range items {
		if items[i].Phase == phase {
			continue
		}
		refs = append(refs, hookRefFromItem(items[i]))
	}
	return refs
}

// hookRefFromItem projects a recorded item back to the controller-owned HookRef.
func hookRefFromItem(it corev1alpha1.ReleaseLifecycleHookItem) inventory.HookRef {
	return inventory.HookRef{
		Name:              it.Name,
		Phase:             it.Phase,
		Weight:            it.Weight,
		Timeout:           it.Timeout,
		Group:             it.Group,
		Version:           it.Version,
		Kind:              it.Kind,
		WorkloadNamespace: it.WorkloadNamespace,
		WorkloadName:      it.WorkloadName,
		CreatedAt:         it.CreatedAt,
		RenderRef:         it.RenderRef,
	}
}

func isTerminalHookItemState(s corev1alpha1.LifecycleHookState) bool {
	switch s {
	case corev1alpha1.LifecycleHookStateSucceeded,
		corev1alpha1.LifecycleHookStateFailed,
		corev1alpha1.LifecycleHookStateTimedOut:
		return true
	default:
		return false
	}
}
