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

package ociregistry_test

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
	ociregistrystorage "github.com/kumodo-io/convoy/internal/registry/source/ociregistry"
)

const (
	dummyOCIRegistryName = "dummy-ociregistry"
	dummyOCIRegistryURL  = "oci://ghcr.io/example/charts"
)

func newStorage(t *testing.T) (*genericregistry.Store, *ociregistrystorage.StatusREST, *etcd3testing.EtcdTestServer) {
	scheme, restOptions, server := registrytest.NewEtcdStorage(t, sourceinstall.Install, sourcev1alpha1.SchemeGroupVersion, source.Resource("ociregistries"))

	restStorage, statusStorage, err := ociregistrystorage.NewREST(scheme, restOptions)
	if err != nil {
		server.Terminate(t)
		t.Fatalf("unexpected error from NewREST: %v", err)
	}
	return restStorage.Store, statusStorage, server
}

func validNewOCIRegistry(namespace string) *source.OCIRegistry {
	return &source.OCIRegistry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyOCIRegistryName,
			Namespace: namespace,
		},
		Spec: source.OCIRegistrySpec{
			URL:                dummyOCIRegistryURL,
			DefaultContentType: source.ContentTypeHelm,
			Auth: source.OCIRegistryAuthSpec{
				Type: source.OCIRegistryAuthTypeNone,
			},
		},
	}
}

func invalidNewOCIRegistryNoURL(namespace string) *source.OCIRegistry {
	return &source.OCIRegistry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyOCIRegistryName,
			Namespace: namespace,
		},
		Spec: source.OCIRegistrySpec{
			Auth: source.OCIRegistryAuthSpec{
				Type: source.OCIRegistryAuthTypeNone,
			},
		},
	}
}

func invalidNewOCIRegistryNoAuth(namespace string) *source.OCIRegistry {
	return &source.OCIRegistry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyOCIRegistryName,
			Namespace: namespace,
		},
		Spec: source.OCIRegistrySpec{
			URL: dummyOCIRegistryURL,
		},
	}
}

func invalidNewOCIRegistryUnknownAuthType(namespace string) *source.OCIRegistry {
	return &source.OCIRegistry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyOCIRegistryName,
			Namespace: namespace,
		},
		Spec: source.OCIRegistrySpec{
			URL: dummyOCIRegistryURL,
			Auth: source.OCIRegistryAuthSpec{
				Type: "unknown",
			},
		},
	}
}

func invalidNewOCIRegistryMultipleAuthConfigs(namespace string) *source.OCIRegistry {
	return &source.OCIRegistry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyOCIRegistryName,
			Namespace: namespace,
		},
		Spec: source.OCIRegistrySpec{
			URL: dummyOCIRegistryURL,
			Auth: source.OCIRegistryAuthSpec{
				Type:  source.OCIRegistryAuthTypeToken,
				Token: &authv1.TokenAuthSpec{},
				OIDC:  &authv1.OIDAuthSpec{},
			},
		},
	}
}

func invalidNewOCIRegistryMismatchAuthConfig(namespace string) *source.OCIRegistry {
	return &source.OCIRegistry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyOCIRegistryName,
			Namespace: namespace,
		},
		Spec: source.OCIRegistrySpec{
			URL: dummyOCIRegistryURL,
			Auth: source.OCIRegistryAuthSpec{
				Type:  source.OCIRegistryAuthTypeOIDC,
				Token: &authv1.TokenAuthSpec{},
			},
		},
	}
}

func invalidNewOCIRegistryNoDefaultContentType(namespace string) *source.OCIRegistry {
	return &source.OCIRegistry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyOCIRegistryName,
			Namespace: namespace,
		},
		Spec: source.OCIRegistrySpec{
			URL: dummyOCIRegistryURL,
			Auth: source.OCIRegistryAuthSpec{
				Type: source.OCIRegistryAuthTypeNone,
			},
		},
	}
}

func invalidNewOCIRegistryUnknownDefaultContentType(namespace string) *source.OCIRegistry {
	return &source.OCIRegistry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyOCIRegistryName,
			Namespace: namespace,
		},
		Spec: source.OCIRegistrySpec{
			URL:                dummyOCIRegistryURL,
			DefaultContentType: "unknown",
			Auth: source.OCIRegistryAuthSpec{
				Type: source.OCIRegistryAuthTypeNone,
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
		validNewOCIRegistry(ns),
		invalidNewOCIRegistryNoURL(ns),
		invalidNewOCIRegistryNoAuth(ns),
		invalidNewOCIRegistryUnknownAuthType(ns),
		invalidNewOCIRegistryMultipleAuthConfigs(ns),
		invalidNewOCIRegistryMismatchAuthConfig(ns),
		invalidNewOCIRegistryNoDefaultContentType(ns),
		invalidNewOCIRegistryUnknownDefaultContentType(ns),
	)
}

func TestUpdate(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestUpdate(
		validNewOCIRegistry(test.TestNamespace()),
		func(obj runtime.Object) runtime.Object {
			r := obj.(*source.OCIRegistry)
			r.Spec.DefaultContentType = source.ContentTypeManifests
			return r
		},
		func(obj runtime.Object) runtime.Object {
			r := obj.(*source.OCIRegistry)
			r.Spec.DefaultContentType = "bogus"
			return r
		},
	)
}

func TestGet(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestGet(validNewOCIRegistry(test.TestNamespace()))
}

func TestList(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestList(validNewOCIRegistry(test.TestNamespace()))
}

func TestDelete(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestDelete(validNewOCIRegistry(test.TestNamespace()))
}

func TestStatusUpdate(t *testing.T) {
	storage, statusStorage, server := newStorage(t)
	defer server.Terminate(t)

	ctx := genericapirequest.WithNamespace(genericapirequest.NewContext(), metav1.NamespaceDefault)

	if _, err := storage.Create(ctx, validNewOCIRegistry(metav1.NamespaceDefault), rest.ValidateAllObjectFunc, &metav1.CreateOptions{}); err != nil {
		t.Fatalf("unexpected error creating ociregistry: %v", err)
	}

	obj, err := statusStorage.Get(ctx, dummyOCIRegistryName, &metav1.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected error getting ociregistry: %v", err)
	}
	reg := obj.(*source.OCIRegistry)

	reg.Status.ReleaseCount = 8
	reg.Spec.URL = "oci://changed.example.com/charts"

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

	updated := updatedObj.(*source.OCIRegistry)
	if updated.Status.ReleaseCount != 8 {
		t.Errorf("expected status.releaseCount to persist, got %d", updated.Status.ReleaseCount)
	}
	if updated.Spec.URL != dummyOCIRegistryURL {
		t.Errorf("expected spec change to be ignored by /status, got url=%q", updated.Spec.URL)
	}
}
