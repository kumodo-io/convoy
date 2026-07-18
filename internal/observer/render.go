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
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/auth"
	"github.com/kumodo-io/convoy/internal/fetch"
	"github.com/kumodo-io/convoy/internal/render"
)

// renderReleaseObjects fetches a Release's source and renders its desired
// objects. It is the render-only path shared by the drift loop, the health loop,
// and the /preview server — a free function so both controllers (distinct types)
// can call it.
func renderReleaseObjects(ctx context.Context, coordinator fetch.Coordinator, resolver auth.SecretResolver, rel *corev1alpha1.Release) ([]*unstructured.Unstructured, error) {
	res, err := coordinator.Ensure(ctx, rel.Spec.Source, rel.Namespace)
	if err != nil {
		return nil, err
	}
	if !res.Ready {
		return nil, fmt.Errorf("source not ready: %s", res.Message)
	}
	valueLayers, err := resolveReleaseValues(ctx, coordinator, resolver, rel)
	if err != nil {
		return nil, err
	}
	return render.Render(rel.Spec.Content, res.Artifact, valueLayers)
}

// resolveReleaseValues resolves the Helm value sources to ordered raw-YAML layers.
func resolveReleaseValues(ctx context.Context, coordinator fetch.Coordinator, resolver auth.SecretResolver, rel *corev1alpha1.Release) ([][]byte, error) {
	if rel.Spec.Content.Type != corev1alpha1.ReleaseContentTypeHelm || rel.Spec.Content.Helm == nil {
		return nil, nil
	}
	return render.ResolveValues(ctx, rel.Spec.Content.Helm.Values, rel.Namespace, resolver, coordinator)
}
