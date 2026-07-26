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

	// Assert ONLY the Applied condition — the sole condition this controller owns.
	// computeStatus deep-copies the whole status (carrying the observer-owned InSync
	// and Healthy conditions), so writing every condition here would make this field
	// manager co-own them and, with Force, clobber the observer's values back to a
	// stale copy on each reconcile. conditions is a listType=map keyed by type, so
	// asserting just Applied leaves the others untouched.
	if applied := apimeta.FindStatusCondition(s.Conditions, conditionApplied); applied != nil {
		condAC := &metav1ac.ConditionApplyConfiguration{}
		if err := jsonRoundTrip(*applied, condAC); err != nil {
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

func (c *Controller) finalize(ctx context.Context, rel *corev1alpha1.Release) error {
	if !slices.Contains(rel.Finalizers, releaseFinalizer) {
		return nil // no-op
	}

	if !rel.Spec.DeletionPolicy.KeepWorkloads {
		if err := c.pruneAll(ctx, rel); err != nil {
			return fmt.Errorf("finalizing %s/%s: %w", rel.Namespace, rel.Name, err)
		}
	}

	// Delete the durable inventory before dropping the finalizer: prune has
	// succeeded (or was skipped), so the record's job is done. Doing it first
	// means a failure here keeps the finalizer, and the delete is retried, rather
	// than the Release vanishing and orphaning its ReleaseInventory.
	if err := c.inventory.Delete(ctx, rel.Namespace, rel.Name); err != nil {
		return fmt.Errorf("deleting inventory for %s/%s: %w", rel.Namespace, rel.Name, err)
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

func (c *Controller) pruneAll(ctx context.Context, rel *corev1alpha1.Release) error {
	refs, ok, err := c.inventory.Get(ctx, rel.Namespace, rel.Name)
	if err != nil {
		return err
	}
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
// todo: fix gocyclo
// nolint:gocyclo
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
	// begins. A wave with a health bar (waitForHealthy*) gates the *next* wave until
	// every prior wave is healthy. Health is assessed by the observer, which records
	// per-resource health on the ReleaseInventory; the controller rolls that up per
	// wave (keyed by the exact refs it applied) rather than reading its own status,
	// and requeues while a prior wave is not yet healthy. waitForHealthyWithTimeout
	// fails the wave once its timeout elapses. All waves are (idempotently)
	// re-applied each reconcile, so drift is still corrected as the rollout
	// progresses.
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
	waveStatuses := make([]corev1alpha1.ResourceWaveStatus, 0, len(waveGroups))
	for i := range waveGroups {
		w := waveGroups[i]

		// Gate: a wave whose resources declare waitForHealthy* waits for ALL prior
		// waves to be healthy before it applies (the strategy is the bar the wave's
		// predecessors must clear before it enters). `applied` already holds exactly
		// those prior waves' refs (deployed by loop order), so their persisted health
		// is the roll-up — no dependency on this controller's own status.
		if i > 0 && waveGroups[i].NeedsHealth && !priorWavesHealthy(applied, stateByID, noHealthBar) {
			// Anchor a bounded wait at the immediately-preceding wave's first apply.
			prevStarted := prior[int64(waveGroups[i-1].Number)].StartedAt
			if waveGroups[i].Bounded && prevStarted != nil && now.Sub(prevStarted.Time) > waveGroups[i].Timeout {
				out, _ := fail(conditionReasonWaveFailed, fmt.Sprintf("sync wave %d: prior waves not healthy within %s", w.Number, waveGroups[i].Timeout), corev1alpha1.ReleaseRenderAttemptStageSyncing, nil)
				out.waves = waveStatuses
				return out, nil
			}
			// Not healthy yet: hold. Partial rollout — do NOT prune or advance
			// lastSuccessful. Requeues on the poll interval; the observer records
			// prior waves' health in the meantime.
			return reconcileOutcome{
				reason:  conditionReasonWaitingForWaveHealth,
				message: fmt.Sprintf("waiting for waves before %d to become healthy", w.Number),
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
		// attribute prior-wave refs.
		for k := range waveApplied {
			waveApplied[k].Wave = w.Number
		}
		applied = append(applied, waveApplied...)

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

// priorWavesHealthy reports whether every already-applied ref is Healthy per the
// observer-written ReleaseInventory (stateByID). A missing or non-Healthy entry
// means the roll-up is not yet satisfied, so a gated wave holds. noHealthBar
// short-circuits to true: a release with no health assessment has no bar to clear.
func priorWavesHealthy(applied []inventory.Ref, stateByID map[string]*corev1alpha1.ResourceState, noHealthBar bool) bool {
	if noHealthBar {
		return true
	}
	for _, r := range applied {
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
