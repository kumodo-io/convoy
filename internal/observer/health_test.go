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
	"encoding/json"
	"errors"
	"testing"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clienttesting "k8s.io/client-go/testing"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	fakeclient "github.com/kumodo-io/convoy/api/generated/clientset/versioned/fake"
	informers "github.com/kumodo-io/convoy/api/generated/informers/externalversions"
	"github.com/kumodo-io/convoy/internal/healthresult"
)

const (
	deploymentKind = "Deployment"
	svcName        = "svc"
)

func newTestHealthController(t *testing.T, rel *corev1alpha1.Release) (*HealthController, *fakeclient.Clientset) {
	t.Helper()
	client := fakeclient.NewSimpleClientset(rel)
	factory := informers.NewSharedInformerFactory(client, 0)
	relInformer := factory.Core().V1alpha1().Releases()
	clInformer := factory.Core().V1alpha1().Clusters()
	inventoryInformer := factory.Core().V1alpha1().ReleaseInventories()
	globalRuleSets := factory.Extensions().V1alpha1().GlobalHealthRuleSets()
	ruleSets := factory.Extensions().V1alpha1().HealthRuleSets()
	if err := relInformer.Informer().GetStore().Add(rel); err != nil {
		t.Fatalf("seeding release store: %v", err)
	}
	c := NewHealthController(client, relInformer, clInformer.Lister(), nil,
		globalRuleSets.Lister(), ruleSets.Lister(), inventoryInformer.Lister())
	fixed := metav1.NewTime(time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC))
	c.now = func() metav1.Time { return fixed }
	return c, client
}

func healthCondition(actions []clienttesting.Action) *metav1.Condition {
	var found *metav1.Condition
	for _, a := range actions {
		p, ok := a.(clienttesting.PatchAction)
		if !ok || !a.Matches("patch", "releases") || a.GetSubresource() != statusFieldName || p.GetPatchType() != types.ApplyPatchType {
			continue
		}
		rel := &corev1alpha1.Release{}
		if err := json.Unmarshal(p.GetPatch(), rel); err != nil {
			return nil
		}
		if c := apimeta.FindStatusCondition(rel.Status.Conditions, conditionHealthy); c != nil {
			found = c
		}
	}
	return found
}

func withAssessment(rel *corev1alpha1.Release) *corev1alpha1.Release {
	rel.Spec.HealthAssessment = &corev1alpha1.ReleaseHealthAssessment{}
	return rel
}

func TestHealth_NoConfigReportsHealthy(t *testing.T) {
	c, client := newTestHealthController(t, testRelease(true)) // no HealthAssessment

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	cond := healthCondition(client.Actions())
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != reasonNotConfigured {
		t.Errorf("Healthy = %+v, want True/NotConfigured", cond)
	}
}

func TestHealth_AllHealthy(t *testing.T) {
	c, client := newTestHealthController(t, withAssessment(testRelease(true)))
	c.assessFn = func(context.Context, *corev1alpha1.Release) (*assessment, error) {
		return stubAssessment(
			healthresult.ObjectHealth{Kind: deploymentKind, Name: "app", State: healthresult.StateHealthy},
		), nil
	}

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	cond := healthCondition(client.Actions())
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != reasonHealthy {
		t.Errorf("Healthy = %+v, want True/Healthy", cond)
	}
}

// stubAssessment builds an *assessment carrying only the aggregate result, for
// the condition-focused tests that stub assessFn (no entries → no wave-complete
// sync, no backoff change).
func stubAssessment(objs ...healthresult.ObjectHealth) *assessment {
	return &assessment{result: &healthresult.Result{Objects: objs}, renderCurrent: true}
}

func TestHealth_DegradedWorstStateWins(t *testing.T) {
	c, client := newTestHealthController(t, withAssessment(testRelease(true)))
	c.assessFn = func(context.Context, *corev1alpha1.Release) (*assessment, error) {
		// A mix: progressing + degraded + unknown → Degraded is the worst.
		return stubAssessment(
			healthresult.ObjectHealth{Kind: deploymentKind, Name: "a", State: healthresult.StateProgressing},
			healthresult.ObjectHealth{Kind: deploymentKind, Name: "b", State: healthresult.StateDegraded},
			healthresult.ObjectHealth{Kind: deploymentKind, Name: "c", State: healthresult.StateUnknown},
		), nil
	}

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	cond := healthCondition(client.Actions())
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != reasonDegraded {
		t.Errorf("Healthy = %+v, want False/Degraded", cond)
	}
}

func TestHealth_AssessErrorIsUnknown(t *testing.T) {
	c, client := newTestHealthController(t, withAssessment(testRelease(true)))
	c.assessFn = func(context.Context, *corev1alpha1.Release) (*assessment, error) {
		return nil, errors.New("cluster unreachable")
	}

	// Undetermined is not a controller error: returns nil, reports Unknown.
	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler returned %v, want nil", err)
	}
	cond := healthCondition(client.Actions())
	if cond == nil || cond.Status != metav1.ConditionUnknown || cond.Reason != reasonUnknown {
		t.Errorf("Healthy = %+v, want Unknown/Unknown", cond)
	}
}

