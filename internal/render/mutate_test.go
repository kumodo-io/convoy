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
	"errors"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

const (
	unstructuredApiVersion = "apiVersion"
	unstructuredKind       = "kind"
	unstructuredMetadata   = "metadata"

	metadataName      = "name"
	metadataNamespace = "namespace"

	appsv1ApiVersion = "apps/v1"
	deploymentKind   = "Deployment"

	clusterMetaRegion = "eu-west"
	releaseName       = "release-1"
	imageValue        = "sc:1"
)

func deployment(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		unstructuredApiVersion: appsv1ApiVersion,
		unstructuredKind:       deploymentKind,
		unstructuredMetadata:   map[string]any{metadataName: name, metadataNamespace: "ns"},
		"spec": map[string]any{
			"replicas": 1,
			"template": map[string]any{"spec": map[string]any{
				"containers": []any{
					map[string]any{metadataName: "app", "image": "app:1"},
					map[string]any{metadataName: "sidecar", "image": imageValue},
				},
			}},
		},
	}}
}

func configMap(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		unstructuredApiVersion: "v1",
		unstructuredKind:       "ConfigMap",
		unstructuredMetadata:   map[string]any{metadataName: name, metadataNamespace: "ns"},
	}}
}

func deploymentTarget() corev1alpha1.ReleaseMutationTarget {
	return corev1alpha1.ReleaseMutationTarget{Group: "apps", Version: "v1", Kind: deploymentKind, Name: "web"}
}

func testCluster() *corev1alpha1.Cluster {
	return &corev1alpha1.Cluster{Spec: corev1alpha1.ClusterSpec{Metadata: map[string]string{"region": clusterMetaRegion}}}
}

// apply runs ApplyMutations with a fixed Release identity ("release-1"/"ns") and
// the given (optional) cluster for templating context.
func apply(objs []*unstructured.Unstructured, muts []corev1alpha1.ReleaseMutation, cluster *corev1alpha1.Cluster) ([]*unstructured.Unstructured, []corev1alpha1.ResolvedMutation, error) {
	return ApplyMutations(context.Background(), MutationInput{
		Objects:   objs,
		Mutations: muts,
		Release:   &corev1alpha1.Release{ObjectMeta: metav1.ObjectMeta{Name: releaseName, Namespace: "ns"}},
		Cluster:   cluster,
	})
}

func TestApplyMutations_JSONPatchTargeted(t *testing.T) {
	objs := []*unstructured.Unstructured{deployment("web"), configMap("cfg")}
	m := corev1alpha1.ReleaseMutation{
		Type:   corev1alpha1.MutationTypeJsonPatch,
		Target: deploymentTarget(),
		Patches: []corev1alpha1.ReleaseMutationPatchSpec{
			{Op: corev1alpha1.JSONPatchOperationReplace, Path: "/spec/replicas", Value: &apiextensionsv1.JSON{Raw: []byte("3")}},
		},
	}

	out, resolved, err := apply(objs, []corev1alpha1.ReleaseMutation{m}, nil)
	if err != nil {
		t.Fatalf("ApplyMutations: %v", err)
	}
	if got, _, _ := unstructured.NestedInt64(out[0].Object, "spec", "replicas"); got != 3 {
		t.Errorf("replicas = %d, want 3 (patched)", got)
	}
	// The ConfigMap didn't match the target → unchanged, same pointer.
	if out[1] != objs[1] {
		t.Errorf("non-matching object was replaced")
	}
	if len(resolved) != 1 || resolved[0].Type != corev1alpha1.MutationTypeJsonPatch || resolved[0].ContentHash == "" {
		t.Errorf("resolved = %+v, want one jsonPatch record with a hash", resolved)
	}
}

func TestApplyMutations_WildcardTargetMatchesKind(t *testing.T) {
	objs := []*unstructured.Unstructured{deployment("a"), deployment("b")}
	// No Name → matches all Deployments.
	m := corev1alpha1.ReleaseMutation{
		Type:   corev1alpha1.MutationTypeJsonPatch,
		Target: corev1alpha1.ReleaseMutationTarget{Group: "apps", Version: "v1", Kind: deploymentKind},
		Patches: []corev1alpha1.ReleaseMutationPatchSpec{
			{Op: corev1alpha1.JSONPatchOperationReplace, Path: "/spec/replicas", Value: &apiextensionsv1.JSON{Raw: []byte("5")}},
		},
	}
	out, _, err := apply(objs, []corev1alpha1.ReleaseMutation{m}, nil)
	if err != nil {
		t.Fatalf("ApplyMutations: %v", err)
	}
	for i, o := range out {
		if got, _, _ := unstructured.NestedInt64(o.Object, "spec", "replicas"); got != 5 {
			t.Errorf("obj %d replicas = %d, want 5", i, got)
		}
	}
}

