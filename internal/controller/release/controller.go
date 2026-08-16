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

// Package release implements the controller that reconciles core.convoy
// Release objects: it renders the source artifact and applies it to the target
// cluster, recording the outcome in the object's status.
package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	corev1alpha1ac "github.com/kumodo-io/convoy/api/generated/applyconfiguration/core/v1alpha1"
	clientset "github.com/kumodo-io/convoy/api/generated/clientset/versioned"
	coreinformers "github.com/kumodo-io/convoy/api/generated/informers/externalversions/core/v1alpha1"
	corelisters "github.com/kumodo-io/convoy/api/generated/listers/core/v1alpha1"
	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
	"github.com/kumodo-io/convoy/internal/artifact"
	"github.com/kumodo-io/convoy/internal/auth"
	"github.com/kumodo-io/convoy/internal/blobstore"
	"github.com/kumodo-io/convoy/internal/clusterconn"
	"github.com/kumodo-io/convoy/internal/controller/base"
	"github.com/kumodo-io/convoy/internal/fetch"
	"github.com/kumodo-io/convoy/internal/inventory"
	"github.com/kumodo-io/convoy/internal/render"
	"github.com/kumodo-io/convoy/internal/renderstore"
	"github.com/kumodo-io/convoy/internal/waves"
)

const (
	CustomResourceDefinitionKind  = "CustomResourceDefinition"
	CustomResourceDefinitionGroup = "apiextensions.k8s.io"

	releaseFinalizer = "release.core.convoy.kumodo.io/prune-workloads"

	DefaultLocalCluster = "https://kubernetes.default.svc"

	// statusFieldManager is the Server-Side Apply field manager the Release
	// controller uses for its status writes. It owns only the fields this
	// controller produces (see ownedStatusApplyConfig); other status fields
	// (health, drift) belong to their own writers under distinct managers, so
	// concurrent writers never clobber each other.
	statusFieldManager = "convoy-release"
)

// ReleasesForClusterFunc returns the Releases targeting a given cluster
// (namespace/name), used to order Releases within a cluster (Axis B sync waves).
// It is backed by the releaseByCluster informer index.
type ReleasesForClusterFunc func(clusterNamespace, clusterName string) ([]*corev1alpha1.Release, error)

// Controller reconciles Release objects: fetch source content → render → apply
// to the target cluster → record status.
type Controller struct {
	*base.Controller
	client        clientset.Interface
	releaseLister corelisters.ReleaseLister
	clusterLister corelisters.ClusterLister
	coordinator   fetch.Coordinator
	resolver      auth.SecretResolver
	inventory     inventory.Store
	// renderStore durably persists each applied render (blob) and the ordered
	// deploy history. nil disables persistence — the controller renders and
	// applies exactly as before, just without a durable render history.
	renderStore        *renderstore.RenderStore
	deployer           Deployer
	hookRunner         HookRunner
	releasesForCluster ReleasesForClusterFunc
	// owns reports whether this replica owns a Release's target-cluster shard.
	// nil disables sharding (every Release is reconciled locally).
	owns func(clusterNamespace, clusterName string) bool
	// failures tracks consecutive transient reconcile failures per Release, to
	// drive per-Release retry backoff. In-memory; reset on a terminal outcome.
	failures *failureCounts
	// now is injectable for tests; defaults to metav1.Now.
	now func() metav1.Time
}

// failureCounts is a concurrency-safe per-key consecutive-failure counter.
type failureCounts struct {
	mu sync.Mutex
	n  map[string]int
}

func newFailureCounts() *failureCounts { return &failureCounts{n: make(map[string]int)} }

// inc increments and returns the failure count for key.
func (f *failureCounts) inc(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n[key]++
	return f.n[key]
}

// reset clears the failure streak for key.
func (f *failureCounts) reset(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.n, key)
}

// NewController wires a Controller to a Release informer. It fetches source
// content via the coordinator, resolves Helm value sources (ConfigMaps/Secrets)
// via resolver, tracks applied resources in inventory (for pruning), resolves
// target clusters from clusterLister, and orders Releases within a cluster
// (Axis B) via releasesForCluster (nil disables cluster-wave ordering).
func NewController(
	client clientset.Interface,
	informer coreinformers.ReleaseInformer,
	clusterLister corelisters.ClusterLister,
	coordinator fetch.Coordinator,
	resolver auth.SecretResolver,
	inv inventory.Store,
	renderStore *renderstore.RenderStore,
	deployer Deployer,
	hookRunner HookRunner,
	releasesForCluster ReleasesForClusterFunc,
	owns func(clusterNamespace, clusterName string) bool,
) *Controller {
	c := &Controller{
		client:             client,
		releaseLister:      informer.Lister(),
		clusterLister:      clusterLister,
		coordinator:        coordinator,
		resolver:           resolver,
		inventory:          inv,
		renderStore:        renderStore,
		deployer:           deployer,
		hookRunner:         hookRunner,
		releasesForCluster: releasesForCluster,
		owns:               owns,
		failures:           newFailureCounts(),
		now:                metav1.Now,
	}
	c.Controller = base.NewController("release", c.syncHandler,
		base.WithReconcileRequestAnnotation(corev1alpha1.SyncRequestedAtAnnotation),
		// Shard by the Release's target cluster: keep only Releases whose cluster
		// this replica owns. The syncHandler re-checks (ownership may shift while
		// queued); this only trims the queue.
		base.WithEnqueueFilter(func(obj any) bool {
			if c.owns == nil {
				return true
			}
			rel, ok := obj.(*corev1alpha1.Release)
			if !ok {
				return true
			}
			return c.owns(c.clusterKeyOf(rel))
		}))
	utilruntime.Must(c.WatchInformer(informer.Informer()))
	return c
}

// clusterKeyOf returns the namespace/name of the cluster a Release targets — the
// Release's shard key.
func (c *Controller) clusterKeyOf(rel *corev1alpha1.Release) (string, string) {
	return orDefault(rel.Spec.ClusterRef.Namespace, rel.Namespace), rel.Spec.ClusterRef.Name
}

func (c *Controller) syncHandler(ctx context.Context, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key %q: %w", key, err))
		return nil // unrecoverable; don't requeue
	}

	rel, err := c.releaseLister.Releases(namespace).Get(name)
	if apierrors.IsNotFound(err) {
		return nil // deleted; nothing to do
	}
	if err != nil {
		return err
	}

	// Sharding gate: another replica owns this Release's target-cluster shard.
	// Skip everything (including finalization — the owner handles it).
	if c.owns != nil && !c.owns(c.clusterKeyOf(rel)) {
		return nil
	}

	// finalize Release on deletion according to deletion policy
	if !rel.DeletionTimestamp.IsZero() {
		return c.finalize(ctx, key, rel)
	}

	rel, err = c.ensureFinalizer(ctx, rel)
	if err != nil {
		return err
	}

	outcome, recErr := c.reconcile(ctx, rel)
	newStatus := computeStatus(rel, c.now(), outcome.ready, outcome.reason, outcome.message)
	newStatus.Pin = pinStatus(rel)
	newStatus.LastAttempted = &outcome.attempt
	if outcome.applied {
		newStatus.LastSuccessful = outcome.attempt.DeepCopy()
	}
	if outcome.handledSyncAt != nil {
		newStatus.LastHandledSyncAt = outcome.handledSyncAt
	}
	newStatus.Waves = outcome.waves
	newStatus.ResolvedRetentionPolicy = c.resolvedRetentionStatus(ctx, rel)

	// Roll up lifecycle-hook progress (status.lifecycle + LifecycleSucceeded) from the
	// durable ReleaseInventory the gate maintains — controller-owned, parity with waves.
	if err := c.applyLifecycleRollup(ctx, rel, &newStatus, c.now()); err != nil {
		return err
	}

	if statusChanged(rel, newStatus) {
		newStatus.ReconciledAt = new(c.now())
		ac, err := ownedStatusApplyConfig(rel, newStatus)
		if err != nil {
			return fmt.Errorf("building status apply: %w", err)
		}
		if _, err := c.client.CoreV1alpha1().Releases(namespace).ApplyStatus(ctx, ac, metav1.ApplyOptions{FieldManager: statusFieldManager, Force: true}); err != nil {
			return fmt.Errorf("applying status: %w", err)
		}
		klog.V(4).InfoS("applied release status", "release", key, "applied", outcome.ready, "reason", outcome.reason)
	}

	// Requeue policy.
	switch {
	case recErr != nil && rel.Spec.SyncPolicy.RetryBackoff.Initial.Duration > 0:
		// Transient failure with a per-Release backoff configured: drive the retry
		// ourselves and return nil so base's default rate limiter doesn't also
		// schedule one (the delaying queue would otherwise pick the earlier timer).
		n := c.failures.inc(key)
		delay := backoffDelay(rel.Spec.SyncPolicy.RetryBackoff, n)
		utilruntime.HandleError(fmt.Errorf("release %s: %w (retry %d in %s)", key, recErr, n, delay))
		c.EnqueueAfter(key, delay)
		return nil
	case recErr != nil:
		// Transient failure, no custom backoff: base's default rate limiter retries.
		return recErr
	default:
		// Terminal (success or hold): clear the failure streak and schedule the next
		// re-check at the Release's poll interval. This runs in both modes — manual
		// mode keeps observing (drift/sync-state) on the interval, it just won't apply
		// until a /sync arrives.
		c.failures.reset(key)
		if d := syncRequeueAfter(rel); d > 0 {
			c.EnqueueAfter(key, d)
		}
		return nil
	}
}

