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
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	extensionsv1alpha1 "github.com/kumodo-io/convoy/api/extensions/v1alpha1"
	clientset "github.com/kumodo-io/convoy/api/generated/clientset/versioned"
	coreinformers "github.com/kumodo-io/convoy/api/generated/informers/externalversions/core/v1alpha1"
	corelisters "github.com/kumodo-io/convoy/api/generated/listers/core/v1alpha1"
	extensionslisters "github.com/kumodo-io/convoy/api/generated/listers/extensions/v1alpha1"
	"github.com/kumodo-io/convoy/internal/clusterconn"
	"github.com/kumodo-io/convoy/internal/controller/base"
	"github.com/kumodo-io/convoy/internal/health"
	"github.com/kumodo-io/convoy/internal/healthresult"
)

// HealthController runs the health-assessment loop, keyed on Release. It is the
// observer's second loop, running alongside the drift Controller in the same
// process. It assesses the resources the release controller recorded on the
// ReleaseInventory (the durable applied set) against the health rule sets the
// Release's selectors pick, records per-resource health back on the
// ReleaseInventory, publishes the Healthy condition + summary on the Release, and
// — when a sync wave completes — requests a sync so the controller advances the
// gate promptly instead of on its poll interval.
type HealthController struct {
	*base.Controller
	client              clientset.Interface
	releaseLister       corelisters.ReleaseLister
	clusterLister       corelisters.ClusterLister
	connector           *clusterconn.Connector
	globalRuleSetLister extensionslisters.GlobalHealthRuleSetLister
	ruleSetLister       extensionslisters.HealthRuleSetLister
	inventoryLister     corelisters.ReleaseInventoryLister
	// owns reports whether this replica owns a Release's target-cluster shard.
	// nil disables sharding.
	owns func(clusterNamespace, clusterName string) bool
	// progress tracks the consecutive not-yet-healthy assessment count per Release,
	// to drive exponential backoff of re-checks while a rollout is progressing.
	progress *backoffCounts
	// now is injectable for tests.
	now func() metav1.Time
	// assessFn is a seam: it defaults to the real assessment and is stubbed in
	// tests so syncHandler can be exercised without a live cluster.
	assessFn func(context.Context, *corev1alpha1.Release) (*assessment, error)
	// syncFn requests a Release reconcile (POST /sync); a seam for tests. It is how
	// the health loop tells the controller a wave became healthy.
	syncFn func(context.Context, *corev1alpha1.Release) error
	// pollHooksFn is a seam: it defaults to the real lifecycle-hook polling and is
	// stubbed in tests so syncHandler can be exercised without a live cluster.
	pollHooksFn func(context.Context, *corev1alpha1.Release) (*hookPollResult, error)
}

// NewHealthController wires the observer's health loop.
func NewHealthController(
	client clientset.Interface,
	informer coreinformers.ReleaseInformer,
	clusterLister corelisters.ClusterLister,
	connector *clusterconn.Connector,
	globalRuleSetLister extensionslisters.GlobalHealthRuleSetLister,
	ruleSetLister extensionslisters.HealthRuleSetLister,
	inventoryLister corelisters.ReleaseInventoryLister,
	owns func(clusterNamespace, clusterName string) bool,
) *HealthController {
	c := &HealthController{
		client:              client,
		releaseLister:       informer.Lister(),
		clusterLister:       clusterLister,
		connector:           connector,
		globalRuleSetLister: globalRuleSetLister,
		ruleSetLister:       ruleSetLister,
		inventoryLister:     inventoryLister,
		owns:                owns,
		progress:            newBackoffCounts(),
		now:                 metav1.Now,
	}
	c.assessFn = c.assess
	c.syncFn = c.requestSync
	c.pollHooksFn = c.pollHooks
	c.Controller = base.NewController("health", c.syncHandler,
		base.WithEnqueueFilter(func(obj any) bool { return releaseOwned(c.owns, obj) }))
	utilruntime.Must(c.WatchInformer(informer.Informer()))
	return c
}

