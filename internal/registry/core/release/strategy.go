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
	"fmt"
	"time"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/names"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"

	"github.com/kumodo-io/convoy/api/core"
	"github.com/kumodo-io/convoy/api/core/v1alpha1"
)

const v1alpha1Version = "core.convoy.kumodo.io/v1alpha1"

// NewStrategy returns the create/update/delete strategy for Release.
func NewStrategy(typer runtime.ObjectTyper) Strategy {
	return Strategy{typer, names.SimpleNameGenerator}
}

// Strategy implements the behavior for Release objects on the main endpoint.
type Strategy struct {
	runtime.ObjectTyper
	names.NameGenerator
}

var (
	_ rest.RESTCreateStrategy = Strategy{}
	_ rest.RESTUpdateStrategy = Strategy{}
	_ rest.RESTDeleteStrategy = Strategy{}
)

// NamespaceScoped returns true: Release objects are namespaced.
func (Strategy) NamespaceScoped() bool { return true }

// PrepareForCreate clears status and initializes the generation.
func (Strategy) PrepareForCreate(_ context.Context, obj runtime.Object) {
	release := obj.(*core.Release)
	release.Status = core.ReleaseStatus{}
	release.Generation = 1
}

// PrepareForUpdate preserves status (mutable only via /status) and bumps the
// generation when the spec changes.
func (Strategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	newRelease := obj.(*core.Release)
	oldRelease := old.(*core.Release)
	newRelease.Status = oldRelease.Status
	if !apiequality.Semantic.DeepEqual(newRelease.Spec, oldRelease.Spec) {
		newRelease.Generation = oldRelease.Generation + 1
	}
}

// Validate validates a new Release.
func (Strategy) Validate(_ context.Context, obj runtime.Object) field.ErrorList {
	return core.ValidateRelease(obj.(*core.Release))
}

// WarningsOnCreate returns no warnings.
func (Strategy) WarningsOnCreate(_ context.Context, _ runtime.Object) []string { return nil }

// Canonicalize is a no-op.
func (Strategy) Canonicalize(_ runtime.Object) {}

// AllowCreateOnUpdate disallows create-on-update.
func (Strategy) AllowCreateOnUpdate() bool { return false }

// AllowUnconditionalUpdate disallows unconditional update.
func (Strategy) AllowUnconditionalUpdate() bool { return false }