func TestApplyMutations_StrategicMergeMergesListByKey(t *testing.T) {
	objs := []*unstructured.Unstructured{deployment("web")}
	// Strategic merge updates the "app" container by name, leaving "sidecar" intact
	// — a plain JSON merge would replace the whole list.
	m := corev1alpha1.ReleaseMutation{
		Type:   corev1alpha1.MutationTypeStrategicMergePatch,
		Target: deploymentTarget(),
		Inline: []string{`spec:
  template:
    spec:
      containers:
        - name: app
          image: app:2`},
	}

	out, _, err := apply(objs, []corev1alpha1.ReleaseMutation{m}, nil)
	if err != nil {
		t.Fatalf("ApplyMutations: %v", err)
	}
	containers, _, _ := unstructured.NestedSlice(out[0].Object, "spec", "template", "spec", "containers")
	if len(containers) != 2 {
		t.Fatalf("containers = %d, want 2 (strategic merge kept sidecar)", len(containers))
	}
	byName := map[string]string{}
	for _, c := range containers {
		cm := c.(map[string]any)
		byName[cm[metadataName].(string)], _ = cm["image"].(string)
	}
	if byName["app"] != "app:2" || byName["sidecar"] != imageValue {
		t.Errorf("images = %v, want app:2 + sidecar sc:1", byName)
	}
}

func TestApplyMutations_StrategicMergeRejectsUnregisteredKind(t *testing.T) {
	// A CRD kind isn't in the built-in scheme → strategic merge is rejected.
	cr := &unstructured.Unstructured{Object: map[string]any{
		unstructuredApiVersion: "example.com/v1",
		unstructuredKind:       "Widget",
		unstructuredMetadata:   map[string]any{metadataName: "w", metadataNamespace: "ns"},
	}}
	m := corev1alpha1.ReleaseMutation{
		Type:   corev1alpha1.MutationTypeStrategicMergePatch,
		Target: corev1alpha1.ReleaseMutationTarget{Group: "example.com", Version: "v1", Kind: "Widget"},
		Inline: []string{"spec: {}"},
	}
	if _, _, err := apply([]*unstructured.Unstructured{cr}, []corev1alpha1.ReleaseMutation{m}, nil); err == nil {
		t.Error("ApplyMutations err = nil, want rejection of strategic merge on a CRD")
	}
}

func TestApplyMutations_GoTemplateOverlayInjectsContext(t *testing.T) {
	objs := []*unstructured.Unstructured{deployment("web")}
	m := corev1alpha1.ReleaseMutation{
		Type:   corev1alpha1.MutationTypeGoTemplate,
		Target: deploymentTarget(),
		Inline: []string{`metadata:
  annotations:
    region: "{{ .cluster.region }}"
    release: "{{ .self.name }}"
    self-name: "{{ .object.metadata.name }}"`},
	}
	out, _, err := apply(objs, []corev1alpha1.ReleaseMutation{m}, testCluster())
	if err != nil {
		t.Fatalf("ApplyMutations: %v", err)
	}
	got, _, _ := unstructured.NestedStringMap(out[0].Object, "metadata", "annotations")
	if got["region"] != clusterMetaRegion || got["release"] != releaseName || got["self-name"] != "web" {
		t.Errorf("annotations = %v, want region=eu-west release=release-1 self-name=web", got)
	}
	// Overlay-merge must not clobber the object — spec.replicas survives.
	if r, _, _ := unstructured.NestedInt64(out[0].Object, "spec", "replicas"); r != 1 {
		t.Errorf("replicas = %d, want 1 preserved (overlay-merge, not replace)", r)
	}
}