// syncHandler runs one health pass for a Release.
func (c *HealthController) syncHandler(ctx context.Context, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key %q: %w", key, err))
		return nil
	}

	rel, err := c.releaseLister.Releases(namespace).Get(name)
	if apierrors.IsNotFound(err) {
		return nil // the release controller owns the ReleaseInventory lifecycle
	}
	if err != nil {
		return err
	}
	if c.owns != nil && !c.owns(clusterKeyOf(rel)) {
		return nil // another replica owns this Release's cluster shard
	}
	if !rel.DeletionTimestamp.IsZero() {
		// During deletion keep polling delete-phase hooks so the controller's finalize
		// gate can advance (PreDelete/PostDelete run to completion), but stop assessing
		// resource health — they are being torn down. The finishedAt watermark makes
		// this quiesce: once every armed hook is terminal, needsHookPoll is false for
		// all, so pollHooks stops writing before the controller deletes the inventory.
		hookRes, hookErr := c.pollHooksFn(ctx, rel)
		if hookErr != nil {
			klog.V(4).InfoS("hook polling undetermined (deleting)", "release", key, "reason", hookErr.Error())
		}
		if hookRes != nil && hookRes.batchCompleted {
			if err := c.syncFn(ctx, rel); err != nil {
				return fmt.Errorf("requesting sync on hook completion for %s: %w", key, err)
			}
		}
		var notSettled []string
		if hookRes != nil {
			notSettled = hookRes.runningHooks
		}
		c.scheduleHealthRecheck(key, rel, notSettled)
		return nil
	}

	// Lifecycle hooks: poll the armed hook workloads recorded on the
	// ReleaseInventory and record their outcome, independent of health assessment
	// (a release without a health assessment can still have hooks). Best-effort like
	// the health/drift loops — a poll error is logged, not returned.
	hookRes, hookErr := c.pollHooksFn(ctx, rel)
	if hookErr != nil {
		klog.V(4).InfoS("hook polling undetermined", "release", key, "reason", hookErr.Error())
	}

	// Health assessment. notSettled accumulates the backoff-target IDs still pending
	// (unhealthy resources + running hooks) so the recheck cadence tracks each on its
	// own staircase; waveDone marks the edge that should wake the controller.
	var notSettled []string
	waveDone, renderAdvanced := false, false
	switch rel.Spec.HealthAssessment {
	case nil:
		// No assessment: report Healthy — everything applied, nothing to check. The
		// controller treats such a release as satisfying every wave's health bar.
		if err := c.writeHealth(ctx, rel, metav1.ConditionTrue, reasonNotConfigured,
			"no health assessment configured", healthresult.Summary{}); err != nil {
			return err
		}
	default:
		a, recErr := c.assessFn(ctx, rel)
		switch {
		case errors.Is(recErr, errInventoryGone):
			// The Release is going away or its inventory was replaced: a later pass reads the new state.
			klog.V(4).InfoS("health not written, inventory gone", "release", key)
			notSettled = append(notSettled, inventoryGoneTarget)
		case recErr != nil:
			// Couldn't assess (cluster unreachable, invalid rules): report
			// Healthy=Unknown and re-check later, preserving the persisted anchors.
			klog.V(4).InfoS("health undetermined", "release", key, "reason", recErr.Error())
			if err := c.writeHealth(ctx, rel, metav1.ConditionUnknown, reasonUnknown, recErr.Error(),
				toWireSummary(rel.Status.HealthSummary)); err != nil {
				return err
			}
		default:
			// assess persisted per-resource health onto the ReleaseInventory (the
			// controller rolls it up for the sync-wave gate). Publish the aggregate
			// Healthy condition + summary on the Release.
			// Report Pending until the current render is reflected on the cluster: with
			// nothing deployed yet, or a re-render not yet applied/assessed, the prior
			// render's health must not read as the new render's healthy.
			var status metav1.ConditionStatus
			var reason, message string
			if a.renderCurrent {
				status, reason, message = conditionFor(a.result)
			} else {
				status, reason, message = metav1.ConditionUnknown, reasonPending, "current render not yet applied and assessed"
				// Not settled: keep re-checking fast until the render is reflected.
				notSettled = append(notSettled, renderPendingTarget)
			}
			if err := c.writeHealth(ctx, rel, status, reason, message, a.result.Summarize()); err != nil {
				return err
			}
			notSettled = append(notSettled, unhealthyResourceTargets(a.entries)...)
			waveDone = frontierWaveCompleted(a.entries, a.prior)
			renderAdvanced = a.renderAdvanced
		}
	}
	if hookRes != nil {
		notSettled = append(notSettled, hookRes.runningHooks...)
	}

	// Wake the controller the moment a sync-wave frontier completes, the assessment
	// catches up to a newly-applied render, or a lifecycle-hook batch completes — so
	// it advances (or arms a render-gated hook) promptly instead of on its poll
	// interval. Edge-triggered, so it doesn't re-fire on steady-state passes. The
	// controller's own periodic reconcile is the safety net.
	if waveDone || renderAdvanced || (hookRes != nil && hookRes.batchCompleted) {
		if err := c.syncFn(ctx, rel); err != nil {
			return fmt.Errorf("requesting sync for %s: %w", key, err)
		}
		klog.V(4).InfoS("requested sync on wave/render/hook completion", "release", key)
	}

	// Re-check cadence: each still-pending target (unhealthy resource or running hook)
	// drives its own staircase, and the release re-checks at the fastest of them — so
	// a freshly-pending target is noticed within ~2s regardless of what else is slow;
	// once nothing is pending, fall back to the normal interval.
	c.scheduleHealthRecheck(key, rel, notSettled)
	return nil
}

