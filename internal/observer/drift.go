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
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gomodules.xyz/jsonpatch/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	"github.com/kumodo-io/convoy/internal/diffresult"
)

const (
	metadataFieldName = "metadata"
	statusFieldName   = "status"
)

// driftFieldManager is the field manager the observer dry-run-applies under. It
// matches the deployer's manager so the dry run reflects exactly what a real
// apply would change; dry-run never persists ownership.
const driftFieldManager = "convoy"

// computeDiff produces the per-object drift of the desired objects against the
// live cluster. Each object is either Missing (absent), Modified (a dry-run
// apply would change it), or InSync. Comparison is over managed state —
// server-side defaulting and admission cancel out because both sides come back
// from the API server. This is the single engine behind both the periodic drift
// loop (via Result.HasDrift) and the on-demand /diff subresource.
func computeDiff(ctx context.Context, dyn dynamic.Interface, mapper meta.RESTMapper, objects []*unstructured.Unstructured) (*diffresult.Result, error) {
	result := &diffresult.Result{Objects: make([]diffresult.ObjectDiff, 0, len(objects))}
	for _, obj := range objects {
		od, err := objectDiff(ctx, dyn, mapper, obj)
		if err != nil {
			return nil, err
		}
		result.Objects = append(result.Objects, od)
	}
	return result, nil
}

// objectDiff computes the drift of a single desired object against its live
// counterpart.
func objectDiff(ctx context.Context, dyn dynamic.Interface, mapper meta.RESTMapper, obj *unstructured.Unstructured) (diffresult.ObjectDiff, error) {
	gvk := obj.GroupVersionKind()
	od := diffresult.ObjectDiff{
		Group:     gvk.Group,
		Version:   gvk.Version,
		Kind:      gvk.Kind,
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}

	ri, err := resourceFor(dyn, mapper, obj)
	if err != nil {
		return od, err
	}
	name := obj.GetName()

	live, err := ri.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		od.Status = diffresult.StatusMissing // desired but absent (or never applied)
		return od, nil
	}
	if err != nil {
		return od, fmt.Errorf("reading live %s %q: %w", obj.GetKind(), name, err)
	}

	data, err := obj.MarshalJSON()
	if err != nil {
		return od, fmt.Errorf("marshaling %s %q: %w", obj.GetKind(), name, err)
	}
	result, err := ri.Patch(ctx, name, types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: driftFieldManager,
		Force:        new(true),
		DryRun:       []string{metav1.DryRunAll},
	})
	if err != nil {
		return od, fmt.Errorf("dry-run applying %s %q: %w", obj.GetKind(), name, err)
	}

	changes, err := fieldChanges(normalize(live), normalize(result))
	if err != nil {
		return od, fmt.Errorf("diffing %s %q: %w", obj.GetKind(), name, err)
	}
	if len(changes) == 0 {
		od.Status = diffresult.StatusInSync
	} else {
		od.Status = diffresult.StatusModified
		od.Changes = changes
	}
	return od, nil
}

// fieldChanges computes the field-level differences that transform live into
// desired, as an RFC6902 patch. Each op becomes a FieldChange carrying the
// target value (add/replace) and, resolved from live, the current value
// (replace/remove).
func fieldChanges(live, desired map[string]any) ([]diffresult.FieldChange, error) {
	liveJSON, err := json.Marshal(live)
	if err != nil {
		return nil, err
	}
	desiredJSON, err := json.Marshal(desired)
	if err != nil {
		return nil, err
	}
	ops, err := jsonpatch.CreatePatch(liveJSON, desiredJSON)
	if err != nil {
		return nil, err
	}

	changes := make([]diffresult.FieldChange, 0, len(ops))
	for _, op := range ops {
		fc := diffresult.FieldChange{Path: op.Path, Op: op.Operation}
		switch op.Operation {
		case "add":
			fc.Desired = op.Value
		case "replace":
			fc.Desired = op.Value
			if v, ok := resolvePointer(live, op.Path); ok {
				fc.Live = v
			}
		case "remove":
			if v, ok := resolvePointer(live, op.Path); ok {
				fc.Live = v
			}
		}
		changes = append(changes, fc)
	}
	return changes, nil
}

// resolvePointer resolves an RFC6901 JSON Pointer against a decoded JSON value
// (map[string]any / []any), returning the value and whether it was found.
func resolvePointer(root any, pointer string) (any, bool) {
	if pointer == "" || pointer == "/" {
		return root, true
	}
	cur := root
	for _, tok := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		tok = strings.ReplaceAll(tok, "~1", "/")
		tok = strings.ReplaceAll(tok, "~0", "~")
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[tok]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(tok)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			cur = node[i]
		default:
			return nil, false
		}
	}
	return cur, true
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

// normalize strips server-managed metadata (resourceVersion, uid, generation,
// creationTimestamp, managedFields) and status before comparison. The desired
// vs dry-run-result comparison cancels defaulting; only convoy-managed state
// remains, so a difference is real drift.
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