// pendingSync reports whether an unhandled /sync request is stamped on the
// Release, returning the (second-granular) request time. A request is pending
// when the sync-requested-at annotation parses to a time after
// status.lastHandledSyncAt. Times are compared at second granularity because
// metav1.Time persists at that resolution, so a sub-second residue would
// otherwise keep a handled request perpetually pending.
func pendingSync(rel *corev1alpha1.Release) (*metav1.Time, bool) {
	raw := rel.Annotations[corev1alpha1.SyncRequestedAtAnnotation]
	if raw == "" {
		return nil, false
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, false // unparseable stamp: treat as no request
	}
	requestedAt := metav1.NewTime(t.Truncate(time.Second))
	if h := rel.Status.LastHandledSyncAt; h != nil && !requestedAt.After(h.Time) {
		return nil, false
	}
	return &requestedAt, true
}

// ownedStatusApplyConfig builds a Server-Side Apply configuration carrying only
// the status fields this controller owns. The top-level config is built field by
// field — NOT round-tripped from the whole ReleaseStatus struct — because that
// struct's sibling fields (healthSummary, lifecycle, …) lack omitempty and would
// serialize as zero values, clobbering fields owned by other writers (the
// observer's health, drift). Only the owned nested objects are projected through
// JSON, where the all-omitempty apply-configuration types make it safe.
func ownedStatusApplyConfig(rel *corev1alpha1.Release, s corev1alpha1.ReleaseStatus) (*corev1alpha1ac.ReleaseApplyConfiguration, error) {
	statusAC := corev1alpha1ac.ReleaseStatus().WithObservedGeneration(s.ObservedGeneration)

	// Assert ONLY the conditions this controller owns — Applied and (when the Release
	// has hooks) LifecycleSucceeded. computeStatus deep-copies the whole status
	// (carrying the observer-owned InSync and Healthy conditions), so writing every
	// condition here would make this field manager co-own them and, with Force,
	// clobber the observer's values back to a stale copy on each reconcile. conditions
	// is a listType=map keyed by type, so asserting only ours leaves the others
	// untouched; omitting LifecycleSucceeded when absent relinquishes it (no hooks).
	for _, condType := range []string{conditionApplied, conditionLifecycleSucceeded} {
		cond := apimeta.FindStatusCondition(s.Conditions, condType)
		if cond == nil {
			continue
		}
		condAC := &metav1ac.ConditionApplyConfiguration{}
		if err := jsonRoundTrip(*cond, condAC); err != nil {
			return nil, err
		}
		statusAC.WithConditions(condAC)
	}

	// status.lifecycle is a controller-owned rollup (parity with waves). Its per-phase
	// lists are all omitempty, so round-tripping the whole struct is safe. Omitting it
	// when empty relinquishes ownership (hooks removed).
	if !lifecycleStatusEmpty(s.Lifecycle) {
		lcAC := &corev1alpha1ac.LifecycleStatusApplyConfiguration{}
		if err := jsonRoundTrip(s.Lifecycle, lcAC); err != nil {
			return nil, err
		}
		statusAC.WithLifecycle(lcAC)
	}

	// Only assert pin when set. Omitting it when nil is how SSA clears a pin this
	// manager previously owned (unpin), rather than writing an empty pin object.
	if s.Pin != nil {
		pinAC := &corev1alpha1ac.ReleasePinStatusApplyConfiguration{}
		if err := jsonRoundTrip(s.Pin, pinAC); err != nil {
			return nil, err
		}
		statusAC.WithPin(pinAC)
	}

	if s.LastAttempted != nil {
		la := &corev1alpha1ac.ReleaseAttemptedRenderStatusApplyConfiguration{}
		if err := jsonRoundTrip(s.LastAttempted, la); err != nil {
			return nil, err
		}
		statusAC.WithLastAttempted(la)
	}
	if s.LastSuccessful != nil {
		ls := &corev1alpha1ac.ReleaseAttemptedRenderStatusApplyConfiguration{}
		if err := jsonRoundTrip(s.LastSuccessful, ls); err != nil {
			return nil, err
		}
		statusAC.WithLastSuccessful(ls)
	}
	// resolvedRetentionPolicy is controller-owned (it reflects the GC policy this
	// controller enforces). Assert it explicitly so it doesn't clobber the sibling
	// observer-owned healthSummary.
	rp := &corev1alpha1ac.ResolvedRetentionPolicyStatusApplyConfiguration{}
	if err := jsonRoundTrip(s.ResolvedRetentionPolicy, rp); err != nil {
		return nil, err
	}
	statusAC.WithResolvedRetentionPolicy(rp)

	if s.ReconciledAt != nil {
		statusAC.WithReconciledAt(*s.ReconciledAt)
	}
	if s.LastHandledSyncAt != nil {
		statusAC.WithLastHandledSyncAt(*s.LastHandledSyncAt)
	}

	// waves is a listType=map keyed by number, fully owned by this controller: it
	// writes applied, healthy (rolled up from the observer-written ReleaseInventory,
	// so there is no second writer on status), and the descriptive fields. Each
	// entry is built field by field, asserting only the fields that are set.
	for i := range s.Waves {
		w := s.Waves[i]
		waveAC := corev1alpha1ac.ResourceWaveStatus().WithNumber(w.Number)
		if w.Applied != nil {
			waveAC.WithApplied(*w.Applied)
		}
		if w.Healthy != nil {
			waveAC.WithHealthy(*w.Healthy)
		}
		if w.Resources != 0 {
			waveAC.WithResources(w.Resources)
		}
		if w.State != "" {
			waveAC.WithState(w.State)
		}
		if w.StartedAt != nil {
			waveAC.WithStartedAt(*w.StartedAt)
		}
		if w.CompletedAt != nil {
			waveAC.WithCompletedAt(*w.CompletedAt)
		}
		if w.FailedAt != nil {
			waveAC.WithFailedAt(*w.FailedAt)
		}
		if w.Message != "" {
			waveAC.WithMessage(w.Message)
		}
		statusAC.WithWaves(waveAC)
	}

	return corev1alpha1ac.Release(rel.Name, rel.Namespace).WithStatus(statusAC), nil
}

