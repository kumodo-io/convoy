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

package cluster

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/generic"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/registry/rest"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"

	"github.com/kumodo-io/convoy/api/core"
	"github.com/kumodo-io/convoy/internal/registry"
)

// NewREST returns RESTStorage for Cluster and its /status subresource.
func NewREST(scheme *runtime.Scheme, optsGetter generic.RESTOptionsGetter) (*registry.REST, *StatusREST, error) {
	strategy := NewStrategy(scheme)

	store := &genericregistry.Store{
		NewFunc:                   func() runtime.Object { return &core.Cluster{} },
		NewListFunc:               func() runtime.Object { return &core.ClusterList{} },
		PredicateFunc:             MatchCluster,
		DefaultQualifiedResource:  core.Resource("clusters"),
		SingularQualifiedResource: core.Resource("cluster"),

		CreateStrategy:      strategy,
		UpdateStrategy:      strategy,
		DeleteStrategy:      strategy,
		ResetFieldsStrategy: strategy,

		TableConvertor: newTableConvertor(),
	}

	options := &generic.StoreOptions{RESTOptions: optsGetter, AttrFunc: GetAttrs}
	if err := store.CompleteWithOptions(options); err != nil {
		return nil, nil, err
	}

	statusStore := *store
	statusStrategy := NewStatusStrategy(strategy)
	statusStore.UpdateStrategy = statusStrategy
	statusStore.ResetFieldsStrategy = statusStrategy

	return &registry.REST{Store: store}, &StatusREST{store: &statusStore}, nil
}

// StatusREST implements the /status subresource for Cluster.
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

// New returns an empty Cluster.
func (r *StatusREST) New() runtime.Object { return &core.Cluster{} }

// Destroy releases resources held by the store.
func (r *StatusREST) Destroy() {}

// Get retrieves the Cluster (including status).
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
