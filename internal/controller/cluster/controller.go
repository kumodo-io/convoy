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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	clientset "github.com/kumodo-io/convoy/api/generated/clientset/versioned"
	informers "github.com/kumodo-io/convoy/api/generated/informers/externalversions/core/v1alpha1"
	listers "github.com/kumodo-io/convoy/api/generated/listers/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/controller/base"
)

// Controller reconciles Cluster objects. The work-queue, workers, and retry
// machinery live in the embedded base.Controller; this type supplies only the
// Cluster-specific reconcile logic.
type Controller struct {
	*base.Controller
	client clientset.Interface
	lister listers.ClusterLister
	prober Prober
	// now is injectable for tests; defaults to metav1.Now.
	now func() metav1.Time
}

// NewController wires a Controller to a Cluster informer. It registers event
// handlers that enqueue changed objects; call Run to start processing.
func NewController(client clientset.Interface, informer informers.ClusterInformer, prober Prober) *Controller {
	c := &Controller{
		client: client,
		lister: informer.Lister(),
		prober: prober,
		now:    metav1.Now,
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
		if errors.Is(probeErr, ErrUnsupported) {
			return nil // terminal; don't hot-loop
		}
		return probeErr
	}
	return nil
}