// jsonRoundTrip marshals src and unmarshals it into dst. Used to convert a typed
// status struct into its apply-configuration mirror.
func jsonRoundTrip(src, dst any) error {
	b, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

func (c *Controller) ensureFinalizer(ctx context.Context, rel *corev1alpha1.Release) (*corev1alpha1.Release, error) {
	if slices.Contains(rel.Finalizers, releaseFinalizer) {
		return rel, nil // no-op
	}

	updated := rel.DeepCopy()
	updated.Finalizers = append(rel.Finalizers, releaseFinalizer)
	return c.client.CoreV1alpha1().Releases(rel.Namespace).Update(ctx, updated, metav1.UpdateOptions{})
}

// deleteRenderRefNone is the delete-hook arming key when the Release never rendered.
const deleteRenderRefNone = "N/A"

// finalizeHoldRequeue is the safety-net requeue while a delete-phase hook runs; the
// observer's /sync poke on hook completion is the primary wake.
const finalizeHoldRequeue = 10 * time.Second

// deleteRenderRef is the arming identity for a Release's delete-phase hooks. Deletion
// happens once and has no render, so the last attempted render is a stable key (a
// sentinel when the Release never rendered), giving arm-once semantics per deletion.
func deleteRenderRef(rel *corev1alpha1.Release) string {
	if la := rel.Status.LastAttempted; la != nil && la.RenderRef != "" {
		return la.RenderRef
	}
	return deleteRenderRefNone
}

func (c *Controller) finalize(ctx context.Context, key string, rel *corev1alpha1.Release) error {
	if !slices.Contains(rel.Finalizers, releaseFinalizer) {
		return nil // no-op
	}

	prune := !rel.Spec.DeletionPolicy.KeepWorkloads
	hasDeleteHooks := c.hookRunner != nil && rel.Lifecycle != nil &&
		(len(rel.Lifecycle.PreDelete) > 0 || len(rel.Lifecycle.PostDelete) > 0)

	// Resolve the target cluster once, only when the teardown needs it (to prune
	// workloads or run delete-phase hooks). Unreachable/gone is governed by the
	// deletion policy exactly as the prune path always has: block when
	// BlockOnClusterUnreachable, else best-effort (skip hooks + prune, still delete).
	var cluster *corev1alpha1.Cluster
	if prune || hasDeleteHooks {
		resolved, usable, err := c.clusterForFinalize(rel)
		if err != nil {
			return err // transient, or blocking on an unreachable cluster
		}
		if !usable {
			prune, hasDeleteHooks = false, false // cluster gone/unreachable: best-effort
		}
		cluster = resolved
	}

	// PreDelete — gate teardown. A hold requeues; a Fail (onFailure=Fail) blocks the
	// deletion (finalizer stays) until the hook succeeds or the finalizer is removed.
	if hasDeleteHooks {
		res, err := c.runHookPhase(ctx, rel, cluster, corev1alpha1.LifecycleHookPhasePreDelete, deleteRenderRef(rel))
		if err != nil {
			return err
		}
		if res.state == hookPhaseHold || res.state == hookPhaseFailed {
			c.EnqueueAfter(key, finalizeHoldRequeue)
			return nil
		}
	}

	if prune {
		if err := c.pruneRefs(ctx, cluster, rel); err != nil {
			return fmt.Errorf("finalizing %s/%s: %w", rel.Namespace, rel.Name, err)
		}
	}

	// PostDelete — after teardown. A hold requeues; a Fail is best-effort (logged),
	// since the workloads are already gone.
	if hasDeleteHooks {
		res, err := c.runHookPhase(ctx, rel, cluster, corev1alpha1.LifecycleHookPhasePostDelete, deleteRenderRef(rel))
		if err != nil {
			return err
		}
		if res.state == hookPhaseHold {
			c.EnqueueAfter(key, finalizeHoldRequeue)
			return nil
		}
		if res.state == hookPhaseFailed {
			klog.V(2).InfoS("postDelete lifecycle hook failed", "release", key, "hook", res.failedHook, "message", res.message)
		}
	}

	// Delete the durable inventory before dropping the finalizer: teardown has
	// succeeded (or was skipped), so the record's job is done. Doing it first
	// means a failure here keeps the finalizer, and the delete is retried, rather
	// than the Release vanishing and orphaning its ReleaseInventory.
	if err := c.inventory.Delete(ctx, rel.Namespace, rel.Name); err != nil {
		return fmt.Errorf("deleting inventory for %s/%s: %w", rel.Namespace, rel.Name, err)
	}

	// Drop the release's render store prefix (all blobs + history) before removing
	// the finalizer, so a failure retries rather than leaking storage after the
	// Release is gone. No-op when no render store is configured.
	if c.renderStore != nil {
		if err := c.renderStore.DeleteRelease(ctx, rel.Namespace, rel.Name); err != nil {
			return fmt.Errorf("deleting render store for %s/%s: %w", rel.Namespace, rel.Name, err)
		}
	}

	// remove finalizer
	updated := rel.DeepCopy()
	updated.Finalizers = slices.DeleteFunc(rel.Finalizers, func(finalizer string) bool {
		return finalizer == releaseFinalizer
	})
	if _, err := c.client.CoreV1alpha1().Releases(rel.Namespace).Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("removing finalizer from %s/%s: %w", rel.Namespace, rel.Name, err)
	}
	return nil
}

// clusterForFinalize resolves the Release's target cluster for teardown. usable is
// false (with a nil error) when the cluster is gone or unreachable-but-not-blocking,
// signaling best-effort deletion (skip hooks + prune, still remove the finalizer). A
// non-nil error blocks the deletion (transient, or an unreachable cluster under
// BlockOnClusterUnreachable) so it retries.
func (c *Controller) clusterForFinalize(rel *corev1alpha1.Release) (*corev1alpha1.Cluster, bool, error) {
	clusterNamespace := orDefault(rel.Spec.ClusterRef.Namespace, rel.Namespace)
	cluster, err := c.clusterLister.Clusters(clusterNamespace).Get(rel.Spec.ClusterRef.Name)
	if apierrors.IsNotFound(err) {
		return nil, false, nil // cluster gone → nothing to tear down on it
	}
	if err != nil {
		return nil, false, fmt.Errorf("resolving target cluster: %w", err)
	}
	if !cluster.Status.Reachable {
		if rel.Spec.DeletionPolicy.BlockOnClusterUnreachable {
			return nil, false, fmt.Errorf("cluster %s not reachable", cluster.Name) // blocks
		}
		return nil, false, nil // best-effort: give up cleanup, allow deletion
	}
	return cluster, true, nil
}

// pruneRefs deletes the Release's inventoried resources from the (resolved,
// reachable) target cluster. NotFound resources are ignored by the deployer.
func (c *Controller) pruneRefs(ctx context.Context, cluster *corev1alpha1.Cluster, rel *corev1alpha1.Release) error {
	refs, ok, err := c.inventory.Get(ctx, rel.Namespace, rel.Name)
	if err != nil {
		return err
	}
	if !ok || len(refs) == 0 {
		return nil // never applied, or already pruned
	}
	return c.deployer.Prune(ctx, cluster, refs)
}

// syncRequeueAfter returns the per-Release drift re-check interval, or 0 when
// none is configured (in which case only the informer resync drives re-checks).
func syncRequeueAfter(rel *corev1alpha1.Release) time.Duration {
	return rel.Spec.SyncPolicy.Interval.Duration
}

// defaultBackoffFactor is used when the configured factor is unset/invalid.
const defaultBackoffFactor = 2.0

// defaultMaxBackoff caps per-Release retry backoff when Max is unset, bounding
// both the wait and float growth.
const defaultMaxBackoff = 1000 * time.Second

// backoffDelay computes the wait before the attempt-th consecutive retry
// (1-based): Initial * Factor^(attempt-1), capped at Max (or defaultMaxBackoff).
func backoffDelay(spec corev1alpha1.ReleaseRetryBackoffSpec, attempt int) time.Duration {
	max := spec.Max.Duration
	if max <= 0 {
		max = defaultMaxBackoff
	}
	factor := spec.Factor
	if factor < 1 {
		factor = defaultBackoffFactor
	}
	if attempt < 1 {
		attempt = 1
	}

	d := float64(spec.Initial.Duration) * math.Pow(factor, float64(attempt-1))
	if math.IsInf(d, 0) || d > float64(max) {
		return max
	}
	return time.Duration(d)
}

// reconcileOutcome is the result of a reconcile: the Ready condition inputs plus
// the attempt record to persist in status.
type reconcileOutcome struct {
	ready   bool
	reason  string
	message string
	attempt corev1alpha1.ReleaseAttemptedRenderStatus
	// applied is true only when the render was actually applied to the cluster this
	// reconcile; it gates advancing status.lastSuccessful (a ready-but-not-applied
	// hold, e.g. an in-sync manual Release, must not overwrite the last real apply).
	applied bool
	// handledSyncAt, when set, is the /sync request time this reconcile satisfied;
	// the caller records it in status.lastHandledSyncAt.
	handledSyncAt *metav1.Time
	// waves is the per-wave apply progress for status.waves (controller owns the
	// `applied` field; the observer owns `healthy`).
	waves []corev1alpha1.ResourceWaveStatus
}

// reconcile runs fetch → render → apply → prune, building an attempt record. In
// automatic mode it applies on the success path (drift correction); in manual
// mode it renders and observes but applies only when a /sync is pending, so
// status (pin, sync-state, later drift/health) stays fresh without mutating the
// cluster. The returned error is a requeue signal (non-nil = transient).
// reconcileState carries the values threaded across reconcile stages: the shared
// attempt record (mutated in place), and the render-stage products (artifact,
// target cluster, rendered objects) the sync stage consumes.
type reconcileState struct {
	rel         *corev1alpha1.Release
	now         metav1.Time
	manual      bool
	pending     bool
	requestedAt *metav1.Time
	attempt     corev1alpha1.ReleaseAttemptedRenderStatus

	// Produced by stageRender, consumed by stageGates/stageSync.
	art     artifact.Artifact
	cluster *corev1alpha1.Cluster
	objects []*unstructured.Unstructured
}

// newReconcileState seeds the per-reconcile state (attempt timestamp, mode, pending
// /sync) before any stage runs.
func (c *Controller) newReconcileState(rel *corev1alpha1.Release) *reconcileState {
	now := c.now()
	requestedAt, pending := pendingSync(rel)
	return &reconcileState{
		rel:         rel,
		now:         now,
		manual:      !rel.Spec.SyncPolicy.Automatic,
		pending:     pending,
		requestedAt: requestedAt,
		attempt: corev1alpha1.ReleaseAttemptedRenderStatus{
			AttemptedAt:    &now,
			ResolvedSource: resolvedSourceFrom(rel),
		},
	}
}

// fail stamps the attempt as failed at the given stage and builds the failure
// outcome. The error is the reconcile's requeue signal (non-nil = transient).
func (st *reconcileState) fail(reason, message string, stage corev1alpha1.ReleaseRenderAttemptStage, err error) (reconcileOutcome, error) {
	st.attempt.Phase = corev1alpha1.ReleaseRenderAttemptPhaseFailed
	st.attempt.Failure = &corev1alpha1.ReleaseAttemptedRenderFailureStatus{Stage: stage, Message: message}
	return reconcileOutcome{reason: reason, message: message, attempt: st.attempt}, err
}

