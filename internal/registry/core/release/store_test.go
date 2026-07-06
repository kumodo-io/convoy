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
	"net/http"
	"net/http/httptest"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	genericregistrytest "k8s.io/apiserver/pkg/registry/generic/testing"
	"k8s.io/apiserver/pkg/registry/rest"
	etcd3testing "k8s.io/apiserver/pkg/storage/etcd3/testing"

	"github.com/kumodo-io/convoy/api/core"
	coreinstall "github.com/kumodo-io/convoy/api/core/install"
	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	releasestorage "github.com/kumodo-io/convoy/internal/registry/core/release"
	"github.com/kumodo-io/convoy/internal/registry/registrytest"
)

const dummyClusterRef = "prod-eu"

func newStorage(t *testing.T) (*genericregistry.Store, *releasestorage.StatusREST, *releasestorage.PinREST, *releasestorage.SyncREST, *etcd3testing.EtcdTestServer) {
	scheme, restOptions, server := registrytest.NewEtcdStorage(t, coreinstall.Install, corev1alpha1.SchemeGroupVersion, core.Resource("releases"))

	restStorage, statusStorage, pinStorage, syncStorage, err := releasestorage.NewREST(scheme, restOptions)
	if err != nil {
		server.Terminate(t)
		t.Fatalf("unexpected error from NewREST: %v", err)
	}
	return restStorage.Store, statusStorage, pinStorage, syncStorage, server
}

func validNewRelease(namespace string) *core.Release {
	return &core.Release{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: namespace,
		},
		Spec: core.ReleaseSpec{
			Source: core.ReleaseSourceSpec{
				Type: core.ReleaseSourceTypeGitRepository,
				GitRepository: &core.ReleaseGitRepositorySpec{
					Name: "payments-charts",
					Ref:  &core.ReleaseGitRepositoryRefSpec{Branch: new("main")},
				},
			},
			Content: core.ReleaseContentSpec{
				Type: core.ReleaseContentTypeHelm,
				Helm: &core.ReleaseContentHelmSpec{
					Path:           "charts/payments",
					DependencyMode: core.ReleaseHelmDependencyModeVendored,
				},
			},
			ClusterRef: core.ReleaseClusterRefSpec{
				Name: dummyClusterRef,
			},
		},
	}
}

// releaseMissingContentType is invalid: content.type is required.
func releaseMissingContentType(namespace string) *core.Release {
	r := validNewRelease(namespace)
	r.Spec.Content.Type = ""
	r.Spec.Content.Helm = nil
	return r
}

// releaseMissingSourceBlock is invalid: the gitRepository block is required when
// source.type is gitRepository.
func releaseMissingSourceBlock(namespace string) *core.Release {
	r := validNewRelease(namespace)
	r.Spec.Source.GitRepository = nil
	return r
}

func TestCreate(t *testing.T) {
	storage, _, _, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	ns := test.TestNamespace()
	test.TestCreate(
		validNewRelease(ns),
		releaseMissingContentType(ns),
		releaseMissingSourceBlock(ns),
	)
}

func TestUpdate(t *testing.T) {
	storage, _, _, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestUpdate(
		validNewRelease(test.TestNamespace()),
		// valid update: add a label
		func(obj runtime.Object) runtime.Object {
			r := obj.(*core.Release)
			if r.Labels == nil {
				r.Labels = map[string]string{}
			}
			r.Labels["updated"] = "true"
			return r
		},
		// invalid update: clears the required content.type
		func(obj runtime.Object) runtime.Object {
			r := obj.(*core.Release)
			r.Spec.Content.Type = ""
			r.Spec.Content.Helm = nil
			return r
		},
		// invalid update: mutates the immutable source
		func(obj runtime.Object) runtime.Object {
			r := obj.(*core.Release)
			r.Spec.Source.GitRepository.Name = "other-charts"
			return r
		},
	)
}

func TestGet(t *testing.T) {
	storage, _, _, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestGet(validNewRelease(test.TestNamespace()))
}

func TestList(t *testing.T) {
	storage, _, _, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestList(validNewRelease(test.TestNamespace()))
}

