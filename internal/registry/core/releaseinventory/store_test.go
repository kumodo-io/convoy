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

package releaseinventory_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	genericregistrytest "k8s.io/apiserver/pkg/registry/generic/testing"
	etcd3testing "k8s.io/apiserver/pkg/storage/etcd3/testing"

	"github.com/kumodo-io/convoy/api/core"
	coreinstall "github.com/kumodo-io/convoy/api/core/install"
	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	releaseinventorystorage "github.com/kumodo-io/convoy/internal/registry/core/releaseinventory"
	"github.com/kumodo-io/convoy/internal/registry/registrytest"
)

func newStorage(t *testing.T) (*genericregistry.Store, *etcd3testing.EtcdTestServer) {
	scheme, restOptions, server := registrytest.NewEtcdStorage(t, coreinstall.Install, corev1alpha1.SchemeGroupVersion, core.Resource("releaseinventories"))

	restStorage, err := releaseinventorystorage.NewREST(scheme, restOptions)
	if err != nil {
		server.Terminate(t)
		t.Fatalf("unexpected error from NewREST: %v", err)
	}
	return restStorage.Store, server
}

func validNewReleaseInventory(namespace string) *core.ReleaseInventory {
	return &core.ReleaseInventory{
		Name:      "payments",
		Namespace: namespace,
		Inventory: []core.ResourceInventoryItem{
			{
				Group:     "apps",
				Version:   "v1",
				Kind:      "Deployment",
				Namespace: namespace,
				Name:      "payments-api",
				UID:       new(types.UID("11111111-1111-1111-1111-111111111111")),
				Wave:      new(0),
				State:     new(core.ResourceStateHealthy),
			},
		},
	}
}

// riInvalidName is invalid: the name is not a valid DNS subdomain, so object
// metadata validation must reject it.
func riInvalidName(namespace string) *core.ReleaseInventory {
	r := validNewReleaseInventory(namespace)
	r.Name = "Invalid_Name!"
	return r
}

func TestCreate(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	ns := test.TestNamespace()
	test.TestCreate(
		validNewReleaseInventory(ns),
		riInvalidName(ns),
	)
}

func TestUpdate(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage).AllowCreateOnUpdate()
	test.TestUpdate(
		validNewReleaseInventory(test.TestNamespace()),
		func(obj runtime.Object) runtime.Object {
			r := obj.(*core.ReleaseInventory)
			r.Inventory[0].State = new(core.ResourceStateDegraded)
			return r
		},
	)
}

func TestGet(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestGet(validNewReleaseInventory(test.TestNamespace()))
}

func TestList(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestList(validNewReleaseInventory(test.TestNamespace()))
}

func TestDelete(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestDelete(validNewReleaseInventory(test.TestNamespace()))
}