// failHook stamps the attempt as failed at a lifecycle-hook phase (recording the
// offending hook) and builds the failure outcome. A hook failure is terminal for
// this reconcile but not a transient error, so err is nil.
func (st *reconcileState) failHook(phase corev1alpha1.LifecycleHookPhase, res hookPhaseResult) (reconcileOutcome, error) {
	p := phase
	st.attempt.Phase = corev1alpha1.ReleaseRenderAttemptPhaseFailed
	st.attempt.Failure = &corev1alpha1.ReleaseAttemptedRenderFailureStatus{
		Stage:     corev1alpha1.ReleaseRenderAttemptStageHookFailed,
		Message:   res.message,
		HookName:  res.failedHook,
		HookPhase: &p,
	}
	return reconcileOutcome{reason: conditionReasonHookFailed, message: res.message, attempt: st.attempt}, nil
}

// stop wraps a terminal (reconcileOutcome, error) as a stage result that halts the
// pipeline. done=true tells reconcile to return the outcome without running the
// remaining stages.
func stop(out reconcileOutcome, err error) (reconcileOutcome, bool, error) {
	return out, true, err
}

// cont is the stage result that lets reconcile proceed to the next stage.
func cont() (reconcileOutcome, bool, error) {
	return reconcileOutcome{}, false, nil
}

// reconcile runs fetch → render → apply → prune as a small pipeline of stages,
// building an attempt record. In automatic mode it applies on the success path
// (drift correction); in manual mode it renders and observes but applies only when
// a /sync is pending, so status (pin, sync-state, drift/health) stays fresh without
// mutating the cluster. The returned error is a requeue signal (non-nil = transient).
func (c *Controller) reconcile(ctx context.Context, rel *corev1alpha1.Release) (reconcileOutcome, error) {
	st := c.newReconcileState(rel)
	if out, done, err := c.stageRender(ctx, st); done {
		return out, err
	}
	if out, done, err := c.stageGates(st); done {
		return out, err
	}
	out, err := c.stageSync(ctx, st)
	// SyncFailed hooks fire (fire-and-forget) when the sync itself failed — the render
	// succeeded (so we have a cluster + renderRef) but an apply/wave/prune step failed.
	// Not on a source/cluster/render failure (no target to run on), nor on a hold, nor
	// on a preSync-hook abort (its own HookFailed stage). renderRef-keyed arming makes
	// it fire once per failed render, not every requeue.
	if isSyncingFailure(out) {
		c.runSyncFailedHooks(ctx, st.rel, st.cluster, st.attempt.RenderRef)
	}
	return out, err
}

// isSyncingFailure reports whether an outcome is a terminal failure at the Syncing
// stage (apply/wave/prune) — the trigger for SyncFailed hooks.
func isSyncingFailure(out reconcileOutcome) bool {
	f := out.attempt.Failure
	return out.attempt.Phase == corev1alpha1.ReleaseRenderAttemptPhaseFailed &&
		f != nil && f.Stage == corev1alpha1.ReleaseRenderAttemptStageSyncing
}

// runSyncFailedHooks arms the SyncFailed lifecycle hooks fire-and-forget: it records
// and creates them but never blocks or changes the (already-failed) sync outcome.
// The observer polls them and status.lifecycle reflects them like any other phase.
func (c *Controller) runSyncFailedHooks(ctx context.Context, rel *corev1alpha1.Release, cluster *corev1alpha1.Cluster, renderRef string) {
	if cluster == nil || renderRef == "" {
		return
	}
	if _, err := c.runHookPhase(ctx, rel, cluster, corev1alpha1.LifecycleHookPhaseSyncFailed, renderRef); err != nil {
		klog.V(4).InfoS("syncFailed lifecycle hook error (ignored)", "release", rel.Name, "err", err.Error())
	}
}

// stageRender fetches the source, resolves the target cluster, renders the content,
// and runs the post-render mutation pipeline, recording the resolved coordinates on
// the attempt. On any failure it returns a terminal outcome; on success it stores
// the artifact, cluster, and rendered objects on the state and lets reconcile
// proceed.
func (c *Controller) stageRender(ctx context.Context, st *reconcileState) (reconcileOutcome, bool, error) {
	rel := st.rel

	// Ensure source content is fetched. A non-nil error is transient (requeue); a
	// not-ready result with no error is a terminal config problem.
	res, err := c.coordinator.Ensure(ctx, rel.Spec.Source, rel.Namespace)
	if err != nil {
		return stop(st.fail(conditionReasonSourceNotReady, err.Error(), corev1alpha1.ReleaseRenderAttemptStageSourceResolution, err))
	}
	if !res.Ready {
		return stop(st.fail(conditionReasonUnsupported, res.Message, corev1alpha1.ReleaseRenderAttemptStageSourceResolution, nil))
	}
	st.attempt.ResolvedSource.Revision = res.Revision
	st.attempt.ResolvedSource.URL = res.URL
	st.attempt.ResolvedSource.UID = res.UID
	st.attempt.ResolvedSource.ObservedGeneration = res.ObservedGeneration
	st.art = res.Artifact

	clusterNamespace := orDefault(rel.Spec.ClusterRef.Namespace, rel.Namespace)
	cluster, err := c.clusterLister.Clusters(clusterNamespace).Get(rel.Spec.ClusterRef.Name)
	if apierrors.IsNotFound(err) {
		return stop(st.fail(conditionReasonClusterNotReady, fmt.Sprintf("target cluster %s/%s not found", clusterNamespace, rel.Spec.ClusterRef.Name), corev1alpha1.ReleaseRenderAttemptStageClusterResolution, fmt.Errorf("cluster not found")))
	}
	if err != nil {
		return stop(st.fail(conditionReasonClusterNotReady, err.Error(), corev1alpha1.ReleaseRenderAttemptStageClusterResolution, err))
	}
	if !cluster.Status.Reachable {
		return stop(st.fail(conditionReasonClusterNotReady, "target cluster is not reachable", corev1alpha1.ReleaseRenderAttemptStageClusterResolution, fmt.Errorf("cluster %s not reachable", cluster.Name)))
	}
	st.cluster = cluster
	st.attempt.ResolvedCluster = resolvedClusterFrom(cluster)

	valueLayers, err := c.resolveValueLayers(ctx, rel)
	if err != nil {
		if errors.Is(err, render.ErrUnsupported) {
			return stop(st.fail(conditionReasonUnsupported, err.Error(), corev1alpha1.ReleaseRenderAttemptStageRendering, nil))
		}
		return stop(st.fail(conditionReasonValuesNotReady, err.Error(), corev1alpha1.ReleaseRenderAttemptStageRendering, err))
	}

	objects, err := render.Render(rel.Spec.Content, st.art, valueLayers)
	if err != nil {
		if errors.Is(err, render.ErrUnsupported) {
			return stop(st.fail(conditionReasonUnsupported, err.Error(), corev1alpha1.ReleaseRenderAttemptStageRendering, nil))
		}
		return stop(st.fail(conditionReasonDeployFailed, err.Error(), corev1alpha1.ReleaseRenderAttemptStageRendering, err))
	}

	// Post-render mutation pipeline. Runs here in the render path so the applied
	// objects (and, via the shared path, the observer's diff/preview/health) all
	// reflect the mutated result.
	objects, resolvedMutations, err := render.ApplyMutations(ctx, render.MutationInput{
		Objects:     objects,
		Mutations:   rel.Mutations,
		Release:     rel,
		Cluster:     cluster,
		Resolver:    c.resolver,
		Coordinator: c.coordinator,
	})
	if err != nil {
		if errors.Is(err, render.ErrUnsupported) {
			return stop(st.fail(conditionReasonUnsupported, err.Error(), corev1alpha1.ReleaseRenderAttemptStageRendering, nil))
		}
		return stop(st.fail(conditionReasonDeployFailed, err.Error(), corev1alpha1.ReleaseRenderAttemptStageRendering, err))
	}
	st.objects = objects
	st.attempt.ResolvedMutations = resolvedMutations
	st.attempt.RenderRef = renderRef(objects)

	// A pin overrides the freshly-rendered source render. Freezing to the current
	// render (refs equal) falls through unchanged. Pinning to a *different* render
	// is a rollback: load that render from the store and apply it instead of
	// re-rendering source (which may not reproduce it). The controller is the
	// validation point for a rollback target — an absent store or missing blob is
	// reported (held), never silently ignored.
	if out, done := c.resolvePinnedRender(ctx, st); done {
		return stop(out, nil)
	}
	return cont()
}

