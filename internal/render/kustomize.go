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
	"fmt"
	"path"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/artifact"
)

// wrapperDir is the synthetic kustomization root used to inject targetNamespace via
// kustomize's own namespace transformer. It is named so it can't collide with a real
// path in the source tree.
const wrapperDir = "__convoy_target_namespace__"

// renderKustomize builds a kustomize bundle from the artifact, in-process (no shelling
// to the kustomize binary), and returns the resulting objects. The artifact's files are
// staged into an in-memory filesystem, so the build only ever sees the fetched source —
// never the real host filesystem. When targetNamespace is set it is applied through
// kustomize's native namespace transformer (which correctly namespaces resources and
// skips known cluster-scoped kinds) by building a thin wrapper kustomization that
// references the user's root; kustomize's parent-wins semantics mean targetNamespace
// overrides any namespace the bundle set itself.
func renderKustomize(spec *corev1alpha1.ReleaseContentKustomizeSpec, art artifact.Artifact) ([]*unstructured.Unstructured, error) {
	root := strings.TrimPrefix(strings.TrimSpace(spec.Path), "/")
	if root == "" {
		root = "."
	}

	fSys := filesys.MakeFsInMemory()
	for name, data := range art.Files {
		// Stage every file at an absolute path so the wrapper can reference the root
		// unambiguously regardless of where in the tree it sits.
		if err := fSys.WriteFile("/"+strings.TrimPrefix(name, "/"), data); err != nil {
			return nil, fmt.Errorf("staging %s: %w", name, err)
		}
	}

	buildPath := "/" + strings.TrimSuffix(root, "/")
	if root == "." {
		buildPath = "/"
	}
	if !fSys.Exists(path.Join(buildPath, "kustomization.yaml")) &&
		!fSys.Exists(path.Join(buildPath, "kustomization.yml")) &&
		!fSys.Exists(path.Join(buildPath, "Kustomization")) {
		return nil, fmt.Errorf("no kustomization file found at path %q", root)
	}

	if ns := strings.TrimSpace(spec.TargetNamespace); ns != "" {
		// Reference the build root from the wrapper by a RELATIVE path: kustomize rejects
		// absolute paths in `resources`. The wrapper sits one level under the fs root, so
		// "../<root>" reaches it ("." → "..").
		rel := ".."
		if root != "." {
			rel = "../" + strings.TrimSuffix(root, "/")
		}
		wrapper := "/" + wrapperDir
		manifest := fmt.Sprintf("namespace: %s\nresources:\n- %s\n", ns, rel)
		if err := fSys.WriteFile(path.Join(wrapper, "kustomization.yaml"), []byte(manifest)); err != nil {
			return nil, fmt.Errorf("staging target-namespace wrapper: %w", err)
		}
		buildPath = wrapper
	}

	// LoadRestrictionsNone lets the wrapper reference the root by absolute path. Safe
	// here: the filesystem is in-memory and holds only the fetched artifact, so there is
	// no host path to escape to.
	opts := krusty.MakeDefaultOptions()
	opts.LoadRestrictions = types.LoadRestrictionsNone
	k := krusty.MakeKustomizer(opts)

	resMap, err := k.Run(fSys, buildPath)
	if err != nil {
		return nil, fmt.Errorf("kustomize build at %q: %w", root, err)
	}
	yml, err := resMap.AsYaml()
	if err != nil {
		return nil, fmt.Errorf("serializing kustomize output: %w", err)
	}
	objs, err := decodeManifests(yml)
	if err != nil {
		return nil, fmt.Errorf("decoding kustomize output: %w", err)
	}
	return objs, nil
}
