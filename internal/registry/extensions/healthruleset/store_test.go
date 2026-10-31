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

package healthruleset_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	genericregistrytest "k8s.io/apiserver/pkg/registry/generic/testing"
	etcd3testing "k8s.io/apiserver/pkg/storage/etcd3/testing"

	"github.com/kumodo-io/convoy/api/extensions"
	extensionsinstall "github.com/kumodo-io/convoy/api/extensions/install"
	extensionsv1alpha1 "github.com/kumodo-io/convoy/api/extensions/v1alpha1"
	healthrulesetstorage "github.com/kumodo-io/convoy/internal/registry/extensions/healthruleset"
	"github.com/kumodo-io/convoy/internal/registry/registrytest"
)

func newStorage(t *testing.T) (*genericregistry.Store, *etcd3testing.EtcdTestServer) {
	scheme, restOptions, server := registrytest.NewEtcdStorage(t, extensionsinstall.Install, extensionsv1alpha1.SchemeGroupVersion, extensions.Resource("HealthRuleSets"))

	restStorage, err := healthrulesetstorage.NewREST(scheme, restOptions)
	if err != nil {
		server.Terminate(t)
		t.Fatalf("unexpected error from NewREST: %v", err)
	}
	return restStorage.Store, server
}

func validNewHealthRuleSet(namespace string) *extensions.HealthRuleSet {
	return &extensions.HealthRuleSet{
		Name:      "payments-db-health",
		Namespace: namespace,
		Spec: extensions.HealthRuleSetSpec{
			Rules: []extensions.RuleSpec{
				{
					ApiVersion:     "apps/v1",
					Resources:      []string{"deployments", "replicasets", "statefulsets"},
					RuleExpression: `object.status.readyReplicas == object.status.replicas ? "Healthy" : "Progressing"`,
				},
			},
		},
	}
}

// rhrMissingExpression is invalid: a CEL expression is required.
func rhrMissingExpression(namespace string) *extensions.HealthRuleSet {
	r := validNewHealthRuleSet(namespace)
	r.Spec.Rules[0].RuleExpression = ""
	return r
}

// rhrMissingTargetName is invalid: api version is required.
func rhrMissingApiVersion(namespace string) *extensions.HealthRuleSet {
	r := validNewHealthRuleSet(namespace)
	r.Spec.Rules[0].ApiVersion = ""
	return r
}

func TestCreate(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	ns := test.TestNamespace()
	test.TestCreate(
		validNewHealthRuleSet(ns),
		rhrMissingExpression(ns),
		rhrMissingApiVersion(ns),
	)
}

func TestUpdate(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestUpdate(
		validNewHealthRuleSet(test.TestNamespace()),
		func(obj runtime.Object) runtime.Object {
			r := obj.(*extensions.HealthRuleSet)
			r.Spec.Rules[0].MessageExpression = `"updated"`
			return r
		},
		func(obj runtime.Object) runtime.Object {
			r := obj.(*extensions.HealthRuleSet)
			r.Spec.Rules[0].RuleExpression = ""
			return r
		},
	)
}

func TestGet(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestGet(validNewHealthRuleSet(test.TestNamespace()))
}

func TestList(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestList(validNewHealthRuleSet(test.TestNamespace()))
}

func TestDelete(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestDelete(validNewHealthRuleSet(test.TestNamespace()))
}
