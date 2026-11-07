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
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/artifact"
)

const (
	cmA = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n"
	cmB = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: b\n"
)

func manifestsContent(paths ...string) corev1alpha1.ReleaseContentSpec {
	return corev1alpha1.ReleaseContentSpec{
		Type:      corev1alpha1.ReleaseContentTypeManifests,
		Manifests: &corev1alpha1.ReleaseContentManifestsSpec{Paths: paths},
	}
}

func TestRender_DirectoryPathMultiDoc(t *testing.T) {
	art := artifact.Artifact{Files: map[string][]byte{
		"manifests/one.yaml":  []byte(cmA),
		"manifests/two.yaml":  []byte(cmB + "---\n" + cmA), // multi-doc
		"manifests/README.md": []byte("ignore me"),
		"other/skip.yaml":     []byte(cmA),
	}}

	objs, err := Render(manifestsContent("manifests"), art, nil)
	if err != nil {
		t.Fatalf("render returned error: %v", err)
	}
	if len(objs) != 3 {
		t.Fatalf("got %d objects, want 3 (README + other/ excluded)", len(objs))
	}
	for _, o := range objs {
		if o.GetKind() != configMapKind {
			t.Errorf("object kind = %q, want ConfigMap", o.GetKind())
		}
	}
}

func TestRender_SingleFilePath(t *testing.T) {
	art := artifact.Artifact{Files: map[string][]byte{dummyFile: []byte(cmA)}}

	objs, err := Render(manifestsContent(dummyFile), art, nil)
	if err != nil {
		t.Fatalf("render returned error: %v", err)
	}
	if len(objs) != 1 || objs[0].GetName() != "a" {
		t.Errorf("objs = %v, want one ConfigMap named a", objs)
	}
}

func TestRender_UnsupportedContentType(t *testing.T) {
	content := corev1alpha1.ReleaseContentSpec{Type: corev1alpha1.ReleaseContentTypeKustomize}
	if _, err := Render(content, artifact.Artifact{}, nil); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestRender_MissingHelmBlock(t *testing.T) {
	content := corev1alpha1.ReleaseContentSpec{Type: corev1alpha1.ReleaseContentTypeHelm}
	if _, err := Render(content, artifact.Artifact{}, nil); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestRender_MissingManifestsBlock(t *testing.T) {
	content := corev1alpha1.ReleaseContentSpec{Type: corev1alpha1.ReleaseContentTypeManifests}
	if _, err := Render(content, artifact.Artifact{}, nil); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestRender_NoFilesAtPath(t *testing.T) {
	art := artifact.Artifact{Files: map[string][]byte{dummyFile: []byte(cmA)}}
	_, err := Render(manifestsContent("nope"), art, nil)
	if err == nil {
		t.Fatalf("render returned nil, want error for missing path")
	}
	if errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want a non-Unsupported error", err)
	}
}

const namespaceMix = `apiVersion: v1
kind: ConfigMap
metadata:
  name: plain
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: pinned
  namespace: other
---
apiVersion: v1
kind: Namespace
metadata:
  name: team
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: reader
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: gizmos.example.com
spec:
  group: example.com
  scope: Cluster
  names: {kind: Gizmo, plural: gizmos}
---
apiVersion: example.com/v1
kind: Gizmo
metadata:
  name: w
---
apiVersion: example.com/v1
kind: Gadget
metadata:
  name: g
`

func namespaceOf(t *testing.T, objs []*unstructured.Unstructured, kind, name string) string {
	t.Helper()
	for _, o := range objs {
		if o.GetKind() == kind && o.GetName() == name {
			return o.GetNamespace()
		}
	}
	t.Fatalf("no %s %q in the render", kind, name)
	return ""
}

// TestRender_ManifestsTargetNamespace is the KB-10 regression: targetNamespace must
// reach namespaced objects that have no namespace, or they go to "default".
func TestRender_ManifestsTargetNamespace(t *testing.T) {
	content := manifestsContent(dummyFile)
	content.Manifests.TargetNamespace = nsProd
	objs, err := Render(content, artifact.Artifact{Files: map[string][]byte{dummyFile: []byte(namespaceMix)}}, nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, tc := range []struct{ kind, name, want string }{
		{configMapKind, "plain", nsProd},
		{configMapKind, "pinned", "other"},
		{"Namespace", "team", ""},
		{"ClusterRole", "reader", ""},
		{"CustomResourceDefinition", "gizmos.example.com", ""},
		{"Gizmo", "w", ""},
		{"Gadget", "g", nsProd},
	} {
		if got := namespaceOf(t, objs, tc.kind, tc.name); got != tc.want {
			t.Errorf("%s %s namespace = %q, want %q", tc.kind, tc.name, got, tc.want)
		}
	}
}

func TestRender_ManifestsWithoutTargetNamespace(t *testing.T) {
	objs, err := Render(manifestsContent(dummyFile), artifact.Artifact{Files: map[string][]byte{dummyFile: []byte(namespaceMix)}}, nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got := namespaceOf(t, objs, configMapKind, "plain"); got != "" {
		t.Errorf("namespace = %q, want empty: without targetNamespace the render must not change", got)
	}
}
