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
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/artifact"
	"github.com/kumodo-io/convoy/internal/fetch"
)

// fakeResolver serves ConfigMaps/Secrets from in-memory maps keyed by name.
type fakeResolver struct {
	configMaps map[string]*corev1.ConfigMap
	secrets    map[string]*corev1.Secret
}

func (f fakeResolver) ConfigMap(_ context.Context, _, name string) (*corev1.ConfigMap, error) {
	if cm, ok := f.configMaps[name]; ok {
		return cm, nil
	}
	return nil, apierrors.NewNotFound(corev1.Resource("configmaps"), name)
}

func (f fakeResolver) Secret(_ context.Context, _, name string) (*corev1.Secret, error) {
	if s, ok := f.secrets[name]; ok {
		return s, nil
	}
	return nil, apierrors.NewNotFound(corev1.Resource("secrets"), name)
}

func boolPtr(b bool) *bool { return &b }

func inlineEntry(raw string) corev1alpha1.ValuesSourceSpec {
	return corev1alpha1.ValuesSourceSpec{
		Type:   corev1alpha1.ValuesSourceTypeInline,
		Inline: &apiextensionsv1.JSON{Raw: []byte(raw)},
	}
}

func configMapEntry(name, key string, optional *bool) corev1alpha1.ValuesSourceSpec {
	return corev1alpha1.ValuesSourceSpec{
		Type: corev1alpha1.ValuesSourceTypeConfigMap,
		ConfigMap: &corev1.ConfigMapKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: name},
			Key:                  key,
			Optional:             optional,
		},
	}
}

func secretEntry(name, key string, optional *bool) corev1alpha1.ValuesSourceSpec {
	return corev1alpha1.ValuesSourceSpec{
		Type: corev1alpha1.ValuesSourceTypeSecret,
		Secret: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: name},
			Key:                  key,
			Optional:             optional,
		},
	}
}

func TestResolveValues_OrderedLayers(t *testing.T) {
	resolver := fakeResolver{
		configMaps: map[string]*corev1.ConfigMap{
			"cm": {Data: map[string]string{"values.yaml": "b: 2\n"}},
		},
		secrets: map[string]*corev1.Secret{
			"sec": {Data: map[string][]byte{"values.yaml": []byte("c: 3\n")}},
		},
	}
	entries := []corev1alpha1.ValuesSourceSpec{
		inlineEntry(`{"a":1}`),
		configMapEntry("cm", "values.yaml", nil),
		secretEntry("sec", "values.yaml", nil),
	}

	layers, err := resolveValues(context.Background(), entries, "ns", resolver, nil)
	if err != nil {
		t.Fatalf("resolveValues returned error: %v", err)
	}
	if len(layers) != 3 {
		t.Fatalf("got %d layers, want 3", len(layers))
	}
	if string(layers[0]) != `{"a":1}` || string(layers[1]) != "b: 2\n" || string(layers[2]) != "c: 3\n" {
		t.Errorf("layers out of order or wrong content: %q", layers)
	}
}

func TestResolveValues_OptionalMissingSkipped(t *testing.T) {
	layers, err := resolveValues(context.Background(),
		[]corev1alpha1.ValuesSourceSpec{configMapEntry("absent", "values.yaml", boolPtr(true))},
		"ns", fakeResolver{}, nil)
	if err != nil {
		t.Fatalf("resolveValues returned error: %v", err)
	}
	if len(layers) != 0 {
		t.Errorf("got %d layers, want 0 (optional missing skipped)", len(layers))
	}
}

func TestResolveValues_RequiredMissingIsTransient(t *testing.T) {
	_, err := resolveValues(context.Background(),
		[]corev1alpha1.ValuesSourceSpec{configMapEntry("absent", "values.yaml", nil)},
		"ns", fakeResolver{}, nil)
	if err == nil {
		t.Fatalf("resolveValues returned nil, want a (transient) error")
	}
	if errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want a non-Unsupported (transient) error", err)
	}
}

