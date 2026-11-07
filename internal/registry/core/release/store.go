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
	"io"
	"net/http"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/generic"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/registry/rest"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"

	"github.com/kumodo-io/convoy/api/core"
	"github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/registry"
	"github.com/kumodo-io/convoy/internal/sharding"
)

// Storage bundles the Release REST and its subresource RESTs. Grouping them in a
// struct (rather than a long positional return) keeps NewREST's signature stable
// as subresources are added and lets callers reference them by name.
type Storage struct {
	Release   *registry.REST
	Status    *StatusREST
	Pin       *PinREST
	Sync      *SyncREST
	Mutations *MutationsREST
	Lifecycle *LifecycleREST
	Preview   *observerProxyREST
	Diff      *observerProxyREST
	Health    *observerProxyREST
	History   *observerProxyREST
}

// Map returns the resource-path → storage entries to install in the API group's
// VersionedResourcesStorageMap. It is the single source of truth for the Release
// subresource paths, so adding a subresource touches only this file.
func (s *Storage) Map() map[string]rest.Storage {
	return map[string]rest.Storage{
		"releases":           s.Release,
		"releases/status":    s.Status,
		"releases/pin":       s.Pin,
		"releases/sync":      s.Sync,
		"releases/mutations": s.Mutations,
		"releases/lifecycle": s.Lifecycle,
		"releases/preview":   s.Preview,
		"releases/diff":      s.Diff,
		"releases/health":    s.Health,
		"releases/history":   s.History,
	}
}

// ObserverResolver returns the base URL of the observer replica that owns a
// shard, or "" when it is unknown.
type ObserverResolver func(shard string) string

// NewREST returns the Release RESTStorage and its subresources. observerURL is
// the base URL of the observer the /preview subresource proxies to (empty
// disables /preview: it returns ServiceUnavailable). resolve, when set, sends
// each call to the observer replica that owns the Release's shard instead.
func NewREST(scheme *runtime.Scheme, optsGetter generic.RESTOptionsGetter, observerURL string, resolve ObserverResolver, shards int) (*Storage, error) {
	strategy := NewStrategy(scheme, shards)

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
		return nil, err
	}

	statusStore := *store
	statusStrategy := NewStatusStrategy(strategy)
	statusStore.UpdateStrategy = statusStrategy
	statusStore.ResetFieldsStrategy = statusStrategy

	pinStore := *store
	pinStrategy := NewPinStrategy(strategy)
	pinStore.UpdateStrategy = pinStrategy
	pinStore.ResetFieldsStrategy = pinStrategy

	mutationsStore := *store
	mutationsStrategy := NewMutationsStrategy(strategy)
	mutationsStore.UpdateStrategy = mutationsStrategy
	mutationsStore.ResetFieldsStrategy = mutationsStrategy

	lifecycleStore := *store
	lifecycleStrategy := NewLifecycleStrategy(strategy)
	lifecycleStore.UpdateStrategy = lifecycleStrategy
	lifecycleStore.ResetFieldsStrategy = lifecycleStrategy

	return &Storage{
		Release:   &registry.REST{Store: store},
		Status:    &StatusREST{store: &statusStore},
		Pin:       &PinREST{store: &pinStore},
		Sync:      &SyncREST{store: store},
		Mutations: &MutationsREST{store: &mutationsStore},
		Lifecycle: &LifecycleREST{store: &lifecycleStore},
		Preview:   &observerProxyREST{observerURL: observerURL, subresource: "preview", releases: store, resolve: resolve},
		Diff:      &observerProxyREST{observerURL: observerURL, subresource: "diff", releases: store, resolve: resolve},
		Health:    &observerProxyREST{observerURL: observerURL, subresource: "health", releases: store, resolve: resolve},
		History:   &observerProxyREST{observerURL: observerURL, subresource: "history", releases: store, resolve: resolve},
	}, nil
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

// MutationsREST implements the /mutations subresource: GET reads the Release, PUT
// replaces its top-level Mutations pipeline. Separately addressable so mutation
// edits can be RBAC-scoped independently of the rest of the Release.
type MutationsREST struct {
	store *genericregistry.Store
}