// assessment is the outcome of one health pass: the aggregate result, the
// freshly-assessed per-resource entries (with wave), and the prior per-resource
// states (read from the ReleaseInventory before this pass) used to detect the
// transition that completes a wave.
type assessment struct {
	result  *healthresult.Result
	entries []healthEntry
	prior   map[string]corev1alpha1.ResourceState
	// renderCurrent is true when every assessed resource belongs to the render the
	// controller last targeted — i.e. the health reflects the CURRENT render, not a
	// prior one still lingering. False when nothing is deployed yet, or a re-render's
	// resources aren't applied/assessed yet; the aggregate Healthy is then Pending
	// (the prior render's health must not read as the new render's).
	renderCurrent bool
	// renderAdvanced is true when this pass caught the assessment up to a render the
	// controller had newly applied (some resource's applied RenderRef differed from
	// the observer's last AssessedRenderRef). It is the edge trigger to poke the
	// controller so a render-gated hook (postSync runAfter:Healthy) re-checks promptly
	// — a render change that doesn't also flip a health STATE would otherwise be
	// noticed only on the poll interval.
	renderAdvanced bool
}

// lastAttemptedRender returns the render the controller last targeted, or "".
func lastAttemptedRender(rel *corev1alpha1.Release) string {
	if la := rel.Status.LastAttempted; la != nil {
		return la.RenderRef
	}
	return ""
}

// entriesAllAtRender reports whether every assessed entry was applied under the
// given render. An empty set is not "at" any render (nothing deployed yet).
func entriesAllAtRender(entries []healthEntry, render string) bool {
	if len(entries) == 0 {
		return false
	}
	for _, e := range entries {
		if e.renderRef != render {
			return false
		}
	}
	return true
}

// frontierWaveCompleted reports whether the highest wave present in the inventory
// (the gate frontier) is now fully Healthy AND at least one of its resources
// transitioned to Healthy this pass (per prior). This is the edge trigger to
// advance the gate: the frontier wave just completed, so the controller should
// apply the next wave, if any. Because it keys off the highest *inventoried* wave,
// it stops firing for a wave once the controller applies a later one (that later
// wave becomes the frontier). prior maps identity → previous state.
func frontierWaveCompleted(entries []healthEntry, prior map[string]corev1alpha1.ResourceState) bool {
	if len(entries) == 0 {
		return false
	}
	maxWave := entries[0].wave
	for _, e := range entries {
		if e.wave > maxWave {
			maxWave = e.wave
		}
	}
	transitioned := false
	for _, e := range entries {
		if e.wave != maxWave {
			continue
		}
		if e.state != corev1alpha1.ResourceStateHealthy {
			return false // frontier not fully healthy yet
		}
		if old := prior[e.ref.identityID()]; old != corev1alpha1.ResourceStateHealthy {
			transitioned = true
		}
	}
	return transitioned
}

