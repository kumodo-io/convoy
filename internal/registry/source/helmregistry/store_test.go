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

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
	"github.com/kumodo-io/convoy/api/source"
	sourceinstall "github.com/kumodo-io/convoy/api/source/install"
	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
	"github.com/kumodo-io/convoy/internal/registry/registrytest"
	helmregistrystorage "github.com/kumodo-io/convoy/internal/registry/source/helmregistry"
)

const (
	dummyHelmRegistryName = "dummy-helmregistry"
	dummyHelmRegistryURL  = "https://dummy.example.com/charts"
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
			Name:      dummyHelmRegistryName,
			Namespace: namespace,
		},
		Spec: source.HelmRegistrySpec{
			URL: dummyHelmRegistryURL,
			Auth: source.HelmRegistryAuthSpec{
				Type: source.HelmRegistryAuthTypeNone,
			},
		},
	}
}

func invalidNewHelmRegistryNoURL(namespace string) *source.HelmRegistry {
	return &source.HelmRegistry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyHelmRegistryName,
			Namespace: namespace,
		},
		Spec: source.HelmRegistrySpec{
			Auth: source.HelmRegistryAuthSpec{
				Type: source.HelmRegistryAuthTypeNone,
			},
		},
	}
}

func invalidNewHelmRegistryNoAuth(namespace string) *source.HelmRegistry {
	return &source.HelmRegistry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyHelmRegistryName,
			Namespace: namespace,
		},
		Spec: source.HelmRegistrySpec{
			URL: dummyHelmRegistryURL,
		},
	}
}

func invalidNewHelmRegistryUnknownAuthType(namespace string) *source.HelmRegistry {
	return &source.HelmRegistry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyHelmRegistryName,
			Namespace: namespace,
		},
		Spec: source.HelmRegistrySpec{
			URL: dummyHelmRegistryURL,
			Auth: source.HelmRegistryAuthSpec{
				Type: "unknown",
			},
		},
	}
}

func invalidNewHelmRegistryMultipleAuthConfigs(namespace string) *source.HelmRegistry {
	return &source.HelmRegistry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyHelmRegistryName,
			Namespace: namespace,
		},
		Spec: source.HelmRegistrySpec{
			URL: dummyHelmRegistryURL,
			Auth: source.HelmRegistryAuthSpec{
				Type:  source.HelmRegistryAuthTypeToken,
				Token: &authv1.TokenAuthSpec{},
				OIDC:  &authv1.OIDAuthSpec{},
			},
		},
	}
}

func invalidNewHelmRegistryMismatchAuthConfig(namespace string) *source.HelmRegistry {
	return &source.HelmRegistry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyHelmRegistryName,
			Namespace: namespace,
		},
		Spec: source.HelmRegistrySpec{
			URL: dummyHelmRegistryURL,
			Auth: source.HelmRegistryAuthSpec{
				Type:  source.HelmRegistryAuthTypeOIDC,
				Token: &authv1.TokenAuthSpec{},
			},
		},
	}
}

func TestCreate(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	ns := test.TestNamespace()
	test.TestCreate(
		validNewHelmRegistry(ns),
		invalidNewHelmRegistryNoURL(ns),
		invalidNewHelmRegistryNoAuth(ns),
		invalidNewHelmRegistryUnknownAuthType(ns),
		invalidNewHelmRegistryMultipleAuthConfigs(ns),
		invalidNewHelmRegistryMismatchAuthConfig(ns),
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

	obj, err := statusStorage.Get(ctx, dummyHelmRegistryName, &metav1.GetOptions{})
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
	if updated.Spec.URL != dummyHelmRegistryURL {
		t.Errorf("expected spec change to be ignored by /status, got url=%q", updated.Spec.URL)
	}
}
