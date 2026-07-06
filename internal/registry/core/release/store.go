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
	"net/http"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/generic"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/registry/rest"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"

	"github.com/kumodo-io/convoy/api/core"
	"github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/registry"
)

// NewREST returns RESTStorage for Release and its /status, /pin, and /sync subresources.
func NewREST(scheme *runtime.Scheme, optsGetter generic.RESTOptionsGetter) (*registry.REST, *StatusREST, *PinREST, *SyncREST, error) {
	strategy := NewStrategy(scheme)

	store := &genericregistry.Store{
		NewFunc:                   func() runtime.Object { return &core.Release{} },
		NewListFunc:               func() runtime.Object { return &core.ReleaseList{} },
		PredicateFunc:             MatchRelease,
		DefaultQualifiedResource:  core.Resource("releases"),
		SingularQualifiedResource: core.Resource("release"),

		CreateStrategy:      strategy,
		UpdateStrategy:      strategy,
		DeleteStrategy:      strategy,
		ResetFieldsStrategy: strategy,

		TableConvertor: newTableConvertor(),
	}

	options := &generic.StoreOptions{RESTOptions: optsGetter, AttrFunc: GetAttrs}
	if err := store.CompleteWithOptions(options); err != nil {
		return nil, nil, nil, nil, err
	}

	statusStore := *store
	statusStrategy := NewStatusStrategy(strategy)
	statusStore.UpdateStrategy = statusStrategy
	statusStore.ResetFieldsStrategy = statusStrategy

	pinStore := *store
	pinStrategy := NewPinStrategy(strategy)
	pinStore.UpdateStrategy = pinStrategy
	pinStore.ResetFieldsStrategy = pinStrategy

	return &registry.REST{Store: store},
		&StatusREST{store: &statusStore},
		&PinREST{store: &pinStore},
		&SyncREST{store: store},
		nil
}

// StatusREST implements the /status subresource for Release.
type StatusREST struct {
	store *genericregistry.Store
}

var (
	_ rest.Storage             = &StatusREST{}
	_ rest.Getter              = &StatusREST{}
	_ rest.Updater             = &StatusREST{}
	_ rest.TableConvertor      = &StatusREST{}
	_ rest.ResetFieldsStrategy = &StatusREST{}
)

// New returns an empty Release.
func (r *StatusREST) New() runtime.Object { return &core.Release{} }

// Destroy releases resources held by the store.
func (r *StatusREST) Destroy() {}

// Get retrieves the Release (including status).
func (r *StatusREST) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	return r.store.Get(ctx, name, options)
}

// Update updates the status subresource.
func (r *StatusREST) Update(
	ctx context.Context,
	name string,
	objInfo rest.UpdatedObjectInfo,
	createValidation rest.ValidateObjectFunc,
	updateValidation rest.ValidateObjectUpdateFunc,
	forceAllowCreate bool,
	options *metav1.UpdateOptions,
) (runtime.Object, bool, error) {
	return r.store.Update(ctx, name, objInfo, createValidation, updateValidation, forceAllowCreate, options)
}

// GetResetFields delegates to the status store strategy.
func (r *StatusREST) GetResetFields() map[fieldpath.APIVersion]*fieldpath.Set {
	return r.store.GetResetFields()
}

// ConvertToTable delegates to the store's table convertor.
func (r *StatusREST) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	return r.store.ConvertToTable(ctx, object, tableOptions)
}

// PinREST implements the /pin subresource: GET reads the Release, PUT sets or
// clears its Pin (empty RenderRef unpins). Only Pin is mutable via this endpoint.
type PinREST struct {
	store *genericregistry.Store
}

var (
	_ rest.Storage        = &PinREST{}
	_ rest.Getter         = &PinREST{}
	_ rest.Updater        = &PinREST{}
	_ rest.TableConvertor = &PinREST{}
)

// New returns an empty Release.
func (r *PinREST) New() runtime.Object { return &core.Release{} }

// Destroy releases resources held by the store.
func (r *PinREST) Destroy() {}

// Get retrieves the Release (including its pin).
func (r *PinREST) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	return r.store.Get(ctx, name, options)
}

// Update sets or clears the Release's pin.
func (r *PinREST) Update(
	ctx context.Context,
	name string,
	objInfo rest.UpdatedObjectInfo,
	createValidation rest.ValidateObjectFunc,
	updateValidation rest.ValidateObjectUpdateFunc,
	forceAllowCreate bool,
	options *metav1.UpdateOptions,
) (runtime.Object, bool, error) {
	return r.store.Update(ctx, name, objInfo, createValidation, updateValidation, forceAllowCreate, options)
}

// ConvertToTable delegates to the store's table convertor.
func (r *PinREST) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	return r.store.ConvertToTable(ctx, object, tableOptions)
}

// SyncREST implements the /sync subresource: POST stamps a reconcile-request
// annotation so the controller reconciles the Release promptly (and, in manual
// mode, applies).
type SyncREST struct {
	store *genericregistry.Store
}

var (
	_ rest.Storage   = &SyncREST{}
	_ rest.Connecter = &SyncREST{}
)

// New returns an empty Release.
func (r *SyncREST) New() runtime.Object { return &core.Release{} }

// Destroy releases resources held by the store.
func (r *SyncREST) Destroy() {}

// ConnectMethods declares POST as the only method for /sync.
func (r *SyncREST) ConnectMethods() []string { return []string{http.MethodPost} }

// NewConnectOptions declares no options for /sync.
func (r *SyncREST) NewConnectOptions() (runtime.Object, bool, string) { return nil, false, "" }

// Connect stamps the reconcile-request annotation on the Release.
func (r *SyncREST) Connect(ctx context.Context, name string, _ runtime.Object, responder rest.Responder) (http.Handler, error) {
	return http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		obj, err := r.store.Get(ctx, name, &metav1.GetOptions{})
		if err != nil {
			responder.Error(err)
			return
		}
		rel := obj.(*core.Release).DeepCopy()
		metav1.SetMetaDataAnnotation(&rel.ObjectMeta, v1alpha1.SyncRequestedAtAnnotation, metav1.Now().UTC().Format(time.RFC3339Nano))

		updated, _, err := r.store.Update(ctx, name, rest.DefaultUpdatedObjectInfo(rel),
			rest.ValidateAllObjectFunc, rest.ValidateAllObjectUpdateFunc, false, &metav1.UpdateOptions{})
		if err != nil {
			responder.Error(err)
			return
		}
		responder.Object(http.StatusOK, updated)
	}), nil
}
