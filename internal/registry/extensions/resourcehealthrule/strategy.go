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

package resourcehealthrule

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

// NewStrategy returns the create/update/delete strategy for ResourceHealthRule.
func NewStrategy(typer runtime.ObjectTyper) Strategy {
	return Strategy{typer, names.SimpleNameGenerator}
}

// Strategy implements the behavior for ResourceHealthRule objects. The resource
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

// NamespaceScoped returns true: ResourceHealthRule is namespaced.
func (Strategy) NamespaceScoped() bool { return true }

// PrepareForCreate initializes the generation.
func (Strategy) PrepareForCreate(_ context.Context, obj runtime.Object) {
	rule := obj.(*extensions.ResourceHealthRule)
	rule.Generation = 1
}

// PrepareForUpdate bumps the generation when the spec changes.
func (Strategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	newRule := obj.(*extensions.ResourceHealthRule)
	oldRule := old.(*extensions.ResourceHealthRule)
	if !apiequality.Semantic.DeepEqual(newRule.Spec, oldRule.Spec) {
		newRule.Generation = oldRule.Generation + 1
	}
}

// Validate validates a new ResourceHealthRule.
func (Strategy) Validate(_ context.Context, obj runtime.Object) field.ErrorList {
	return extensions.ValidateResourceHealthRule(obj.(*extensions.ResourceHealthRule))
}

// WarningsOnCreate returns no warnings.
func (Strategy) WarningsOnCreate(_ context.Context, _ runtime.Object) []string { return nil }

// Canonicalize is a no-op.
func (Strategy) Canonicalize(_ runtime.Object) {}

// AllowCreateOnUpdate disallows create-on-update.
func (Strategy) AllowCreateOnUpdate() bool { return false }

// AllowUnconditionalUpdate disallows unconditional update.
func (Strategy) AllowUnconditionalUpdate() bool { return false }

// ValidateUpdate validates a ResourceHealthRule spec/metadata update.
func (Strategy) ValidateUpdate(_ context.Context, obj, old runtime.Object) field.ErrorList {
	return extensions.ValidateResourceHealthRuleUpdate(obj.(*extensions.ResourceHealthRule), old.(*extensions.ResourceHealthRule))
}

// WarningsOnUpdate returns no warnings.
func (Strategy) WarningsOnUpdate(_ context.Context, _, _ runtime.Object) []string { return nil }

// GetAttrs returns the labels and fields of a ResourceHealthRule for filtering.
func GetAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
	rule, ok := obj.(*extensions.ResourceHealthRule)
	if !ok {
		return nil, nil, fmt.Errorf("given object is not a ResourceHealthRule")
	}
	return labels.Set(rule.ObjectMeta.Labels), selectableFields(rule), nil
}

// MatchResourceHealthRule returns a selection predicate for the given selectors.
func MatchResourceHealthRule(label labels.Selector, fld fields.Selector) storage.SelectionPredicate {
	return storage.SelectionPredicate{
		Label:    label,
		Field:    fld,
		GetAttrs: GetAttrs,
	}
}

func selectableFields(obj *extensions.ResourceHealthRule) fields.Set {
	return generic.ObjectMetaFieldsSet(&obj.ObjectMeta, true)
}
