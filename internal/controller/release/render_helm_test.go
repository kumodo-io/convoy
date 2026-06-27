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
	"testing"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/artifact"
)

const (
	dummyChartPath = "charts/demo"
)

// demoChart returns an artifact holding a minimal chart under charts/demo. The
// ConfigMap template uses .Release.Name/.Release.Namespace/.Values; a partial and
// a NOTES.txt are included to verify they're excluded from the output.
func demoChart() artifact.Artifact {
	return artifact.Artifact{Files: map[string][]byte{
		"charts/demo/Chart.yaml":             []byte("apiVersion: v2\nname: demo\nversion: 0.1.0\n"),
		"charts/demo/values.yaml":            []byte("greeting: hello\n"),
		"charts/demo/templates/cm.yaml":      []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}-cm\n  namespace: {{ .Release.Namespace }}\ndata:\n  greeting: {{ .Values.greeting }}\n"),
		"charts/demo/templates/_helpers.tpl": []byte("{{- define \"demo.foo\" -}}bar{{- end -}}\n"),
		"charts/demo/templates/NOTES.txt":    []byte("Thanks for installing {{ .Release.Name }}\n"),
	}}
}

func TestRenderHelm_RendersChartWithDefaults(t *testing.T) {
	objs, err := renderHelm(&corev1alpha1.ReleaseContentHelmSpec{Path: dummyChartPath}, demoChart())
	if err != nil {
		t.Fatalf("renderHelm returned error: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("got %d objects, want 1 (partial + NOTES excluded)", len(objs))
	}
	cm := objs[0]
	if cm.GetKind() != configMapKind {
		t.Errorf("kind = %q, want ConfigMap", cm.GetKind())
	}
	// releaseName defaults to the chart name, releaseNamespace to "default".
	if cm.GetName() != "demo-cm" {
		t.Errorf("name = %q, want demo-cm (release name defaulted to chart name)", cm.GetName())
	}
	if cm.GetNamespace() != "default" {
		t.Errorf("namespace = %q, want default", cm.GetNamespace())
	}
	greeting, _ := unstructuredString(cm.Object, "data", "greeting")
	if greeting != "hello" {
		t.Errorf("data.greeting = %q, want hello (chart default value)", greeting)
	}
}

func TestRenderHelm_HonorsReleaseNameAndNamespace(t *testing.T) {
	spec := &corev1alpha1.ReleaseContentHelmSpec{
		Path:             dummyChartPath,
		ReleaseName:      "myapp",
		ReleaseNamespace: "prod",
	}
	objs, err := renderHelm(spec, demoChart())
	if err != nil {
		t.Fatalf("renderHelm returned error: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("got %d objects, want 1", len(objs))
	}
	if objs[0].GetName() != "myapp-cm" {
		t.Errorf("name = %q, want myapp-cm", objs[0].GetName())
	}
	if objs[0].GetNamespace() != "prod" {
		t.Errorf("namespace = %q, want prod", objs[0].GetNamespace())
	}
}

func TestRenderHelm_ResolveDependencyModeUnsupported(t *testing.T) {
	spec := &corev1alpha1.ReleaseContentHelmSpec{
		Path:           dummyChartPath,
		DependencyMode: corev1alpha1.ReleaseHelmDependencyModeResolve,
	}
	if _, err := renderHelm(spec, demoChart()); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestRenderHelm_MissingChartAtPath(t *testing.T) {
	art := artifact.Artifact{Files: map[string][]byte{"charts/demo/Chart.yaml": []byte("apiVersion: v2\nname: demo\nversion: 0.1.0\n")}}
	_, err := renderHelm(&corev1alpha1.ReleaseContentHelmSpec{Path: "charts/missing"}, art)
	if err == nil {
		t.Fatalf("renderHelm returned nil, want error for missing chart")
	}
	if errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want a non-Unsupported error", err)
	}
}

// unstructuredString is a tiny nested-string getter to avoid pulling in the
// unstructured helpers package just for the tests.
func unstructuredString(obj map[string]any, fields ...string) (string, bool) {
	cur := obj
	for i, f := range fields {
		v, ok := cur[f]
		if !ok {
			return "", false
		}
		if i == len(fields)-1 {
			s, ok := v.(string)
			return s, ok
		}
		next, ok := v.(map[string]any)
		if !ok {
			return "", false
		}
		cur = next
	}
	return "", false
}
