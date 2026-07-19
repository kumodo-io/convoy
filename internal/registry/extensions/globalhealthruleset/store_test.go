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

package globalhealthruleset_test

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
	GlobalHealthRuleSetstorage "github.com/kumodo-io/convoy/internal/registry/extensions/globalhealthruleset"
	"github.com/kumodo-io/convoy/internal/registry/registrytest"
)

func newStorage(t *testing.T) (*genericregistry.Store, *etcd3testing.EtcdTestServer) {
	scheme, restOptions, server := registrytest.NewEtcdStorage(t, extensionsinstall.Install, extensionsv1alpha1.SchemeGroupVersion, extensions.Resource("GlobalHealthRuleSets"))

	restStorage, err := GlobalHealthRuleSetstorage.NewREST(scheme, restOptions)
	if err != nil {
		server.Terminate(t)
		t.Fatalf("unexpected error from NewREST: %v", err)
	}
	return restStorage.Store, server
}

// validNewGlobalHealthRuleSet returns a cluster-scoped rule (no namespace).
func validNewGlobalHealthRuleSet() *extensions.GlobalHealthRuleSet {
	return &extensions.GlobalHealthRuleSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "certificate-health",
		},
		Spec: extensions.GlobalHealthRuleSetSpec{
			Rules: []extensions.RuleSpec{
				{
					ApiVersion:     "cert-manager/v1",
					Resources:      []string{"certificates"},
					RuleExpression: `object.status.phase == "Ready" ? "Healthy" : "Progressing"`,
				},
			},
		},
	}
}

// khrMissingExpression is invalid: a CEL expression is required.
func khrMissingExpression() *extensions.GlobalHealthRuleSet {
	r := validNewGlobalHealthRuleSet()
	r.Spec.Rules[0].RuleExpression = ""
	return r
}

// khrMissingKind is invalid: api version is required.
func khrMissingApiVersion() *extensions.GlobalHealthRuleSet {
	r := validNewGlobalHealthRuleSet()
	r.Spec.Rules[0].ApiVersion = ""
	return r
}

func TestCreate(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage).ClusterScope()
	test.TestCreate(
		validNewGlobalHealthRuleSet(),
		khrMissingExpression(),
		khrMissingApiVersion(),
	)
}

func TestUpdate(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage).ClusterScope()
	test.TestUpdate(
		validNewGlobalHealthRuleSet(),
		// valid update: change the message expression
		func(obj runtime.Object) runtime.Object {
			r := obj.(*extensions.GlobalHealthRuleSet)
			r.Spec.Rules[0].MessageExpression = `"updated"`
			return r
		},
		// invalid update: clears the required expression
		func(obj runtime.Object) runtime.Object {
			r := obj.(*extensions.GlobalHealthRuleSet)
			r.Spec.Rules[0].RuleExpression = ""
			return r
		},
	)
}

func TestGet(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage).ClusterScope()
	test.TestGet(validNewGlobalHealthRuleSet())
}

func TestList(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage).ClusterScope()
	test.TestList(validNewGlobalHealthRuleSet())
}

func TestDelete(t *testing.T) {
	storage, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage).ClusterScope()
	test.TestDelete(validNewGlobalHealthRuleSet())
}
