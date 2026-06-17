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

package helmregistry_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	genericregistrytest "k8s.io/apiserver/pkg/registry/generic/testing"
	"k8s.io/apiserver/pkg/registry/rest"
	etcd3testing "k8s.io/apiserver/pkg/storage/etcd3/testing"

	"github.com/kumodo-io/convoy/api/source"
	sourceinstall "github.com/kumodo-io/convoy/api/source/install"
	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
	"github.com/kumodo-io/convoy/internal/registry/registrytest"
	helmregistrystorage "github.com/kumodo-io/convoy/internal/registry/source/helmregistry"
)

func newStorage(t *testing.T) (*genericregistry.Store, *helmregistrystorage.StatusREST, *etcd3testing.EtcdTestServer) {
	scheme, restOptions, server := registrytest.NewEtcdStorage(t, sourceinstall.Install, sourcev1alpha1.SchemeGroupVersion, source.Resource("helmregistries"))

	restStorage, statusStorage, err := helmregistrystorage.NewREST(scheme, restOptions)
	if err != nil {
		server.Terminate(t)
		t.Fatalf("unexpected error from NewREST: %v", err)
	}
	return restStorage.Store, statusStorage, server
}

func validNewHelmRegistry(namespace string) *source.HelmRegistry {
	return &source.HelmRegistry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: namespace,
		},
		Spec: source.HelmRegistrySpec{
			URL: "https://charts.example.com",
			Auth: source.HelmRegistryAuthSpec{
				Type: source.HelmRegistryAuthTypeNone,
			},
		},
	}
}

// helmMissingURL is invalid: url is required.
func helmMissingURL(namespace string) *source.HelmRegistry {
	r := validNewHelmRegistry(namespace)
	r.Spec.URL = ""
	return r
}

func TestCreate(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	ns := test.TestNamespace()
	// todo: add test for invalid auth type
	test.TestCreate(
		validNewHelmRegistry(ns),
		helmMissingURL(ns),
	)
}

func TestUpdate(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestUpdate(
		validNewHelmRegistry(test.TestNamespace()),
		func(obj runtime.Object) runtime.Object {
			r := obj.(*source.HelmRegistry)
			r.Spec.URL = "https://charts2.example.com"
			return r
		},
		func(obj runtime.Object) runtime.Object {
			r := obj.(*source.HelmRegistry)
			r.Spec.URL = ""
			return r
		},
	)
}

func TestGet(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestGet(validNewHelmRegistry(test.TestNamespace()))
}

func TestList(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestList(validNewHelmRegistry(test.TestNamespace()))
}

func TestDelete(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestDelete(validNewHelmRegistry(test.TestNamespace()))
}

func TestStatusUpdate(t *testing.T) {
	storage, statusStorage, server := newStorage(t)
	defer server.Terminate(t)

	ctx := genericapirequest.WithNamespace(genericapirequest.NewContext(), metav1.NamespaceDefault)

	if _, err := storage.Create(ctx, validNewHelmRegistry(metav1.NamespaceDefault), rest.ValidateAllObjectFunc, &metav1.CreateOptions{}); err != nil {
		t.Fatalf("unexpected error creating helmregistry: %v", err)
	}

	obj, err := statusStorage.Get(ctx, "foo", &metav1.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected error getting helmregistry: %v", err)
	}
	reg := obj.(*source.HelmRegistry)

	reg.Status.LastFetched.ChartCount = 87
	reg.Spec.URL = "https://changed.example.com"

	updatedObj, _, err := statusStorage.Update(
		ctx,
		reg.Name,
		rest.DefaultUpdatedObjectInfo(reg),
		rest.ValidateAllObjectFunc,
		rest.ValidateAllObjectUpdateFunc,
		false,
		&metav1.UpdateOptions{},
	)
	if err != nil {
		t.Fatalf("unexpected error updating status: %v", err)
	}

	updated := updatedObj.(*source.HelmRegistry)
	if updated.Status.LastFetched.ChartCount != 87 {
		t.Errorf("expected status.lastFetched.chartCount to persist, got %d", updated.Status.LastFetched.ChartCount)
	}
	if updated.Spec.URL != "https://charts.example.com" {
		t.Errorf("expected spec change to be ignored by /status, got url=%q", updated.Spec.URL)
	}
}
