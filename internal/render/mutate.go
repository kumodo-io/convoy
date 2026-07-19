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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/yaml"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/auth"
	"github.com/kumodo-io/convoy/internal/fetch"
)

// ApplyMutations runs the ordered post-render mutation pipeline over the rendered
// objects. Each mutation transforms the objects matching its target, in order;
// objects that match no mutation pass through unchanged. It runs in the shared
// render path so every consumer (apply, diff, preview, health) sees the same
// mutated output. It returns the mutated objects and a ResolvedMutation record per
// mutation for status.
func ApplyMutations(ctx context.Context, objects []*unstructured.Unstructured, mutations []corev1alpha1.ReleaseMutation, resolver auth.SecretResolver, coordinator fetch.Coordinator, releaseNamespace string) ([]*unstructured.Unstructured, []corev1alpha1.ResolvedMutation, error) {
	if len(mutations) == 0 {
		return objects, nil, nil
	}
	resolved := make([]corev1alpha1.ResolvedMutation, 0, len(mutations))
	for i := range mutations {
		m := mutations[i]
		mutated, hash, err := applyMutation(ctx, objects, m, resolver, coordinator, releaseNamespace)
		if err != nil {
			return nil, nil, fmt.Errorf("mutation %d (%s): %w", i, m.Type, err)
		}
		objects = mutated

		rm := corev1alpha1.ResolvedMutation{Index: i, Type: m.Type, ContentHash: hash}
		if m.From != nil {
			rm.SourceRef = *m.From
		}
		resolved = append(resolved, rm)
	}
	return objects, resolved, nil
}

// applyMutation transforms every object matching the mutation's target and
// returns the resulting object set plus a content hash of the applied patch.
func applyMutation(ctx context.Context, objects []*unstructured.Unstructured, m corev1alpha1.ReleaseMutation, resolver auth.SecretResolver, coordinator fetch.Coordinator, releaseNamespace string) ([]*unstructured.Unstructured, string, error) {
	switch m.Type {
	case corev1alpha1.MutationTypeJsonPatch:
		return applyJSONPatch(objects, m)
	case corev1alpha1.MutationTypeStrategicMergePatch:
		return applyStrategicMerge(ctx, objects, m, resolver, coordinator, releaseNamespace)
	case corev1alpha1.MutationTypeGoTemplate, corev1alpha1.MutationTypeCelTransform:
		return nil, "", fmt.Errorf("%w: mutation type %q is not implemented yet", ErrUnsupported, m.Type)
	default:
		return nil, "", fmt.Errorf("%w: unknown mutation type %q", ErrUnsupported, m.Type)
	}
}

// applyJSONPatch applies an RFC6902 JSON patch (built from the mutation's Patches)
// to each matching object. The Patches field's JSON tags already match RFC6902.
func applyJSONPatch(objects []*unstructured.Unstructured, m corev1alpha1.ReleaseMutation) ([]*unstructured.Unstructured, string, error) {
	patchJSON, err := json.Marshal(m.Patches)
	if err != nil {
		return nil, "", fmt.Errorf("marshaling patches: %w", err)
	}
	patch, err := jsonpatch.DecodePatch(patchJSON)
	if err != nil {
		return nil, "", fmt.Errorf("decoding json patch: %w", err)
	}

	out := make([]*unstructured.Unstructured, len(objects))
	for i, obj := range objects {
		if !matchesTarget(m.Target, obj) {
			out[i] = obj
			continue
		}
		objJSON, err := obj.MarshalJSON()
		if err != nil {
			return nil, "", fmt.Errorf("marshaling %s %q: %w", obj.GetKind(), obj.GetName(), err)
		}
		patched, err := patch.Apply(objJSON)
		if err != nil {
			return nil, "", fmt.Errorf("applying json patch to %s %q: %w", obj.GetKind(), obj.GetName(), err)
		}
		next := &unstructured.Unstructured{}
		if err := next.UnmarshalJSON(patched); err != nil {
			return nil, "", fmt.Errorf("decoding patched %s %q: %w", obj.GetKind(), obj.GetName(), err)
		}
		out[i] = next
	}
	return out, hashBytes(patchJSON), nil
}