// resolvePinnedRender applies a rollback pin: when the Release is pinned to a
// render other than the freshly-rendered one, it replaces the render to apply with
// the pinned render loaded from the store. It returns (outcome, true) to halt the
// pipeline when the pin cannot be honored (no store, or the render is missing).
func (c *Controller) resolvePinnedRender(ctx context.Context, st *reconcileState) (reconcileOutcome, bool) {
	p := st.rel.Pin
	if p == nil || p.RenderRef == "" || p.RenderRef == st.attempt.RenderRef {
		return reconcileOutcome{}, false // no pin, or freezing to the current render
	}
	if c.renderStore == nil {
		out, _ := st.fail(conditionReasonPinned,
			fmt.Sprintf("cannot roll back to render %s: no render store configured", p.RenderRef),
			corev1alpha1.ReleaseRenderAttemptStageRendering, nil)
		return out, true
	}
	data, err := c.renderStore.GetRender(ctx, st.rel.Namespace, st.rel.Name, p.RenderRef)
	if errors.Is(err, blobstore.ErrNotFound) {
		out, _ := st.fail(conditionReasonPinned,
			fmt.Sprintf("pinned render %s is not available in the store", p.RenderRef),
			corev1alpha1.ReleaseRenderAttemptStageRendering, nil)
		return out, true
	}
	if err != nil {
		out, e := st.fail(conditionReasonDeployFailed,
			fmt.Sprintf("loading pinned render %s: %v", p.RenderRef, err),
			corev1alpha1.ReleaseRenderAttemptStageRendering, err)
		_ = e
		return out, true
	}
	pinned, err := render.ParseManifest(data)
	if err != nil {
		out, _ := st.fail(conditionReasonDeployFailed,
			fmt.Sprintf("parsing pinned render %s: %v", p.RenderRef, err),
			corev1alpha1.ReleaseRenderAttemptStageRendering, err)
		return out, true
	}
	// Apply the pinned render, not the source render.
	st.objects = pinned
	st.attempt.RenderRef = p.RenderRef
	return reconcileOutcome{}, false
}

// stageGates evaluates the pre-apply holds — freeze pin, manual-mode sync gate, and
// the Axis-B cluster-wave gate — that withhold the apply without failing. It halts
// the pipeline when any hold (or a bounded-wait failure) applies.
func (c *Controller) stageGates(st *reconcileState) (reconcileOutcome, bool, error) {
	rel := st.rel

	// The pin has already been resolved in stageRender: st.attempt.RenderRef is the
	// render to apply (the current source render, or a pinned/rolled-back render
	// loaded from the store), so no freeze-withhold is needed here — a valid pin
	// applies its render idempotently, holding the cluster at it.

	// Manual-mode sync gate. In manual mode (syncPolicy.automatic=false) apply only
	// when an explicit /sync is pending. Otherwise observe without applying: report
	// in-sync when the current render already matches the last applied one, else
	// WaitingForSync (out of sync, or never synced). A spec change or resync alone
	// never auto-applies in manual mode.
	if st.manual && !st.pending {
		if ls := rel.Status.LastSuccessful; ls != nil && ls.RenderRef == st.attempt.RenderRef {
			return stop(reconcileOutcome{
				ready:   true,
				reason:  conditionReasonApplied,
				message: "in sync; manual sync mode (apply withheld until /sync)",
				attempt: st.attempt,
			}, nil)
		}
		return stop(reconcileOutcome{
			reason:  conditionReasonWaitingForSync,
			message: "manual sync mode: changes await a /sync request",
			attempt: st.attempt,
		}, nil)
	}

	// Axis B — Release-within-cluster ordering. Before applying anything, hold until
	// the predecessor Releases on this cluster (lower spec.syncPolicy.waveStrategy.
	// wave) clear this Release's bar (applied / healthy). A bounded
	// (waitForHealthyWithTimeout) wait fails once its deadline elapses. This gates
	// the whole Release; Axis A below then orders resources within it.
	if hold, err := c.clusterWaveGate(rel, st.attempt, st.now); err != nil {
		return stop(reconcileOutcome{attempt: st.attempt}, err)
	} else if hold != nil {
		return stop(*hold, nil)
	}
	return cont()
}

