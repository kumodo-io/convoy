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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	extensionsv1alpha1 "github.com/kumodo-io/convoy/api/extensions/v1alpha1"
	clientset "github.com/kumodo-io/convoy/api/generated/clientset/versioned"
	coreinformers "github.com/kumodo-io/convoy/api/generated/informers/externalversions/core/v1alpha1"
	corelisters "github.com/kumodo-io/convoy/api/generated/listers/core/v1alpha1"
	extensionslisters "github.com/kumodo-io/convoy/api/generated/listers/extensions/v1alpha1"
	"github.com/kumodo-io/convoy/internal/auth"
	"github.com/kumodo-io/convoy/internal/clusterconn"
	"github.com/kumodo-io/convoy/internal/controller/base"
	"github.com/kumodo-io/convoy/internal/fetch"
	"github.com/kumodo-io/convoy/internal/health"
	"github.com/kumodo-io/convoy/internal/healthresult"
	"github.com/kumodo-io/convoy/internal/waves"
)

// HealthController runs the health-assessment loop, keyed on Release. It is the
// observer's second loop, running alongside the drift Controller in the same
// process and sharing the render + cluster-connection paths. It evaluates each
// Release's managed objects against the health rule sets its selectors pick, and
// records the Healthy condition + health summary.
type HealthController struct {
	*base.Controller
	client              clientset.Interface
	releaseLister       corelisters.ReleaseLister
	clusterLister       corelisters.ClusterLister
	coordinator         fetch.Coordinator
	resolver            auth.SecretResolver
	connector           *clusterconn.Connector
	globalRuleSetLister extensionslisters.GlobalHealthRuleSetLister
	ruleSetLister       extensionslisters.HealthRuleSetLister
	inventoryLister     corelisters.ReleaseInventoryLister
	// now is injectable for tests.
	now func() metav1.Time
	// assessFn is a seam: it defaults to the real assessment and is stubbed in
	// tests so syncHandler can be exercised without a live cluster.
	assessFn func(context.Context, *corev1alpha1.Release) (*healthresult.Result, error)
}

// NewHealthController wires the observer's health loop.
func NewHealthController(
	client clientset.Interface,
	informer coreinformers.ReleaseInformer,
	clusterLister corelisters.ClusterLister,
	coordinator fetch.Coordinator,
	resolver auth.SecretResolver,
	connector *clusterconn.Connector,
	globalRuleSetLister extensionslisters.GlobalHealthRuleSetLister,
	ruleSetLister extensionslisters.HealthRuleSetLister,
	inventoryLister corelisters.ReleaseInventoryLister,
) *HealthController {
	c := &HealthController{
		client:              client,
		releaseLister:       informer.Lister(),
		clusterLister:       clusterLister,
		coordinator:         coordinator,
		resolver:            resolver,
		connector:           connector,
		globalRuleSetLister: globalRuleSetLister,
		ruleSetLister:       ruleSetLister,
		inventoryLister:     inventoryLister,
		now:                 metav1.Now,
	}
	c.assessFn = c.assess
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

	// No assessment configured: report Healthy — everything applied, nothing to
	// check. Per the "no applicable rules → healthy" rule, every applied wave is
	// healthy too (nothing gates it); the controller treats a release without a
	// health assessment as satisfying every wave's health bar, so gating still
	// advances without the observer writing per-resource health.
	if rel.Spec.HealthAssessment == nil {
		if err := c.writeHealth(ctx, rel, metav1.ConditionTrue, reasonNotConfigured,
			"no health assessment configured", healthresult.Summary{}); err != nil {
			return err
		}
		c.scheduleRecheck(key, rel)
		return nil
	}

	result, recErr := c.assessFn(ctx, rel)
	if recErr != nil {
		// Couldn't assess (source/cluster unreachable, invalid rules): report
		// Healthy=Unknown and re-check later, preserving the persisted anchors. Not
		// a controller error — best-effort, like the drift loop.
		klog.V(4).InfoS("health undetermined", "release", key, "reason", recErr.Error())
		if err := c.writeHealth(ctx, rel, metav1.ConditionUnknown, reasonUnknown, recErr.Error(),
			toWireSummary(rel.Status.HealthSummary)); err != nil {
			return err
		}
		c.scheduleRecheck(key, rel)
		return nil
	}

	// assess has already persisted per-resource health onto the ReleaseInventory
	// (the controller rolls it up for the sync-wave gate). Here we only publish the
	// aggregate Healthy condition + summary on the Release.
	status, reason, message := conditionFor(result)
	if err := c.writeHealth(ctx, rel, status, reason, message, result.Summarize()); err != nil {
		return err
	}
	c.scheduleRecheck(key, rel)
	return nil
}

