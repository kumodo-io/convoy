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

package helmregistry

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
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"

	"github.com/kumodo-io/convoy/api/source"
)

const v1alpha1Version = "source.convoy.kumodo.io/v1alpha1"

// NewStrategy returns the create/update/delete strategy for HelmRegistry.
func NewStrategy(typer runtime.ObjectTyper) Strategy {
	return Strategy{typer, names.SimpleNameGenerator}
}

// Strategy implements the behavior for HelmRegistry objects on the main endpoint.
type Strategy struct {
	runtime.ObjectTyper
	names.NameGenerator
}

var (
	_ rest.RESTCreateStrategy = Strategy{}
	_ rest.RESTUpdateStrategy = Strategy{}
	_ rest.RESTDeleteStrategy = Strategy{}
)

// NamespaceScoped returns true: HelmRegistry objects are namespaced.
func (Strategy) NamespaceScoped() bool { return true }

// PrepareForCreate clears status and initializes the generation.
func (Strategy) PrepareForCreate(_ context.Context, obj runtime.Object) {
	reg := obj.(*source.HelmRegistry)
	reg.Status = source.HelmRegistryStatus{}
	reg.Generation = 1
}

// PrepareForUpdate preserves status (mutable only via /status) and bumps the
// generation when the spec changes.
func (Strategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	newReg := obj.(*source.HelmRegistry)
	oldReg := old.(*source.HelmRegistry)
	newReg.Status = oldReg.Status
	if !apiequality.Semantic.DeepEqual(newReg.Spec, oldReg.Spec) {
		newReg.Generation = oldReg.Generation + 1
	}
}

// Validate validates a new HelmRegistry.
func (Strategy) Validate(_ context.Context, obj runtime.Object) field.ErrorList {
	return source.ValidateHelmRegistry(obj.(*source.HelmRegistry))
}

// WarningsOnCreate returns no warnings.
func (Strategy) WarningsOnCreate(_ context.Context, _ runtime.Object) []string { return nil }

// Canonicalize is a no-op.
func (Strategy) Canonicalize(_ runtime.Object) {}

// AllowCreateOnUpdate disallows create-on-update.
func (Strategy) AllowCreateOnUpdate() bool { return false }

// AllowUnconditionalUpdate disallows unconditional update.
func (Strategy) AllowUnconditionalUpdate() bool { return false }

// ValidateUpdate validates a HelmRegistry spec/metadata update.
func (Strategy) ValidateUpdate(_ context.Context, obj, old runtime.Object) field.ErrorList {
	return source.ValidateHelmRegistryUpdate(obj.(*source.HelmRegistry), old.(*source.HelmRegistry))
}

// WarningsOnUpdate returns no warnings.
func (Strategy) WarningsOnUpdate(_ context.Context, _, _ runtime.Object) []string { return nil }

// GetResetFields declares that the main endpoint owns everything except status.
func (Strategy) GetResetFields() map[fieldpath.APIVersion]*fieldpath.Set {
	return map[fieldpath.APIVersion]*fieldpath.Set{
		v1alpha1Version: fieldpath.NewSet(
			fieldpath.MakePathOrDie("status"),
		),
	}
}

// StatusStrategy implements the behavior for the HelmRegistry /status subresource.
type StatusStrategy struct {
	Strategy
}

// NewStatusStrategy returns the strategy for the /status subresource.
func NewStatusStrategy(strategy Strategy) StatusStrategy {
	return StatusStrategy{strategy}
}

// PrepareForUpdate preserves everything except status.
func (StatusStrategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	newReg := obj.(*source.HelmRegistry)
	oldReg := old.(*source.HelmRegistry)
	newReg.Spec = oldReg.Spec
	newReg.Webhook = oldReg.Webhook
	newReg.Generation = oldReg.Generation
}

// ValidateUpdate validates a HelmRegistry status update.
func (StatusStrategy) ValidateUpdate(_ context.Context, obj, old runtime.Object) field.ErrorList {
	return source.ValidateHelmRegistryStatusUpdate(obj.(*source.HelmRegistry), old.(*source.HelmRegistry))
}

// WarningsOnUpdate returns no warnings.
func (StatusStrategy) WarningsOnUpdate(_ context.Context, _, _ runtime.Object) []string { return nil }

// GetResetFields declares that the status endpoint owns only status.
func (StatusStrategy) GetResetFields() map[fieldpath.APIVersion]*fieldpath.Set {
	return map[fieldpath.APIVersion]*fieldpath.Set{
		v1alpha1Version: fieldpath.NewSet(
			fieldpath.MakePathOrDie("spec"),
			fieldpath.MakePathOrDie("webhook"),
		),
	}
}

// GetAttrs returns the labels and fields of a HelmRegistry for filtering.
func GetAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
	reg, ok := obj.(*source.HelmRegistry)
	if !ok {
		return nil, nil, fmt.Errorf("given object is not a HelmRegistry")
	}
	return labels.Set(reg.Labels), selectableFields(reg), nil
}

// MatchHelmRegistry returns a selection predicate for the given selectors.
func MatchHelmRegistry(label labels.Selector, fld fields.Selector) storage.SelectionPredicate {
	return storage.SelectionPredicate{
		Label:    label,
		Field:    fld,
		GetAttrs: GetAttrs,
	}
}

func selectableFields(obj *source.HelmRegistry) fields.Set {
	return generic.ObjectMetaFieldsSet(&obj.ObjectMeta, true)
}
