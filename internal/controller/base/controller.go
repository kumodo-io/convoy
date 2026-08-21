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

// Package base provides a reusable work-queue controller harness. A concrete
// controller embeds *base.Controller, registers its informer(s) with
// WatchInformer, and supplies a ReconcileFunc; the harness owns the queue,
// workers, cache-sync wait, and rate-limited retries.
package base

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

// ReconcileFunc reconciles the object identified by a namespace/name key.
type ReconcileFunc func(ctx context.Context, key string) error

// Controller drives a rate-limited work queue: it enqueues namespace/name keys
// from one or more informers and invokes a ReconcileFunc for each, retrying
// failures with backoff, until its context is cancelled.
type Controller struct {
	name      string
	queue     workqueue.TypedRateLimitingInterface[string]
	synced    []cache.InformerSynced
	reconcile ReconcileFunc
	// reconcileRequestAnnotation, when non-empty, is an annotation key whose value
	// change forces an enqueue on update (e.g. a /sync reconcile-request stamp).
	// Empty means only resync + spec changes enqueue.
	reconcileRequestAnnotation string
	// enqueueFilter, when non-nil, drops informer add/update events whose object
	// this replica does not own (a sharding optimization). It is not applied to
	// deletes or to EnqueueKeys/EnqueueAfter; correctness still rests on the
	// ReconcileFunc's own ownership gate, so a stale filter only wastes work.
	enqueueFilter func(obj any) bool
}

// Option configures a Controller.
type Option func(*Controller)

// WithReconcileRequestAnnotation makes updates that change the given annotation
// key enqueue the object. Used so an out-of-process reconcile request (a /sync
// subresource stamping the annotation) promptly wakes the controller, without
// broadly enqueuing on every unrelated annotation edit.
func WithReconcileRequestAnnotation(key string) Option {
	return func(c *Controller) { c.reconcileRequestAnnotation = key }
}

// WithEnqueueFilter installs a predicate that keeps only informer add/update
// events for objects this replica owns, so unowned objects don't churn the
// queue under sharding. keep receives the informer object (never a delete
// tombstone). It is an optimization only — the ReconcileFunc must still gate on
// ownership, since ownership can change while a key is queued.
func WithEnqueueFilter(keep func(obj any) bool) Option {
	return func(c *Controller) { c.enqueueFilter = keep }
}

// WithShardGate installs sharding for a controller whose shard key is the
// object's own namespace/name (Cluster and the source kinds). owns reports
// whether this replica owns that identity. It both gates the ReconcileFunc (a
// non-owned key returns nil without work — the correctness layer) and installs
// the matching enqueue filter (the efficiency layer). A nil owns is a no-op.
//
// Controllers keyed by a *different* identity than their own object (the Release
// controller, keyed by its target cluster) do not use this; they gate inside
// their reconcile and set WithEnqueueFilter directly.
func WithShardGate(owns func(namespace, name string) bool) Option {
	return func(c *Controller) {
		if owns == nil {
			return
		}
		inner := c.reconcile
		c.reconcile = func(ctx context.Context, key string) error {
			namespace, name, err := cache.SplitMetaNamespaceKey(key)
			if err == nil && !owns(namespace, name) {
				return nil // not ours; skip without requeue
			}
			return inner(ctx, key)
		}
		c.enqueueFilter = func(obj any) bool {
			m, err := meta.Accessor(obj)
			if err != nil {
				return true // can't tell; enqueue and let the gate decide
			}
			return owns(m.GetNamespace(), m.GetName())
		}
	}
}

// NewController returns a harness that calls reconcile for each queued key.
// name is used in logs and error messages.
func NewController(name string, reconcile ReconcileFunc, opts ...Option) *Controller {
	c := &Controller{
		name: name,
		queue: workqueue.NewTypedRateLimitingQueue(
			workqueue.DefaultTypedControllerRateLimiter[string](),
		),
		reconcile: reconcile,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WatchInformer enqueues add/update/delete events from the informer and records
// its cache-sync function so Run waits for it before processing.
func (c *Controller) WatchInformer(informer cache.SharedIndexInformer) error {
	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.filteredEnqueue,
		UpdateFunc: func(oldObj, newObj any) {
			if c.shouldEnqueueOnUpdate(oldObj, newObj) {
				c.filteredEnqueue(newObj)
			}
		},
		// Deletes are always enqueued (no filter, no tombstone unwrapping): a
		// non-owner simply no-ops at the reconcile gate.
		DeleteFunc: c.enqueue,
	})
	if err != nil {
		return err
	}
	c.synced = append(c.synced, informer.HasSynced)
	return nil
}

// shouldEnqueueOnUpdate decides whether an informer update warrants a reconcile.
// It enqueues on:
//   - resync — the informer re-delivers a cached object as (obj, obj) with an
//     identical ResourceVersion, driving periodic re-reconciliation;
//   - spec changes — a bumped metadata.generation (status writes don't bump it,
//     thanks to the /status subresource); and
//   - a change to the configured reconcile-request annotation, if any.
//
// It skips other status/metadata-only updates (generation unchanged, no
// reconcile-request change, ResourceVersion changed) so a controller's own
// status writes don't re-trigger it. If either object lacks accessible metadata
// it errs toward enqueuing.
func (c *Controller) shouldEnqueueOnUpdate(oldObj, newObj any) bool {
	oldMeta, err := meta.Accessor(oldObj)
	if err != nil {
		return true
	}
	newMeta, err := meta.Accessor(newObj)
	if err != nil {
		return true
	}
	if oldMeta.GetResourceVersion() == newMeta.GetResourceVersion() {
		return true // resync
	}
	if oldMeta.GetGeneration() != newMeta.GetGeneration() {
		return true // spec change
	}
	if k := c.reconcileRequestAnnotation; k != "" &&
		oldMeta.GetAnnotations()[k] != newMeta.GetAnnotations()[k] {
		return true // reconcile requested (e.g. /sync stamp)
	}
	return false
}

// filteredEnqueue enqueues obj unless an installed enqueue filter rejects it.
func (c *Controller) filteredEnqueue(obj any) {
	if c.enqueueFilter != nil && !c.enqueueFilter(obj) {
		return
	}
	c.enqueue(obj)
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

// EnqueueKeys queues the given namespace/name keys directly. It is used to
// re-drive newly-owned work after a sharding reassignment; the keys are assumed
// already scoped to what this replica should reconcile.
func (c *Controller) EnqueueKeys(keys []string) {
	for _, key := range keys {
		c.queue.Add(key)
	}
}

// EnqueueAfter schedules key to be reprocessed after d. It lets a ReconcileFunc
// request its own future re-sync (e.g. a per-resource poll interval) on top of
// informer-driven events. A non-positive d enqueues immediately.
func (c *Controller) EnqueueAfter(key string, d time.Duration) {
	c.queue.AddAfter(key, d)
}

// Run waits for the watched caches to sync, starts workers, and blocks until
// ctx is cancelled.
func (c *Controller) Run(ctx context.Context, workers int) error {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	klog.InfoS("starting controller", "name", c.name, "workers", workers)
	if !cache.WaitForCacheSync(ctx.Done(), c.synced...) {
		return fmt.Errorf("%s: failed to wait for informer caches to sync", c.name)
	}

	for range workers {
		go wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}

	<-ctx.Done()
	klog.InfoS("shutting down controller", "name", c.name)
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

	if err := c.reconcile(ctx, key); err != nil {
		c.queue.AddRateLimited(key)
		utilruntime.HandleError(fmt.Errorf("%s: syncing %q: %w", c.name, key, err))
		return true
	}
	c.queue.Forget(key)
	return true
}
