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

package gitrepository

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

// NewStrategy returns the create/update/delete strategy for GitRepository.
func NewStrategy(typer runtime.ObjectTyper) Strategy {
	return Strategy{typer, names.SimpleNameGenerator}
}

// Strategy implements the behavior for GitRepository objects on the main endpoint.
type Strategy struct {
	runtime.ObjectTyper
	names.NameGenerator
}

var (
	_ rest.RESTCreateStrategy = Strategy{}
	_ rest.RESTUpdateStrategy = Strategy{}
	_ rest.RESTDeleteStrategy = Strategy{}
)

// NamespaceScoped returns true: GitRepository objects are namespaced.
func (Strategy) NamespaceScoped() bool { return true }

// PrepareForCreate clears status and initializes the generation.
func (Strategy) PrepareForCreate(_ context.Context, obj runtime.Object) {
	repo := obj.(*source.GitRepository)
	repo.Status = source.GitRepositoryStatus{}
	repo.Generation = 1
}

// PrepareForUpdate preserves status (mutable only via /status) and bumps the
// generation when the spec changes.
func (Strategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	newRepo := obj.(*source.GitRepository)
	oldRepo := old.(*source.GitRepository)
	newRepo.Status = oldRepo.Status
	if !apiequality.Semantic.DeepEqual(newRepo.Spec, oldRepo.Spec) {
		newRepo.Generation = oldRepo.Generation + 1
	}
}

// Validate validates a new GitRepository.
func (Strategy) Validate(_ context.Context, obj runtime.Object) field.ErrorList {
	return source.ValidateGitRepository(obj.(*source.GitRepository))
}

// WarningsOnCreate returns no warnings.
func (Strategy) WarningsOnCreate(_ context.Context, _ runtime.Object) []string { return nil }

// Canonicalize is a no-op.
func (Strategy) Canonicalize(_ runtime.Object) {}

// AllowCreateOnUpdate disallows create-on-update.
func (Strategy) AllowCreateOnUpdate() bool { return false }

// AllowUnconditionalUpdate disallows unconditional update.
func (Strategy) AllowUnconditionalUpdate() bool { return false }

// ValidateUpdate validates a GitRepository spec/metadata update.
func (Strategy) ValidateUpdate(_ context.Context, obj, old runtime.Object) field.ErrorList {
	return source.ValidateGitRepositoryUpdate(obj.(*source.GitRepository), old.(*source.GitRepository))
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

// StatusStrategy implements the behavior for the GitRepository /status subresource.
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
	newRepo := obj.(*source.GitRepository)
	oldRepo := old.(*source.GitRepository)
	newRepo.Spec = oldRepo.Spec
	newRepo.Webhook = oldRepo.Webhook
	newRepo.Generation = oldRepo.Generation
}

// ValidateUpdate validates a GitRepository status update.
func (StatusStrategy) ValidateUpdate(_ context.Context, obj, old runtime.Object) field.ErrorList {
	return source.ValidateGitRepositoryStatusUpdate(obj.(*source.GitRepository), old.(*source.GitRepository))
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

// GetAttrs returns the labels and fields of a GitRepository for filtering.
func GetAttrs(obj runtime.Object) (labels.Set, fields.Set, error) {
	repo, ok := obj.(*source.GitRepository)
	if !ok {
		return nil, nil, fmt.Errorf("given object is not a GitRepository")
	}
	return labels.Set(repo.Labels), selectableFields(repo), nil
}

// MatchGitRepository returns a selection predicate for the given selectors.
func MatchGitRepository(label labels.Selector, fld fields.Selector) storage.SelectionPredicate {
	return storage.SelectionPredicate{
		Label:    label,
		Field:    fld,
		GetAttrs: GetAttrs,
	}
}

func selectableFields(obj *source.GitRepository) fields.Set {
	return generic.ObjectMetaFieldsSet(&obj.ObjectMeta, true)
}
