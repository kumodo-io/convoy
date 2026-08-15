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

// Package backfill stamps the shard label onto objects created before shard
// labeling existed, so they become visible to shard-scoped informers. It is a
// one-time, idempotent migration: objects already carrying the correct label are
// skipped, and re-running it is a no-op (see docs/architecture/05-shard-scoped-caches.md).
package backfill

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	clientset "github.com/kumodo-io/convoy/api/generated/clientset/versioned"
	"github.com/kumodo-io/convoy/internal/sharding"
)

// Run stamps the shard label on every Release, Cluster, source, and
// ReleaseInventory that is missing it (or carries a stale value). Idempotent.
func Run(ctx context.Context, client clientset.Interface, sharder sharding.Sharder) error {
	if err := backfillClusters(ctx, client, sharder); err != nil {
		return err
	}
	if err := backfillSources(ctx, client, sharder); err != nil {
		return err
	}
	// Releases before inventories: an inventory's shard is copied from its Release.
	if err := backfillReleases(ctx, client, sharder); err != nil {
		return err
	}
	return backfillInventories(ctx, client, sharder)
}

// labelPatch is a merge patch that sets only the shard label.
func labelPatch(shard string) []byte {
	return []byte(fmt.Sprintf(`{"metadata":{"labels":{%q:%q}}}`, sharding.ShardLabel, shard))
}

// ensure patches the object to want unless it already carries it.
func ensure(ctx context.Context, current map[string]string, want string, patch func(context.Context, []byte) error) (bool, error) {
	if current[sharding.ShardLabel] == want {
		return false, nil
	}
	if err := patch(ctx, labelPatch(want)); err != nil {
		return false, err
	}
	return true, nil
}

func backfillClusters(ctx context.Context, client clientset.Interface, sharder sharding.Sharder) error {
	list, err := client.CoreV1alpha1().Clusters(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing clusters: %w", err)
	}
	patched := 0
	for i := range list.Items {
		c := &list.Items[i]
		want := sharder.ShardLabelValue(c.Namespace, c.Name)
		did, err := ensure(ctx, c.Labels, want, func(ctx context.Context, data []byte) error {
			_, e := client.CoreV1alpha1().Clusters(c.Namespace).Patch(ctx, c.Name, types.MergePatchType, data, metav1.PatchOptions{})
			return e
		})
		if err != nil {
			return fmt.Errorf("patching cluster %s/%s: %w", c.Namespace, c.Name, err)
		}
		if did {
			patched++
		}
	}
	klog.InfoS("backfill: clusters", "total", len(list.Items), "patched", patched)
	return nil
}

func backfillSources(ctx context.Context, client clientset.Interface, sharder sharding.Sharder) error {
	gits, err := client.SourceV1alpha1().GitRepositories(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing gitrepositories: %w", err)
	}
	for i := range gits.Items {
		g := &gits.Items[i]
		if _, err := ensure(ctx, g.Labels, sharder.ShardLabelValue(g.Namespace, g.Name), func(ctx context.Context, data []byte) error {
			_, e := client.SourceV1alpha1().GitRepositories(g.Namespace).Patch(ctx, g.Name, types.MergePatchType, data, metav1.PatchOptions{})
			return e
		}); err != nil {
			return fmt.Errorf("patching gitrepository %s/%s: %w", g.Namespace, g.Name, err)
		}
	}
	helms, err := client.SourceV1alpha1().HelmRegistries(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing helmregistries: %w", err)
	}
	for i := range helms.Items {
		h := &helms.Items[i]
		if _, err := ensure(ctx, h.Labels, sharder.ShardLabelValue(h.Namespace, h.Name), func(ctx context.Context, data []byte) error {
			_, e := client.SourceV1alpha1().HelmRegistries(h.Namespace).Patch(ctx, h.Name, types.MergePatchType, data, metav1.PatchOptions{})
			return e
		}); err != nil {
			return fmt.Errorf("patching helmregistry %s/%s: %w", h.Namespace, h.Name, err)
		}
	}
	ocis, err := client.SourceV1alpha1().OCIRegistries(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing ociregistries: %w", err)
	}
	for i := range ocis.Items {
		o := &ocis.Items[i]
		if _, err := ensure(ctx, o.Labels, sharder.ShardLabelValue(o.Namespace, o.Name), func(ctx context.Context, data []byte) error {
			_, e := client.SourceV1alpha1().OCIRegistries(o.Namespace).Patch(ctx, o.Name, types.MergePatchType, data, metav1.PatchOptions{})
			return e
		}); err != nil {
			return fmt.Errorf("patching ociregistry %s/%s: %w", o.Namespace, o.Name, err)
		}
	}
	return nil
}

func backfillReleases(ctx context.Context, client clientset.Interface, sharder sharding.Sharder) error {
	list, err := client.CoreV1alpha1().Releases(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing releases: %w", err)
	}
	patched := 0
	for i := range list.Items {
		r := &list.Items[i]
		cns := r.Spec.ClusterRef.Namespace
		if cns == "" {
			cns = r.Namespace
		}
		did, err := ensure(ctx, r.Labels, sharder.ShardLabelValue(cns, r.Spec.ClusterRef.Name), func(ctx context.Context, data []byte) error {
			_, e := client.CoreV1alpha1().Releases(r.Namespace).Patch(ctx, r.Name, types.MergePatchType, data, metav1.PatchOptions{})
			return e
		})
		if err != nil {
			return fmt.Errorf("patching release %s/%s: %w", r.Namespace, r.Name, err)
		}
		if did {
			patched++
		}
	}
	klog.InfoS("backfill: releases", "total", len(list.Items), "patched", patched)
	return nil
}

func backfillInventories(ctx context.Context, client clientset.Interface, sharder sharding.Sharder) error {
	list, err := client.CoreV1alpha1().ReleaseInventories(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing releaseinventories: %w", err)
	}
	for i := range list.Items {
		inv := &list.Items[i]
		// An inventory shards with its Release (same namespace/name).
		rel, err := client.CoreV1alpha1().Releases(inv.Namespace).Get(ctx, inv.Name, metav1.GetOptions{})
		if err != nil {
			klog.V(4).InfoS("backfill: skipping inventory with no Release", "namespace", inv.Namespace, "name", inv.Name)
			continue
		}
		cns := rel.Spec.ClusterRef.Namespace
		if cns == "" {
			cns = rel.Namespace
		}
		if _, err := ensure(ctx, inv.Labels, sharder.ShardLabelValue(cns, rel.Spec.ClusterRef.Name), func(ctx context.Context, data []byte) error {
			_, e := client.CoreV1alpha1().ReleaseInventories(inv.Namespace).Patch(ctx, inv.Name, types.MergePatchType, data, metav1.PatchOptions{})
			return e
		}); err != nil {
			return fmt.Errorf("patching inventory %s/%s: %w", inv.Namespace, inv.Name, err)
		}
	}
	return nil
}
