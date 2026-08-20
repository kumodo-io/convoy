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

package release

import (
	"context"
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

// fakeDynamicWithConfigMap builds a namespaced ConfigMap dynamic client + RESTMapper
// that knows only the ConfigMap kind — so any other kind has no REST mapping.
func fakeDynamicWithConfigMap() (dynamic.Interface, meta.RESTMapper) {
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{gvr: "ConfigMapList"})
	m := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "", Version: "v1"}})
	m.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)
	return dyn, m
}

func cmObject(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		apiVersionKey: "v1", kindKey: "ConfigMap",
		metadataKey: map[string]any{nameKey: name, namespaceKey: "ns"},
	}}
}

// TestDryRunApply_MappedKindRejectionPropagates verifies that a real rejection on a
// mapped kind (immutable field, schema, RBAC — simulated here by a patch reactor) is
// returned, not swallowed like a no-match. (SSA success can't be exercised against
// the fake dynamic client, which doesn't implement server-side apply.)
func TestDryRunApply_MappedKindRejectionPropagates(t *testing.T) {
	dyn, mapper := fakeDynamicWithConfigMap()
	dyn.(*dynamicfake.FakeDynamicClient).PrependReactor("patch", "configmaps",
		func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("field is immutable")
		})
	err := dryRunApply(context.Background(), dyn, mapper, cmObject("cm"))
	if err == nil {
		t.Fatal("dryRunApply swallowed a real rejection on a mapped kind")
	}
	if !strings.Contains(err.Error(), "immutable") {
		t.Errorf("error = %v, want the underlying rejection", err)
	}
}

// TestDryRunApply_UnmappedKindIsSkipped verifies the leniency: an object whose kind
// has no REST mapping (e.g. a CR whose CRD is created in an earlier wave) is skipped,
// not reported as a failure.
func TestDryRunApply_UnmappedKindIsSkipped(t *testing.T) {
	dyn, mapper := fakeDynamicWithConfigMap() // mapper knows only ConfigMap
	cr := &unstructured.Unstructured{Object: map[string]any{
		apiVersionKey: "example.com/v1", kindKey: "Widget",
		metadataKey: map[string]any{nameKey: "w", namespaceKey: "ns"},
	}}
	if err := dryRunApply(context.Background(), dyn, mapper, cr); err != nil {
		t.Errorf("dryRunApply of an unmapped kind should be skipped, got error: %v", err)
	}
}