var (
	_ rest.Storage        = &MutationsREST{}
	_ rest.Getter         = &MutationsREST{}
	_ rest.Updater        = &MutationsREST{}
	_ rest.TableConvertor = &MutationsREST{}
)

// New returns an empty Release.
func (r *MutationsREST) New() runtime.Object { return &core.Release{} }

// Destroy releases resources held by the store.
func (r *MutationsREST) Destroy() {}

// Get retrieves the Release (including its mutations).
func (r *MutationsREST) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	return r.store.Get(ctx, name, options)
}

// Update replaces the Release's mutation pipeline.
func (r *MutationsREST) Update(
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
func (r *MutationsREST) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	return r.store.ConvertToTable(ctx, object, tableOptions)
}

// LifecycleREST implements the /lifecycle subresource: GET reads the Release, PUT
// replaces its top-level Lifecycle hooks. Separately addressable so lifecycle
// edits can be RBAC-scoped independently of the rest of the Release.
type LifecycleREST struct {
	store *genericregistry.Store
}

var (
	_ rest.Storage        = &LifecycleREST{}
	_ rest.Getter         = &LifecycleREST{}
	_ rest.Updater        = &LifecycleREST{}
	_ rest.TableConvertor = &LifecycleREST{}
)

// New returns an empty Release.
func (r *LifecycleREST) New() runtime.Object { return &core.Release{} }

// Destroy releases resources held by the store.
func (r *LifecycleREST) Destroy() {}

// Get retrieves the Release (including its lifecycle hooks).
func (r *LifecycleREST) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	return r.store.Get(ctx, name, options)
}

// Update replaces the Release's lifecycle hooks.
func (r *LifecycleREST) Update(
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
func (r *LifecycleREST) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
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

// Connect stamps the reconcile-request annotation on the Release. With ?force=true
// it additionally stamps the force-request annotation: the controller then bypasses
// the pre-apply dry-run and clears a held Failed/RolledBack rollout, re-driving the
// render.
func (r *SyncREST) Connect(ctx context.Context, name string, _ runtime.Object, responder rest.Responder) (http.Handler, error) {
	return http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		obj, err := r.store.Get(ctx, name, &metav1.GetOptions{})
		if err != nil {
			responder.Error(err)
			return
		}
		rel := obj.(*core.Release).DeepCopy()
		now := metav1.Now().UTC().Format(time.RFC3339Nano)
		// The normal annotation always wakes the controller (as today).
		metav1.SetMetaDataAnnotation(&rel.ObjectMeta, v1alpha1.SyncRequestedAtAnnotation, now)
		if forceParam(req) {
			metav1.SetMetaDataAnnotation(&rel.ObjectMeta, v1alpha1.SyncForceRequestedAtAnnotation, now)
		}

		updated, _, err := r.store.Update(ctx, name, rest.DefaultUpdatedObjectInfo(rel),
			rest.ValidateAllObjectFunc, rest.ValidateAllObjectUpdateFunc, false, &metav1.UpdateOptions{})
		if err != nil {
			responder.Error(err)
			return
		}
		responder.Object(http.StatusOK, updated)
	}), nil
}

// forceParam reports whether the request asks for a forced sync (?force=true).
func forceParam(req *http.Request) bool {
	v := req.URL.Query().Get("force")
	return v == "true" || v == "1"
}

// observerProxyREST implements a read-only Release subresource (GET) that the
// apiserver proxies to the observer's HTTP server (the pods/log → kubelet
// pattern). The apiserver serves API only, so rendering and diffing live in the
// observer; /preview and /diff differ only by which observer path they hit.
type observerProxyREST struct {
	// observerURL is the observer's base URL; empty means the subresource is
	// unavailable (returns ServiceUnavailable).
	observerURL string
	// subresource is the observer path segment ("preview" or "diff").
	subresource string
	// releases reads the Release for its shard label.
	releases rest.Getter
	// resolve finds the observer replica that owns a shard; nil uses observerURL.
	resolve ObserverResolver
}