func TestResolveValues_MissingKeyIsTransient(t *testing.T) {
	resolver := fakeResolver{configMaps: map[string]*corev1.ConfigMap{
		"cm": {Data: map[string]string{"other": "x"}},
	}}
	_, err := resolveValues(context.Background(),
		[]corev1alpha1.ValuesSourceSpec{configMapEntry("cm", "values.yaml", nil)},
		"ns", resolver, nil)
	if err == nil {
		t.Fatalf("resolveValues returned nil, want error for missing key")
	}
	if errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want a non-Unsupported error", err)
	}
}

func gitValuesEntry(paths ...string) corev1alpha1.ValuesSourceSpec {
	return corev1alpha1.ValuesSourceSpec{
		Type: corev1alpha1.ValuesSourceTypeGitRepository,
		GitRepository: &corev1alpha1.ValuesSourceGitRepositorySpec{
			ReleaseGitRepositorySpec: &corev1alpha1.ReleaseGitRepositorySpec{
				Name: "repo",
				Ref:  &corev1alpha1.ReleaseGitRepositoryRefSpec{Branch: new("main")},
			},
			Paths: paths,
		},
	}
}

func TestResolveValues_GitRepositoryReadsPathsInOrder(t *testing.T) {
	coord := &fakeCoordinator{result: fetch.Result{
		Ready:    true,
		Revision: "abc123",
		Artifact: artifact.Artifact{Files: map[string][]byte{
			"env/values.yaml":   []byte("a: 1\n"),
			"env/override.yaml": []byte("b: 2\n"),
		}},
	}}
	layers, err := resolveValues(context.Background(),
		[]corev1alpha1.ValuesSourceSpec{gitValuesEntry("env/values.yaml", "env/override.yaml")},
		"ns", fakeResolver{}, coord)
	if err != nil {
		t.Fatalf("resolveValues returned error: %v", err)
	}
	if len(layers) != 2 || string(layers[0]) != "a: 1\n" || string(layers[1]) != "b: 2\n" {
		t.Errorf("layers = %q, want [a:1, b:2] in path order", layers)
	}
}

func TestResolveValues_GitRepositoryNotReadyIsTerminal(t *testing.T) {
	coord := &fakeCoordinator{result: fetch.Result{Ready: false, Message: "unsupported ref"}}
	_, err := resolveValues(context.Background(),
		[]corev1alpha1.ValuesSourceSpec{gitValuesEntry("values.yaml")}, "ns", fakeResolver{}, coord)
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported (terminal)", err)
	}
}

func TestResolveValues_GitRepositoryFetchErrorIsTransient(t *testing.T) {
	coord := &fakeCoordinator{err: errors.New("source not found")}
	_, err := resolveValues(context.Background(),
		[]corev1alpha1.ValuesSourceSpec{gitValuesEntry("values.yaml")}, "ns", fakeResolver{}, coord)
	if err == nil {
		t.Fatalf("resolveValues returned nil, want transient error")
	}
	if errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want a non-Unsupported (transient) error", err)
	}
}

func TestResolveValues_GitRepositoryMissingFile(t *testing.T) {
	coord := &fakeCoordinator{result: fetch.Result{
		Ready:    true,
		Artifact: artifact.Artifact{Files: map[string][]byte{"other.yaml": []byte("x: 1\n")}},
	}}
	_, err := resolveValues(context.Background(),
		[]corev1alpha1.ValuesSourceSpec{gitValuesEntry("values.yaml")}, "ns", fakeResolver{}, coord)
	if err == nil {
		t.Fatalf("resolveValues returned nil, want error for missing values file")
	}
}

func TestResolveValues_EmptyInlineUnsupported(t *testing.T) {
	entry := corev1alpha1.ValuesSourceSpec{Type: corev1alpha1.ValuesSourceTypeInline}
	_, err := resolveValues(context.Background(), []corev1alpha1.ValuesSourceSpec{entry}, "ns", fakeResolver{}, nil)
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}
