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
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/artifact"
)

// ErrUnsupported marks a Release configuration this build does not implement yet
// (unsupported content type, non-GitRepository source). Terminal — reconcile
// surfaces it without requeuing.
var ErrUnsupported = errors.New("unsupported release configuration")

// Render turns a source artifact into the objects to apply, dispatching on the
// content type. valueLayers are the ordered, pre-resolved Helm values (used only
// by the helm content type; ignored otherwise).
func Render(content corev1alpha1.ReleaseContentSpec, art artifact.Artifact, valueLayers [][]byte) ([]*unstructured.Unstructured, error) {
	switch content.Type {
	case corev1alpha1.ReleaseContentTypeManifests:
		if content.Manifests == nil {
			return nil, errors.Join(ErrUnsupported, fmt.Errorf("manifests content block is missing"))
		}
		return renderManifests(content.Manifests, art)
	case corev1alpha1.ReleaseContentTypeHelm:
		if content.Helm == nil {
			return nil, errors.Join(ErrUnsupported, fmt.Errorf("helm content block is missing"))
		}
		return renderHelm(content.Helm, art, valueLayers)
	case corev1alpha1.ReleaseContentTypeKustomize:
		if content.Kustomize == nil {
			return nil, errors.Join(ErrUnsupported, fmt.Errorf("kustomize content block is missing"))
		}
		return renderKustomize(content.Kustomize, art)
	default:
		return nil, errors.Join(ErrUnsupported, fmt.Errorf("content type %q is not supported yet", content.Type))
	}
}

// renderManifests collects raw YAML/JSON manifest documents from the artifact at
// the configured paths.
func renderManifests(spec *corev1alpha1.ReleaseContentManifestsSpec, art artifact.Artifact) ([]*unstructured.Unstructured, error) {
	var objs []*unstructured.Unstructured
	for _, p := range spec.Paths {
		names := gatherFiles(art.Files, p)
		if len(names) == 0 {
			return nil, fmt.Errorf("no manifest files found at path %q", p)
		}
		for _, name := range names {
			docs, err := decodeManifests(art.Files[name])
			if err != nil {
				return nil, fmt.Errorf("decoding %s: %w", name, err)
			}
			objs = append(objs, docs...)
		}
	}
	setNamespace(objs, spec.TargetNamespace)
	return objs, nil
}

// gatherFiles resolves a content path (a single file, a directory prefix, or
// "."/"" for the whole tree) to a sorted list of YAML file keys.
func gatherFiles(files map[string][]byte, p string) []string {
	p = strings.TrimPrefix(p, "/")
	if _, ok := files[p]; ok {
		return []string{p}
	}

	prefix := strings.TrimSuffix(p, "/") + "/"
	if p == "" || p == "." {
		prefix = ""
	}
	var matched []string
	for name := range files {
		if (prefix == "" || strings.HasPrefix(name, prefix)) && isYAML(name) {
			matched = append(matched, name)
		}
	}
	sort.Strings(matched)
	return matched
}

func isYAML(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}

// decodeManifests splits a (possibly multi-document) YAML file into objects.
func decodeManifests(data []byte) ([]*unstructured.Unstructured, error) {
	var objs []*unstructured.Unstructured
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := dec.Decode(obj); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		if len(obj.Object) == 0 {
			continue // skip empty documents
		}
		objs = append(objs, obj)
	}
	return objs, nil
}
