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
	store               *healthStore
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
		store:               newHealthStore(),
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
		c.store.delete(key)
		return nil
	}
	if err != nil {
		return err
	}
	if !rel.DeletionTimestamp.IsZero() {
		return nil
	}

	// No assessment configured: report Healthy — everything applied, nothing to
	// check. Drop any stale store entries.
	if rel.Spec.HealthAssessment == nil {
		c.store.delete(key)
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
		// Healthy=Unknown and re-check later, preserving the store's anchors. Not a
		// controller error — best-effort, like the drift loop.
		klog.V(4).InfoS("health undetermined", "release", key, "reason", recErr.Error())
		if err := c.writeHealth(ctx, rel, metav1.ConditionUnknown, reasonUnknown, recErr.Error(),
			toWireSummary(rel.Status.HealthSummary)); err != nil {
			return err
		}
		c.scheduleRecheck(key, rel)
		return nil
	}

	status, reason, message := conditionFor(result)
	if err := c.writeHealth(ctx, rel, status, reason, message, result.Summarize()); err != nil {
		return err
	}
	c.scheduleRecheck(key, rel)
	return nil
}

// assess renders the Release's desired objects, reads them live, and evaluates
// each against the applicable rules, updating the store and returning the result.
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
	releaseKey := rel.Namespace + "/" + rel.Name

	result := &healthresult.Result{Objects: make([]healthresult.ObjectHealth, 0, len(objects))}
	entries := make(map[string]healthEntry, len(objects))
	for _, obj := range objects {
		entry := c.assessObject(ctx, evaluator, dyn, mapper, releaseKey, obj, deadline, now)
		entries[entry.ref.key()] = entry
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
	c.store.replace(releaseKey, entries)
	return result, nil
}

// assessObject evaluates a single managed object's health.
func (c *HealthController) assessObject(ctx context.Context, evaluator *health.Evaluator, dyn dynamic.Interface, mapper meta.RESTMapper, releaseKey string, obj *unstructured.Unstructured, deadline *time.Duration, now time.Time) healthEntry {
	gvk := obj.GroupVersionKind()
	ref := objectRef{gvk: gvk, namespace: obj.GetNamespace(), name: obj.GetName()}

	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return healthEntry{ref: ref, state: health.StateUnknown, message: fmt.Sprintf("cannot resolve resource for %s: %v", gvk, err)}
	}
	resource := mapping.Resource.Resource

	// No applicable rule → Healthy without a live read.
	if !evaluator.HasRules(obj.GetAPIVersion(), []string{resource}) {
		return healthEntry{ref: ref, state: health.StateHealthy, message: "no applicable health rules"}
	}

	ri := resourceClient(dyn, mapping, obj.GetNamespace())
	live, err := ri.Get(ctx, obj.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return healthEntry{ref: ref, state: health.StateUnknown, message: "resource not found in cluster"}
	}
	if err != nil {
		return healthEntry{ref: ref, state: health.StateUnknown, message: fmt.Sprintf("reading live resource: %v", err)}
	}

	assessment := evaluator.Assess(live, []string{resource})
	state, anchor := health.DeriveState(assessment.Passed, assessment.Err, c.store.anchor(releaseKey, ref.key()), deadline, now)
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
// /health subresource, or nil if it has not been assessed.
func (c *HealthController) HealthResult(releaseKey string) *healthresult.Result {
	return c.store.result(releaseKey)
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
