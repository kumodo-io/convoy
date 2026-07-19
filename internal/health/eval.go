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

// Package health is the pure health-assessment engine: compiling and evaluating
// the CEL rules of a health rule set against live cluster objects, matching rules
// to objects by API version + resource, and deriving a health state from an
// assessment plus a progress deadline. It holds no Kubernetes clients and no
// state, so it is fully unit-testable in isolation; the observer's health loop
// drives it.
package health

import (
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	extensionsv1alpha1 "github.com/kumodo-io/convoy/api/extensions/v1alpha1"
)

// objectVar is the CEL variable bound to the live resource being assessed.
const (
	objectVar        = "object"
	healthRuleFailed = "health rule failed"
)

// compiledRule is a rule with its CEL programs compiled once for reuse.
type compiledRule struct {
	rule      extensionsv1alpha1.RuleSpec
	predicate cel.Program
	message   cel.Program // may be nil when the rule has no MessageExpression
}

// Assessment is the outcome of evaluating an object's applicable rules.
type Assessment struct {
	// Passed is true when every applicable rule's predicate evaluated to true.
	// An object with no applicable rules passes vacuously.
	Passed bool
	// Message is a human-readable summary — the message of the first failing rule,
	// or "" when all passed.
	Message string
	// Err is set when a rule could not be evaluated (compile/eval error). The
	// caller maps this to the Unknown state.
	Err error
}

// Evaluator compiles a set of rules once and assesses live objects against them.
// It is safe to reuse across objects but is rebuilt when the rule set changes.
type Evaluator struct {
	env   *cel.Env
	rules []compiledRule
}

// NewEvaluator compiles the given rules. A compile error in any rule is returned
// immediately (the whole rule set is rejected rather than silently dropping a
// rule).
func NewEvaluator(rules []extensionsv1alpha1.RuleSpec) (*Evaluator, error) {
	env, err := cel.NewEnv(cel.Variable(objectVar, cel.DynType))
	if err != nil {
		return nil, fmt.Errorf("building CEL env: %w", err)
	}

	compiled := make([]compiledRule, 0, len(rules))
	for i := range rules {
		r := rules[i]
		predicate, err := compileProgram(env, r.RuleExpression)
		if err != nil {
			return nil, fmt.Errorf("rule %d ruleExpression: %w", i, err)
		}
		cr := compiledRule{rule: r, predicate: predicate}
		if r.MessageExpression != "" {
			msg, err := compileProgram(env, r.MessageExpression)
			if err != nil {
				return nil, fmt.Errorf("rule %d messageExpression: %w", i, err)
			}
			cr.message = msg
		}
		compiled = append(compiled, cr)
	}
	return &Evaluator{env: env, rules: compiled}, nil
}

// Assess evaluates every rule applicable to obj (matched by API version +
// resource) and returns the conjunction: the object passes only when all
// applicable predicates are true. rules names the resources of obj's kind (from
// the caller's RESTMapper) so a rule targeting those resources applies. An empty
// applicable set passes vacuously (no message).
func (e *Evaluator) Assess(obj *unstructured.Unstructured, resources []string) Assessment {
	input := map[string]any{objectVar: obj.Object}
	assessed := false
	for _, cr := range e.rules {
		if !ruleApplies(cr.rule, obj.GetAPIVersion(), resources) {
			continue
		}
		assessed = true
		passed, err := evalBool(cr.predicate, input)
		if err != nil {
			return Assessment{Err: fmt.Errorf("evaluating ruleExpression: %w", err)}
		}
		if !passed {
			return Assessment{Passed: false, Message: e.messageFor(cr, input)}
		}
	}
	_ = assessed // an all-passed or no-rule object is Passed=true; the caller
	// distinguishes "no rules" via HasRules if it needs a distinct message.
	return Assessment{Passed: true}
}

// HasRules reports whether any compiled rule applies to an object of the given
// API version + resources. The caller uses it to label a no-rule object.
func (e *Evaluator) HasRules(apiVersion string, resources []string) bool {
	for _, cr := range e.rules {
		if ruleApplies(cr.rule, apiVersion, resources) {
			return true
		}
	}
	return false
}

// messageFor evaluates a failing rule's message expression, falling back to a
// generic message when there is none or it errors (a message-eval failure must
// not mask the health verdict).
func (e *Evaluator) messageFor(cr compiledRule, input map[string]any) string {
	if cr.message == nil {
		return healthRuleFailed
	}
	out, _, err := cr.message.Eval(input)
	if err != nil {
		return healthRuleFailed
	}
	if s, ok := out.Value().(string); ok {
		return s
	}
	return healthRuleFailed
}

func compileProgram(env *cel.Env, expr string) (cel.Program, error) {
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	return env.Program(ast)
}

// evalBool evaluates a predicate program and coerces the result to bool. A
// non-bool result is an error (a rule expression must yield a boolean).
func evalBool(p cel.Program, input map[string]any) (bool, error) {
	out, _, err := p.Eval(input)
	if err != nil {
		return false, err
	}
	return coerceBool(out)
}

func coerceBool(v ref.Val) (bool, error) {
	b, ok := v.Value().(bool)
	if !ok {
		return false, fmt.Errorf("ruleExpression did not evaluate to a bool (got %T)", v.Value())
	}
	return b, nil
}