func TestApplyMutations_CelTransformOverlayInjectsContext(t *testing.T) {
	objs := []*unstructured.Unstructured{deployment("web")}
	m := corev1alpha1.ReleaseMutation{
		Type:   corev1alpha1.MutationTypeCelTransform,
		Target: deploymentTarget(),
		Inline: []string{`{"metadata": {"annotations": {"region": cluster.region, "release": self.name}}}`},
	}
	out, _, err := apply(objs, []corev1alpha1.ReleaseMutation{m}, testCluster())
	if err != nil {
		t.Fatalf("ApplyMutations: %v", err)
	}
	got, _, _ := unstructured.NestedStringMap(out[0].Object, "metadata", "annotations")
	if got["region"] != clusterMetaRegion || got["release"] != releaseName {
		t.Errorf("annotations = %v, want region=eu-west release=release-1", got)
	}
	if r, _, _ := unstructured.NestedInt64(out[0].Object, "spec", "replicas"); r != 1 {
		t.Errorf("replicas = %d, want 1 preserved", r)
	}
}

func TestApplyMutations_StrategicMergeTemplated(t *testing.T) {
	objs := []*unstructured.Unstructured{deployment("web")}
	// Injects .cluster.region into a label AND updates the "app" container by name
	// — proving templating + strategic list-merge together.
	m := corev1alpha1.ReleaseMutation{
		Type:   corev1alpha1.MutationTypeStrategicMergePatch,
		Target: deploymentTarget(),
		Inline: []string{`metadata:
  labels:
    region: "{{ .cluster.region }}"
spec:
  template:
    spec:
      containers:
        - name: app
          image: "app:{{ .cluster.region }}"`},
	}
	out, _, err := apply(objs, []corev1alpha1.ReleaseMutation{m}, testCluster())
	if err != nil {
		t.Fatalf("ApplyMutations: %v", err)
	}
	if got, _, _ := unstructured.NestedString(out[0].Object, "metadata", "labels", "region"); got != clusterMetaRegion {
		t.Errorf("label region = %q, want %q (templated)", got, clusterMetaRegion)
	}
	containers, _, _ := unstructured.NestedSlice(out[0].Object, "spec", "template", "spec", "containers")
	if len(containers) != 2 {
		t.Fatalf("containers = %d, want 2 (strategic merge kept sidecar)", len(containers))
	}
	byName := map[string]string{}
	for _, c := range containers {
		cm := c.(map[string]any)
		byName[cm[metadataName].(string)], _ = cm["image"].(string)
	}
	if byName["app"] != "app:eu-west" || byName["sidecar"] != imageValue {
		t.Errorf("images = %v, want app:app:eu-west + sidecar sc:1", byName)
	}
}

func TestApplyMutations_JSONPatchTemplated(t *testing.T) {
	objs := []*unstructured.Unstructured{deployment("web")}
	// The patch value injects .self.name (a string) into a new labels map.
	m := corev1alpha1.ReleaseMutation{
		Type:   corev1alpha1.MutationTypeJsonPatch,
		Target: deploymentTarget(),
		Patches: []corev1alpha1.ReleaseMutationPatchSpec{
			{
				Op: corev1alpha1.JSONPatchOperationAdd, Path: "/metadata/labels",
				Value: &apiextensionsv1.JSON{Raw: []byte(`{"release": "{{ .self.name }}"}`)},
			},
		},
	}
	out, _, err := apply(objs, []corev1alpha1.ReleaseMutation{m}, testCluster())
	if err != nil {
		t.Fatalf("ApplyMutations: %v", err)
	}
	if got, _, _ := unstructured.NestedString(out[0].Object, "metadata", "labels", "release"); got != releaseName {
		t.Errorf("label release = %q, want %q (templated)", got, releaseName)
	}
}

func TestApplyMutations_UnknownTypeIsErrUnsupported(t *testing.T) {
	m := corev1alpha1.ReleaseMutation{Type: "bogus", Target: deploymentTarget(), Inline: []string{"x"}}
	_, _, err := apply([]*unstructured.Unstructured{deployment("web")}, []corev1alpha1.ReleaseMutation{m}, nil)
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported for an unknown type", err)
	}
}

func TestApplyMutations_NoneIsNoop(t *testing.T) {
	objs := []*unstructured.Unstructured{deployment("web")}
	out, resolved, err := apply(objs, nil, nil)
	if err != nil || resolved != nil {
		t.Fatalf("ApplyMutations = %v, %v; want no-op", resolved, err)
	}
	if len(out) != 1 || out[0] != objs[0] {
		t.Errorf("objects changed on no-op")
	}
}