// assess evaluates the resources recorded on the Release's ReleaseInventory (the
// durable applied set) against the applicable rules, reading each live from the
// target cluster, persists per-resource health back onto the ReleaseInventory,
// and returns the aggregate result plus the entries and prior states needed to
// detect a completed wave. It does NOT render — the inventory is the source of
// truth for what to assess, so a resource is assessed as soon as it is applied.
func (c *HealthController) assess(ctx context.Context, rel *corev1alpha1.Release) (*assessment, error) {
	rules, err := c.collectRules(rel)
	if err != nil {
		return nil, err
	}
	evaluator, err := health.NewEvaluator(rules)
	if err != nil {
		return nil, fmt.Errorf("compiling health rules: %w", err)
	}

	inv, err := c.inventoryLister.ReleaseInventories(rel.Namespace).Get(rel.Name)
	if apierrors.IsNotFound(err) {
		return &assessment{result: &healthresult.Result{}}, nil // nothing applied yet
	}
	if err != nil {
		return nil, fmt.Errorf("reading release inventory: %w", err)
	}
	if len(inv.Inventory) == 0 {
		return &assessment{result: &healthresult.Result{}}, nil
	}

	clusterNamespace := rel.Spec.ClusterRef.Namespace
	if clusterNamespace == "" {
		clusterNamespace = rel.Namespace
	}
	cluster, err := c.clusterLister.Clusters(clusterNamespace).Get(rel.Spec.ClusterRef.Name)
	if err != nil {
		return nil, fmt.Errorf("resolving target cluster: %w", err)
	}
	if !cluster.Status.Reachable {
		return nil, fmt.Errorf("target cluster %s is not reachable", cluster.Name)
	}

	dyn, mapper, err := c.connector.DynamicClients(ctx, cluster)
	if err != nil {
		return nil, err
	}

	var deadline *time.Duration
	if d := rel.Spec.HealthAssessment.Deadline; d != nil {
		dd := d.Duration
		deadline = &dd
	}
	now := c.now().Time

	// Snapshot prior per-resource state + progress anchors from the durable
	// inventory BEFORE writing this pass: prior states detect the wave-completing
	// transition, and anchors (firstUnhealthy) survive a restart so an in-flight
	// progress clock isn't reset.
	prior, anchors := priorAndAnchors(inv)

	items := controllerItems(inv.Inventory)
	result := &healthresult.Result{Objects: make([]healthresult.ObjectHealth, 0, len(items))}
	entries := make([]healthEntry, 0, len(items))
	for i := range items {
		entry := c.assessItem(ctx, evaluator, dyn, mapper, items[i], anchors, deadline, now)
		// Tag the assessment with the render the resource was actually applied under
		// (controller-owned), NOT the render the controller is currently attempting.
		entry.renderRef = items[i].RenderRef
		entries = append(entries, entry)
		result.Objects = append(result.Objects, healthresult.ObjectHealth{
			Group:     entry.ref.gvk.Group,
			Version:   entry.ref.gvk.Version,
			Kind:      entry.ref.gvk.Kind,
			Namespace: entry.ref.namespace,
			Name:      entry.ref.name,
			State:     healthresult.State(entry.state),
			Message:   entry.message,
		})
	}
	// Detect whether this pass advances the assessment to a newly-applied render: any
	// item whose controller-applied RenderRef differs from the observer's last
	// AssessedRenderRef is about to be updated below, so the render moved forward.
	renderAdvanced := false
	for i := range inv.Inventory {
		it := inv.Inventory[i]
		if it.RenderRef != "" && it.RenderRef != it.AssessedRenderRef {
			renderAdvanced = true
			break
		}
	}

	if err := c.writeInventoryHealth(ctx, rel, inv.UID, entries); err != nil {
		return nil, err
	}
	return &assessment{
		result:         result,
		entries:        entries,
		prior:          prior,
		renderAdvanced: renderAdvanced,
		renderCurrent:  entriesAllAtRender(entries, lastAttemptedRender(rel)),
	}, nil
}