// stageSync applies the rendered objects to the target cluster in ascending
// sync-wave order, prunes orphans on the success path, and records the durable
// inventory. It is the terminal stage: it always produces the reconcile outcome.
func (c *Controller) stageSync(ctx context.Context, st *reconcileState) (reconcileOutcome, error) {
	rel, now, cluster, objects := st.rel, st.now, st.cluster, st.objects
	art := st.art
	manual, pending, requestedAt := st.manual, st.pending, st.requestedAt
	fail := st.fail
	// attempt is threaded by value here and updated on the success path; the fail
	// helper stamps st.attempt, which equals this copy on every non-success path.
	attempt := st.attempt

	// persist-then-apply: durably store this render (content-addressed by its
	// renderRef) before applying anything. A crash mid-apply then leaves the render
	// recoverable — it is referenced by status.lastAttempted, so it is not orphaned —
	// and rollback/diff can read it later. If persistence fails we do NOT apply.
	if err := c.persistRender(ctx, rel.Namespace, rel.Name, attempt.RenderRef, objects); err != nil {
		return fail(conditionReasonDeployFailed, err.Error(), corev1alpha1.ReleaseRenderAttemptStageSyncing, err)
	}

	// PreSync hooks run before anything is applied. Hold (nothing applied) until the
	// phase completes; a hook with onFailure=Fail aborts the sync. Advancing falls
	// through to the wave loop.
	if res, err := c.runHookPhase(ctx, rel, cluster, corev1alpha1.LifecycleHookPhasePreSync, attempt.RenderRef); err != nil {
		return fail(conditionReasonDeployFailed, err.Error(), corev1alpha1.ReleaseRenderAttemptStageSyncing, err)
	} else if res.state == hookPhaseHold {
		return reconcileOutcome{
			reason:  conditionReasonWaitingForHook,
			message: "waiting for preSync lifecycle hook(s) to complete",
			attempt: attempt,
		}, nil
	} else if res.state == hookPhaseFailed {
		return st.failHook(corev1alpha1.LifecycleHookPhasePreSync, res)
	}

	// Apply in ascending sync-wave order. Each wave applies fully before the next
	// begins. A wave with a health bar (waitForHealthy*) holds until the
	// IMMEDIATELY-PRECEDING wave is healthy — a wave depends on the one directly
	// before it, not transitively on all lower waves. Health is assessed by the
	// observer, which records per-resource health on the ReleaseInventory; the
	// controller rolls that up over the preceding wave's refs rather than reading
	// its own status, and requeues while that wave is not yet healthy.
	// waitForHealthyWithTimeout fails the wave once its timeout elapses. All waves
	// are (idempotently) re-applied each reconcile, so drift is still corrected as
	// the rollout progresses.
	waveGroups := waves.Group(objects)
	// Prior status.waves carries a wave's original StartedAt/CompletedAt across
	// reconciles (the controller's own progress timestamps).
	prior := map[int64]corev1alpha1.ResourceWaveStatus{}
	for i := range rel.Status.Waves {
		prior[rel.Status.Waves[i].Number] = rel.Status.Waves[i]
	}
	// Per-resource health the observer persisted on the ReleaseInventory, keyed by
	// identity, used to roll up per-wave health for the gate. A release with no
	// health assessment has no bar to clear, so every wave counts as healthy.
	invRefs, _, err := c.inventory.Get(ctx, rel.Namespace, rel.Name)
	if err != nil {
		return fail(conditionReasonDeployFailed, err.Error(), corev1alpha1.ReleaseRenderAttemptStageSyncing, err)
	}
	stateByID := make(map[string]*corev1alpha1.ResourceState, len(invRefs))
	for _, r := range invRefs {
		stateByID[r.ID()] = r.State
	}
	noHealthBar := rel.Spec.HealthAssessment == nil

	var applied []inventory.Ref
	// prevWaveApplied holds the immediately-preceding wave's applied refs — the only
	// wave a health-gated wave waits on. A wave depends on the wave directly before
	// it, not transitively on every lower wave: e.g. wave 2 (waitForHealthy,
	// depending on wave 1's Secret) must proceed once wave 1 is healthy even if
	// wave 0 is still degraded.
	var prevWaveApplied []inventory.Ref
	waveStatuses := make([]corev1alpha1.ResourceWaveStatus, 0, len(waveGroups))
	for i := range waveGroups {
		w := waveGroups[i]

		// Gate: a wave whose resources declare waitForHealthy* waits for the
		// immediately-preceding wave to be healthy before it applies (the strategy is
		// the bar that predecessor must clear before this wave enters). prevWaveApplied
		// holds exactly that wave's refs; health is the observer-persisted rollup over
		// them — no dependency on this controller's own status.
		if i > 0 && waveGroups[i].NeedsHealth && !precedingWaveHealthy(prevWaveApplied, stateByID, noHealthBar) {
			// Anchor a bounded wait at the immediately-preceding wave's first apply.
			prevStarted := prior[int64(waveGroups[i-1].Number)].StartedAt
			if waveGroups[i].Bounded && prevStarted != nil && now.Sub(prevStarted.Time) > waveGroups[i].Timeout {
				out, _ := fail(conditionReasonWaveFailed, fmt.Sprintf("sync wave %d: preceding wave %d not healthy within %s", w.Number, waveGroups[i-1].Number, waveGroups[i].Timeout), corev1alpha1.ReleaseRenderAttemptStageSyncing, nil)
				out.waves = waveStatuses
				return out, nil
			}
			// Not healthy yet: hold. Partial rollout — do NOT prune or advance
			// lastSuccessful. Requeues on the poll interval; the observer records the
			// preceding wave's health in the meantime.
			return reconcileOutcome{
				reason:  conditionReasonWaitingForWaveHealth,
				message: fmt.Sprintf("waiting for wave %d to become healthy before applying wave %d", waveGroups[i-1].Number, w.Number),
				attempt: attempt,
				waves:   waveStatuses,
			}, nil
		}

		waveApplied, err := c.deployer.Deploy(ctx, cluster, w.Objects)
		if err != nil {
			if errors.Is(err, render.ErrUnsupported) || errors.Is(err, clusterconn.ErrUnsupported) {
				return fail(conditionReasonUnsupported, err.Error(), corev1alpha1.ReleaseRenderAttemptStageSyncing, nil)
			}
			return fail(conditionReasonDeployFailed, err.Error(), corev1alpha1.ReleaseRenderAttemptStageSyncing, err)
		}
		// Tag each ref with the wave it applied in, so the persisted inventory
		// records per-resource wave membership (for observability) and the gate can
		// attribute the preceding wave's refs. Also stamp the render it was applied
		// under, so a render change is visible on the inventory (and the observer can
		// mark which render its health assessment corresponds to).
		for k := range waveApplied {
			waveApplied[k].Wave = w.Number
			waveApplied[k].RenderRef = attempt.RenderRef
		}
		applied = append(applied, waveApplied...)
		prevWaveApplied = waveApplied // this wave becomes the next wave's gate target

		// Persist the applied-so-far set right after each wave, not only on full
		// success: the ReleaseInventory then exists as soon as the first wave lands
		// (so the observer can start assessing health, and a gated rollout has a
		// prune record even while it holds). Unioned with the prior inventory so a
		// re-rollout doesn't drop the previous generation's entries that the
		// success-path prune diff still needs; the final Put below writes the exact
		// set. A wave-loop that returns early (gate hold/fail) leaves this record in
		// place rather than the stale prior one.
		if err := c.inventory.Put(ctx, rel.Namespace, rel.Name, unionByID(invRefs, applied)); err != nil {
			return fail(conditionReasonDeployFailed, err.Error(), corev1alpha1.ReleaseRenderAttemptStageSyncing, err)
		}

		// Preserve the wave's original StartedAt/CompletedAt across reconciles: the
		// wave applies once and is re-applied idempotently each reconcile, so its
		// timestamps must reflect that first apply, not the current pass. (StartedAt
		// also anchors a successor wave's bounded health wait.)
		startedAt, completedAt := &now, &now
		if p, ok := prior[int64(w.Number)]; ok {
			if p.StartedAt != nil {
				startedAt = p.StartedAt
			}
			if p.CompletedAt != nil {
				completedAt = p.CompletedAt
			}
		}
		waveStatuses = append(waveStatuses, corev1alpha1.ResourceWaveStatus{
			Number:      int64(w.Number),
			Applied:     new(true),
			Healthy:     waveHealth(waveApplied, stateByID, noHealthBar),
			Resources:   len(w.Objects),
			State:       corev1alpha1.ResourceWaveStateCompleted,
			StartedAt:   startedAt,
			CompletedAt: completedAt,
		})
	}

	// Steady-state prune: apply succeeded, so anything this Release managed last
	// time but no longer renders is an orphan. Only reached on the success path,
	// so we never prune from a failed/partial render.
	if err := c.prune(ctx, cluster, rel.Namespace, rel.Name, applied, rel.Spec.SyncPolicy.Prune); err != nil {
		return fail(conditionReasonPruneFailed, err.Error(), corev1alpha1.ReleaseRenderAttemptStageSyncing, err)
	}
	// Persist the exact managed set durably (survives a controller restart, so the
	// delete path can still prune after a bounce). The wave loop writes incremental
	// (unioned) snapshots as it applies; this final write, reached only on the full
	// success path after prune, replaces them with the precise current set — so any
	// previous-generation entries that were carried through the rollout are dropped.
	if err := c.inventory.Put(ctx, rel.Namespace, rel.Name, applied); err != nil {
		return fail(conditionReasonDeployFailed, err.Error(), corev1alpha1.ReleaseRenderAttemptStageSyncing, err)
	}

	// PostSync hooks run after a successful apply + prune. The apply already
	// happened, so the sync outcome stands regardless: a still-running phase is
	// re-driven by the observer's completion poke + poll interval (the hooks are
	// armed here), and a hook with onFailure=Fail stops further postSync groups but
	// does not un-apply — its failure is surfaced via status.lifecycle. Only a
	// transient arming/cleanup error requeues as a failure.
	if res, err := c.runHookPhase(ctx, rel, cluster, corev1alpha1.LifecycleHookPhasePostSync, attempt.RenderRef); err != nil {
		return fail(conditionReasonDeployFailed, err.Error(), corev1alpha1.ReleaseRenderAttemptStageSyncing, err)
	} else if res.state == hookPhaseFailed {
		klog.V(2).InfoS("postSync lifecycle hook failed", "release", rel.Name, "hook", res.failedHook, "message", res.message)
	}

	attempt.Phase = corev1alpha1.ReleaseRenderAttemptPhaseSucceeded
	attempt.SyncedAt = &now
	attempt.SyncedResources = int64(len(applied))

	c.recordHistory(ctx, rel, now, attempt)
	c.runGC(ctx, rel, attempt.RenderRef)

	outcome := reconcileOutcome{
		ready:   true,
		applied: true,
		reason:  conditionReasonApplied,
		message: fmt.Sprintf("applied %d object(s) from revision %s", len(applied), art.Revision),
		attempt: attempt,
		waves:   waveStatuses,
	}
	// In manual mode, record which /sync request this apply satisfied so it isn't
	// re-applied on the next reconcile; a failed apply above returns early, leaving
	// the request pending to retry.
	if manual && pending {
		outcome.handledSyncAt = requestedAt
	}
	return outcome, nil
}

// resolvedSourceFrom snapshots the Release's declared source coordinate for status.
func resolvedSourceFrom(rel *corev1alpha1.Release) corev1alpha1.ResolvedSource {
	s := rel.Spec.Source
	rs := corev1alpha1.ResolvedSource{
		Kind:        sourceKind(s.Type),
		ContentType: rel.Spec.Content.Type,
		Path:        contentPath(rel.Spec.Content),
	}
	switch s.Type {
	case corev1alpha1.ReleaseSourceTypeGitRepository:
		if s.GitRepository != nil {
			rs.Name, rs.Namespace = s.GitRepository.Name, s.GitRepository.Namespace
			rs.Ref = gitRefString(s.GitRepository.Ref)
		}
	case corev1alpha1.ReleaseSourceTypeHelmRegistry:
		if s.HelmRegistry != nil {
			rs.Name, rs.Namespace = s.HelmRegistry.Name, s.HelmRegistry.Namespace
			if s.HelmRegistry.Chart != nil {
				rs.Ref = s.HelmRegistry.Chart.Version
			}
		}
	case corev1alpha1.ReleaseSourceTypeOCIRegistry:
		if s.OCIRegistry != nil {
			rs.Name, rs.Namespace = s.OCIRegistry.Name, s.OCIRegistry.Namespace
			if s.OCIRegistry.Artifact != nil {
				rs.Ref = s.OCIRegistry.Artifact.Version
			}
		}
	}
	return rs
}

// resolvedClusterFrom snapshots the target Cluster's identity for status.
func resolvedClusterFrom(cluster *corev1alpha1.Cluster) corev1alpha1.ResolvedCluster {
	var serverURL string
	switch cluster.Spec.ConnectionMode {
	case corev1alpha1.ClusterConnectionModeInCluster:
		serverURL = DefaultLocalCluster
	case corev1alpha1.ClusterConnectionModeAgentless:
		serverURL = cluster.Spec.Agentless.Server
	default:
		serverURL = "notApplicable"
	}
	return corev1alpha1.ResolvedCluster{
		Name:               cluster.Name,
		Namespace:          cluster.Namespace,
		UID:                string(cluster.UID),
		ObservedGeneration: cluster.Status.ObservedGeneration,
		Server:             serverURL,
	}
}

func sourceKind(t corev1alpha1.ReleaseSourceType) string {
	switch t {
	case corev1alpha1.ReleaseSourceTypeGitRepository:
		return sourcev1alpha1.GitRepositoryKind
	case corev1alpha1.ReleaseSourceTypeHelmRegistry:
		return sourcev1alpha1.HelmRegistryKind
	case corev1alpha1.ReleaseSourceTypeOCIRegistry:
		return sourcev1alpha1.OCIRegistryKind
	default:
		return string(t)
	}
}

