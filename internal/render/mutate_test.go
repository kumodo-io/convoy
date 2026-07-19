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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

func deployment(name string, replicas int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": name, "namespace": "ns"},
		"spec": map[string]any{
			"replicas": replicas,
			"template": map[string]any{"spec": map[string]any{
				"containers": []any{
					map[string]any{"name": "app", "image": "app:1"},
					map[string]any{"name": "sidecar", "image": "sc:1"},
				},
			}},
		},
	}}
}

func configMap(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": name, "namespace": "ns"},
	}}
}

func deploymentTarget(name string) corev1alpha1.ReleaseMutationTarget {
	return corev1alpha1.ReleaseMutationTarget{Group: "apps", Version: "v1", Kind: "Deployment", Name: name}
}

func TestApplyMutations_JSONPatchTargeted(t *testing.T) {
	objs := []*unstructured.Unstructured{deployment("web", 1), configMap("cfg")}
	m := corev1alpha1.ReleaseMutation{
		Type:   corev1alpha1.MutationTypeJsonPatch,
		Target: deploymentTarget("web"),
		Patches: []corev1alpha1.ReleaseMutationPatchSpec{
			{Op: corev1alpha1.JSONPatchOperationReplace, Path: "/spec/replicas", Value: &apiextensionsv1.JSON{Raw: []byte("3")}},
		},
	}

	out, resolved, err := ApplyMutations(context.Background(), objs, []corev1alpha1.ReleaseMutation{m}, nil, nil, "ns")
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
	objs := []*unstructured.Unstructured{deployment("a", 1), deployment("b", 1)}
	// No Name → matches all Deployments.
	m := corev1alpha1.ReleaseMutation{
		Type:   corev1alpha1.MutationTypeJsonPatch,
		Target: corev1alpha1.ReleaseMutationTarget{Group: "apps", Version: "v1", Kind: "Deployment"},
		Patches: []corev1alpha1.ReleaseMutationPatchSpec{
			{Op: corev1alpha1.JSONPatchOperationReplace, Path: "/spec/replicas", Value: &apiextensionsv1.JSON{Raw: []byte("5")}},
		},
	}
	out, _, err := ApplyMutations(context.Background(), objs, []corev1alpha1.ReleaseMutation{m}, nil, nil, "ns")
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
	objs := []*unstructured.Unstructured{deployment("web", 1)}
	// Strategic merge updates the "app" container by name, leaving "sidecar" intact
	// — a plain JSON merge would replace the whole list.
	m := corev1alpha1.ReleaseMutation{
		Type:   corev1alpha1.MutationTypeStrategicMergePatch,
		Target: deploymentTarget("web"),
		Inline: []string{`spec:
  template:
    spec:
      containers:
        - name: app
          image: app:2`},
	}

	out, _, err := ApplyMutations(context.Background(), objs, []corev1alpha1.ReleaseMutation{m}, nil, nil, "ns")
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
		byName[cm["name"].(string)], _ = cm["image"].(string)
	}
	if byName["app"] != "app:2" || byName["sidecar"] != "sc:1" {
		t.Errorf("images = %v, want app:2 + sidecar sc:1", byName)
	}
}

func TestApplyMutations_StrategicMergeRejectsUnregisteredKind(t *testing.T) {
	// A CRD kind isn't in the built-in scheme → strategic merge is rejected.
	cr := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1", "kind": "Widget",
		"metadata": map[string]any{"name": "w", "namespace": "ns"},
	}}
	m := corev1alpha1.ReleaseMutation{
		Type:   corev1alpha1.MutationTypeStrategicMergePatch,
		Target: corev1alpha1.ReleaseMutationTarget{Group: "example.com", Version: "v1", Kind: "Widget"},
		Inline: []string{"spec: {}"},
	}
	if _, _, err := ApplyMutations(context.Background(), []*unstructured.Unstructured{cr}, []corev1alpha1.ReleaseMutation{m}, nil, nil, "ns"); err == nil {
		t.Error("ApplyMutations err = nil, want rejection of strategic merge on a CRD")
	}
}

func TestApplyMutations_UnsupportedTypeIsErrUnsupported(t *testing.T) {
	m := corev1alpha1.ReleaseMutation{Type: corev1alpha1.MutationTypeGoTemplate, Target: deploymentTarget("web"), Inline: []string{"x"}}
	_, _, err := ApplyMutations(context.Background(), []*unstructured.Unstructured{deployment("web", 1)}, []corev1alpha1.ReleaseMutation{m}, nil, nil, "ns")
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported for goTemplate (slice 2)", err)
	}
}

func TestApplyMutations_NoneIsNoop(t *testing.T) {
	objs := []*unstructured.Unstructured{deployment("web", 1)}
	out, resolved, err := ApplyMutations(context.Background(), objs, nil, nil, nil, "ns")
	if err != nil || resolved != nil {
		t.Fatalf("ApplyMutations = %v, %v; want no-op", resolved, err)
	}
	if len(out) != 1 || out[0] != objs[0] {
		t.Errorf("objects changed on no-op")
	}
}
