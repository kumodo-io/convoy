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

package release_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"

	"github.com/kumodo-io/convoy/api/core"
	coreinstall "github.com/kumodo-io/convoy/api/core/install"
	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	releasestorage "github.com/kumodo-io/convoy/internal/registry/core/release"
	"github.com/kumodo-io/convoy/internal/registry/registrytest"
	"github.com/kumodo-io/convoy/internal/sharding"
)

func newProxyStorage(t *testing.T, observerURL string, resolve releasestorage.ObserverResolver) *releasestorage.Storage {
	t.Helper()
	scheme, restOptions, server := registrytest.NewEtcdStorage(t, coreinstall.Install, corev1alpha1.SchemeGroupVersion, core.Resource("releases"))
	t.Cleanup(func() { server.Terminate(t) })
	storage, err := releasestorage.NewREST(scheme, restOptions, observerURL, resolve, 4)
	if err != nil {
		t.Fatalf("NewREST: %v", err)
	}
	return storage
}

// observerReplica answers every request with its name and the request path.
func observerReplica(t *testing.T, name string, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		_, _ = fmt.Fprintf(w, "%s %s", name, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func createProxyRelease(t *testing.T, storage *releasestorage.Storage) (context.Context, *core.Release) {
	t.Helper()
	ctx := genericapirequest.WithNamespace(genericapirequest.NewContext(), metav1.NamespaceDefault)
	obj, err := storage.Release.Create(ctx, validNewRelease(metav1.NamespaceDefault), rest.ValidateAllObjectFunc, &metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("creating release: %v", err)
	}
	return ctx, obj.(*core.Release)
}

func proxyCall(t *testing.T, ctx context.Context, sub rest.Connecter) (string, error) {
	t.Helper()
	responder := &captureResponder{}
	handler, err := sub.Connect(ctx, "foo", nil, responder)
	if err != nil {
		return "", err
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec.Body.String(), responder.err
}

func subresources(s *releasestorage.Storage) map[string]rest.Connecter {
	return map[string]rest.Connecter{"preview": s.Preview, "diff": s.Diff, "health": s.Health, "history": s.History}
}

// TestObserverProxy_RoutesToShardOwner is the KB-5 regression: with a shard-scoped
// observer cache, only the replica that owns the Release's shard can serve it.
func TestObserverProxy_RoutesToShardOwner(t *testing.T) {
	service := observerReplica(t, "service", nil)
	owner := observerReplica(t, "owner", nil)
	var asked []string
	storage := newProxyStorage(t, service.URL, func(shard string) string {
		asked = append(asked, shard)
		return owner.URL
	})
	ctx, rel := createProxyRelease(t, storage)

	for name, sub := range subresources(storage) {
		body, err := proxyCall(t, ctx, sub)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if want := "owner /" + name + "/default/foo"; body != want {
			t.Errorf("%s answered %q, want %q", name, body, want)
		}
	}
	shard := rel.Labels[sharding.ShardLabel]
	for _, got := range asked {
		if got != shard {
			t.Errorf("resolved shard %q, want the Release's shard %q", got, shard)
		}
	}
	if len(asked) != 4 {
		t.Errorf("resolved %d times, want 4", len(asked))
	}
}

func TestObserverProxy_FallsBackToService(t *testing.T) {
	for _, tc := range []struct {
		name    string
		resolve releasestorage.ObserverResolver
	}{
		{name: "no resolver"},
		{name: "owner unknown", resolve: func(string) string { return "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := observerReplica(t, "service", nil)
			storage := newProxyStorage(t, service.URL, tc.resolve)
			ctx, _ := createProxyRelease(t, storage)

			body, err := proxyCall(t, ctx, storage.Preview)
			if err != nil {
				t.Fatalf("preview: %v", err)
			}
			if want := "service /preview/default/foo"; body != want {
				t.Errorf("preview answered %q, want %q", body, want)
			}
		})
	}
}

// TestObserverProxy_KeepsObserverError: the observer's status and text reach the
// client, not a generic "the server could not find the requested resource".
func TestObserverProxy_KeepsObserverError(t *testing.T) {
	for _, tc := range []struct {
		code   int
		text   string
		reason metav1.StatusReason
	}{
		{code: http.StatusNotFound, text: "release default/foo not found", reason: metav1.StatusReasonNotFound},
		{code: http.StatusUnprocessableEntity, text: "fetching source: connection refused", reason: metav1.StatusReasonUnknown},
		{code: http.StatusServiceUnavailable, text: "render history is not configured (no render store)", reason: metav1.StatusReasonServiceUnavailable},
	} {
		t.Run(http.StatusText(tc.code), func(t *testing.T) {
			observer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, tc.text, tc.code)
			}))
			t.Cleanup(observer.Close)
			storage := newProxyStorage(t, observer.URL, nil)
			ctx, _ := createProxyRelease(t, storage)

			_, err := proxyCall(t, ctx, storage.Diff)
			var status apierrors.APIStatus
			if err == nil || !errors.As(err, &status) {
				t.Fatalf("err = %v, want an API status error", err)
			}
			got := status.Status()
			if int(got.Code) != tc.code || got.Reason != tc.reason || strings.TrimSpace(got.Message) != tc.text {
				t.Errorf("status = %d/%s %q, want %d/%s %q", got.Code, got.Reason, got.Message, tc.code, tc.reason, tc.text)
			}
		})
	}
}

func TestObserverProxy_MissingRelease(t *testing.T) {
	var hits atomic.Int32
	service := observerReplica(t, "service", &hits)
	storage := newProxyStorage(t, service.URL, nil)
	ctx := genericapirequest.WithNamespace(genericapirequest.NewContext(), metav1.NamespaceDefault)

	_, err := proxyCall(t, ctx, storage.Health)
	if !apierrors.IsNotFound(err) {
		t.Errorf("err = %v, want NotFound", err)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("observer called %d times for a missing Release, want 0", n)
	}
}
