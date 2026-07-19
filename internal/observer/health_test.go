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

const deploymentKind = "Deployment"

func newTestHealthController(t *testing.T, rel *corev1alpha1.Release) (*HealthController, *fakeclient.Clientset) {
	t.Helper()
	client := fakeclient.NewSimpleClientset(rel)
	factory := informers.NewSharedInformerFactory(client, 0)
	relInformer := factory.Core().V1alpha1().Releases()
	clInformer := factory.Core().V1alpha1().Clusters()
	globalRuleSets := factory.Extensions().V1alpha1().GlobalHealthRuleSets()
	ruleSets := factory.Extensions().V1alpha1().HealthRuleSets()
	if err := relInformer.Informer().GetStore().Add(rel); err != nil {
		t.Fatalf("seeding release store: %v", err)
	}
	c := NewHealthController(client, relInformer, clInformer.Lister(), nil, nil, nil,
		globalRuleSets.Lister(), ruleSets.Lister())
	fixed := metav1.NewTime(time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC))
	c.now = func() metav1.Time { return fixed }
	return c, client
}

func healthCondition(actions []clienttesting.Action) *metav1.Condition {
	var found *metav1.Condition
	for _, a := range actions {
		p, ok := a.(clienttesting.PatchAction)
		if !ok || !a.Matches("patch", "releases") || a.GetSubresource() != "status" || p.GetPatchType() != types.ApplyPatchType {
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
	c.assessFn = func(context.Context, *corev1alpha1.Release) (*healthresult.Result, error) {
		return &healthresult.Result{Objects: []healthresult.ObjectHealth{
			{Kind: deploymentKind, Name: "app", State: healthresult.StateHealthy},
		}}, nil
	}

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	cond := healthCondition(client.Actions())
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != reasonHealthy {
		t.Errorf("Healthy = %+v, want True/Healthy", cond)
	}
}

func TestHealth_DegradedWorstStateWins(t *testing.T) {
	c, client := newTestHealthController(t, withAssessment(testRelease(true)))
	c.assessFn = func(context.Context, *corev1alpha1.Release) (*healthresult.Result, error) {
		// A mix: progressing + degraded + unknown → Degraded is the worst.
		return &healthresult.Result{Objects: []healthresult.ObjectHealth{
			{Kind: deploymentKind, Name: "a", State: healthresult.StateProgressing},
			{Kind: deploymentKind, Name: "b", State: healthresult.StateDegraded},
			{Kind: deploymentKind, Name: "c", State: healthresult.StateUnknown},
		}}, nil
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
	c.assessFn = func(context.Context, *corev1alpha1.Release) (*healthresult.Result, error) {
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

func TestHealth_ProgressingWhenNoWorse(t *testing.T) {
	c, client := newTestHealthController(t, withAssessment(testRelease(true)))
	c.assessFn = func(context.Context, *corev1alpha1.Release) (*healthresult.Result, error) {
		return &healthresult.Result{Objects: []healthresult.ObjectHealth{
			{Kind: deploymentKind, Name: "a", State: healthresult.StateHealthy},
			{Kind: deploymentKind, Name: "b", State: healthresult.StateProgressing},
		}}, nil
	}

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	cond := healthCondition(client.Actions())
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != reasonProgressing {
		t.Errorf("Healthy = %+v, want False/Progressing", cond)
	}
}