var (
	_ rest.Storage   = &observerProxyREST{}
	_ rest.Connecter = &observerProxyREST{}
)

// New returns an empty Release.
func (r *observerProxyREST) New() runtime.Object { return &core.Release{} }

// Destroy releases resources held by the store.
func (r *observerProxyREST) Destroy() {}

// ConnectMethods declares GET as the only method.
func (r *observerProxyREST) ConnectMethods() []string { return []string{http.MethodGet} }

// NewConnectOptions declares no options.
func (r *observerProxyREST) NewConnectOptions() (runtime.Object, bool, string) {
	return nil, false, ""
}

// Connect proxies the GET to the observer replica that owns the Release's shard,
// streaming its response back to the client. It does not touch etcd beyond
// reading the Release — the observer renders/diffs on demand.
func (r *observerProxyREST) Connect(ctx context.Context, name string, _ runtime.Object, responder rest.Responder) (http.Handler, error) {
	if r.observerURL == "" {
		return nil, apierrors.NewServiceUnavailable(fmt.Sprintf("%s is not configured (no observer URL)", r.subresource))
	}
	base, err := r.observerFor(ctx, name)
	if err != nil {
		return nil, err
	}
	namespace := genericapirequest.NamespaceValue(ctx)
	target := fmt.Sprintf("%s/%s/%s/%s", strings.TrimRight(base, "/"), r.subresource, namespace, name)

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Forward the client's query string (e.g. /diff?render=<ref> for a
		// historical diff) to the observer.
		url := target
		if req.URL.RawQuery != "" {
			url += "?" + req.URL.RawQuery
		}
		proxyReq, err := http.NewRequestWithContext(req.Context(), http.MethodGet, url, nil)
		if err != nil {
			responder.Error(err)
			return
		}
		resp, err := http.DefaultClient.Do(proxyReq)
		if err != nil {
			responder.Error(apierrors.NewServiceUnavailable(fmt.Sprintf("observer unavailable: %v", err)))
			return
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode >= http.StatusBadRequest {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, maxObserverErrorBytes))
			responder.Error(observerError(resp.StatusCode, body))
			return
		}
		for key, values := range resp.Header {
			for _, v := range values {
				w.Header().Add(key, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}), nil
}

// observerFor returns the base URL of the observer replica that owns the
// Release's shard: with a shard-scoped cache, any other replica answers 404.
func (r *observerProxyREST) observerFor(ctx context.Context, name string) (string, error) {
	if r.releases == nil {
		return r.observerURL, nil
	}
	obj, err := r.releases.Get(ctx, name, &metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if r.resolve == nil {
		return r.observerURL, nil
	}
	rel, ok := obj.(*core.Release)
	if !ok {
		return "", fmt.Errorf("unexpected object %T", obj)
	}
	shard, ok := rel.Labels[sharding.ShardLabel]
	if !ok {
		return r.observerURL, nil
	}
	if url := r.resolve(shard); url != "" {
		return url, nil
	}
	return r.observerURL, nil
}

// maxObserverErrorBytes caps how much of an observer error body becomes the
// API error message.
const maxObserverErrorBytes = 64 << 10

// observerError turns an observer error response into an API status, so the
// client sees the observer's message instead of a generic one for the code.
func observerError(code int, body []byte) error {
	var reason metav1.StatusReason
	switch code {
	case http.StatusNotFound:
		reason = metav1.StatusReasonNotFound
	case http.StatusServiceUnavailable:
		reason = metav1.StatusReasonServiceUnavailable
	case http.StatusInternalServerError:
		reason = metav1.StatusReasonInternalError
	}
	return &apierrors.StatusError{ErrStatus: metav1.Status{
		Status:  metav1.StatusFailure,
		Code:    int32(code), //nolint:gosec // an HTTP status code fits in int32
		Reason:  reason,
		Message: strings.TrimSpace(string(body)),
	}}
}
