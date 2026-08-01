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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/inventory"
)

func runAfter(v corev1alpha1.LifecycleHookRunAfter) *corev1alpha1.LifecycleHookRunAfter { return &v }

// relWithPostSync builds a release whose postSync hook carries the given runAfter,
// optionally with a health assessment (so releaseHealthy has a bar to clear).
func relWithPostSync(ra *corev1alpha1.LifecycleHookRunAfter, withHealth bool) *corev1alpha1.Release {
	hook := podHookSpec(hookNameMigrate, 0, "", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupNever)
	hook.RunAfter = ra
	rel := &corev1alpha1.Release{
		ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: "ns", UID: "uid-1"},
		Lifecycle:  &corev1alpha1.ReleaseLifecycle{PostSync: []corev1alpha1.LifecycleHookSpec{hook}},
	}
	if withHealth {
		rel.Spec.HealthAssessment = &corev1alpha1.ReleaseHealthAssessment{}
	}
	return rel
}

func runPostSync(t *testing.T, c *Controller, rel *corev1alpha1.Release) hookPhaseResult {
	t.Helper()
	res, err := c.runHookPhase(context.Background(), rel, hookCluster, corev1alpha1.LifecycleHookPhasePostSync, hookRenderRef)
	if err != nil {
		t.Fatalf("runHookPhase: %v", err)
	}
	return res
}

func TestRunAfter_AppliedArmsImmediately(t *testing.T) {
	fr := &fakeHookRunner{resolveNS: appName}
	// runAfter=Applied (explicit) with an unhealthy resource → still arms (no health wait).
	fs := &fakeHookStore{resources: []inventory.Ref{degradedRef("api")}}
	c := newHookController(fr, fs)
	rel := relWithPostSync(runAfter(corev1alpha1.LifecycleHookRunAfterApplied), true)
	if res := runPostSync(t, c, rel); res.state != hookPhaseHold {
		t.Fatalf("runAfter=Applied: state = %v, want hold (armed)", res.state)
	}
	if len(fr.created) != 1 {
		t.Errorf("runAfter=Applied armed %d, want 1 (no health wait)", len(fr.created))
	}
}

func TestRunAfter_NilDefaultsToApplied(t *testing.T) {
	fr := &fakeHookRunner{resolveNS: appName}
	fs := &fakeHookStore{resources: []inventory.Ref{degradedRef("api")}}
	c := newHookController(fr, fs)
	rel := relWithPostSync(nil, true) // no runAfter → Applied
	if res := runPostSync(t, c, rel); res.state != hookPhaseHold {
		t.Fatalf("runAfter=nil: state = %v, want hold (armed)", res.state)
	}
	if len(fr.created) != 1 {
		t.Errorf("runAfter=nil armed %d, want 1 (default Applied)", len(fr.created))
	}
}

func TestRunAfter_HealthyHoldsUntilHealthy(t *testing.T) {
	fr := &fakeHookRunner{resolveNS: appName}
	// A resource still progressing → the whole release is not healthy → hold, no arm.
	fs := &fakeHookStore{resources: []inventory.Ref{healthyRef("svc"), degradedRef("api")}}
	c := newHookController(fr, fs)
	rel := relWithPostSync(runAfter(corev1alpha1.LifecycleHookRunAfterHealthy), true)
	if res := runPostSync(t, c, rel); res.state != hookPhaseHold {
		t.Fatalf("runAfter=Healthy (unhealthy): state = %v, want hold", res.state)
	}
	if len(fr.created) != 0 {
		t.Errorf("runAfter=Healthy armed %d while unhealthy, want 0", len(fr.created))
	}
}

func TestRunAfter_HealthyArmsWhenAllHealthy(t *testing.T) {
	fr := &fakeHookRunner{resolveNS: appName}
	// Every resource across the release is healthy → arm.
	fs := &fakeHookStore{resources: []inventory.Ref{healthyRef("svc"), healthyRef("api")}}
	c := newHookController(fr, fs)
	rel := relWithPostSync(runAfter(corev1alpha1.LifecycleHookRunAfterHealthy), true)
	if res := runPostSync(t, c, rel); res.state != hookPhaseHold {
		t.Fatalf("runAfter=Healthy (healthy): state = %v, want hold (armed)", res.state)
	}
	if len(fr.created) != 1 {
		t.Errorf("runAfter=Healthy armed %d when all healthy, want 1", len(fr.created))
	}
}

func TestRunAfter_HealthyNoAssessmentIsTriviallyHealthy(t *testing.T) {
	fr := &fakeHookRunner{resolveNS: appName}
	// No health assessment → no bar → runAfter:Healthy behaves like Applied (arms now).
	fs := &fakeHookStore{}
	c := newHookController(fr, fs)
	rel := relWithPostSync(runAfter(corev1alpha1.LifecycleHookRunAfterHealthy), false)
	if res := runPostSync(t, c, rel); res.state != hookPhaseHold {
		t.Fatalf("runAfter=Healthy (no assessment): state = %v, want hold (armed)", res.state)
	}
	if len(fr.created) != 1 {
		t.Errorf("no assessment armed %d, want 1 (trivially healthy)", len(fr.created))
	}
}

func TestReleaseHealthy(t *testing.T) {
	ctx := context.Background()
	relNoHA := &corev1alpha1.Release{ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: "ns"}}
	relHA := &corev1alpha1.Release{
		ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: "ns"},
		Spec:       corev1alpha1.ReleaseSpec{HealthAssessment: &corev1alpha1.ReleaseHealthAssessment{}},
	}

	// No assessment → trivially healthy regardless of inventory.
	c := newHookController(&fakeHookRunner{}, &fakeHookStore{resources: []inventory.Ref{degradedRef("api")}})
	if ok, _ := c.releaseHealthy(ctx, relNoHA); !ok {
		t.Error("no assessment: want healthy=true")
	}

	// Empty inventory with an assessment → not healthy (nothing assessed yet).
	c = newHookController(&fakeHookRunner{}, &fakeHookStore{})
	if ok, _ := c.releaseHealthy(ctx, relHA); ok {
		t.Error("empty inventory: want healthy=false")
	}

	// All healthy → true; one progressing → false.
	c = newHookController(&fakeHookRunner{}, &fakeHookStore{resources: []inventory.Ref{healthyRef("a"), healthyRef("b")}})
	if ok, _ := c.releaseHealthy(ctx, relHA); !ok {
		t.Error("all healthy: want true")
	}
	c = newHookController(&fakeHookRunner{}, &fakeHookStore{resources: []inventory.Ref{healthyRef("a"), degradedRef("b")}})
	if ok, _ := c.releaseHealthy(ctx, relHA); ok {
		t.Error("one progressing: want false")
	}
}
