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

	objs, err := render(manifestsContent("manifests"), art, nil)
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

	objs, err := render(manifestsContent(dummyFile), art, nil)
	if err != nil {
		t.Fatalf("render returned error: %v", err)
	}
	if len(objs) != 1 || objs[0].GetName() != "a" {
		t.Errorf("objs = %v, want one ConfigMap named a", objs)
	}
}

func TestRender_UnsupportedContentType(t *testing.T) {
	content := corev1alpha1.ReleaseContentSpec{Type: corev1alpha1.ReleaseContentTypeKustomize}
	if _, err := render(content, artifact.Artifact{}, nil); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestRender_MissingHelmBlock(t *testing.T) {
	content := corev1alpha1.ReleaseContentSpec{Type: corev1alpha1.ReleaseContentTypeHelm}
	if _, err := render(content, artifact.Artifact{}, nil); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestRender_MissingManifestsBlock(t *testing.T) {
	content := corev1alpha1.ReleaseContentSpec{Type: corev1alpha1.ReleaseContentTypeManifests}
	if _, err := render(content, artifact.Artifact{}, nil); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestRender_NoFilesAtPath(t *testing.T) {
	art := artifact.Artifact{Files: map[string][]byte{dummyFile: []byte(cmA)}}
	_, err := render(manifestsContent("nope"), art, nil)
	if err == nil {
		t.Fatalf("render returned nil, want error for missing path")
	}
	if errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want a non-Unsupported error", err)
	}
}
