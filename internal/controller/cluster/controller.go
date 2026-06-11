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
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	clientset "github.com/kumodo-io/convoy/api/generated/clientset/versioned"
	informers "github.com/kumodo-io/convoy/api/generated/informers/externalversions/core/v1alpha1"
	listers "github.com/kumodo-io/convoy/api/generated/listers/core/v1alpha1"
)

// Controller reconciles Cluster objects.
type Controller struct {
	client clientset.Interface
	lister listers.ClusterLister
	synced cache.InformerSynced
	prober Prober
	queue  workqueue.TypedRateLimitingInterface[string]
	// now is injectable for tests; defaults to metav1.Now.
	now func() metav1.Time
}

// NewController wires a Controller to a Cluster informer. It registers event
// handlers that enqueue changed objects; call Run to start processing.
func NewController(client clientset.Interface, informer informers.ClusterInformer, prober Prober) *Controller {
	c := &Controller{
		client: client,
		lister: informer.Lister(),
		synced: informer.Informer().HasSynced,
		prober: prober,
		queue: workqueue.NewTypedRateLimitingQueue(
			workqueue.DefaultTypedControllerRateLimiter[string](),
		),
		now: metav1.Now,
	}

	_, err := informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.enqueue,
		UpdateFunc: func(_, newObj any) {
			c.enqueue(newObj)
		},
		DeleteFunc: c.enqueue,
	})
	utilruntime.Must(err)

	return c
}

// enqueue extracts a namespace/name key from an object and queues it.
func (c *Controller) enqueue(obj any) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.queue.Add(key)
}

// Run starts workers and blocks until ctx is cancelled.
func (c *Controller) Run(ctx context.Context, workers int) error {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	klog.InfoS("starting cluster controller", "workers", workers)
	if !cache.WaitForCacheSync(ctx.Done(), c.synced) {
		return fmt.Errorf("failed to wait for cluster informer cache to sync")
	}

	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}

	<-ctx.Done()
	klog.InfoS("shutting down cluster controller")
	return nil
}

func (c *Controller) runWorker(ctx context.Context) {
	for c.processNextItem(ctx) {
	}
}

func (c *Controller) processNextItem(ctx context.Context) bool {
	key, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(key)

	if err := c.syncHandler(ctx, key); err != nil {
		c.queue.AddRateLimited(key)
		utilruntime.HandleError(fmt.Errorf("syncing cluster %q: %w", key, err))
		return true
	}
	c.queue.Forget(key)
	return true
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
		if _, ok := probeErr.(*ErrUnsupported); ok {
			return nil // not transient; don't hot-loop
		}
		return probeErr
	}
	return nil
}