func contentPath(content corev1alpha1.ReleaseContentSpec) string {
	switch content.Type {
	case corev1alpha1.ReleaseContentTypeHelm:
		if content.Helm != nil {
			return content.Helm.Path
		}
	case corev1alpha1.ReleaseContentTypeKustomize:
		if content.Kustomize != nil {
			return content.Kustomize.Path
		}
	case corev1alpha1.ReleaseContentTypeJsonnet:
		if content.Jsonnet != nil {
			return content.Jsonnet.Path
		}
	}
	return "" // manifests carries multiple paths; not a single value
}

func gitRefString(ref *corev1alpha1.ReleaseGitRepositoryRefSpec) string {
	switch {
	case ref == nil:
		return ""
	case ref.Branch != nil:
		return "branch:" + *ref.Branch
	case ref.Tag != nil:
		return "tag:" + *ref.Tag
	case ref.Commit != nil:
		return "commit:" + *ref.Commit
	case ref.Semver != nil:
		return "semver:" + *ref.Semver
	default:
		return ""
	}
}

// pinStatus projects the Release's pin intent (top-level .pin, set by the /pin
// subresource) into the observed .status.pin. Intent is derived by comparing the
// pinned render against the last successful render: a match is a freeze, anything
// else a rollback (rollback is storage-gated and not yet producible). RenderInputs
// snapshots the inputs of the pinned render, taken from status.lastSuccessful —
// while a pin holds the controller never applies, so lastSuccessful is the pinned
// render.
func pinStatus(rel *corev1alpha1.Release) *corev1alpha1.ReleasePinStatus {
	if rel.Pin == nil {
		return nil // unpinned
	}
	ls := rel.Status.LastSuccessful

	intent := corev1alpha1.ReleasePinIntentFreeze
	if ls == nil || rel.Pin.RenderRef != ls.RenderRef {
		intent = corev1alpha1.ReleasePinIntentRollback
	}
	status := new(corev1alpha1.ReleasePinStatus{Intent: intent})
	if ls != nil {
		rs := ls.ResolvedSource
		status.RenderInputs = &corev1alpha1.ReleasePinRenderInputsStatus{
			SourceKind:      rs.Kind,
			SourceName:      rs.Name,
			SourceNamespace: rs.Namespace,
			SourceRevision:  rs.Revision,
			ContentType:     rs.ContentType,
			ContentPath:     rs.Path,
			MutationsHash:   mutationsHash(ls.ResolvedMutations),
		}
	}
	return status
}

