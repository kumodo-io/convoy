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
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/artifact"
)

// renderHelm loads the Helm chart at spec.Path inside the artifact and renders
// it in-process with default values — no install, no kube client, no external
// binary. Only vendored dependencies are supported. Namespace injection onto
// rendered objects (for resources that omit one) is left to the deployer, which
// has the cluster's RESTMapper to tell namespaced from cluster-scoped kinds.
func renderHelm(spec *corev1alpha1.ReleaseContentHelmSpec, art artifact.Artifact, valueLayers [][]byte) ([]*unstructured.Unstructured, error) {
	switch spec.DependencyMode {
	case "", corev1alpha1.ReleaseHelmDependencyModeVendored:
		// supported
	case corev1alpha1.ReleaseHelmDependencyModeResolve:
		return nil, errors.Join(ErrUnsupported, fmt.Errorf("dependencyMode %q is not supported yet (only %q)", spec.DependencyMode, corev1alpha1.ReleaseHelmDependencyModeVendored))
	default:
		return nil, errors.Join(ErrUnsupported, fmt.Errorf("unknown dependencyMode %q", spec.DependencyMode))
	}

	files := chartFiles(art.Files, spec.Path)
	if len(files) == 0 {
		return nil, fmt.Errorf("no chart files found at path %q", spec.Path)
	}
	chrt, err := loader.LoadFiles(files)
	if err != nil {
		return nil, fmt.Errorf("loading chart at %q: %w", spec.Path, err)
	}

	// Vendored mode: every declared dependency must already be in charts/.
	// Mirrors Helm's action.CheckDependencies.
	loaded := make(map[string]bool, len(chrt.Dependencies()))
	for _, d := range chrt.Dependencies() {
		loaded[d.Name()] = true
	}
	var missing []string
	for _, dep := range chrt.Metadata.Dependencies {
		if !loaded[dep.Name] {
			missing = append(missing, dep.Name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("chart %q is missing vendored dependencies %v (dependencyMode=vendored requires charts/ to be pre-populated)", chrt.Name(), missing)
	}

	name := spec.ReleaseName
	if name == "" {
		name = chrt.Name()
	}
	namespace := spec.ReleaseNamespace
	if namespace == "" {
		namespace = metav1.NamespaceDefault
	}

	// Merge the ordered user value layers (later wins), to be coalesced over the
	// chart's own values.yaml by ToRenderValues.
	userValues, err := mergeValueLayers(valueLayers)
	if err != nil {
		return nil, err
	}

	values, err := chartutil.ToRenderValues(chrt, userValues, chartutil.ReleaseOptions{
		Name:      name,
		Namespace: namespace,
		IsInstall: true,
	}, chartutil.DefaultCapabilities)
	if err != nil {
		return nil, fmt.Errorf("building render values: %w", err)
	}

	rendered, err := engine.Render(chrt, values)
	if err != nil {
		return nil, fmt.Errorf("rendering chart %q: %w", chrt.Name(), err)
	}

	// Render output order is non-deterministic; sort for stable apply order.
	keys := make([]string, 0, len(rendered))
	for k := range rendered {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var objs []*unstructured.Unstructured
	for _, k := range keys {
		base := path.Base(k)
		// Skip partials (_*.tpl) and NOTES.txt — they produce no manifests.
		if strings.HasPrefix(base, "_") || base == "NOTES.txt" {
			continue
		}
		docs, err := decodeManifests([]byte(rendered[k]))
		if err != nil {
			return nil, fmt.Errorf("decoding rendered %s: %w", k, err)
		}
		for _, d := range docs {
			if isTestHook(d) {
				continue // don't apply test resources
			}
			objs = append(objs, d)
		}
	}
	return objs, nil
}

// chartFiles extracts the chart subtree rooted at p from the artifact, re-rooting
// keys so Chart.yaml sits at the root (as loader.LoadFiles expects). p may be
// "", ".", or a directory like "charts/app".
func chartFiles(files map[string][]byte, p string) []*loader.BufferedFile {
	p = strings.TrimPrefix(p, "/")
	prefix := strings.TrimSuffix(p, "/") + "/"
	if p == "" || p == "." {
		prefix = ""
	}
	var out []*loader.BufferedFile
	for name, data := range files {
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		rel := strings.TrimPrefix(name, prefix)
		if rel == "" {
			continue
		}
		out = append(out, &loader.BufferedFile{Name: rel, Data: data})
	}
	return out
}

// isTestHook reports whether an object is a Helm test hook (helm.sh/hook: test*),
// which should not be applied as part of a release.
func isTestHook(obj *unstructured.Unstructured) bool {
	return strings.Contains(obj.GetAnnotations()["helm.sh/hook"], "test")
}

// mergeValueLayers deep-merges the ordered raw-YAML value layers into a single
// values map. Later layers win over earlier ones.
func mergeValueLayers(layers [][]byte) (map[string]any, error) {
	merged := map[string]any{}
	for _, layer := range layers {
		if len(layer) == 0 {
			continue
		}
		m := map[string]any{}
		if err := yaml.Unmarshal(layer, &m); err != nil {
			return nil, fmt.Errorf("parsing values: %w", err)
		}
		merged = mergeMaps(merged, m)
	}
	return merged, nil
}

// mergeMaps deep-merges src into dst, returning a new map. On key collisions src
// wins, except where both sides are maps, which merge recursively.
func mergeMaps(dst, src map[string]any) map[string]any {
	out := make(map[string]any, len(dst))
	for k, v := range dst {
		out[k] = v
	}
	for k, v := range src {
		if srcMap, ok := v.(map[string]any); ok {
			if dstMap, ok := out[k].(map[string]any); ok {
				out[k] = mergeMaps(dstMap, srcMap)
				continue
			}
		}
		out[k] = v
	}
	return out
}