// assessItem evaluates a single inventoried resource's health, reading it live
// from the target cluster. Identity (GVK + namespace) comes straight from the
// inventory entry (the controller stored the resolved namespace), so the entry
// this produces lines up with the controller-owned ReleaseInventory entry.
func (c *HealthController) assessItem(ctx context.Context, evaluator *health.Evaluator, dyn dynamic.Interface, mapper meta.RESTMapper, it corev1alpha1.ResourceInventoryItem, anchors map[string]time.Time, deadline *time.Duration, now time.Time) healthEntry {
	gvk := schema.GroupVersionKind{Group: it.Group, Version: it.Version, Kind: it.Kind}
	ref := objectRef{gvk: gvk, namespace: it.Namespace, name: it.Name}
	wave := 0
	if it.Wave != nil {
		wave = *it.Wave
	}
	// A resource it cannot read is Unknown, which keeps the stored anchor as DeriveState does.
	anchor := anchors[ref.identityID()]

	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return healthEntry{ref: ref, wave: wave, state: corev1alpha1.ResourceStateUnknown, message: fmt.Sprintf("cannot resolve resource for %s: %v", gvk, err), firstUnhealthy: anchor}
	}
	resource := mapping.Resource.Resource

	// No applicable rule → Healthy without a live read.
	if !evaluator.HasRules(gvk.GroupVersion().String(), []string{resource}) {
		return healthEntry{ref: ref, wave: wave, state: corev1alpha1.ResourceStateHealthy, message: "no applicable health rules"}
	}

	live, err := resourceClient(dyn, mapping, it.Namespace).Get(ctx, it.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return healthEntry{ref: ref, wave: wave, state: corev1alpha1.ResourceStateUnknown, message: "resource not found in cluster", firstUnhealthy: anchor}
	}
	if err != nil {
		return healthEntry{ref: ref, wave: wave, state: corev1alpha1.ResourceStateUnknown, message: fmt.Sprintf("reading live resource: %v", err), firstUnhealthy: anchor}
	}

	assessed := evaluator.Assess(live, []string{resource})
	state, anchor := health.DeriveState(assessed.Passed, assessed.Err, anchor, deadline, now)
	message := assessed.Message
	if assessed.Err != nil {
		message = assessed.Err.Error()
	}
	return healthEntry{ref: ref, wave: wave, state: state, message: message, firstUnhealthy: anchor}
}

// collectRules gathers the RuleSpecs of the rule sets the Release's selectors
// pick: GlobalHealthRuleSets cluster-wide + HealthRuleSets in the Release's
// namespace. A nil selector matches nothing (opt-in per scope).
func (c *HealthController) collectRules(rel *corev1alpha1.Release) ([]extensionsv1alpha1.RuleSpec, error) {
	ha := rel.Spec.HealthAssessment
	var rules []extensionsv1alpha1.RuleSpec

	gsel, err := metav1.LabelSelectorAsSelector(ha.GlobalRuleSetSelector)
	if err != nil {
		return nil, fmt.Errorf("invalid globalRuleSetSelector: %w", err)
	}
	globals, err := c.globalRuleSetLister.List(gsel)
	if err != nil {
		return nil, err
	}
	for _, g := range globals {
		rules = append(rules, g.Spec.Rules...)
	}

	lsel, err := metav1.LabelSelectorAsSelector(ha.LocalRuleSetSelector)
	if err != nil {
		return nil, fmt.Errorf("invalid localRuleSetSelector: %w", err)
	}
	locals, err := c.ruleSetLister.HealthRuleSets(rel.Namespace).List(lsel)
	if err != nil {
		return nil, err
	}
	for _, l := range locals {
		rules = append(rules, l.Spec.Rules...)
	}
	return rules, nil
}

const progressBackoffJitter = 0.2

// renderPendingTarget is a synthetic backoff target that keeps the observer
// re-checking fast while the current render is not yet reflected on the cluster
// (health Pending), so the transition out of Pending is noticed promptly.
const renderPendingTarget = "render-pending"

// inventoryGoneTarget keeps the re-check fast after a write found the inventory
// deleted or replaced, so the next pass reads the new state soon.
const inventoryGoneTarget = "inventory-gone"

// progressBackoffLevels is the staircase re-check cadence for a not-yet-settled
// release (any resource Progressing/Unknown, or any lifecycle hook still Running).
// Each level runs `attempts` consecutive passes at `delay` before easing to the
// next; the final level (attempts == 0) repeats forever. Holding at 2s for the
// first several passes keeps a short-lived change — a hook finishing, a Deployment
// going Ready — reflected within a couple of seconds, while later passes back off
// so a slow rollout isn't hammered. The tail (32s) also caps the fallback cadence
// when no sync interval is configured.
var progressBackoffLevels = []struct {
	delay    time.Duration
	attempts int
}{
	{2 * time.Second, 5},
	{4 * time.Second, 4},
	{8 * time.Second, 3},
	{16 * time.Second, 2},
	{32 * time.Second, 0}, // 0 → repeats for every further attempt
}

