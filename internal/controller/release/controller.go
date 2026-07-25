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
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	"github.com/kumodo-io/convoy/internal/auth"
	"github.com/kumodo-io/convoy/internal/clusterconn"
	"github.com/kumodo-io/convoy/internal/controller/base"
	"github.com/kumodo-io/convoy/internal/fetch"
	"github.com/kumodo-io/convoy/internal/inventory"
	"github.com/kumodo-io/convoy/internal/render"
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
	deployer      Deployer
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
// via resolver, tracks applied resources in inventory (for pruning), and
// resolves target clusters from clusterLister.
func NewController(
	client clientset.Interface,
	informer coreinformers.ReleaseInformer,
	clusterLister corelisters.ClusterLister,
	coordinator fetch.Coordinator,
	resolver auth.SecretResolver,
	inv inventory.Store,
	deployer Deployer,
) *Controller {
	c := &Controller{
		client:        client,
		releaseLister: informer.Lister(),
		clusterLister: clusterLister,
		coordinator:   coordinator,
		resolver:      resolver,
		inventory:     inv,
		deployer:      deployer,
		failures:      newFailureCounts(),
		now:           metav1.Now,
	}
	c.Controller = base.NewController("release", c.syncHandler,
		base.WithReconcileRequestAnnotation(corev1alpha1.SyncRequestedAtAnnotation))
	utilruntime.Must(c.WatchInformer(informer.Informer()))
	return c
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

	// finalize Release on deletion according to deletion policy
	if !rel.DeletionTimestamp.IsZero() {
		return c.finalize(ctx, rel)
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

	for i := range s.Conditions {
		condAC := &metav1ac.ConditionApplyConfiguration{}
		if err := jsonRoundTrip(s.Conditions[i], condAC); err != nil {
			return nil, err
		}
		statusAC.WithConditions(condAC)
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
	if s.ReconciledAt != nil {
		statusAC.WithReconciledAt(*s.ReconciledAt)
	}
	if s.LastHandledSyncAt != nil {
		statusAC.WithLastHandledSyncAt(*s.LastHandledSyncAt)
	}

	// waves is a listType=map keyed by number, co-owned with the observer: the
	// controller owns `applied` (+ the descriptive fields it sets), the observer
	// owns `healthy`. Each entry is built field by field and deliberately omits
	// Healthy so this manager never asserts (and thus clobbers) the observer's field.
	for i := range s.Waves {
		w := s.Waves[i]
		waveAC := corev1alpha1ac.ResourceWaveStatus().WithNumber(w.Number)
		if w.Applied != nil {
			waveAC.WithApplied(*w.Applied)
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

func (c *Controller) finalize(ctx context.Context, rel *corev1alpha1.Release) error {
	if !slices.Contains(rel.Finalizers, releaseFinalizer) {
		return nil // no-op
	}

	if !rel.Spec.DeletionPolicy.KeepWorkloads {
		if err := c.pruneAll(ctx, rel); err != nil {
			return fmt.Errorf("finalizing %s/%s: %w", rel.Namespace, rel.Name, err)
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
	c.inventory.Delete(rel.Namespace + "/" + rel.Name)
	return nil
}

func (c *Controller) pruneAll(ctx context.Context, rel *corev1alpha1.Release) error {
	refs, ok := c.inventory.Get(rel.Namespace + "/" + rel.Name)
	if !ok || len(refs) == 0 {
		return nil // never applied, or already pruned
	}
	clusterNamespace := orDefault(rel.Spec.ClusterRef.Namespace, rel.Namespace)
	cluster, err := c.clusterLister.Clusters(clusterNamespace).Get(rel.Spec.ClusterRef.Name)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolving target cluster: %w", err)
	}
	if !cluster.Status.Reachable {
		if rel.Spec.DeletionPolicy.BlockOnClusterUnreachable {
			return fmt.Errorf("cluster %s not reachable", cluster.Name) // blocks
		}
		return nil // best-effort: give up cleanup, allow deletion (workloads orphaned)
	}
	return c.deployer.Prune(ctx, cluster, refs) // reuses the E2 delete path; ignores NotFound
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
func (c *Controller) reconcile(ctx context.Context, rel *corev1alpha1.Release) (reconcileOutcome, error) {
	now := c.now()
	manual := !rel.Spec.SyncPolicy.Automatic
	requestedAt, pending := pendingSync(rel)
	attempt := corev1alpha1.ReleaseAttemptedRenderStatus{
		AttemptedAt:    &now,
		ResolvedSource: resolvedSourceFrom(rel),
	}
	fail := func(reason, message string, stage corev1alpha1.ReleaseRenderAttemptStage, err error) (reconcileOutcome, error) {
		attempt.Phase = corev1alpha1.ReleaseRenderAttemptPhaseFailed
		attempt.Failure = &corev1alpha1.ReleaseAttemptedRenderFailureStatus{Stage: stage, Message: message}
		return reconcileOutcome{reason: reason, message: message, attempt: attempt}, err
	}

	// Ensure source content is fetched. A non-nil error is transient (requeue); a
	// not-ready result with no error is a terminal config problem.
	res, err := c.coordinator.Ensure(ctx, rel.Spec.Source, rel.Namespace)
	if err != nil {
		return fail(conditionReasonSourceNotReady, err.Error(), corev1alpha1.ReleaseRenderAttemptStageSourceResolution, err)
	}
	if !res.Ready {
		return fail(conditionReasonUnsupported, res.Message, corev1alpha1.ReleaseRenderAttemptStageSourceResolution, nil)
	}
	attempt.ResolvedSource.Revision = res.Revision
	attempt.ResolvedSource.URL = res.URL
	attempt.ResolvedSource.UID = res.UID
	attempt.ResolvedSource.ObservedGeneration = res.ObservedGeneration
	art := res.Artifact

	clusterNamespace := orDefault(rel.Spec.ClusterRef.Namespace, rel.Namespace)
	cluster, err := c.clusterLister.Clusters(clusterNamespace).Get(rel.Spec.ClusterRef.Name)
	if apierrors.IsNotFound(err) {
		return fail(conditionReasonClusterNotReady, fmt.Sprintf("target cluster %s/%s not found", clusterNamespace, rel.Spec.ClusterRef.Name), corev1alpha1.ReleaseRenderAttemptStageClusterResolution, fmt.Errorf("cluster not found"))
	}
	if err != nil {
		return fail(conditionReasonClusterNotReady, err.Error(), corev1alpha1.ReleaseRenderAttemptStageClusterResolution, err)
	}
	if !cluster.Status.Reachable {
		return fail(conditionReasonClusterNotReady, "target cluster is not reachable", corev1alpha1.ReleaseRenderAttemptStageClusterResolution, fmt.Errorf("cluster %s not reachable", cluster.Name))
	}
	attempt.ResolvedCluster = resolvedClusterFrom(cluster)

	valueLayers, err := c.resolveValueLayers(ctx, rel)
	if err != nil {
		if errors.Is(err, render.ErrUnsupported) {
			return fail(conditionReasonUnsupported, err.Error(), corev1alpha1.ReleaseRenderAttemptStageRendering, nil)
		}
		return fail(conditionReasonValuesNotReady, err.Error(), corev1alpha1.ReleaseRenderAttemptStageRendering, err)
	}

	objects, err := render.Render(rel.Spec.Content, art, valueLayers)
	if err != nil {
		if errors.Is(err, render.ErrUnsupported) {
			return fail(conditionReasonUnsupported, err.Error(), corev1alpha1.ReleaseRenderAttemptStageRendering, nil)
		}
		return fail(conditionReasonDeployFailed, err.Error(), corev1alpha1.ReleaseRenderAttemptStageRendering, err)
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
			return fail(conditionReasonUnsupported, err.Error(), corev1alpha1.ReleaseRenderAttemptStageRendering, nil)
		}
		return fail(conditionReasonDeployFailed, err.Error(), corev1alpha1.ReleaseRenderAttemptStageRendering, err)
	}
	attempt.ResolvedMutations = resolvedMutations
	attempt.RenderRef = renderRef(objects)

	// Honor a freeze pin: hold (do not apply) when the current render differs from
	// the pinned render — i.e. spec/source/values changed since the pin. Freezing
	// to the current render (refs equal) applies normally (idempotent). Pinning to
	// a *previous* render (rollback) is storage-gated and not handled here.
	if p := rel.Pin; p != nil && p.RenderRef != "" && p.RenderRef != attempt.RenderRef {
		return reconcileOutcome{
			reason:  conditionReasonPinned,
			message: fmt.Sprintf("pinned to render %s; withholding current render %s", p.RenderRef, attempt.RenderRef),
			attempt: attempt,
		}, nil // terminal hold; resync/unpin re-checks
	}

	// Manual-mode sync gate. In manual mode (syncPolicy.automatic=false) apply only
	// when an explicit /sync is pending. Otherwise observe without applying: report
	// in-sync when the current render already matches the last applied one, else
	// WaitingForSync (out of sync, or never synced). A spec change or resync alone
	// never auto-applies in manual mode.
	if manual && !pending {
		if ls := rel.Status.LastSuccessful; ls != nil && ls.RenderRef == attempt.RenderRef {
			return reconcileOutcome{
				ready:   true,
				reason:  conditionReasonApplied,
				message: "in sync; manual sync mode (apply withheld until /sync)",
				attempt: attempt,
			}, nil
		}
		return reconcileOutcome{
			reason:  conditionReasonWaitingForSync,
			message: "manual sync mode: changes await a /sync request",
			attempt: attempt,
		}, nil
	}

	// Apply in ascending sync-wave order. Each wave applies fully before the next
	// begins. A wave that requires a health bar (waitForHealthy*) can only gate the
	// next wave once health is known — which the observer assesses, not this
	// controller — so A2a applies through the first such wave and then halts,
	// surfacing a non-ready condition rather than silently applying later waves
	// before the health gate is satisfied. (Health-gated advancement is A2b.)
	relKey := rel.Namespace + "/" + rel.Name
	waveGroups := waves.Group(objects)
	var applied []inventory.Ref
	waveStatuses := make([]corev1alpha1.ResourceWaveStatus, 0, len(waveGroups))
	haltedForHealth := false
	for i := range waveGroups {
		w := waveGroups[i]
		waveApplied, err := c.deployer.Deploy(ctx, cluster, w.Objects)
		if err != nil {
			if errors.Is(err, render.ErrUnsupported) || errors.Is(err, clusterconn.ErrUnsupported) {
				return fail(conditionReasonUnsupported, err.Error(), corev1alpha1.ReleaseRenderAttemptStageSyncing, nil)
			}
			return fail(conditionReasonDeployFailed, err.Error(), corev1alpha1.ReleaseRenderAttemptStageSyncing, err)
		}
		applied = append(applied, waveApplied...)
		waveStatuses = append(waveStatuses, corev1alpha1.ResourceWaveStatus{
			Number:      int64(w.Number),
			Applied:     new(true),
			Resources:   len(w.Objects),
			State:       corev1alpha1.ResourceWaveStateCompleted,
			StartedAt:   &now,
			CompletedAt: &now,
		})
		if w.NeedsHealth {
			haltedForHealth = true
			break
		}
	}

	if haltedForHealth {
		// Partial rollout: do NOT prune (would delete still-rendered objects in
		// unapplied waves) and do NOT advance lastSuccessful.
		return reconcileOutcome{
			reason:  conditionReasonWaitingForWaveHealth,
			message: "waiting on health-gated sync wave; health-gated wave advancement is not supported yet",
			attempt: attempt,
			waves:   waveStatuses,
		}, nil
	}

	// Steady-state prune: apply succeeded, so anything this Release managed last
	// time but no longer renders is an orphan. Only reached on the success path,
	// so we never prune from a failed/partial render.
	if err := c.prune(ctx, cluster, relKey, applied, rel.Spec.SyncPolicy.Prune); err != nil {
		return fail(conditionReasonPruneFailed, err.Error(), corev1alpha1.ReleaseRenderAttemptStageSyncing, err)
	}
	c.inventory.Put(relKey, applied)

	attempt.Phase = corev1alpha1.ReleaseRenderAttemptPhaseSucceeded
	attempt.SyncedAt = &now
	attempt.SyncedResources = int64(len(applied))
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

// prune deletes resources dropped from the render set since the last successful
// apply, honoring the prune policy. On prune failure the inventory is left
// unchanged so the orphans are retried on the next reconcile. When pruning is
// disabled the orphans are simply untracked (the caller advances the inventory).
func (c *Controller) prune(ctx context.Context, cluster *corev1alpha1.Cluster, relKey string, applied []inventory.Ref, policy corev1alpha1.PruneSpec) error {
	if !policy.Orphaned {
		return nil
	}
	old, ok := c.inventory.Get(relKey)
	if !ok {
		return nil // nothing tracked yet (first apply / post-restart)
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