// ValidateUpdate validates a Release spec/metadata update.
func (Strategy) ValidateUpdate(_ context.Context, obj, old runtime.Object) field.ErrorList {
	return core.ValidateReleaseUpdate(obj.(*core.Release), old.(*core.Release))
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

// StatusStrategy implements the behavior for the Release /status subresource.
type StatusStrategy struct {
	Strategy
}

// NewStatusStrategy returns the strategy for the /status subresource.
func NewStatusStrategy(strategy Strategy) StatusStrategy {
	return StatusStrategy{strategy}
}

// PrepareForUpdate preserves everything except status: only status is mutable
// via the /status subresource.
func (StatusStrategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	newRelease := obj.(*core.Release)
	oldRelease := old.(*core.Release)
	newRelease.Spec = oldRelease.Spec
	newRelease.Mutations = oldRelease.Mutations
	newRelease.Lifecycle = oldRelease.Lifecycle
	newRelease.Pin = oldRelease.Pin
	newRelease.Generation = oldRelease.Generation
}

// ValidateUpdate validates a Release status update.
func (StatusStrategy) ValidateUpdate(_ context.Context, obj, old runtime.Object) field.ErrorList {
	return core.ValidateReleaseStatusUpdate(obj.(*core.Release), old.(*core.Release))
}

// WarningsOnUpdate returns no warnings.
func (StatusStrategy) WarningsOnUpdate(_ context.Context, _, _ runtime.Object) []string { return nil }

// GetResetFields declares that the status endpoint owns only status.
func (StatusStrategy) GetResetFields() map[fieldpath.APIVersion]*fieldpath.Set {
	return map[fieldpath.APIVersion]*fieldpath.Set{
		v1alpha1Version: fieldpath.NewSet(
			fieldpath.MakePathOrDie("spec"),
			fieldpath.MakePathOrDie("mutations"),
			fieldpath.MakePathOrDie("lifecycle"),
			fieldpath.MakePathOrDie("pin"),
		),
	}
}

// PinStrategy implements the behavior for the Release /pin subresource: only the
// Pin field is mutable. A non-empty RenderRef pins (freeze); an empty RenderRef
// unpins. The server stamps PinnedAt/PinnedBy and a reconcile-request annotation
// (a Pin change does not bump generation, so the annotation wakes the controller).
type PinStrategy struct {
	Strategy
}

// NewPinStrategy returns the strategy for the /pin subresource.
func NewPinStrategy(strategy Strategy) PinStrategy {
	return PinStrategy{strategy}
}

// PrepareForUpdate preserves everything except Pin, fills server-managed pin
// fields, and stamps the reconcile-request annotation.
func (PinStrategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
	newRelease := obj.(*core.Release)
	oldRelease := old.(*core.Release)
	newRelease.Spec = oldRelease.Spec
	newRelease.Status = oldRelease.Status
	newRelease.Mutations = oldRelease.Mutations
	newRelease.Lifecycle = oldRelease.Lifecycle
	newRelease.Generation = oldRelease.Generation

	if newRelease.Pin == nil || newRelease.Pin.RenderRef == "" {
		newRelease.Pin = nil // unpin (nil or empty RenderRef both clear the pin)
	} else {
		newRelease.Pin.PinnedAt = metav1.Now()
		if u, ok := genericapirequest.UserFrom(ctx); ok {
			newRelease.Pin.PinnedBy = u.GetName()
		}
	}
	// A Pin change doesn't bump generation; stamp the reconcile-request annotation
	// so the controller reconciles promptly.
	metav1.SetMetaDataAnnotation(&newRelease.ObjectMeta, v1alpha1.SyncRequestedAtAnnotation, metav1.Now().UTC().Format(time.RFC3339Nano))
}

// ValidateUpdate allows only a freeze pin to the current render. Pinning to any
// other render (rollback) requires durable render history and is rejected here.
func (PinStrategy) ValidateUpdate(_ context.Context, obj, old runtime.Object) field.ErrorList {
	newRelease := obj.(*core.Release)
	oldRelease := old.(*core.Release)
	// PrepareForUpdate has already cleared an empty pin to nil, so a non-nil Pin
	// here always carries a RenderRef the caller intends to freeze.
	if newRelease.Pin == nil {
		return nil // unpin is always allowed
	}
	ref := newRelease.Pin.RenderRef
	current := ""
	if oldRelease.Status.LastSuccessful != nil {
		current = oldRelease.Status.LastSuccessful.RenderRef
	}
	if current == "" {
		return field.ErrorList{field.Forbidden(field.NewPath("pin", "renderRef"), "cannot pin before a successful render")}
	}
	if ref != current {
		return field.ErrorList{field.Invalid(field.NewPath("pin", "renderRef"), ref, "only the current render may be pinned (rollback requires render history)")}
	}
	return nil
}

// GetResetFields declares that the pin endpoint owns only pin.
func (PinStrategy) GetResetFields() map[fieldpath.APIVersion]*fieldpath.Set {
	return map[fieldpath.APIVersion]*fieldpath.Set{
		v1alpha1Version: fieldpath.NewSet(
			fieldpath.MakePathOrDie("spec"),
			fieldpath.MakePathOrDie("status"),
			fieldpath.MakePathOrDie("mutations"),
			fieldpath.MakePathOrDie("lifecycle"),
		),
	}
}

// GetAttrs returns the labels and fields of a Release for filtering/indexing.
func GetAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
	release, ok := obj.(*core.Release)
	if !ok {
		return nil, nil, fmt.Errorf("given object is not a Release")
	}
	return labels.Set(release.Labels), selectableFields(release), nil
}

// MatchRelease returns a selection predicate for the given label/field selectors.
func MatchRelease(label labels.Selector, fld fields.Selector) storage.SelectionPredicate {
	return storage.SelectionPredicate{
		Label:    label,
		Field:    fld,
		GetAttrs: GetAttrs,
	}
}

func selectableFields(obj *core.Release) fields.Set {
	releaseFields := fields.Set{
		"spec.clusterRef.name":      obj.Spec.ClusterRef.Name,
		"spec.clusterRef.namespace": obj.Spec.ClusterRef.Namespace,
	}
	return generic.MergeFieldsSets(generic.ObjectMetaFieldsSet(&obj.ObjectMeta, true), releaseFields)
}
