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

package healthruleset

import (
	"context"
	"fmt"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/names"

	"github.com/kumodo-io/convoy/api/extensions"
)

// NewStrategy returns the create/update/delete strategy for HealthRuleSet.
func NewStrategy(typer runtime.ObjectTyper) Strategy {
	return Strategy{typer, names.SimpleNameGenerator}
}

// Strategy implements the behavior for HealthRuleSet objects. The resource
// is namespaced and has no status subresource.
type Strategy struct {
	runtime.ObjectTyper
	names.NameGenerator
}

var (
	_ rest.RESTCreateStrategy = Strategy{}
	_ rest.RESTUpdateStrategy = Strategy{}
	_ rest.RESTDeleteStrategy = Strategy{}
)

// NamespaceScoped returns true: HealthRuleSet is namespaced.
func (Strategy) NamespaceScoped() bool { return true }

// PrepareForCreate initializes the generation.
func (Strategy) PrepareForCreate(_ context.Context, obj runtime.Object) {
	rule := obj.(*extensions.HealthRuleSet)
	rule.Generation = 1
}

// PrepareForUpdate bumps the generation when the spec changes.
func (Strategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	newRule := obj.(*extensions.HealthRuleSet)
	oldRule := old.(*extensions.HealthRuleSet)
	if !apiequality.Semantic.DeepEqual(newRule.Spec, oldRule.Spec) {
		newRule.Generation = oldRule.Generation + 1
	}
}

// Validate validates a new HealthRuleSet.
func (Strategy) Validate(_ context.Context, obj runtime.Object) field.ErrorList {
	return extensions.ValidateHealthRuleSet(obj.(*extensions.HealthRuleSet))
}

// WarningsOnCreate returns no warnings.
func (Strategy) WarningsOnCreate(_ context.Context, _ runtime.Object) []string { return nil }

// Canonicalize is a no-op.
func (Strategy) Canonicalize(_ runtime.Object) {}

// AllowCreateOnUpdate disallows create-on-update.
func (Strategy) AllowCreateOnUpdate() bool { return false }

// AllowUnconditionalUpdate disallows unconditional update.
func (Strategy) AllowUnconditionalUpdate() bool { return false }

// ValidateUpdate validates a HealthRuleSet spec/metadata update.
func (Strategy) ValidateUpdate(_ context.Context, obj, old runtime.Object) field.ErrorList {
	return extensions.ValidateHealthRuleSetUpdate(obj.(*extensions.HealthRuleSet), old.(*extensions.HealthRuleSet))
}

// WarningsOnUpdate returns no warnings.
func (Strategy) WarningsOnUpdate(_ context.Context, _, _ runtime.Object) []string { return nil }

// GetAttrs returns the labels and fields of a HealthRuleSet for filtering.
func GetAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
	rule, ok := obj.(*extensions.HealthRuleSet)
	if !ok {
		return nil, nil, fmt.Errorf("given object is not a HealthRuleSet")
	}
	return labels.Set(rule.Labels), selectableFields(rule), nil
}

// MatchHealthRuleSet returns a selection predicate for the given selectors.
func MatchHealthRuleSet(label labels.Selector, fld fields.Selector) storage.SelectionPredicate {
	return storage.SelectionPredicate{
		Label:    label,
		Field:    fld,
		GetAttrs: GetAttrs,
	}
}

func selectableFields(obj *extensions.HealthRuleSet) fields.Set {
	return generic.ObjectMetaFieldsSet(&obj.ObjectMeta, true)
}
