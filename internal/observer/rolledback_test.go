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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/blobstore"
	"github.com/kumodo-io/convoy/internal/render"
	"github.com/kumodo-io/convoy/internal/renderstore"
)

// configMapUnstructured builds a minimal namespaced ConfigMap object for render-store
// seeding in the observer's desired-render tests (pin- and rollout-awareness).
func configMapUnstructured(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": kindConfigMap,
		"metadata": map[string]any{"name": name, "namespace": "ns"},
	}}
}

// TestRolledBackRender covers the observer's rollout-awareness: while a Release's
// rollout is RolledBack, the controller keeps the LAST-HEALTHY render applied, so
// drift/preview/diff must assess against it (not against what source renders now).
// Otherwise it falls back to the source render.
func TestRolledBackRender(t *testing.T) {
	ctx := context.Background()
	rs := renderstore.New(blobstore.NewMemory())
	manifest, err := render.MarshalManifest([]*unstructured.Unstructured{configMapUnstructured("lh-cm")})
	if err != nil {
		t.Fatalf("MarshalManifest: %v", err)
	}
	if err := rs.PutRender(ctx, "ns", "r1", "sha256:lasthealthy", manifest); err != nil {
		t.Fatalf("PutRender: %v", err)
	}
	c := &Controller{renderStore: rs}

	rolledBack := func() *corev1alpha1.Release {
		return &corev1alpha1.Release{
			ObjectMeta: metav1.ObjectMeta{Name: "r1", Namespace: "ns"},
			Status: corev1alpha1.ReleaseStatus{
				LastHealthy: &corev1alpha1.LastHealthy{RenderRef: "sha256:lasthealthy"},
				Rollout: &corev1alpha1.RolloutStatus{
					State: corev1alpha1.RolloutStateRolledBack, RenderRef: "sha256:failed",
				},
			},
		}
	}

	objs, ok, err := c.rolledBackRender(ctx, rolledBack())
	if err != nil || !ok {
		t.Fatalf("rolledBackRender = ok=%v err=%v; want ok=true", ok, err)
	}
	if len(objs) != 1 || objs[0].GetName() != "lh-cm" {
		t.Errorf("rolledBackRender objects = %v, want the stored last-healthy render", objs)
	}

	// Not RolledBack (e.g. Succeeded) → fall back to source render (ok=false).
	succeeded := rolledBack()
	succeeded.Status.Rollout.State = corev1alpha1.RolloutStateSucceeded
	if _, ok, err := c.rolledBackRender(ctx, succeeded); err != nil || ok {
		t.Errorf("rolledBackRender(not RolledBack) = ok=%v err=%v; want ok=false", ok, err)
	}

	// No lastHealthy pointer → fall back (ok=false).
	noLH := rolledBack()
	noLH.Status.LastHealthy = nil
	if _, ok, err := c.rolledBackRender(ctx, noLH); err != nil || ok {
		t.Errorf("rolledBackRender(no lastHealthy) = ok=%v err=%v; want ok=false", ok, err)
	}

	// last-healthy render not in the store → fall back (ok=false).
	ghost := rolledBack()
	ghost.Status.LastHealthy = &corev1alpha1.LastHealthy{RenderRef: "sha256:ghost"}
	if _, ok, err := c.rolledBackRender(ctx, ghost); err != nil || ok {
		t.Errorf("rolledBackRender(unstored) = ok=%v err=%v; want ok=false", ok, err)
	}

	// No store → fall back (ok=false).
	if _, ok, err := (&Controller{}).rolledBackRender(ctx, rolledBack()); err != nil || ok {
		t.Errorf("rolledBackRender(no store) = ok=%v err=%v; want ok=false", ok, err)
	}
}