// appliedWaves returns the set of wave numbers the controller has marked applied
// in status.waves. The observer assesses per-wave health only for applied waves;
// an unapplied wave's objects aren't in the cluster yet, so its health is not
// meaningful and is left unwritten.
func appliedWaves(rel *corev1alpha1.Release) map[int64]bool {
	out := map[int64]bool{}
	for i := range rel.Status.Waves {
		w := rel.Status.Waves[i]
		if w.Applied != nil && *w.Applied {
			out[w.Number] = true
		}
	}
	return out
}

// assess renders the Release's desired objects, reads them live, evaluates each
// against the applicable rules, persists per-resource health onto the
// ReleaseInventory, and returns the aggregate result.
func (c *HealthController) assess(ctx context.Context, rel *corev1alpha1.Release) (*healthresult.Result, error) {
	rules, err := c.collectRules(rel)
	if err != nil {
		return nil, err
	}
	evaluator, err := health.NewEvaluator(rules)
	if err != nil {
		return nil, fmt.Errorf("compiling health rules: %w", err)
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

	objects, err := renderReleaseObjects(ctx, c.coordinator, c.resolver, rel, cluster)
	if err != nil {
		return nil, err
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

	// Prior progress anchors come from the durable ReleaseInventory (they survive a
	// restart, so an in-flight progress clock isn't reset). appliedWaves bounds
	// which resources get their health persisted: only resources in an applied wave
	// are in the cluster and co-owned with the controller's inventory entry.
	anchors := c.inventoryAnchors(rel)
	applied := appliedWaves(rel)

	result := &healthresult.Result{Objects: make([]healthresult.ObjectHealth, 0, len(objects))}
	var writes []healthEntry
	for _, obj := range objects {
		entry := c.assessObject(ctx, evaluator, dyn, mapper, obj, anchors, deadline, now)
		result.Objects = append(result.Objects, healthresult.ObjectHealth{
			Group:     entry.ref.gvk.Group,
			Version:   entry.ref.gvk.Version,
			Kind:      entry.ref.gvk.Kind,
			Namespace: entry.ref.namespace,
			Name:      entry.ref.name,
			State:     healthresult.State(entry.state),
			Message:   entry.message,
		})
		if applied[int64(waves.WaveOf(obj))] {
			writes = append(writes, entry)
		}
	}
	if err := c.writeInventoryHealth(ctx, rel, writes); err != nil {
		return nil, err
	}
	return result, nil
}

// assessObject evaluates a single managed object's health. The returned ref
// carries the resolved namespace (defaulted for namespaced kinds) so its identity
// matches the controller's ReleaseInventory entry for co-ownership.
func (c *HealthController) assessObject(ctx context.Context, evaluator *health.Evaluator, dyn dynamic.Interface, mapper meta.RESTMapper, obj *unstructured.Unstructured, anchors map[string]time.Time, deadline *time.Duration, now time.Time) healthEntry {
	gvk := obj.GroupVersionKind()

	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		ref := objectRef{gvk: gvk, namespace: obj.GetNamespace(), name: obj.GetName()}
		return healthEntry{ref: ref, state: corev1alpha1.ResourceStateUnknown, message: fmt.Sprintf("cannot resolve resource for %s: %v", gvk, err)}
	}
	resource := mapping.Resource.Resource

	namespace := obj.GetNamespace()
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace && namespace == "" {
		namespace = metav1.NamespaceDefault
	}
	ref := objectRef{gvk: gvk, namespace: namespace, name: obj.GetName()}

	// No applicable rule → Healthy without a live read.
	if !evaluator.HasRules(obj.GetAPIVersion(), []string{resource}) {
		return healthEntry{ref: ref, state: corev1alpha1.ResourceStateHealthy, message: "no applicable health rules"}
	}

	live, err := resourceClient(dyn, mapping, namespace).Get(ctx, obj.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return healthEntry{ref: ref, state: corev1alpha1.ResourceStateUnknown, message: "resource not found in cluster"}
	}
	if err != nil {
		return healthEntry{ref: ref, state: corev1alpha1.ResourceStateUnknown, message: fmt.Sprintf("reading live resource: %v", err)}
	}

	assessment := evaluator.Assess(live, []string{resource})
	state, anchor := health.DeriveState(assessment.Passed, assessment.Err, anchors[ref.identityID()], deadline, now)
	message := assessment.Message
	if assessment.Err != nil {
		message = assessment.Err.Error()
	}
	return healthEntry{ref: ref, state: state, message: message, firstUnhealthy: anchor}
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

// scheduleRecheck schedules the next health re-check at the Release's sync
// interval, if set; otherwise the informer resync drives re-checks.
func (c *HealthController) scheduleRecheck(key string, rel *corev1alpha1.Release) {
	if d := rel.Spec.SyncPolicy.Interval.Duration; d > 0 {
		c.EnqueueAfter(key, d)
	}
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
