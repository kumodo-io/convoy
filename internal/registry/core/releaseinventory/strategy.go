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

package releaseinventory

import (
	"context"
	"fmt"

	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/names"

	"github.com/kumodo-io/convoy/api/core"
)

// NewStrategy returns the create/update/delete strategy for ReleaseInventory.
func NewStrategy(typer runtime.ObjectTyper) Strategy {
	return Strategy{typer, names.SimpleNameGenerator}
}

// Strategy implements the behavior for ReleaseInventory objects. The resource is
// namespaced and has no status subresource — the whole object is machine-written
// (the release controller and the observer co-own its inventory entries via
// Server-Side Apply), so there is no user-authored spec to guard.
type Strategy struct {
	runtime.ObjectTyper
	names.NameGenerator
}

var (
	_ rest.RESTCreateStrategy = Strategy{}
	_ rest.RESTUpdateStrategy = Strategy{}
	_ rest.RESTDeleteStrategy = Strategy{}
)

// NamespaceScoped returns true: ReleaseInventory is namespaced (colocated with
// its Release).
func (Strategy) NamespaceScoped() bool { return true }

// PrepareForCreate is a no-op: every field is data written by its owner.
func (Strategy) PrepareForCreate(_ context.Context, _ runtime.Object) {}

// PrepareForUpdate is a no-op.
func (Strategy) PrepareForUpdate(_ context.Context, _, _ runtime.Object) {}

// Validate validates a new ReleaseInventory: object metadata only (the inventory
// entries are machine-produced).
func (Strategy) Validate(_ context.Context, obj runtime.Object) field.ErrorList {
	inv := obj.(*core.ReleaseInventory)
	return apivalidation.ValidateObjectMeta(&inv.ObjectMeta, true, apivalidation.NameIsDNSSubdomain, field.NewPath("metadata"))
}

// WarningsOnCreate returns no warnings.
func (Strategy) WarningsOnCreate(_ context.Context, _ runtime.Object) []string { return nil }

// Canonicalize is a no-op.
func (Strategy) Canonicalize(_ runtime.Object) {}

// AllowCreateOnUpdate allows create-on-update so the controller/observer can
// upsert the inventory via a single Apply without a prior create.
func (Strategy) AllowCreateOnUpdate() bool { return true }

// AllowUnconditionalUpdate allows unconditional update.
func (Strategy) AllowUnconditionalUpdate() bool { return true }

// ValidateUpdate validates a ReleaseInventory update (object metadata only).
func (Strategy) ValidateUpdate(_ context.Context, obj, old runtime.Object) field.ErrorList {
	inv := obj.(*core.ReleaseInventory)
	oldInv := old.(*core.ReleaseInventory)
	return apivalidation.ValidateObjectMetaUpdate(&inv.ObjectMeta, &oldInv.ObjectMeta, field.NewPath("metadata"))
}

// WarningsOnUpdate returns no warnings.
func (Strategy) WarningsOnUpdate(_ context.Context, _, _ runtime.Object) []string { return nil }

// GetAttrs returns the labels and fields of a ReleaseInventory for filtering.
func GetAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
	inv, ok := obj.(*core.ReleaseInventory)
	if !ok {
		return nil, nil, fmt.Errorf("given object is not a ReleaseInventory")
	}
	return labels.Set(inv.Labels), selectableFields(inv), nil
}

// MatchReleaseInventory returns a selection predicate for the given selectors.
func MatchReleaseInventory(label labels.Selector, fld fields.Selector) storage.SelectionPredicate {
	return storage.SelectionPredicate{
		Label:    label,
		Field:    fld,
		GetAttrs: GetAttrs,
	}
}

func selectableFields(obj *core.ReleaseInventory) fields.Set {
	return generic.ObjectMetaFieldsSet(&obj.ObjectMeta, true)
}
