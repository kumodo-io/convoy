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
	corelisters "github.com/kumodo-io/convoy/api/generated/listers/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/auth"
	"github.com/kumodo-io/convoy/internal/fetch"
	"github.com/kumodo-io/convoy/internal/render"
)

// renderReleaseObjects fetches a Release's source and renders its desired
// objects. It is the render-only path shared by the drift loop, the health loop,
// and the /preview server — a free function so both controllers (distinct types)
// can call it. cluster is the resolved target (may be nil), used only for the
// mutation pipeline's .Cluster templating context.
func renderReleaseObjects(ctx context.Context, coordinator fetch.Coordinator, resolver auth.SecretResolver, rel *corev1alpha1.Release, cluster *corev1alpha1.Cluster) ([]*unstructured.Unstructured, error) {
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
	objects, err := render.Render(rel.Spec.Content, res.Artifact, valueLayers)
	if err != nil {
		return nil, err
	}
	// Apply the same post-render mutation pipeline the controller-manager does, so
	// drift/preview/health compare against the mutated desired state.
	objects, _, err = render.ApplyMutations(ctx, render.MutationInput{
		Objects:     objects,
		Mutations:   rel.Mutations,
		Release:     rel,
		Cluster:     cluster,
		Resolver:    resolver,
		Coordinator: coordinator,
	})
	return objects, err
}

// clusterKeyOf returns the namespace/name of the cluster a Release targets — the
// Release's shard key for sharding.
func clusterKeyOf(rel *corev1alpha1.Release) (string, string) {
	ns := rel.Spec.ClusterRef.Namespace
	if ns == "" {
		ns = rel.Namespace
	}
	return ns, rel.Spec.ClusterRef.Name
}

// releaseOwned reports whether obj (a Release) belongs to a cluster shard this
// replica owns. It is the observer controllers' enqueue filter. A nil owns (no
// sharding) or a non-Release object passes.
func releaseOwned(owns func(clusterNamespace, clusterName string) bool, obj any) bool {
	if owns == nil {
		return true
	}
	rel, ok := obj.(*corev1alpha1.Release)
	if !ok {
		return true
	}
	return owns(clusterKeyOf(rel))
}

// resolveClusterRef looks up a Release's target Cluster from the lister,
// best-effort — nil if not found, since the mutation context tolerates a missing
// cluster (an empty .Cluster). Callers that require the cluster for live reads
// (drift/health) resolve and check it separately.
func resolveClusterRef(clusterLister corelisters.ClusterLister, rel *corev1alpha1.Release) *corev1alpha1.Cluster {
	ns := rel.Spec.ClusterRef.Namespace
	if ns == "" {
		ns = rel.Namespace
	}
	cluster, err := clusterLister.Clusters(ns).Get(rel.Spec.ClusterRef.Name)
	if err != nil {
		return nil
	}
	return cluster
}

// resolveReleaseValues resolves the Helm value sources to ordered raw-YAML layers.
func resolveReleaseValues(ctx context.Context, coordinator fetch.Coordinator, resolver auth.SecretResolver, rel *corev1alpha1.Release) ([][]byte, error) {
	if rel.Spec.Content.Type != corev1alpha1.ReleaseContentTypeHelm || rel.Spec.Content.Helm == nil {
		return nil, nil
	}
	return render.ResolveValues(ctx, rel.Spec.Content.Helm.Values, rel.Namespace, resolver, coordinator)
}
