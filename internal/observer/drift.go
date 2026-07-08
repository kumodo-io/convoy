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

package observer

import (
	"context"
	"fmt"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

const (
	metadataFieldName = "metadata"
	statusFieldName   = "status"
)

// driftFieldManager is the field manager the observer dry-run-applies under. It
// matches the deployer's manager so the dry run reflects exactly what a real
// apply would change; dry-run never persists ownership.
const driftFieldManager = "convoy"

// detectDrift reports whether the live cluster has drifted from the desired
// objects: true if any desired object is absent or a dry-run apply would change
// it. Comparison is over managed state — server-side defaulting and admission
// cancel out because both sides come back from the API server.
func detectDrift(ctx context.Context, dyn dynamic.Interface, mapper meta.RESTMapper, objects []*unstructured.Unstructured) (bool, error) {
	for _, obj := range objects {
		drifted, err := objectDrifted(ctx, dyn, mapper, obj)
		if err != nil {
			return false, err
		}
		if drifted {
			return true, nil
		}
	}
	return false, nil
}

// objectDrifted reports whether a single desired object differs from its live
// counterpart. A missing object is drift; otherwise a dry-run apply is compared
// to the live object, ignoring server-managed metadata noise.
func objectDrifted(ctx context.Context, dyn dynamic.Interface, mapper meta.RESTMapper, obj *unstructured.Unstructured) (bool, error) {
	ri, err := resourceFor(dyn, mapper, obj)
	if err != nil {
		return false, err
	}
	name := obj.GetName()

	live, err := ri.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return true, nil // desired but absent — drifted (or never applied)
	}
	if err != nil {
		return false, fmt.Errorf("reading live %s %q: %w", obj.GetKind(), name, err)
	}

	data, err := obj.MarshalJSON()
	if err != nil {
		return false, fmt.Errorf("marshaling %s %q: %w", obj.GetKind(), name, err)
	}
	result, err := ri.Patch(ctx, name, types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: driftFieldManager,
		Force:        new(true),
		DryRun:       []string{metav1.DryRunAll},
	})
	if err != nil {
		return false, fmt.Errorf("dry-run applying %s %q: %w", obj.GetKind(), name, err)
	}

	return !equalIgnoringNoise(live, result), nil
}

// resourceFor resolves obj's GVK to a namespaced-or-cluster dynamic resource client.
func resourceFor(dyn dynamic.Interface, mapper meta.RESTMapper, obj *unstructured.Unstructured) (dynamic.ResourceInterface, error) {
	gvk := obj.GroupVersionKind()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("rest mapping for %s: %w", gvk, err)
	}
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ns := obj.GetNamespace()
		if ns == "" {
			ns = metav1.NamespaceDefault
		}
		return dyn.Resource(mapping.Resource).Namespace(ns), nil
	}
	return dyn.Resource(mapping.Resource), nil
}

// equalIgnoringNoise compares two objects after stripping server-managed metadata
// (resourceVersion, uid, generation, creationTimestamp, managedFields) and status.
// The desired vs dry-run-result comparison cancels defaulting; only convoy-managed
// state remains, so a difference is real drift.
func equalIgnoringNoise(live, desired *unstructured.Unstructured) bool {
	return apiequality.Semantic.DeepEqual(normalize(live), normalize(desired))
}

func normalize(o *unstructured.Unstructured) map[string]any {
	c := o.DeepCopy()
	for _, f := range [][]string{
		{metadataFieldName, "resourceVersion"},
		{metadataFieldName, "uid"},
		{metadataFieldName, "generation"},
		{metadataFieldName, "creationTimestamp"},
		{metadataFieldName, "managedFields"},
		{statusFieldName},
	} {
		unstructured.RemoveNestedField(c.Object, f...)
	}
	return c.Object
}
