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

package render

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/kyaml/openapi"
	kyaml "sigs.k8s.io/kustomize/kyaml/yaml"
)

// setNamespace gives namespace ns to each rendered object that has none, unless its
// kind is cluster-scoped. It works without the target cluster, like the kustomize
// namespace transformer, so the render (and its renderRef) does not depend on cluster
// state: a kind the built-in schema does not know takes the scope of its CRD in the
// same render, and counts as namespaced without one.
func setNamespace(objs []*unstructured.Unstructured, ns string) {
	if ns == "" {
		return
	}
	clusterScoped := clusterScopedCRDKinds(objs)
	for _, o := range objs {
		if o.GetNamespace() != "" {
			continue
		}
		gvk := o.GroupVersionKind()
		namespaced, known := openapi.IsNamespaceScoped(kyaml.TypeMeta{APIVersion: o.GetAPIVersion(), Kind: gvk.Kind})
		if known && !namespaced {
			continue
		}
		if !known && clusterScoped[gvk.GroupKind().String()] {
			continue
		}
		o.SetNamespace(ns)
	}
}

// clusterScopedCRDKinds returns the group-kinds ("Kind.group") that the CRDs in the
// render declare as cluster-scoped.
func clusterScopedCRDKinds(objs []*unstructured.Unstructured) map[string]bool {
	kinds := map[string]bool{}
	for _, o := range objs {
		gvk := o.GroupVersionKind()
		if gvk.Group != "apiextensions.k8s.io" || gvk.Kind != "CustomResourceDefinition" {
			continue
		}
		scope, _, _ := unstructured.NestedString(o.Object, "spec", "scope")
		group, _, _ := unstructured.NestedString(o.Object, "spec", "group")
		kind, _, _ := unstructured.NestedString(o.Object, "spec", "names", "kind")
		if scope == "Cluster" && kind != "" {
			kinds[kind+"."+group] = true
		}
	}
	return kinds
}
