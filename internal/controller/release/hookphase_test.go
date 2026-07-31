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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/inventory"
)

const (
	hookRenderRef = "sha256:hookrender"
	appName       = "app"
)

// fakeHookRunner records Create/Delete/DeleteMatching calls; Create stamps a
// server-resolved namespace to mimic the API server defaulting an empty namespace.
type fakeHookRunner struct {
	created          []*unstructured.Unstructured
	deletedNames     []string
	deletedSelectors []string
	resolveNS        string
	createErr        error
}

func (f *fakeHookRunner) Create(_ context.Context, _ *corev1alpha1.Cluster, w *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	out := w.DeepCopy()
	if out.GetNamespace() == "" && f.resolveNS != "" {
		out.SetNamespace(f.resolveNS)
	}
	f.created = append(f.created, out)
	return out, nil
}

func (f *fakeHookRunner) Delete(_ context.Context, _ *corev1alpha1.Cluster, _ schema.GroupVersionKind, _, name string) error {
	f.deletedNames = append(f.deletedNames, name)
	return nil
}

func (f *fakeHookRunner) DeleteMatching(_ context.Context, _ *corev1alpha1.Cluster, _ schema.GroupVersionKind, _, selector string) error {
	f.deletedSelectors = append(f.deletedSelectors, selector)
	return nil
}

// fakeHookStore serves seeded hook items from GetHooks and captures PutHooks; the
// resource-inventory methods are unused by the gate.
type fakeHookStore struct {
	hooks   []corev1alpha1.ReleaseLifecycleHookItem
	putLast []inventory.HookRef
	putN    int
}

func (s *fakeHookStore) Get(context.Context, string, string) ([]inventory.Ref, bool, error) {
	return nil, false, nil
}
func (s *fakeHookStore) Put(context.Context, string, string, []inventory.Ref) error { return nil }
func (s *fakeHookStore) Delete(context.Context, string, string) error               { return nil }
func (s *fakeHookStore) PutHooks(_ context.Context, _, _ string, hooks []inventory.HookRef) error {
	s.putLast = hooks
	s.putN++
	return nil
}

func (s *fakeHookStore) GetHooks(context.Context, string, string) ([]corev1alpha1.ReleaseLifecycleHookItem, error) {
	return s.hooks, nil
}

func podHookSpec(name string, weight int32, when string, onFail corev1alpha1.LifecycleHookOnFailureAction, cleanup corev1alpha1.LifecycleHookCleanup) corev1alpha1.LifecycleHookSpec {
	return corev1alpha1.LifecycleHookSpec{
		Name:      name,
		Type:      corev1alpha1.LifecycleHookTypePod,
		When:      when,
		Weight:    weight,
		OnFailure: onFail,
		Cleanup:   cleanup,
		PodTemplate: &corev1alpha1.LifecycleHookPodTemplate{
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "busybox"}}},
		},
	}
}

func armedItem(name string, renderRef string, state *corev1alpha1.LifecycleHookState) corev1alpha1.ReleaseLifecycleHookItem {
	return corev1alpha1.ReleaseLifecycleHookItem{
		Name:              name,
		Phase:             corev1alpha1.LifecycleHookPhasePreSync,
		Version:           "v1",
		Kind:              "Pod",
		WorkloadName:      "presync-" + name + "-abcd1234",
		WorkloadNamespace: appName,
		RenderRef:         renderRef,
		State:             state,
	}
}

func hookState(s corev1alpha1.LifecycleHookState) *corev1alpha1.LifecycleHookState { return &s }

func newHookController(runner HookRunner, store inventory.Store) *Controller {
	fixed := metav1.NewTime(time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC))
	return &Controller{hookRunner: runner, inventory: store, now: func() metav1.Time { return fixed }}
}

func relWithPreSync(hooks ...corev1alpha1.LifecycleHookSpec) *corev1alpha1.Release {
	return &corev1alpha1.Release{
		ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: "ns", UID: "uid-1"},
		Lifecycle:  &corev1alpha1.ReleaseLifecycle{PreSync: hooks},
	}
}

var hookCluster = &corev1alpha1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "target"}}

func runPreSync(t *testing.T, c *Controller, rel *corev1alpha1.Release) hookPhaseResult {
	t.Helper()
	res, err := c.runHookPhase(context.Background(), rel, hookCluster, corev1alpha1.LifecycleHookPhasePreSync, hookRenderRef)
	if err != nil {
		t.Fatalf("runHookPhase: %v", err)
	}
	return res
}

