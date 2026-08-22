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
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/artifact"
)

const kustomizePath = "base"

// demoKustomize returns an artifact with a base kustomization overlaying a name prefix
// and a label onto a ConfigMap and a (cluster-scoped) Namespace, under base/.
func demoKustomize() artifact.Artifact {
	return artifact.Artifact{Files: map[string][]byte{
		"base/kustomization.yaml": []byte(
			"namePrefix: demo-\n" +
				"labels:\n- pairs:\n    team: platform\n" +
				"resources:\n- cm.yaml\n- ns.yaml\n"),
		"base/cm.yaml": []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\ndata:\n  key: value\n"),
		"base/ns.yaml": []byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: team-ns\n"),
	}}
}

func byKind(objs []*unstructured.Unstructured, kind string) *unstructured.Unstructured {
	for _, o := range objs {
		if o.GetKind() == kind {
			return o
		}
	}
	return nil
}

func TestRenderKustomize_BuildsBundle(t *testing.T) {
	objs, err := renderKustomize(&corev1alpha1.ReleaseContentKustomizeSpec{Path: kustomizePath}, demoKustomize())
	if err != nil {
		t.Fatalf("renderKustomize returned error: %v", err)
	}
	if len(objs) != 2 {
		t.Fatalf("got %d objects, want 2 (ConfigMap + Namespace)", len(objs))
	}
	cm := byKind(objs, configMapKind)
	if cm == nil {
		t.Fatalf("no ConfigMap in output: %+v", objs)
	}
	// namePrefix + labels transformers ran.
	if cm.GetName() != "demo-app" {
		t.Errorf("name = %q, want demo-app (namePrefix applied)", cm.GetName())
	}
	if got := unstructuredString(cm.Object, "metadata", "labels", "team"); got != "platform" {
		t.Errorf("label team = %q, want platform (labels transformer applied)", got)
	}
}

func TestRenderKustomize_HonorsTargetNamespace(t *testing.T) {
	spec := &corev1alpha1.ReleaseContentKustomizeSpec{Path: kustomizePath, TargetNamespace: nsProd}
	objs, err := renderKustomize(spec, demoKustomize())
	if err != nil {
		t.Fatalf("renderKustomize returned error: %v", err)
	}
	if cm := byKind(objs, configMapKind); cm == nil || cm.GetNamespace() != "prod" {
		t.Errorf("ConfigMap namespace = %q, want prod (targetNamespace applied)", cm.GetNamespace())
	}
	// Cluster-scoped kinds must NOT be namespaced by the transformer.
	if ns := byKind(objs, "Namespace"); ns == nil || ns.GetNamespace() != "" {
		t.Errorf("Namespace object namespace = %q, want empty (cluster-scoped, must be skipped)", ns.GetNamespace())
	}
}

func TestRenderKustomize_MissingKustomizationErrors(t *testing.T) {
	art := artifact.Artifact{Files: map[string][]byte{
		"base/cm.yaml": []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n"),
	}}
	if _, err := renderKustomize(&corev1alpha1.ReleaseContentKustomizeSpec{Path: kustomizePath}, art); err == nil {
		t.Error("expected an error when no kustomization file is present, got nil")
	}
}

func TestRenderKustomize_DefaultPathIsRoot(t *testing.T) {
	art := artifact.Artifact{Files: map[string][]byte{
		"kustomization.yaml": []byte("resources:\n- cm.yaml\n"),
		"cm.yaml":            []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: root-cm\n"),
	}}
	objs, err := renderKustomize(&corev1alpha1.ReleaseContentKustomizeSpec{Path: ""}, art)
	if err != nil {
		t.Fatalf("renderKustomize (root path) error: %v", err)
	}
	if len(objs) != 1 || objs[0].GetName() != "root-cm" {
		t.Errorf("objects = %+v, want one root-cm (empty path → repo root)", objs)
	}
}
