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
) *HealthController {
	c := &HealthController{
		client:              client,
		releaseLister:       informer.Lister(),
		clusterLister:       clusterLister,
		connector:           connector,
		globalRuleSetLister: globalRuleSetLister,
		ruleSetLister:       ruleSetLister,
		inventoryLister:     inventoryLister,
		progress:            newBackoffCounts(),
		now:                 metav1.Now,
	}
	c.assessFn = c.assess
	c.syncFn = c.requestSync
	c.pollHooksFn = c.pollHooks
	c.Controller = base.NewController("health", c.syncHandler)
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
	if !rel.DeletionTimestamp.IsZero() {
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

	// Health assessment. healthSettled stays true when there is nothing to wait for
	// (no assessment, or an undetermined pass); waveDone marks the edge that should
	// wake the controller.
	healthSettled, waveDone := true, false
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
		if recErr != nil {
			// Couldn't assess (cluster unreachable, invalid rules): report
			// Healthy=Unknown and re-check later, preserving the persisted anchors.
			klog.V(4).InfoS("health undetermined", "release", key, "reason", recErr.Error())
			if err := c.writeHealth(ctx, rel, metav1.ConditionUnknown, reasonUnknown, recErr.Error(),
				toWireSummary(rel.Status.HealthSummary)); err != nil {
				return err
			}
		} else {
			// assess persisted per-resource health onto the ReleaseInventory (the
			// controller rolls it up for the sync-wave gate). Publish the aggregate
			// Healthy condition + summary on the Release.
			status, reason, message := conditionFor(a.result)
			if err := c.writeHealth(ctx, rel, status, reason, message, a.result.Summarize()); err != nil {
				return err
			}
			healthSettled = allHealthy(a.entries)
			waveDone = frontierWaveCompleted(a.entries, a.prior)
		}
	}

	// Wake the controller the moment a sync-wave frontier or a lifecycle-hook batch
	// completes, so it advances promptly instead of on its poll interval.
	// Edge-triggered (only on the transition to done), so it doesn't re-fire on
	// steady-state passes. The controller's own periodic reconcile is the safety net.
	if waveDone || (hookRes != nil && hookRes.batchCompleted) {
		if err := c.syncFn(ctx, rel); err != nil {
			return fmt.Errorf("requesting sync for %s: %w", key, err)
		}
		klog.V(4).InfoS("requested sync on wave/hook completion", "release", key)
	}

	// Re-check cadence: while any resource is still Progressing/Unknown or any hook
	// is still Running, retry with exponential backoff (bounded by the poll interval)
	// so completion is noticed within seconds; once everything is settled, fall back
	// to the normal interval.
	settled := healthSettled && (hookRes == nil || hookRes.allSettled)
	c.scheduleHealthRecheck(key, rel, settled)
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
}

// allHealthy reports whether every assessed entry is Healthy. An empty set is
// healthy (nothing is unhealthy).
func allHealthy(entries []healthEntry) bool {
	for _, e := range entries {
		if e.state != corev1alpha1.ResourceStateHealthy {
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

	result := &healthresult.Result{Objects: make([]healthresult.ObjectHealth, 0, len(inv.Inventory))}
	entries := make([]healthEntry, 0, len(inv.Inventory))
	for i := range inv.Inventory {
		entry := c.assessItem(ctx, evaluator, dyn, mapper, inv.Inventory[i], anchors, deadline, now)
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
	if err := c.writeInventoryHealth(ctx, rel, entries); err != nil {
		return nil, err
	}
	return &assessment{result: result, entries: entries, prior: prior}, nil
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

	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return healthEntry{ref: ref, wave: wave, state: corev1alpha1.ResourceStateUnknown, message: fmt.Sprintf("cannot resolve resource for %s: %v", gvk, err)}
	}
	resource := mapping.Resource.Resource

	// No applicable rule → Healthy without a live read.
	if !evaluator.HasRules(gvk.GroupVersion().String(), []string{resource}) {
		return healthEntry{ref: ref, wave: wave, state: corev1alpha1.ResourceStateHealthy, message: "no applicable health rules"}
	}

	live, err := resourceClient(dyn, mapping, it.Namespace).Get(ctx, it.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return healthEntry{ref: ref, wave: wave, state: corev1alpha1.ResourceStateUnknown, message: "resource not found in cluster"}
	}
	if err != nil {
		return healthEntry{ref: ref, wave: wave, state: corev1alpha1.ResourceStateUnknown, message: fmt.Sprintf("reading live resource: %v", err)}
	}

	assessed := evaluator.Assess(live, []string{resource})
	state, anchor := health.DeriveState(assessed.Passed, assessed.Err, anchors[ref.identityID()], deadline, now)
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

// progress backoff tuning: start at 2s and double each not-yet-healthy pass,
// capped at the poll interval (or defaultProgressBackoffMax when none is set),
// with jitter to avoid synchronized re-assessments across many Releases.
const (
	progressBackoffBase       = 2 * time.Second
	defaultProgressBackoffMax = 30 * time.Second
	progressBackoffJitter     = 0.2
)

// scheduleHealthRecheck schedules the next health pass. Once every resource is
// healthy it resets the backoff and re-checks at the normal poll interval;
// otherwise it re-checks sooner, with exponential backoff bounded by the interval,
// so a resource turning healthy (e.g. a Deployment finishing its rollout) is
// noticed within seconds instead of a whole interval — which is what lets the
// sync-wave gate advance promptly.
func (c *HealthController) scheduleHealthRecheck(key string, rel *corev1alpha1.Release, healthy bool) {
	interval := rel.Spec.SyncPolicy.Interval.Duration
	if healthy {
		c.progress.reset(key)
		if interval > 0 {
			c.EnqueueAfter(key, interval)
		}
		return
	}
	capDelay := interval
	if capDelay <= 0 {
		capDelay = defaultProgressBackoffMax
	}
	n := c.progress.inc(key)
	c.EnqueueAfter(key, wait.Jitter(progressBackoff(n, capDelay), progressBackoffJitter))
}

// progressBackoff returns the base (un-jittered) re-check delay for the attempt-th
// consecutive not-yet-healthy pass (1-based): progressBackoffBase * 2^(attempt-1),
// capped at capDelay.
func progressBackoff(attempt int, capDelay time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 30 { // guard the shift; long past the cap anyway
		return capDelay
	}
	d := progressBackoffBase << uint(attempt-1)
	if d <= 0 || d > capDelay {
		return capDelay
	}
	return d
}

// requestSync triggers a reconcile of the Release by POSTing its /sync
// subresource — the same channel the drift loop, CLI, and users use. Here it tells
// the release controller a sync wave became healthy so it advances the gate.
func (c *HealthController) requestSync(ctx context.Context, rel *corev1alpha1.Release) error {
	return c.client.CoreV1alpha1().RESTClient().
		Post().
		Namespace(rel.Namespace).
		Resource("releases").
		Name(rel.Name).
		SubResource("sync").
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