func TestRunHookPhase_NoHooksAdvances(t *testing.T) {
	c := newHookController(&fakeHookRunner{}, &fakeHookStore{})
	rel := &corev1alpha1.Release{ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: "ns"}}
	if res := runPreSync(t, c, rel); res.state != hookPhaseAdvance {
		t.Errorf("no hooks: state = %v, want advance", res.state)
	}
}

func TestRunHookPhase_NilRunnerAdvances(t *testing.T) {
	c := newHookController(nil, &fakeHookStore{})
	rel := relWithPreSync(podHookSpec("migrate", 0, "", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupNever))
	if res := runPreSync(t, c, rel); res.state != hookPhaseAdvance {
		t.Errorf("nil runner: state = %v, want advance (hooks disabled)", res.state)
	}
}

func TestRunHookPhase_WhenFalseSkips(t *testing.T) {
	fr := &fakeHookRunner{}
	c := newHookController(fr, &fakeHookStore{})
	rel := relWithPreSync(podHookSpec("prod-only", 0, "false", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupNever))
	if res := runPreSync(t, c, rel); res.state != hookPhaseAdvance {
		t.Errorf("when=false: state = %v, want advance", res.state)
	}
	if len(fr.created) != 0 {
		t.Errorf("when=false created %d workload(s), want 0", len(fr.created))
	}
}

func TestRunHookPhase_ArmsAndHolds(t *testing.T) {
	fr := &fakeHookRunner{resolveNS: appName}
	fs := &fakeHookStore{}
	c := newHookController(fr, fs)
	rel := relWithPreSync(podHookSpec("migrate", 0, "", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupNever))

	res := runPreSync(t, c, rel)
	if res.state != hookPhaseHold {
		t.Fatalf("first pass: state = %v, want hold (armed)", res.state)
	}
	if len(fr.created) != 1 {
		t.Fatalf("armed %d workload(s), want 1", len(fr.created))
	}
	if got := fr.created[0].GetNamespace(); got != appName {
		t.Errorf("workload namespace = %q, want app (server-resolved)", got)
	}
	if len(fs.putLast) != 1 || fs.putLast[0].RenderRef != hookRenderRef || fs.putLast[0].WorkloadName == "" {
		t.Errorf("recorded hook identity = %+v, want one entry with renderRef+workloadName", fs.putLast)
	}
	if fs.putLast[0].CreatedAt == nil {
		t.Error("recorded hook identity missing createdAt")
	}
}

func TestRunHookPhase_DeleteBeforeCreation(t *testing.T) {
	fr := &fakeHookRunner{resolveNS: appName}
	c := newHookController(fr, &fakeHookStore{})
	hook := podHookSpec("migrate", 0, "", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupNever)
	hook.DeleteBeforeCreation = true
	runPreSync(t, c, relWithPreSync(hook))
	if len(fr.deletedSelectors) != 1 {
		t.Fatalf("deleteBeforeCreation issued %d selector deletes, want 1", len(fr.deletedSelectors))
	}
}

func TestRunHookPhase_RunningHolds(t *testing.T) {
	fr := &fakeHookRunner{}
	fs := &fakeHookStore{hooks: []corev1alpha1.ReleaseLifecycleHookItem{
		armedItem("migrate", hookRenderRef, hookState(corev1alpha1.LifecycleHookStateRunning)),
	}}
	c := newHookController(fr, fs)
	rel := relWithPreSync(podHookSpec("migrate", 0, "", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupNever))
	if res := runPreSync(t, c, rel); res.state != hookPhaseHold {
		t.Errorf("running hook: state = %v, want hold", res.state)
	}
	if len(fr.created) != 0 {
		t.Errorf("running hook re-created %d workload(s), want 0", len(fr.created))
	}
}

func TestRunHookPhase_SucceededAdvancesAndCleans(t *testing.T) {
	fr := &fakeHookRunner{}
	fs := &fakeHookStore{hooks: []corev1alpha1.ReleaseLifecycleHookItem{
		armedItem("migrate", hookRenderRef, hookState(corev1alpha1.LifecycleHookStateSucceeded)),
	}}
	c := newHookController(fr, fs)
	// cleanup=OnCompletion → the succeeded workload is deleted.
	rel := relWithPreSync(podHookSpec("migrate", 0, "", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupOnCompletion))
	if res := runPreSync(t, c, rel); res.state != hookPhaseAdvance {
		t.Errorf("succeeded hook: state = %v, want advance", res.state)
	}
	if len(fr.deletedNames) != 1 {
		t.Errorf("cleanup deleted %d workload(s), want 1", len(fr.deletedNames))
	}
}