func TestDelete(t *testing.T) {
	storage, _, _, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestDelete(validNewRelease(test.TestNamespace()))
}

// TestStatusUpdate verifies the /status subresource updates status while leaving
// spec untouched.
func TestStatusUpdate(t *testing.T) {
	storage, statusStorage, _, _, server := newStorage(t)
	defer server.Terminate(t)

	ctx := genericapirequest.WithNamespace(genericapirequest.NewContext(), metav1.NamespaceDefault)

	if _, err := storage.Create(ctx, validNewRelease(metav1.NamespaceDefault), rest.ValidateAllObjectFunc, &metav1.CreateOptions{}); err != nil {
		t.Fatalf("unexpected error creating release: %v", err)
	}

	obj, err := statusStorage.Get(ctx, "foo", &metav1.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected error getting release: %v", err)
	}
	release := obj.(*core.Release)

	release.Status.ObservedGeneration = release.Generation
	release.Status.HealthSummary.Total = 14
	release.Status.HealthSummary.Healthy = 14
	// A spec change submitted through /status must be ignored.
	release.Spec.RetentionPolicy.KeepHistory = 99

	updatedObj, _, err := statusStorage.Update(
		ctx,
		release.Name,
		rest.DefaultUpdatedObjectInfo(release),
		rest.ValidateAllObjectFunc,
		rest.ValidateAllObjectUpdateFunc,
		false,
		&metav1.UpdateOptions{},
	)
	if err != nil {
		t.Fatalf("unexpected error updating status: %v", err)
	}

	updated := updatedObj.(*core.Release)
	if updated.Status.HealthSummary.Total != 14 {
		t.Errorf("expected status.healthSummary.total to persist, got %d", updated.Status.HealthSummary.Total)
	}
	if updated.Spec.RetentionPolicy.KeepHistory != 0 {
		t.Errorf("expected spec change to be ignored by /status, got keepHistory=%d", updated.Spec.RetentionPolicy.KeepHistory)
	}
}

// setLastSuccessfulRender records a successful render on status so /pin has a
// current render to freeze.
func setLastSuccessfulRender(t *testing.T, ctx context.Context, statusStorage *releasestorage.StatusREST, name, renderRef string) {
	t.Helper()
	obj, err := statusStorage.Get(ctx, name, &metav1.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected error getting release: %v", err)
	}
	release := obj.(*core.Release)
	release.Status.LastSuccessful = &core.ReleaseAttemptedRenderStatus{RenderRef: renderRef}
	if _, _, err := statusStorage.Update(ctx, name, rest.DefaultUpdatedObjectInfo(release),
		rest.ValidateAllObjectFunc, rest.ValidateAllObjectUpdateFunc, false, &metav1.UpdateOptions{}); err != nil {
		t.Fatalf("unexpected error updating status: %v", err)
	}
}

