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

package resourcehealthrule_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	genericregistrytest "k8s.io/apiserver/pkg/registry/generic/testing"
	etcd3testing "k8s.io/apiserver/pkg/storage/etcd3/testing"

	"github.com/kumodo-io/convoy/api/extensions"
	extensionsinstall "github.com/kumodo-io/convoy/api/extensions/install"
	extensionsv1alpha1 "github.com/kumodo-io/convoy/api/extensions/v1alpha1"
	resourcehealthrulestorage "github.com/kumodo-io/convoy/internal/registry/extensions/resourcehealthrule"
	"github.com/kumodo-io/convoy/internal/registry/registrytest"
)

func newStorage(t *testing.T) (*genericregistry.Store, *etcd3testing.EtcdTestServer) {
	scheme, restOptions, server := registrytest.NewEtcdStorage(t, extensionsinstall.Install, extensionsv1alpha1.SchemeGroupVersion, extensions.Resource("resourcehealthrules"))

	restStorage, err := resourcehealthrulestorage.NewREST(scheme, restOptions)
	if err != nil {
		server.Terminate(t)
		t.Fatalf("unexpected error from NewREST: %v", err)
	}
	return restStorage.Store, server
}

func validNewResourceHealthRule(namespace string) *extensions.ResourceHealthRule {
	return &extensions.ResourceHealthRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "payments-db-health",
			Namespace: namespace,
		},
		Spec: extensions.ResourceHealthRuleSpec{
			Target: extensions.ResourceTarget{
				Group:     "apps",
				Kind:      "StatefulSet",
				Version:   "v1",
				Name:      "payments-db",
				Namespace: namespace,
			},
			HealthRule: extensions.HealthRule{
				Expression: `object.status.readyReplicas == object.status.replicas ? "Healthy" : "Progressing"`,
			},
		},
	}
}

// rhrMissingExpression is invalid: a CEL expression is required.
func rhrMissingExpression(namespace string) *extensions.ResourceHealthRule {
	r := validNewResourceHealthRule(namespace)
	r.Spec.Expression = ""
	return r
}

// rhrMissingTargetName is invalid: target.name is required.
func rhrMissingTargetName(namespace string) *extensions.ResourceHealthRule {
	r := validNewResourceHealthRule(namespace)
	r.Spec.Target.Name = ""
	return r
}

func TestCreate(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	ns := test.TestNamespace()
	test.TestCreate(
		validNewResourceHealthRule(ns),
		rhrMissingExpression(ns),
		rhrMissingTargetName(ns),
	)
}

func TestUpdate(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestUpdate(
		validNewResourceHealthRule(test.TestNamespace()),
		func(obj runtime.Object) runtime.Object {
			r := obj.(*extensions.ResourceHealthRule)
			r.Spec.MessageExpression = `"updated"`
			return r
		},
		func(obj runtime.Object) runtime.Object {
			r := obj.(*extensions.ResourceHealthRule)
			r.Spec.Expression = ""
			return r
		},
	)
}

func TestGet(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestGet(validNewResourceHealthRule(test.TestNamespace()))
}

func TestList(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestList(validNewResourceHealthRule(test.TestNamespace()))
}

func TestDelete(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestDelete(validNewResourceHealthRule(test.TestNamespace()))
}