// applyStrategicMerge applies one or more strategic-merge patch documents (from
// the mutation's Inline strings and/or its From source) to each matching object.
// Strategic merge needs the object's built-in Go type to know list-merge keys, so
// CRDs/unregistered kinds are rejected (use jsonPatch for those).
func applyStrategicMerge(ctx context.Context, objects []*unstructured.Unstructured, m corev1alpha1.ReleaseMutation, resolver auth.SecretResolver, coordinator fetch.Coordinator, releaseNamespace string) ([]*unstructured.Unstructured, string, error) {
	docs, err := mutationDocs(ctx, m, resolver, coordinator, releaseNamespace)
	if err != nil {
		return nil, "", err
	}
	if len(docs) == 0 {
		return nil, "", fmt.Errorf("strategicMergePatch has no patch content (set inline or from)")
	}

	out := make([]*unstructured.Unstructured, len(objects))
	for i, obj := range objects {
		if !matchesTarget(m.Target, obj) {
			out[i] = obj
			continue
		}
		typed, err := kubescheme.Scheme.New(obj.GroupVersionKind())
		if err != nil {
			return nil, "", fmt.Errorf("strategicMergePatch requires a built-in type; %s is not registered (use jsonPatch)", obj.GroupVersionKind())
		}
		objJSON, err := obj.MarshalJSON()
		if err != nil {
			return nil, "", fmt.Errorf("marshaling %s %q: %w", obj.GetKind(), obj.GetName(), err)
		}
		for _, doc := range docs {
			patchJSON, err := yaml.YAMLToJSON(doc)
			if err != nil {
				return nil, "", fmt.Errorf("parsing strategic merge patch: %w", err)
			}
			objJSON, err = strategicpatch.StrategicMergePatch(objJSON, patchJSON, typed)
			if err != nil {
				return nil, "", fmt.Errorf("strategic-merging %s %q: %w", obj.GetKind(), obj.GetName(), err)
			}
		}
		next := &unstructured.Unstructured{}
		if err := next.UnmarshalJSON(objJSON); err != nil {
			return nil, "", fmt.Errorf("decoding merged %s %q: %w", obj.GetKind(), obj.GetName(), err)
		}
		out[i] = next
	}
	return out, hashDocs(docs), nil
}

// mutationDocs collects a mutation's patch documents: its inline strings first,
// then its From source (ConfigMap/Secret) content, in order.
func mutationDocs(ctx context.Context, m corev1alpha1.ReleaseMutation, resolver auth.SecretResolver, coordinator fetch.Coordinator, releaseNamespace string) ([][]byte, error) {
	var docs [][]byte
	for _, s := range m.Inline {
		if s != "" {
			docs = append(docs, []byte(s))
		}
	}
	if m.From != nil {
		data, err := resolveFrom(ctx, m.From, resolver, coordinator, releaseNamespace)
		if err != nil {
			return nil, err
		}
		docs = append(docs, data)
	}
	return docs, nil
}

// resolveFrom fetches a mutation's external content. ConfigMap/Secret are read via
// the resolver, with From.Path naming the data key. GitRepository sources need a
// revision the From spec does not carry yet and are not supported here.
func resolveFrom(ctx context.Context, from *corev1alpha1.ReleaseMutationFromSpec, resolver auth.SecretResolver, coordinator fetch.Coordinator, releaseNamespace string) ([]byte, error) {
	ns := from.Namespace
	if ns == "" {
		ns = releaseNamespace
	}
	switch from.Kind {
	case "ConfigMap":
		cm, err := resolver.ConfigMap(ctx, ns, from.Name)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil, fmt.Errorf("configMap %s/%s not found", ns, from.Name)
			}
			return nil, err
		}
		v, ok := cm.Data[from.Path]
		if !ok {
			return nil, fmt.Errorf("configMap %s/%s has no key %q", ns, from.Name, from.Path)
		}
		return []byte(v), nil
	case "Secret":
		s, err := resolver.Secret(ctx, ns, from.Name)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil, fmt.Errorf("secret %s/%s not found", ns, from.Name)
			}
			return nil, err
		}
		v, ok := s.Data[from.Path]
		if !ok {
			return nil, fmt.Errorf("secret %s/%s has no key %q", ns, from.Name, from.Path)
		}
		return v, nil
	default:
		return nil, fmt.Errorf("%w: mutation from.kind %q is not supported yet", ErrUnsupported, from.Kind)
	}
}

// matchesTarget reports whether obj matches the mutation target. Empty target
// fields are wildcards, so a GVK-only target matches all objects of that kind.
func matchesTarget(t corev1alpha1.ReleaseMutationTarget, obj *unstructured.Unstructured) bool {
	gvk := obj.GroupVersionKind()
	if t.Group != "" && t.Group != gvk.Group {
		return false
	}
	if t.Version != "" && t.Version != gvk.Version {
		return false
	}
	if t.Kind != "" && t.Kind != gvk.Kind {
		return false
	}
	if t.Name != "" && t.Name != obj.GetName() {
		return false
	}
	if t.Namespace != "" && t.Namespace != obj.GetNamespace() {
		return false
	}
	return true
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func hashDocs(docs [][]byte) string {
	h := sha256.New()
	for _, d := range docs {
		_, _ = h.Write(d)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
