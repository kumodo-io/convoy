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

package health

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	extensionsv1alpha1 "github.com/kumodo-io/convoy/api/extensions/v1alpha1"
)

const (
	deploymentsResource  = "deployments"
	deploymentDesiredNum = 3
)

func deploymentRule(expr, msg string) extensionsv1alpha1.RuleSpec {
	return extensionsv1alpha1.RuleSpec{
		ApiVersion:        "apps/v1",
		Resources:         []string{deploymentsResource},
		RuleExpression:    expr,
		MessageExpression: msg,
	}
}

func deployment(available int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "app", "namespace": "ns"},
		"spec":       map[string]any{"replicas": deploymentDesiredNum},
		"status":     map[string]any{"availableReplicas": available},
	}}
}

func TestEvaluator_Passes(t *testing.T) {
	e, err := NewEvaluator([]extensionsv1alpha1.RuleSpec{
		deploymentRule("object.status.availableReplicas == object.spec.replicas", "'not enough replicas'"),
	})
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}
	got := e.Assess(deployment(3), []string{deploymentsResource})
	if !got.Passed || got.Err != nil {
		t.Errorf("Assess = %+v, want Passed with no error", got)
	}
}

func TestEvaluator_FailsWithMessage(t *testing.T) {
	e, err := NewEvaluator([]extensionsv1alpha1.RuleSpec{
		deploymentRule("object.status.availableReplicas == object.spec.replicas",
			"'only ' + string(object.status.availableReplicas) + '/' + string(object.spec.replicas)"),
	})
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}
	got := e.Assess(deployment(1), []string{deploymentsResource})
	if got.Passed {
		t.Fatalf("Assess passed, want failed")
	}
	if got.Message != "only 1/3" {
		t.Errorf("message = %q, want %q", got.Message, "only 1/3")
	}
}

func TestEvaluator_Conjunction(t *testing.T) {
	// Two rules apply to deployments; the object satisfies the first but not the
	// second → overall fail (conjunction).
	e, err := NewEvaluator([]extensionsv1alpha1.RuleSpec{
		deploymentRule("object.spec.replicas > 0", ""),
		deploymentRule("object.status.availableReplicas == object.spec.replicas", "'not all available'"),
	})
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}
	if got := e.Assess(deployment(1), []string{deploymentsResource}); got.Passed {
		t.Error("Assess passed, want failed on the second rule")
	}
}

func TestEvaluator_NoApplicableRulePassesVacuously(t *testing.T) {
	e, err := NewEvaluator([]extensionsv1alpha1.RuleSpec{deploymentRule("false", "")})
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}
	// A ConfigMap has no applicable rule → passes, and HasRules is false.
	cm := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": "cm", "namespace": "ns"},
	}}
	if got := e.Assess(cm, []string{"configmaps"}); !got.Passed {
		t.Errorf("Assess = %+v, want vacuous pass for an unmatched object", got)
	}
	if e.HasRules("v1", []string{"configmaps"}) {
		t.Error("HasRules = true for an unmatched object, want false")
	}
	if !e.HasRules("apps/v1", []string{deploymentsResource}) {
		t.Error("HasRules = false for a matched object, want true")
	}
}

func TestEvaluator_NonBoolIsError(t *testing.T) {
	e, err := NewEvaluator([]extensionsv1alpha1.RuleSpec{deploymentRule("object.spec.replicas", "")})
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}
	got := e.Assess(deployment(3), []string{deploymentsResource})
	if got.Err == nil {
		t.Error("Assess err = nil, want an error for a non-bool ruleExpression")
	}
}

func TestNewEvaluator_CompileErrorRejectsRuleSet(t *testing.T) {
	_, err := NewEvaluator([]extensionsv1alpha1.RuleSpec{deploymentRule("object.spec.(", "")})
	if err == nil {
		t.Error("NewEvaluator err = nil, want a compile error")
	}
}