// mutationsHash returns a stable content hash of the resolved mutations, or ""
// when there are none.
func mutationsHash(mutations []corev1alpha1.ResolvedMutation) string {
	if len(mutations) == 0 {
		return ""
	}
	b, err := json.Marshal(mutations)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// renderRef is a content hash of the rendered objects, stable across identical
// renders (objects arrive in deterministic order from render).
func renderRef(objects []*unstructured.Unstructured) string {
	h := sha256.New()
	for _, o := range objects {
		b, err := o.MarshalJSON()
		if err != nil {
			continue
		}
		_, _ = h.Write(b)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// renderManifest serializes the rendered objects into the bytes stored as the
// render blob (see render.MarshalManifest for the format).
func renderManifest(objects []*unstructured.Unstructured) ([]byte, error) {
	return render.MarshalManifest(objects)
}

// persistRender durably stores a render before it is applied (persist-then-apply).
// A nil render store disables persistence and this is a no-op.
func (c *Controller) persistRender(ctx context.Context, namespace, name, renderRef string, objects []*unstructured.Unstructured) error {
	if c.renderStore == nil {
		return nil
	}
	manifest, err := renderManifest(objects)
	if err != nil {
		return fmt.Errorf("serializing render: %w", err)
	}
	if err := c.renderStore.PutRender(ctx, namespace, name, renderRef, manifest); err != nil {
		return fmt.Errorf("persisting render: %w", err)
	}
	return nil
}

// recordHistory appends a deploy-log entry when the render differs from the last
// logged deploy. It compares against the last *history* entry, not
// status.lastSuccessful: the latter lags in the informer cache, so several quick
// reconciles of one deploy (e.g. lifecycle-hook phases) would each still see the
// pre-write status and append duplicate entries. The store is read-after-write
// consistent, so the last history entry always reflects a preceding append.
//
// Best-effort: the render blob is already persisted (the rollback target is safe),
// so a failed append is logged rather than failing an otherwise-successful deploy
// — it would only leave a gap in the log. A nil render store is a no-op.
func (c *Controller) recordHistory(ctx context.Context, rel *corev1alpha1.Release, now metav1.Time, attempt corev1alpha1.ReleaseAttemptedRenderStatus) {
	if c.renderStore == nil {
		return
	}
	last, err := c.renderStore.LastHistoryRef(ctx, rel.Namespace, rel.Name)
	if err != nil {
		klog.ErrorS(err, "reading last render history", "release", rel.Name)
		return
	}
	if attempt.RenderRef == last {
		return
	}
	if err := c.renderStore.AppendHistory(ctx, rel.Namespace, rel.Name, renderstore.HistoryEntry{
		RenderRef: attempt.RenderRef,
		SyncedAt:  now,
		Summary:   renderSummary(attempt.ResolvedSource),
	}); err != nil {
		klog.ErrorS(err, "appending render history", "release", rel.Name, "renderRef", attempt.RenderRef)
	}
}

// defaultKeepHistory is the retention applied when spec.retentionPolicy.keepHistory
// is unset (nil): keep the newest N deploys. An explicit 0 means unlimited.
const defaultKeepHistory = 10

// effectiveKeepHistory resolves the retention policy: nil (unset) ⇒ the default;
// an explicit value is used as-is (0 ⇒ unlimited, N ⇒ keep the newest N). The
// resolved value is what status.resolvedRetentionPolicy.keepHistory reports and
// what GC enforces (GC treats <= 0 as unlimited).
func effectiveKeepHistory(rel *corev1alpha1.Release) int {
	if kh := rel.Spec.RetentionPolicy.KeepHistory; kh != nil {
		return *kh
	}
	return defaultKeepHistory
}

// resolvedRetentionStatus reports the effective retention policy and the actual
// number of history entries currently in the store (0 when no store is configured).
func (c *Controller) resolvedRetentionStatus(ctx context.Context, rel *corev1alpha1.Release) corev1alpha1.ResolvedRetentionPolicyStatus {
	out := corev1alpha1.ResolvedRetentionPolicyStatus{KeepHistory: effectiveKeepHistory(rel)}
	if c.renderStore != nil {
		if n, err := c.renderStore.HistoryCount(ctx, rel.Namespace, rel.Name); err != nil {
			klog.ErrorS(err, "counting render history", "release", rel.Name)
		} else {
			out.HistoryCount = n
		}
	}
	return out
}

// runGC applies the Release's retention policy to its render store: prune the
// history log to keepHistory entries and reclaim unreferenced blobs. Best-effort
// (logged, never fails a reconcile). protectedRefs shields renders not in the
// pruned log that must survive: the just-applied render, the prior current/attempted
// pointers (covering the persist→commit window), and any active rollback pin.
func (c *Controller) runGC(ctx context.Context, rel *corev1alpha1.Release, currentRef string) {
	if c.renderStore == nil {
		return
	}
	keep := effectiveKeepHistory(rel)
	if keep <= 0 {
		return // unlimited (explicit keepHistory=0): retain everything
	}
	protected := []string{currentRef}
	if ls := rel.Status.LastSuccessful; ls != nil {
		protected = append(protected, ls.RenderRef)
	}
	if la := rel.Status.LastAttempted; la != nil {
		protected = append(protected, la.RenderRef)
	}
	if p := rel.Pin; p != nil {
		protected = append(protected, p.RenderRef)
	}
	if err := c.renderStore.GC(ctx, rel.Namespace, rel.Name, keep, protected...); err != nil {
		klog.ErrorS(err, "render store GC", "release", rel.Name)
	}
}

// renderSummary is a short human description of a render's resolved source for the
// deploy-history log, e.g. "https://…/podinfo@1.2.3" or "gitRepository/podinfo@<sha>".
func renderSummary(src corev1alpha1.ResolvedSource) string {
	loc := src.URL
	if loc == "" {
		loc = strings.Trim(src.Kind+"/"+src.Name, "/")
	}
	if src.Revision != "" {
		return loc + "@" + src.Revision
	}
	return loc
}

// prune deletes resources dropped from the render set since the last successful
// apply, honoring the prune policy. On prune failure the inventory is left
// unchanged so the orphans are retried on the next reconcile. When pruning is
// disabled the orphans are simply untracked (the caller advances the inventory).
func (c *Controller) prune(ctx context.Context, cluster *corev1alpha1.Cluster, namespace, name string, applied []inventory.Ref, policy corev1alpha1.PruneSpec) error {
	if !policy.Orphaned {
		return nil
	}
	old, ok, err := c.inventory.Get(ctx, namespace, name)
	if err != nil {
		return err
	}
	if !ok {
		return nil // nothing tracked yet (first apply)
	}
	orphans := protectedFilter(inventory.Diff(old, applied), policy)
	return c.deployer.Prune(ctx, cluster, orphans)
}

// protectedFilter drops high-blast-radius kinds (CRDs, Namespaces) from the
// orphan set unless the policy explicitly opts into pruning them.
func protectedFilter(orphans []inventory.Ref, policy corev1alpha1.PruneSpec) []inventory.Ref {
	kept := orphans[:0]
	for _, r := range orphans {
		if isCRD(r) && !policy.IncludeCRDs {
			continue
		}
		if isNamespace(r) && !policy.IncludeNamespaces {
			continue
		}
		kept = append(kept, r)
	}
	return kept
}

func isCRD(r inventory.Ref) bool {
	return r.Kind == CustomResourceDefinitionKind && r.Group == CustomResourceDefinitionGroup
}

func isNamespace(r inventory.Ref) bool {
	return r.Kind == "Namespace" && r.Group == ""
}

// unionByID returns applied plus any base ref whose identity applied does not
// already cover. applied wins on collision (its wave/uid/version are current).
// Used to carry the previous inventory generation through an in-progress rollout,
// so a partial (gated) rollout's prune record never drops resources the eventual
// prune still needs to reconcile.
func unionByID(base, applied []inventory.Ref) []inventory.Ref {
	have := make(map[string]struct{}, len(applied))
	for _, r := range applied {
		have[r.ID()] = struct{}{}
	}
	out := append([]inventory.Ref(nil), applied...)
	for _, r := range base {
		if _, ok := have[r.ID()]; !ok {
			out = append(out, r)
		}
	}
	return out
}

// clusterWaveGate implements Axis B: it holds this Release until the predecessor
// Releases on its target cluster (those with a lower spec.syncPolicy.waveStrategy.
// wave) clear its bar. Returns (nil, nil) to proceed; a non-nil outcome to hold
// (WaitingForClusterWave) or fail (ClusterWaveFailed, for a bounded wait that
// timed out); a non-nil error for a transient lookup failure (requeue). The bar
// is this Release's strategy — waitForApplied checks each predecessor's Applied
// condition, waitForHealthy* checks Healthy. The bounded deadline is measured
// from the predecessors' apply time (their Applied condition's transition time),
// so no extra "waiting since" state is needed.
func (c *Controller) clusterWaveGate(rel *corev1alpha1.Release, attempt corev1alpha1.ReleaseAttemptedRenderStatus, now metav1.Time) (*reconcileOutcome, error) {
	if c.releasesForCluster == nil {
		return nil, nil // cluster-wave ordering not wired (e.g. unit tests)
	}
	clusterNamespace := orDefault(rel.Spec.ClusterRef.Namespace, rel.Namespace)
	siblings, err := c.releasesForCluster(clusterNamespace, rel.Spec.ClusterRef.Name)
	if err != nil {
		return nil, fmt.Errorf("listing releases for cluster %s/%s: %w", clusterNamespace, rel.Spec.ClusterRef.Name, err)
	}
	preds := waves.ClusterPredecessors(rel, siblings)
	if len(preds) == 0 {
		return nil, nil // nothing ahead of us on this cluster
	}

	healthBar := waves.NeedsHealthBar(waves.ClusterStrategyOf(rel))
	var unmet []string
	var anchor metav1.Time
	for _, p := range preds {
		if a := conditionTransitionTime(p, conditionApplied); a.After(anchor.Time) {
			anchor = a
		}
		if !predecessorClears(p, healthBar) {
			unmet = append(unmet, p.Namespace+"/"+p.Name)
		}
	}
	if len(unmet) == 0 {
		return nil, nil // all predecessors cleared → proceed to apply
	}
	slices.Sort(unmet) // deterministic message

	// Bounded wait (waitForHealthyWithTimeout + explicit timeout): give up once the
	// deadline, measured from the predecessors' apply time, elapses.
	if timeout, bounded := waves.ClusterWaveBound(rel); bounded && !anchor.IsZero() && now.Sub(anchor.Time) > timeout {
		msg := fmt.Sprintf("cluster wave %d: predecessor release(s) %s not healthy within %s", waves.ClusterWaveOf(rel), strings.Join(unmet, ", "), timeout)
		attempt.Phase = corev1alpha1.ReleaseRenderAttemptPhaseFailed
		attempt.Failure = &corev1alpha1.ReleaseAttemptedRenderFailureStatus{Stage: corev1alpha1.ReleaseRenderAttemptStageSyncing, Message: msg}
		return &reconcileOutcome{reason: conditionReasonClusterWaveFailed, message: msg, attempt: attempt}, nil
	}

	// Hold: predecessors not yet ready. No apply, no prune, no lastSuccessful
	// advance. Requeues on the poll interval; the fan-out handler re-enqueues this
	// Release promptly when a predecessor's Applied/Healthy flips.
	return &reconcileOutcome{
		reason:  conditionReasonWaitingForClusterWave,
		message: fmt.Sprintf("waiting for predecessor release(s) on cluster: %s", strings.Join(unmet, ", ")),
		attempt: attempt,
	}, nil
}

// predecessorClears reports whether a predecessor Release meets this Release's
// cluster-wave bar: Healthy when healthBar is set (waitForHealthy*), else Applied.
func predecessorClears(pred *corev1alpha1.Release, healthBar bool) bool {
	if healthBar {
		return conditionIsTrue(pred, conditionHealthy)
	}
	return conditionIsTrue(pred, conditionApplied)
}

func conditionIsTrue(rel *corev1alpha1.Release, condType string) bool {
	c := apimeta.FindStatusCondition(rel.Status.Conditions, condType)
	return c != nil && c.Status == metav1.ConditionTrue
}

// ClusterWaveSignalChanged reports whether a Release's Applied or Healthy
// condition status changed between two revisions — the signals an Axis B
// successor gates on. The controller-manager uses it to fan out: when a
// predecessor's signal flips, re-enqueue its higher-wave successors on the same
// cluster so they advance promptly instead of waiting for the poll interval. It
// is only a wake; correctness lives in clusterWaveGate, which each woken
// successor re-evaluates.
func ClusterWaveSignalChanged(oldRel, newRel *corev1alpha1.Release) bool {
	return conditionStatusOf(oldRel, conditionApplied) != conditionStatusOf(newRel, conditionApplied) ||
		conditionStatusOf(oldRel, conditionHealthy) != conditionStatusOf(newRel, conditionHealthy)
}

func conditionStatusOf(rel *corev1alpha1.Release, condType string) metav1.ConditionStatus {
	if c := apimeta.FindStatusCondition(rel.Status.Conditions, condType); c != nil {
		return c.Status
	}
	return ""
}

func conditionTransitionTime(rel *corev1alpha1.Release, condType string) metav1.Time {
	if c := apimeta.FindStatusCondition(rel.Status.Conditions, condType); c != nil {
		return c.LastTransitionTime
	}
	return metav1.Time{}
}

// precedingWaveHealthy reports whether every ref of the immediately-preceding
// wave is Healthy per the observer-written ReleaseInventory (stateByID). A missing
// or non-Healthy entry means the preceding wave is not yet healthy, so a gated
// wave holds. Only the immediately-preceding wave is considered — a wave depends
// on the one directly before it, not transitively on all lower waves. noHealthBar
// short-circuits to true: a release with no health assessment has no bar to clear.
func precedingWaveHealthy(precedingRefs []inventory.Ref, stateByID map[string]*corev1alpha1.ResourceState, noHealthBar bool) bool {
	if noHealthBar {
		return true
	}
	for _, r := range precedingRefs {
		s := stateByID[r.ID()]
		if s == nil || *s != corev1alpha1.ResourceStateHealthy {
			return false
		}
	}
	return true
}

// waveHealth rolls up a single wave's health from the persisted per-resource
// health, for status.waves[].healthy: true when every ref is Healthy, false when
// any is assessed but unhealthy, and nil (unknown) while any ref is not yet
// assessed. A release with no health assessment (noHealthBar) is always healthy.
func waveHealth(refs []inventory.Ref, stateByID map[string]*corev1alpha1.ResourceState, noHealthBar bool) *bool {
	if noHealthBar {
		return new(true)
	}
	healthy := true
	for _, r := range refs {
		s := stateByID[r.ID()]
		if s == nil {
			return nil // not yet assessed
		}
		if *s != corev1alpha1.ResourceStateHealthy {
			healthy = false
		}
	}
	return &healthy
}

// resolveValueLayers resolves the Helm values sources to ordered raw-YAML layers.
// Only the helm content type carries values; everything else yields no layers.
func (c *Controller) resolveValueLayers(ctx context.Context, rel *corev1alpha1.Release) ([][]byte, error) {
	if rel.Spec.Content.Type != corev1alpha1.ReleaseContentTypeHelm || rel.Spec.Content.Helm == nil {
		return nil, nil
	}
	return render.ResolveValues(ctx, rel.Spec.Content.Helm.Values, rel.Namespace, c.resolver, c.coordinator)
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
