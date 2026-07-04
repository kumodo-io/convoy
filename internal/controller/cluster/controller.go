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

// Package cluster implements the controller that reconciles core.convoy
// Cluster objects: it probes each target cluster and records reachability,
// discovered version, and conditions in the object's status.
package cluster

import (
	"context"
	"errors"
	"fmt"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	clientset "github.com/kumodo-io/convoy/api/generated/clientset/versioned"
	informers "github.com/kumodo-io/convoy/api/generated/informers/externalversions/core/v1alpha1"
	listers "github.com/kumodo-io/convoy/api/generated/listers/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/clusterconn"
	"github.com/kumodo-io/convoy/internal/controller/base"
)

const (
	clusterFinalizer = "cluster.core.convoy.kumodo.io/prune-releases"
)

type ReleasesForClusterFunc func(clusterNs, clusterName string) ([]*corev1alpha1.Release, error)

// Controller reconciles Cluster objects. The work-queue, workers, and retry
// machinery live in the embedded base.Controller; this type supplies only the
// Cluster-specific reconcile logic.
type Controller struct {
	*base.Controller
	client clientset.Interface
	lister listers.ClusterLister
	prober Prober
	// now is injectable for tests; defaults to metav1.Now.
	now                func() metav1.Time
	releasesForCluster func(clusterNs, clusterName string) ([]*corev1alpha1.Release, error)
}

// NewController wires a Controller to a Cluster informer. It registers event
// handlers that enqueue changed objects; call Run to start processing.
func NewController(
	client clientset.Interface,
	informer informers.ClusterInformer,
	prober Prober,
	releasesForCluster ReleasesForClusterFunc,
) *Controller {
	c := &Controller{
		client:             client,
		lister:             informer.Lister(),
		prober:             prober,
		now:                metav1.Now,
		releasesForCluster: releasesForCluster,
	}
	c.Controller = base.NewController("cluster", c.syncHandler)
	utilruntime.Must(c.WatchInformer(informer.Informer()))
	return c
}

// syncHandler reconciles a single Cluster: probe the target and write status.
func (c *Controller) syncHandler(ctx context.Context, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key %q: %w", key, err))
		return nil // unrecoverable; don't requeue
	}

	cluster, err := c.lister.Clusters(namespace).Get(name)
	if apierrors.IsNotFound(err) {
		return nil // deleted; nothing to do
	}
	if err != nil {
		return err
	}

	// finalize Cluster on deletion according to deletion policy
	if !cluster.DeletionTimestamp.IsZero() {
		return c.finalize(ctx, cluster)
	}

	cluster, err = c.ensureFinalizer(ctx, cluster)
	if err != nil {
		return err
	}

	result, probeErr := c.prober.Probe(ctx, cluster)
	newStatus := computeStatus(cluster, result, probeErr, c.now())

	if statusChanged(cluster, newStatus) {
		updated := cluster.DeepCopy()
		updated.Status = newStatus
		if _, err := c.client.CoreV1alpha1().Clusters(namespace).UpdateStatus(ctx, updated, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("updating status: %w", err)
		}
		klog.V(4).InfoS("updated cluster status", "cluster", key, "reachable", newStatus.Reachable)
	}

	// A transient probe failure is reported as an error so the rate-limited
	// queue retries it. Periodic re-probes of healthy clusters come from the
	// informer's resync.
	if probeErr != nil {
		if errors.Is(probeErr, clusterconn.ErrUnsupported) {
			return nil // terminal; don't hot-loop
		}
		return probeErr
	}
	return nil
}

func (c *Controller) finalize(ctx context.Context, cluster *corev1alpha1.Cluster) error {
	if !slices.Contains(cluster.Finalizers, clusterFinalizer) {
		return nil // no-op
	}

	if cluster.Spec.DeletionPolicy.PruneReleases {
		if err := c.pruneReleases(ctx, cluster); err != nil {
			return fmt.Errorf("finalizing %s/%s: %w", cluster.Namespace, cluster.Name, err)
		}
	}

	// remove finalizer
	updated := cluster.DeepCopy()
	updated.Finalizers = slices.DeleteFunc(cluster.Finalizers, func(finalizer string) bool {
		return finalizer == clusterFinalizer
	})
	if _, err := c.client.CoreV1alpha1().Clusters(cluster.Namespace).Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("removing finalizer from %s/%s: %w", cluster.Namespace, cluster.Name, err)
	}
	return nil
}

func (c *Controller) ensureFinalizer(ctx context.Context, cluster *corev1alpha1.Cluster) (*corev1alpha1.Cluster, error) {
	if slices.Contains(cluster.Finalizers, clusterFinalizer) {
		return cluster, nil // no-op
	}

	updated := cluster.DeepCopy()
	updated.Finalizers = append(cluster.Finalizers, clusterFinalizer)
	return c.client.CoreV1alpha1().Clusters(cluster.Namespace).Update(ctx, updated, metav1.UpdateOptions{})
}

func (c *Controller) pruneReleases(ctx context.Context, cluster *corev1alpha1.Cluster) error {
	rels, err := c.releasesForCluster(cluster.Namespace, cluster.Name)
	if err != nil {
		return err
	}
	for _, rel := range rels {
		if err := c.client.CoreV1alpha1().Releases(rel.Namespace).
			Delete(ctx, rel.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	// Keep the Cluster finalizer until every referencing Release is gone (each
	// runs its own workload cleanup first). Re-check next pass:
	if len(rels) > 0 {
		return fmt.Errorf("waiting for %d release(s) to finalize", len(rels))
	}
	return nil
}
