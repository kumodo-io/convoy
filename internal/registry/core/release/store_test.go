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
	"testing"

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

func newStorage(t *testing.T) (*genericregistry.Store, *releasestorage.StatusREST, *etcd3testing.EtcdTestServer) {
	scheme, restOptions, server := registrytest.NewEtcdStorage(t, coreinstall.Install, corev1alpha1.SchemeGroupVersion, core.Resource("releases"))

	restStorage, statusStorage, err := releasestorage.NewREST(scheme, restOptions)
	if err != nil {
		server.Terminate(t)
		t.Fatalf("unexpected error from NewREST: %v", err)
	}
	return restStorage.Store, statusStorage, server
}

func validNewRelease(namespace string) *core.Release {
	return &core.Release{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: namespace,
		},
		Spec: core.ReleaseSpec{
			Source: core.ReleaseSourceSpec{
				Kind: "GitRepository",
				Name: "payments-charts",
			},
			Content: core.ReleaseContentSpec{
				Type: core.ReleaseContentTypeHelm,
				Helm: &core.ReleaseContentHelmSpec{
					Path:           "charts/payments",
					DependencyMode: core.ReleaseHelmDependencyModeVendored,
				},
			},
			ClusterRef: core.ReleaseClusterRefSpec{
				Name: "prod-eu",
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

// releaseMissingSourceName is invalid: source.name is required.
func releaseMissingSourceName(namespace string) *core.Release {
	r := validNewRelease(namespace)
	r.Spec.Source.Name = ""
	return r
}

func TestCreate(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	ns := test.TestNamespace()
	test.TestCreate(
		validNewRelease(ns),
		releaseMissingContentType(ns),
		releaseMissingSourceName(ns),
	)
}

func TestUpdate(t *testing.T) {
	storage, _, server := newStorage(t)
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
			r.Spec.Source.Kind = "OCIRegistry"
			return r
		},
	)
}

func TestGet(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestGet(validNewRelease(test.TestNamespace()))
}

func TestList(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestList(validNewRelease(test.TestNamespace()))
}

func TestDelete(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestDelete(validNewRelease(test.TestNamespace()))
}

// TestStatusUpdate verifies the /status subresource updates status while leaving
// spec untouched.
func TestStatusUpdate(t *testing.T) {
	storage, statusStorage, server := newStorage(t)
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
