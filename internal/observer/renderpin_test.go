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

package observer

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/blobstore"
	"github.com/kumodo-io/convoy/internal/render"
	"github.com/kumodo-io/convoy/internal/renderstore"
)

// TestPinnedRender covers the observer's pin-awareness: when a Release is pinned to
// a render present in the store, that render is the desired state (so drift/preview
// assess against it, not against what source renders now). Otherwise it falls back
// to the source render.
func TestPinnedRender(t *testing.T) {
	ctx := context.Background()
	rs := renderstore.New(blobstore.NewMemory())
	manifest, err := render.MarshalManifest([]*unstructured.Unstructured{configMapUnstructured("pinned-cm")})
	if err != nil {
		t.Fatalf("MarshalManifest: %v", err)
	}
	if err := rs.PutRender(ctx, "ns", "r1", "sha256:pinned", manifest); err != nil {
		t.Fatalf("PutRender: %v", err)
	}
	c := &Controller{renderStore: rs}

	rel := &corev1alpha1.Release{
		Name: "r1", Namespace: "ns",
		Pin: &corev1alpha1.ReleasePin{RenderRef: "sha256:pinned"},
	}

	objs, ok, err := c.pinnedRender(ctx, rel)
	if err != nil || !ok {
		t.Fatalf("pinnedRender = ok=%v err=%v; want ok=true", ok, err)
	}
	if len(objs) != 1 || objs[0].GetName() != "pinned-cm" {
		t.Errorf("pinnedRender objects = %v, want the stored pinned render", objs)
	}

	// Not pinned → fall back to source render (ok=false).
	if _, ok, err := c.pinnedRender(ctx, &corev1alpha1.Release{Name: "r1", Namespace: "ns"}); err != nil || ok {
		t.Errorf("pinnedRender(no pin) = ok=%v err=%v; want ok=false", ok, err)
	}

	// Pinned to an unstored render → fall back to source render (ok=false).
	ghost := &corev1alpha1.Release{
		Name: "r1", Namespace: "ns",
		Pin: &corev1alpha1.ReleasePin{RenderRef: "sha256:ghost"},
	}
	if _, ok, err := c.pinnedRender(ctx, ghost); err != nil || ok {
		t.Errorf("pinnedRender(unstored) = ok=%v err=%v; want ok=false", ok, err)
	}

	// No store → fall back (ok=false).
	if _, ok, err := (&Controller{}).pinnedRender(ctx, rel); err != nil || ok {
		t.Errorf("pinnedRender(no store) = ok=%v err=%v; want ok=false", ok, err)
	}
}