func pin(ctx context.Context, pinStorage *releasestorage.PinREST, name, renderRef string) (*core.Release, error) { //nolint:unparam
	obj, err := pinStorage.Get(ctx, name, &metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	release := obj.(*core.Release)
	release.Pin = &core.ReleasePin{RenderRef: renderRef}
	updated, _, err := pinStorage.Update(ctx, name, rest.DefaultUpdatedObjectInfo(release),
		rest.ValidateAllObjectFunc, rest.ValidateAllObjectUpdateFunc, false, &metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	return updated.(*core.Release), nil
}

// TestPinFreezeCurrentAndUnpin verifies /pin freezes the current render (stamping
// PinnedAt and the reconcile-request annotation) and that an empty RenderRef unpins.
func TestPinFreezeCurrentAndUnpin(t *testing.T) {
	storage, statusStorage, pinStorage, _, server := newStorage(t)
	defer server.Terminate(t)

	ctx := genericapirequest.WithNamespace(genericapirequest.NewContext(), metav1.NamespaceDefault)
	if _, err := storage.Create(ctx, validNewRelease(metav1.NamespaceDefault), rest.ValidateAllObjectFunc, &metav1.CreateOptions{}); err != nil {
		t.Fatalf("unexpected error creating release: %v", err)
	}
	setLastSuccessfulRender(t, ctx, statusStorage, "foo", "sha-abc")

	pinned, err := pin(ctx, pinStorage, "foo", "sha-abc")
	if err != nil {
		t.Fatalf("unexpected error pinning to current render: %v", err)
	}
	if pinned.Pin == nil || pinned.Pin.RenderRef != "sha-abc" {
		t.Errorf("expected pin.renderRef=sha-abc, got %+v", pinned.Pin)
	}
	if pinned.Pin.PinnedAt.IsZero() {
		t.Error("expected pin.pinnedAt to be stamped")
	}
	if _, ok := pinned.Annotations[corev1alpha1.SyncRequestedAtAnnotation]; !ok {
		t.Errorf("expected %s annotation to be stamped on pin", corev1alpha1.SyncRequestedAtAnnotation)
	}

	unpinned, err := pin(ctx, pinStorage, "foo", "")
	if err != nil {
		t.Fatalf("unexpected error unpinning: %v", err)
	}
	if unpinned.Pin != nil {
		t.Errorf("expected pin to be cleared, got %+v", unpinned.Pin)
	}
}

// TestPinRejectsNonCurrentAndPremature verifies /pin only allows freezing the
// current render: pinning a different render (rollback) or pinning before any
// successful render is rejected.
func TestPinRejectsNonCurrentAndPremature(t *testing.T) {
	storage, statusStorage, pinStorage, _, server := newStorage(t)
	defer server.Terminate(t)

	ctx := genericapirequest.WithNamespace(genericapirequest.NewContext(), metav1.NamespaceDefault)
	if _, err := storage.Create(ctx, validNewRelease(metav1.NamespaceDefault), rest.ValidateAllObjectFunc, &metav1.CreateOptions{}); err != nil {
		t.Fatalf("unexpected error creating release: %v", err)
	}

	// No successful render yet: pinning is rejected (field errors aggregate to Invalid).
	if _, err := pin(ctx, pinStorage, "foo", "sha-abc"); !apierrors.IsInvalid(err) {
		t.Fatalf("expected Invalid pinning before a successful render, got %v", err)
	}

	setLastSuccessfulRender(t, ctx, statusStorage, "foo", "sha-abc")

	// Pinning a render other than the current one (rollback) is rejected.
	if _, err := pin(ctx, pinStorage, "foo", "sha-old"); !apierrors.IsInvalid(err) {
		t.Fatalf("expected Invalid pinning a non-current render, got %v", err)
	}
}

// captureResponder records the object or error produced by a Connecter handler.
type captureResponder struct {
	obj runtime.Object
	err error
}

func (r *captureResponder) Object(_ int, obj runtime.Object) { r.obj = obj }
func (r *captureResponder) Error(err error)                  { r.err = err }

// TestSyncStampsAnnotation verifies POST /sync stamps the reconcile-request
// annotation on the Release without altering its spec.
func TestSyncStampsAnnotation(t *testing.T) {
	storage, _, _, syncStorage, server := newStorage(t)
	defer server.Terminate(t)

	ctx := genericapirequest.WithNamespace(genericapirequest.NewContext(), metav1.NamespaceDefault)
	if _, err := storage.Create(ctx, validNewRelease(metav1.NamespaceDefault), rest.ValidateAllObjectFunc, &metav1.CreateOptions{}); err != nil {
		t.Fatalf("unexpected error creating release: %v", err)
	}

	responder := &captureResponder{}
	handler, err := syncStorage.Connect(ctx, "foo", nil, responder)
	if err != nil {
		t.Fatalf("unexpected error from Connect: %v", err)
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil))

	if responder.err != nil {
		t.Fatalf("unexpected error from /sync handler: %v", responder.err)
	}
	release, ok := responder.obj.(*core.Release)
	if !ok {
		t.Fatalf("expected a Release from /sync, got %T", responder.obj)
	}
	if _, ok := release.Annotations[corev1alpha1.SyncRequestedAtAnnotation]; !ok {
		t.Errorf("expected %s annotation to be stamped by /sync", corev1alpha1.SyncRequestedAtAnnotation)
	}
}