func TestRunHookPhase_FailedWithFailAborts(t *testing.T) {
	fs := &fakeHookStore{hooks: []corev1alpha1.ReleaseLifecycleHookItem{
		armedItem("migrate", hookRenderRef, hookState(corev1alpha1.LifecycleHookStateFailed)),
	}}
	c := newHookController(&fakeHookRunner{}, fs)
	rel := relWithPreSync(podHookSpec("migrate", 0, "", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupNever))
	res := runPreSync(t, c, rel)
	if res.state != hookPhaseFailed {
		t.Fatalf("failed+Fail: state = %v, want failed", res.state)
	}
	if res.failedHook != "migrate" {
		t.Errorf("failedHook = %q, want migrate", res.failedHook)
	}
}

func TestRunHookPhase_FailedWithContinueAdvances(t *testing.T) {
	fs := &fakeHookStore{hooks: []corev1alpha1.ReleaseLifecycleHookItem{
		armedItem("smoke", hookRenderRef, hookState(corev1alpha1.LifecycleHookStateFailed)),
	}}
	c := newHookController(&fakeHookRunner{}, fs)
	rel := relWithPreSync(podHookSpec("smoke", 0, "", corev1alpha1.LifecycleHookOnFailureActionContinue, corev1alpha1.LifecycleHookCleanupNever))
	if res := runPreSync(t, c, rel); res.state != hookPhaseAdvance {
		t.Errorf("failed+Continue: state = %v, want advance", res.state)
	}
}

func TestRunHookPhase_StaleRenderReArms(t *testing.T) {
	fr := &fakeHookRunner{resolveNS: appName}
	// Entry succeeded but for a PRIOR render → must re-arm for the current render.
	fs := &fakeHookStore{hooks: []corev1alpha1.ReleaseLifecycleHookItem{
		armedItem("migrate", "sha256:oldrender", hookState(corev1alpha1.LifecycleHookStateSucceeded)),
	}}
	c := newHookController(fr, fs)
	rel := relWithPreSync(podHookSpec("migrate", 0, "", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupNever))
	if res := runPreSync(t, c, rel); res.state != hookPhaseHold {
		t.Errorf("stale render: state = %v, want hold (re-armed)", res.state)
	}
	if len(fr.created) != 1 {
		t.Errorf("stale render re-armed %d workload(s), want 1", len(fr.created))
	}
}

func TestRunHookPhase_WeightGroupsOrdered(t *testing.T) {
	g0 := podHookSpec("first", 0, "", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupNever)
	g1 := podHookSpec("second", 1, "", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupNever)

	// Weight-0 still running → weight-1 must NOT be armed yet.
	fr := &fakeHookRunner{}
	fs := &fakeHookStore{hooks: []corev1alpha1.ReleaseLifecycleHookItem{
		armedItem("first", hookRenderRef, hookState(corev1alpha1.LifecycleHookStateRunning)),
	}}
	c := newHookController(fr, fs)
	if res := runPreSync(t, c, relWithPreSync(g0, g1)); res.state != hookPhaseHold {
		t.Errorf("group0 running: state = %v, want hold", res.state)
	}
	if len(fr.created) != 0 {
		t.Errorf("group0 running armed group1 (%d creates), want 0 — ordering violated", len(fr.created))
	}

	// Weight-0 succeeded, weight-1 not armed → weight-1 gets armed now.
	fr2 := &fakeHookRunner{resolveNS: appName}
	fs2 := &fakeHookStore{hooks: []corev1alpha1.ReleaseLifecycleHookItem{
		armedItem("first", hookRenderRef, hookState(corev1alpha1.LifecycleHookStateSucceeded)),
	}}
	c2 := newHookController(fr2, fs2)
	if res := runPreSync(t, c2, relWithPreSync(g0, g1)); res.state != hookPhaseHold {
		t.Errorf("group0 done: state = %v, want hold (arming group1)", res.state)
	}
	if len(fr2.created) != 1 || fr2.created[0].GetName() == "" {
		t.Errorf("group0 done armed %d group1 workload(s), want 1", len(fr2.created))
	}
}
