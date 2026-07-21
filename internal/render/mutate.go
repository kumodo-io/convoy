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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/yaml"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/auth"
	"github.com/kumodo-io/convoy/internal/fetch"
)

// MutationInput carries everything the post-render mutation pipeline needs: the
// rendered objects, the pipeline, the target Release (for the .Self templating
// context) and Cluster (for .Cluster), and the resolver/coordinator used to
// resolve a mutation's From source.
type MutationInput struct {
	Objects     []*unstructured.Unstructured
	Mutations   []corev1alpha1.ReleaseMutation
	Release     *corev1alpha1.Release
	Cluster     *corev1alpha1.Cluster // may be nil (e.g. preview when unresolvable)
	Resolver    auth.SecretResolver
	Coordinator fetch.Coordinator
}

// ApplyMutations runs the ordered post-render mutation pipeline over the rendered
// objects. Each mutation transforms the objects matching its target, in order;
// objects that match no mutation pass through unchanged. It runs in the shared
// render path so every consumer (apply, diff, preview, health) sees the same
// mutated output. It returns the mutated objects and a ResolvedMutation record per
// mutation for status.
func ApplyMutations(ctx context.Context, in MutationInput) ([]*unstructured.Unstructured, []corev1alpha1.ResolvedMutation, error) {
	if len(in.Mutations) == 0 {
		return in.Objects, nil, nil
	}
	objects := in.Objects
	resolved := make([]corev1alpha1.ResolvedMutation, 0, len(in.Mutations))
	for i := range in.Mutations {
		m := in.Mutations[i]
		mutated, hash, err := applyMutation(ctx, objects, m, in)
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
func applyMutation(ctx context.Context, objects []*unstructured.Unstructured, m corev1alpha1.ReleaseMutation, in MutationInput) ([]*unstructured.Unstructured, string, error) {
	switch m.Type {
	case corev1alpha1.MutationTypeJsonPatch:
		return applyJSONPatch(objects, m, in)
	case corev1alpha1.MutationTypeStrategicMergePatch:
		return applyStrategicMerge(ctx, objects, m, in)
	case corev1alpha1.MutationTypeGoTemplate:
		return applyGoTemplate(ctx, objects, m, in)
	case corev1alpha1.MutationTypeCelTransform:
		return applyCELTransform(ctx, objects, m, in)
	default:
		return nil, "", fmt.Errorf("%w: unknown mutation type %q", ErrUnsupported, m.Type)
	}
}

// applyJSONPatch applies an RFC6902 JSON patch (built from the mutation's Patches)
// to each matching object. The patch is Go-templated per object against the
// {cluster,self,object} context before it is applied, so patch fields can inject
// context values. Note: templating runs over the patch JSON text, so substituted
// values land as strings — natural for string values; for typed values prefer
// strategicMergePatch/goTemplate.
func applyJSONPatch(objects []*unstructured.Unstructured, m corev1alpha1.ReleaseMutation, in MutationInput) ([]*unstructured.Unstructured, string, error) {
	patchJSON, err := json.Marshal(m.Patches)
	if err != nil {
		return nil, "", fmt.Errorf("marshaling patches: %w", err)
	}
	tmpl, err := template.New("mutation").Option("missingkey=zero").Parse(string(patchJSON))
	if err != nil {
		return nil, "", fmt.Errorf("parsing json patch template: %w", err)
	}

	out := make([]*unstructured.Unstructured, len(objects))
	for i, obj := range objects {
		if !matchesTarget(m.Target, obj) {
			out[i] = obj
			continue
		}
		rendered, err := renderWith(tmpl, templateContext(in.Release, in.Cluster, obj))
		if err != nil {
			return nil, "", fmt.Errorf("rendering json patch for %s %q: %w", obj.GetKind(), obj.GetName(), err)
		}
		patch, err := jsonpatch.DecodePatch(rendered)
		if err != nil {
			return nil, "", fmt.Errorf("decoding json patch for %s %q: %w", obj.GetKind(), obj.GetName(), err)
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
// Each doc is Go-templated per object against the {cluster,self,object} context
// before merging, so patches can inject context values while keeping strategic
// (list-merge-by-key) semantics. Strategic merge needs the object's built-in Go
// type, so CRDs/unregistered kinds are rejected (use jsonPatch for those).
func applyStrategicMerge(ctx context.Context, objects []*unstructured.Unstructured, m corev1alpha1.ReleaseMutation, in MutationInput) ([]*unstructured.Unstructured, string, error) {
	docs, err := mutationDocs(ctx, m, in.Resolver, in.Coordinator, in.Release.Namespace)
	if err != nil {
		return nil, "", err
	}
	if len(docs) == 0 {
		return nil, "", fmt.Errorf("strategicMergePatch has no patch content (set inline or from)")
	}
	templates, err := parseTemplates(docs)
	if err != nil {
		return nil, "", err
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
		data := templateContext(in.Release, in.Cluster, obj)
		for _, t := range templates {
			rendered, err := renderWith(t, data)
			if err != nil {
				return nil, "", fmt.Errorf("rendering strategic merge patch for %s %q: %w", obj.GetKind(), obj.GetName(), err)
			}
			patchJSON, err := yaml.YAMLToJSON(rendered)
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

// applyGoTemplate renders each template document with the {.cluster,.self,.object}
// context and RFC7386-merges the resulting overlay onto each matching object. The
// overlay is a partial: it injects/updates fields, it does not replace the object.
func applyGoTemplate(ctx context.Context, objects []*unstructured.Unstructured, m corev1alpha1.ReleaseMutation, in MutationInput) ([]*unstructured.Unstructured, string, error) {
	docs, err := mutationDocs(ctx, m, in.Resolver, in.Coordinator, in.Release.Namespace)
	if err != nil {
		return nil, "", err
	}
	if len(docs) == 0 {
		return nil, "", fmt.Errorf("goTemplate has no template content (set inline or from)")
	}
	templates, err := parseTemplates(docs)
	if err != nil {
		return nil, "", err
	}

	out := make([]*unstructured.Unstructured, len(objects))
	for i, obj := range objects {
		if !matchesTarget(m.Target, obj) {
			out[i] = obj
			continue
		}
		data := templateContext(in.Release, in.Cluster, obj)
		objJSON, err := obj.MarshalJSON()
		if err != nil {
			return nil, "", fmt.Errorf("marshaling %s %q: %w", obj.GetKind(), obj.GetName(), err)
		}
		for _, t := range templates {
			rendered, err := renderWith(t, data)
			if err != nil {
				return nil, "", fmt.Errorf("rendering goTemplate for %s %q: %w", obj.GetKind(), obj.GetName(), err)
			}
			overlayJSON, err := yaml.YAMLToJSON(rendered)
			if err != nil {
				return nil, "", fmt.Errorf("parsing goTemplate output: %w", err)
			}
			objJSON, err = jsonpatch.MergePatch(objJSON, overlayJSON)
			if err != nil {
				return nil, "", fmt.Errorf("merging goTemplate overlay onto %s %q: %w", obj.GetKind(), obj.GetName(), err)
			}
		}
		next := &unstructured.Unstructured{}
		if err := next.UnmarshalJSON(objJSON); err != nil {
			return nil, "", fmt.Errorf("decoding mutated %s %q: %w", obj.GetKind(), obj.GetName(), err)
		}
		out[i] = next
	}
	return out, hashDocs(docs), nil
}

// contextValues projects Cluster.Spec.Metadata and the Release's self-identity
// into the shapes templating consumes. Cluster credentials are never exposed —
// only Cluster.Spec.Metadata.
func contextValues(rel *corev1alpha1.Release, cluster *corev1alpha1.Cluster) (clusterMeta map[string]string, self map[string]any) {
	clusterMeta = map[string]string{}
	if cluster != nil && cluster.Spec.Metadata != nil {
		clusterMeta = cluster.Spec.Metadata
	}
	self = map[string]any{
		"name":        rel.Name,
		"namespace":   rel.Namespace,
		"labels":      orEmpty(rel.Labels),
		"annotations": orEmpty(rel.Annotations),
	}
	return clusterMeta, self
}

func orEmpty(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// templateContext builds the per-object {cluster,self,object} context shared by
// every templated mutation type — Go templates access `.cluster`/`.self`/
// `.object`, CEL uses the same names as variables. Keys are lowercase across both
// engines for consistency.
func templateContext(rel *corev1alpha1.Release, cluster *corev1alpha1.Cluster, obj *unstructured.Unstructured) map[string]any {
	clusterMeta, self := contextValues(rel, cluster)
	return map[string]any{"cluster": clusterMeta, "self": self, "object": obj.Object}
}

// parseTemplates compiles each mutation document as a text/template. missingkey=zero
// makes an absent key (e.g. .cluster.region) render empty rather than error.
func parseTemplates(docs [][]byte) ([]*template.Template, error) {
	ts := make([]*template.Template, len(docs))
	for i, doc := range docs {
		t, err := template.New("mutation").Option("missingkey=zero").Parse(string(doc))
		if err != nil {
			return nil, fmt.Errorf("parsing template: %w", err)
		}
		ts[i] = t
	}
	return ts, nil
}

// renderWith executes a template against the per-object context.
func renderWith(t *template.Template, data map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// applyCELTransform evaluates each CEL expression (vars cluster/self/object) to an
// overlay map and RFC7386-merges it onto each matching object.
func applyCELTransform(ctx context.Context, objects []*unstructured.Unstructured, m corev1alpha1.ReleaseMutation, in MutationInput) ([]*unstructured.Unstructured, string, error) {
	docs, err := mutationDocs(ctx, m, in.Resolver, in.Coordinator, in.Release.Namespace)
	if err != nil {
		return nil, "", err
	}
	if len(docs) == 0 {
		return nil, "", fmt.Errorf("celTransform has no expression content (set inline or from)")
	}
	env, err := cel.NewEnv(
		cel.Variable("cluster", cel.MapType(cel.StringType, cel.StringType)),
		cel.Variable("self", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("object", cel.DynType),
	)
	if err != nil {
		return nil, "", fmt.Errorf("building CEL env: %w", err)
	}
	programs := make([]cel.Program, len(docs))
	for i, doc := range docs {
		ast, issues := env.Compile(string(doc))
		if issues != nil && issues.Err() != nil {
			return nil, "", fmt.Errorf("compiling celTransform: %w", issues.Err())
		}
		prg, err := env.Program(ast)
		if err != nil {
			return nil, "", fmt.Errorf("building celTransform program: %w", err)
		}
		programs[i] = prg
	}

	out := make([]*unstructured.Unstructured, len(objects))
	for i, obj := range objects {
		if !matchesTarget(m.Target, obj) {
			out[i] = obj
			continue
		}
		vars := templateContext(in.Release, in.Cluster, obj)
		objJSON, err := obj.MarshalJSON()
		if err != nil {
			return nil, "", fmt.Errorf("marshaling %s %q: %w", obj.GetKind(), obj.GetName(), err)
		}
		for _, prg := range programs {
			result, _, err := prg.Eval(vars)
			if err != nil {
				return nil, "", fmt.Errorf("evaluating celTransform for %s %q: %w", obj.GetKind(), obj.GetName(), err)
			}
			native, err := celToNative(result)
			if err != nil {
				return nil, "", fmt.Errorf("celTransform result for %s %q: %w", obj.GetKind(), obj.GetName(), err)
			}
			overlay, ok := native.(map[string]any)
			if !ok {
				return nil, "", fmt.Errorf("celTransform must return a map overlay for %s %q, got %T", obj.GetKind(), obj.GetName(), native)
			}
			overlayJSON, err := json.Marshal(overlay)
			if err != nil {
				return nil, "", err
			}
			objJSON, err = jsonpatch.MergePatch(objJSON, overlayJSON)
			if err != nil {
				return nil, "", fmt.Errorf("merging celTransform overlay onto %s %q: %w", obj.GetKind(), obj.GetName(), err)
			}
		}
		next := &unstructured.Unstructured{}
		if err := next.UnmarshalJSON(objJSON); err != nil {
			return nil, "", fmt.Errorf("decoding mutated %s %q: %w", obj.GetKind(), obj.GetName(), err)
		}
		out[i] = next
	}
	return out, hashDocs(docs), nil
}

// celToNative recursively converts a CEL result to a JSON-marshalable Go value
// (map[string]any / []any / scalars), using only cel-go's own ref/traits — no
// protobuf. CEL's ConvertToNative(map[string]any) is not recursive, so we walk it.
func celToNative(v ref.Val) (any, error) {
	if v == types.NullValue {
		return nil, nil
	}
	switch val := v.(type) {
	case traits.Mapper:
		out := map[string]any{}
		it := val.Iterator()
		for it.HasNext() == types.True {
			k := it.Next()
			key, ok := k.Value().(string)
			if !ok {
				return nil, fmt.Errorf("map keys must be strings, got %T", k.Value())
			}
			nv, err := celToNative(val.Get(k))
			if err != nil {
				return nil, err
			}
			out[key] = nv
		}
		return out, nil
	case traits.Lister:
		n := int(val.Size().(types.Int))
		out := make([]any, 0, n)
		for i := 0; i < n; i++ {
			nv, err := celToNative(val.Get(types.Int(i)))
			if err != nil {
				return nil, err
			}
			out = append(out, nv)
		}
		return out, nil
	default:
		return v.Value(), nil
	}
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
// the resolver (From.Path names the data key); GitRepository is fetched through
// the coordinator at From.Ref (From.Path names the file within the repo).
func resolveFrom(ctx context.Context, from *corev1alpha1.ReleaseMutationFromSpec, resolver auth.SecretResolver, coordinator fetch.Coordinator, releaseNamespace string) ([]byte, error) {
	ns := from.Namespace
	if ns == "" {
		ns = releaseNamespace
	}
	switch from.Kind {
	case "GitRepository":
		res, err := coordinator.Ensure(ctx, corev1alpha1.ReleaseSourceSpec{
			Type: corev1alpha1.ReleaseSourceTypeGitRepository,
			GitRepository: &corev1alpha1.ReleaseGitRepositorySpec{
				Name:      from.Name,
				Namespace: from.Namespace,
				Ref:       from.Ref,
			},
		}, releaseNamespace)
		if err != nil {
			return nil, err // transient (requeue)
		}
		if !res.Ready {
			return nil, fmt.Errorf("mutation source GitRepository %s not ready: %s", from.Name, res.Message)
		}
		data, ok := res.Artifact.Files[strings.TrimPrefix(from.Path, "/")]
		if !ok {
			return nil, fmt.Errorf("mutation file %q not found in GitRepository %s@%s", from.Path, from.Name, res.Revision)
		}
		return data, nil
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