func TestHealth_RequestsSyncOnWaveComplete(t *testing.T) {
	c, _ := newTestHealthController(t, withAssessment(testRelease(true)))
	progressing := corev1alpha1.ResourceStateProgressing
	// Frontier wave (1) is now fully healthy, having transitioned from progressing.
	c.assessFn = func(context.Context, *corev1alpha1.Release) (*assessment, error) {
		return &assessment{
			result:        &healthresult.Result{Objects: []healthresult.ObjectHealth{{Kind: deploymentKind, Name: svcName, State: healthresult.StateHealthy}}},
			entries:       []healthEntry{waveEntry("gate", 0, corev1alpha1.ResourceStateHealthy), waveEntry(svcName, 1, corev1alpha1.ResourceStateHealthy)},
			prior:         map[string]corev1alpha1.ResourceState{waveEntry("gate", 0, "").ref.identityID(): corev1alpha1.ResourceStateHealthy, waveEntry(svcName, 1, "").ref.identityID(): progressing},
			renderCurrent: true,
		}, nil
	}
	var synced int
	c.syncFn = func(context.Context, *corev1alpha1.Release) error { synced++; return nil }

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if synced != 1 {
		t.Errorf("requestSync called %d times, want 1 (frontier wave completed)", synced)
	}
}

func TestHealth_NoSyncWhileFrontierProgressing(t *testing.T) {
	c, _ := newTestHealthController(t, withAssessment(testRelease(true)))
	c.assessFn = func(context.Context, *corev1alpha1.Release) (*assessment, error) {
		return &assessment{
			result:        &healthresult.Result{Objects: []healthresult.ObjectHealth{{Kind: deploymentKind, Name: svcName, State: healthresult.StateProgressing}}},
			entries:       []healthEntry{waveEntry("gate", 0, corev1alpha1.ResourceStateHealthy), waveEntry(svcName, 1, corev1alpha1.ResourceStateProgressing)},
			prior:         map[string]corev1alpha1.ResourceState{},
			renderCurrent: true,
		}, nil
	}
	var synced int
	c.syncFn = func(context.Context, *corev1alpha1.Release) error { synced++; return nil }

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if synced != 0 {
		t.Errorf("requestSync called %d times, want 0 (frontier still progressing)", synced)
	}
}

func TestHealth_PendingWhenRenderNotCurrent(t *testing.T) {
	c, client := newTestHealthController(t, withAssessment(testRelease(true)))
	// Resources are Healthy but belong to a prior render (renderCurrent=false): a
	// re-render is in flight, or nothing of the current render is deployed yet.
	c.assessFn = func(context.Context, *corev1alpha1.Release) (*assessment, error) {
		return &assessment{
			result:        &healthresult.Result{Objects: []healthresult.ObjectHealth{{Kind: deploymentKind, Name: svcName, State: healthresult.StateHealthy}}},
			entries:       []healthEntry{waveEntry(svcName, 0, corev1alpha1.ResourceStateHealthy)},
			renderCurrent: false,
		}, nil
	}
	c.syncFn = func(context.Context, *corev1alpha1.Release) error { return nil }
	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	cond := healthCondition(client.Actions())
	if cond == nil || cond.Status != metav1.ConditionUnknown || cond.Reason != reasonPending {
		t.Errorf("Healthy = %+v, want Unknown/Pending (render not current)", cond)
	}
}

func TestHealth_DuringDeletion_PollsHooksNotHealth(t *testing.T) {
	rel := withAssessment(testRelease(true))
	now := metav1.Now()
	rel.DeletionTimestamp = &now
	c, _ := newTestHealthController(t, rel)

	assessed := false
	c.assessFn = func(context.Context, *corev1alpha1.Release) (*assessment, error) {
		assessed = true
		return stubAssessment(), nil
	}
	polled := false
	c.pollHooksFn = func(context.Context, *corev1alpha1.Release) (*hookPollResult, error) {
		polled = true
		return &hookPollResult{allSettled: true}, nil
	}
	var synced int
	c.syncFn = func(context.Context, *corev1alpha1.Release) error { synced++; return nil }

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if assessed {
		t.Error("assessed health during deletion; want health skipped")
	}
	if !polled {
		t.Error("did not poll hooks during deletion; want hook polling")
	}
	if synced != 0 {
		t.Errorf("requested sync %d times with no completed hook, want 0", synced)
	}
}

func TestHealth_DuringDeletion_PokesSyncOnHookCompletion(t *testing.T) {
	rel := withAssessment(testRelease(true))
	now := metav1.Now()
	rel.DeletionTimestamp = &now
	c, _ := newTestHealthController(t, rel)

	c.pollHooksFn = func(context.Context, *corev1alpha1.Release) (*hookPollResult, error) {
		return &hookPollResult{batchCompleted: true, allSettled: true}, nil
	}
	var synced int
	c.syncFn = func(context.Context, *corev1alpha1.Release) error { synced++; return nil }

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if synced != 1 {
		t.Errorf("requested sync %d times on delete-hook completion, want 1", synced)
	}
}

func TestHealth_ProgressingWhenNoWorse(t *testing.T) {
	c, client := newTestHealthController(t, withAssessment(testRelease(true)))
	c.assessFn = func(context.Context, *corev1alpha1.Release) (*assessment, error) {
		return stubAssessment(
			healthresult.ObjectHealth{Kind: deploymentKind, Name: "a", State: healthresult.StateHealthy},
			healthresult.ObjectHealth{Kind: deploymentKind, Name: "b", State: healthresult.StateProgressing},
		), nil
	}

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	cond := healthCondition(client.Actions())
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != reasonProgressing {
		t.Errorf("Healthy = %+v, want False/Progressing", cond)
	}
}