// progressBackoffMax is the staircase tail, used as the fallback cap when no sync
// interval is set.
const progressBackoffMax = 32 * time.Second

// unhealthyResourceTargets returns the backoff-target IDs of the not-yet-Healthy
// resources, namespaced apart from hook targets so the two never collide.
func unhealthyResourceTargets(entries []healthEntry) []string {
	var ids []string
	for _, e := range entries {
		if e.state != corev1alpha1.ResourceStateHealthy {
			ids = append(ids, "res|"+e.ref.identityID())
		}
	}
	return ids
}

// scheduleHealthRecheck schedules the next health pass. With nothing pending it
// clears the per-target staircase and re-checks at the normal poll interval;
// otherwise it re-checks at the fastest staircase across the pending targets
// (bounded by the interval so it never polls slower than steady state), so a
// resource turning healthy or a hook completing is noticed within seconds instead
// of a whole interval — which is what lets the sync-wave gate advance and the status
// reflect completion promptly.
func (c *HealthController) scheduleHealthRecheck(key string, rel *corev1alpha1.Release, notSettled []string) {
	interval := rel.Spec.SyncPolicy.Interval.Duration
	if len(notSettled) == 0 {
		c.progress.clear(key)
		if interval > 0 {
			c.EnqueueAfter(key, interval)
		}
		return
	}
	capDelay := interval
	if capDelay <= 0 {
		capDelay = progressBackoffMax
	}
	c.EnqueueAfter(key, wait.Jitter(c.progress.advance(key, notSettled, capDelay), progressBackoffJitter))
}

// progressBackoff returns the base (un-jittered) re-check delay for the attempt-th
// consecutive not-yet-settled pass (1-based), per the staircase levels, never
// exceeding capDelay (so it doesn't poll slower than the steady interval).
func progressBackoff(attempt int, capDelay time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	remaining := attempt
	for _, lvl := range progressBackoffLevels {
		if lvl.attempts == 0 || remaining <= lvl.attempts {
			return minDuration(lvl.delay, capDelay)
		}
		remaining -= lvl.attempts
	}
	return minDuration(progressBackoffMax, capDelay)
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// requestSync triggers a reconcile of the Release by POSTing its /sync
// subresource — the same channel the drift loop, CLI, and users use. Here it tells
// the release controller a sync wave became healthy so it advances the gate. It is
// a wake-up only (?wake=true): in manual mode it must not count as a user /sync.
func (c *HealthController) requestSync(ctx context.Context, rel *corev1alpha1.Release) error {
	return c.client.CoreV1alpha1().RESTClient().
		Post().
		Namespace(rel.Namespace).
		Resource("releases").
		Name(rel.Name).
		SubResource("sync").
		Param("wake", "true").
		Do(ctx).
		Error()
}

// HealthResult returns the last assessed per-object health for a Release, for the
// /health subresource, or nil if it has not been assessed. It is served from the
// durable ReleaseInventory, so it survives a restart and reflects the applied
// resource set.
func (c *HealthController) HealthResult(releaseKey string) *healthresult.Result {
	namespace, name, err := cache.SplitMetaNamespaceKey(releaseKey)
	if err != nil {
		return nil
	}
	inv, err := c.inventoryLister.ReleaseInventories(namespace).Get(name)
	if err != nil {
		return nil
	}
	return resultFromInventory(inv)
}

// resourceClient returns the dynamic client for a mapping, namespaced per the
// object when the resource is namespace-scoped.
func resourceClient(dyn dynamic.Interface, mapping *meta.RESTMapping, namespace string) dynamic.ResourceInterface {
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if namespace == "" {
			namespace = metav1.NamespaceDefault
		}
		return dyn.Resource(mapping.Resource).Namespace(namespace)
	}
	return dyn.Resource(mapping.Resource)
}

// toWireSummary maps a core HealthSummary to the wire summary (for preserving the
// last summary on an undetermined pass).
func toWireSummary(s corev1alpha1.ReleaseHealthSummary) healthresult.Summary {
	return healthresult.Summary{
		Total:       s.Total,
		Healthy:     s.Healthy,
		Progressing: s.Progressing,
		Degraded:    s.Degraded,
		Unknown:     s.Unknown,
	}
}
