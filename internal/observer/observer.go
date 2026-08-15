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

// Package observer implements the drift-detection loop of the convoy observer.
// Per Release it renders the desired state, reads the live target-cluster state,
// compares them, and records the InSync condition. In automatic mode it also
// requests a sync so the controller-manager re-applies; in manual mode it only
// reports drift. It reuses internal/render for rendering and clusterconn to reach
// the target cluster — it never applies anything itself.
package observer

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	clientset "github.com/kumodo-io/convoy/api/generated/clientset/versioned"
	coreinformers "github.com/kumodo-io/convoy/api/generated/informers/externalversions/core/v1alpha1"
	corelisters "github.com/kumodo-io/convoy/api/generated/listers/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/auth"
	"github.com/kumodo-io/convoy/internal/blobstore"
	"github.com/kumodo-io/convoy/internal/clusterconn"
	"github.com/kumodo-io/convoy/internal/controller/base"
	"github.com/kumodo-io/convoy/internal/diffresult"
	"github.com/kumodo-io/convoy/internal/fetch"
	"github.com/kumodo-io/convoy/internal/healthresult"
	"github.com/kumodo-io/convoy/internal/render"
	"github.com/kumodo-io/convoy/internal/renderstore"
)

// Controller runs the drift-detection loop, keyed on Release. It embeds the
// shared base.Controller for the work queue, workers, and periodic resync.
type Controller struct {
	*base.Controller
	client        clientset.Interface
	releaseLister corelisters.ReleaseLister
	clusterLister corelisters.ClusterLister
	coordinator   fetch.Coordinator
	resolver      auth.SecretResolver
	connector     *clusterconn.Connector
	// renderStore serves the store-backed read features: /history (the deploy log)
	// and historical /diff (a stored render vs live). nil disables them (they
	// return ServiceUnavailable).
	renderStore *renderstore.RenderStore
	// owns reports whether this replica owns a Release's target-cluster shard.
	// nil disables sharding.
	owns func(clusterNamespace, clusterName string) bool
	// now is injectable for tests; defaults to metav1.Now.
	now func() metav1.Time
	// assessFn and syncFn are seams: they default to the real drift assessment and
	// /sync request, and are stubbed in tests so syncHandler can be exercised
	// without a live target cluster.
	assessFn func(context.Context, *corev1alpha1.Release) (bool, error)
	syncFn   func(context.Context, *corev1alpha1.Release) error
	// healthResults, when set, serves the /health subresource from the health
	// loop's store. The observer's HTTP server hosts /preview, /diff (drift) and
	// /health (health) together; the manager wires this to the HealthController.
	healthResults func(releaseKey string) *healthresult.Result
}

// SetHealthResults wires the /health lookup (the HealthController's store) into
// the observer's HTTP server.
func (c *Controller) SetHealthResults(fn func(releaseKey string) *healthresult.Result) {
	c.healthResults = fn
}

// NewController wires an observer Controller to a Release informer. It renders
// via the coordinator + resolver and reaches target clusters via the connector.
func NewController(
	client clientset.Interface,
	informer coreinformers.ReleaseInformer,
	clusterLister corelisters.ClusterLister,
	coordinator fetch.Coordinator,
	resolver auth.SecretResolver,
	connector *clusterconn.Connector,
	renderStore *renderstore.RenderStore,
	owns func(clusterNamespace, clusterName string) bool,
) *Controller {
	c := &Controller{
		client:        client,
		releaseLister: informer.Lister(),
		clusterLister: clusterLister,
		coordinator:   coordinator,
		resolver:      resolver,
		connector:     connector,
		renderStore:   renderStore,
		owns:          owns,
		now:           metav1.Now,
	}
	c.assessFn = c.assess
	c.syncFn = c.requestSync
	c.Controller = base.NewController("observer", c.syncHandler,
		base.WithEnqueueFilter(func(obj any) bool { return releaseOwned(c.owns, obj) }))
	utilruntime.Must(c.WatchInformer(informer.Informer()))
	return c
}

// syncHandler runs one drift pass for a Release: render desired, compare to live,
// record InSync, and (automatic mode) request a sync on drift.
func (c *Controller) syncHandler(ctx context.Context, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key %q: %w", key, err))
		return nil
	}

	rel, err := c.releaseLister.Releases(namespace).Get(name)
	if apierrors.IsNotFound(err) {
		return nil // deleted
	}
	if err != nil {
		return err
	}
	if c.owns != nil && !c.owns(clusterKeyOf(rel)) {
		return nil // another replica owns this Release's cluster shard
	}
	if !rel.DeletionTimestamp.IsZero() {
		return nil // being finalized; nothing to observe
	}

	drifted, recErr := c.assessFn(ctx, rel)
	if recErr != nil {
		// Drift can't be determined yet — the source or target cluster isn't present
		// or reachable, or the config is unsupported. These are expected,
		// self-resolving states, not controller errors: report InSync=Unknown and
		// re-check on the next interval/resync. Returning the error here would log
		// loudly ("Unhandled Error") and hot-retry, which for a best-effort drift
		// reporter is just noise.
		klog.V(4).InfoS("drift undetermined", "release", key, "reason", recErr.Error())
		if err := c.writeInSync(ctx, rel, metav1.ConditionUnknown, reasonUndetermined, recErr.Error()); err != nil {
			return err // the status write itself failing IS a real error worth retrying
		}
		c.scheduleRecheck(key, rel)
		return nil
	}

	status, reason, message := metav1.ConditionTrue, reasonInSync, "live state matches the desired render"
	if drifted {
		status, reason, message = metav1.ConditionFalse, reasonDrifted, "live state has drifted from the desired render"
	}
	if err := c.writeInSync(ctx, rel, status, reason, message); err != nil {
		return err
	}

	// Auto-correct only in automatic mode; manual mode reports drift and waits.
	if drifted && rel.Spec.SyncPolicy.Automatic {
		if err := c.syncFn(ctx, rel); err != nil {
			return fmt.Errorf("requesting sync for %s: %w", key, err)
		}
		klog.V(4).InfoS("requested sync on detected drift", "release", key)
	}

	c.scheduleRecheck(key, rel)
	return nil
}

// assess renders the desired state and compares it to the live cluster. A non-nil
// error means drift could not be determined (transient fetch/connect, or a
// terminal config problem); the boolean is only meaningful when err is nil.
func (c *Controller) assess(ctx context.Context, rel *corev1alpha1.Release) (bool, error) {
	result, err := c.diff(ctx, rel)
	if err != nil {
		return false, err
	}
	return result.HasDrift(), nil
}

// diff renders the desired state, reads the live cluster, and computes the
// structured per-object diff. It is the engine behind both the drift loop (via
// assess → HasDrift) and the on-demand /diff subresource, so the two never
// disagree. A non-nil error means the diff couldn't be produced (source/cluster
// absent or unreachable, unsupported config).
func (c *Controller) diff(ctx context.Context, rel *corev1alpha1.Release) (*diffresult.Result, error) {
	objects, err := c.renderDesired(ctx, rel)
	if err != nil {
		return nil, err
	}
	return c.diffObjects(ctx, rel, objects)
}

// diffStored diffs a stored (past) render against the live cluster: "what would
// change if this Release were rolled back to renderRef". It reads the render from
// the store rather than re-rendering source, so it works even when source has
// moved on. Requires a render store.
func (c *Controller) diffStored(ctx context.Context, rel *corev1alpha1.Release, renderRef string) (*diffresult.Result, error) {
	if c.renderStore == nil {
		return nil, fmt.Errorf("historical diff requires a render store (none configured)")
	}
	data, err := c.renderStore.GetRender(ctx, rel.Namespace, rel.Name, renderRef)
	if errors.Is(err, blobstore.ErrNotFound) {
		return nil, fmt.Errorf("render %s not found in store for %s/%s", renderRef, rel.Namespace, rel.Name)
	}
	if err != nil {
		return nil, err
	}
	objects, err := render.ParseManifest(data)
	if err != nil {
		return nil, fmt.Errorf("parsing stored render %s: %w", renderRef, err)
	}
	return c.diffObjects(ctx, rel, objects)
}

// diffObjects resolves the target cluster and diffs the given desired objects
// against its live state.
func (c *Controller) diffObjects(ctx context.Context, rel *corev1alpha1.Release, objects []*unstructured.Unstructured) (*diffresult.Result, error) {
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
	return computeDiff(ctx, dyn, mapper, objects)
}

// renderDesired renders the Release's desired objects via the shared helper,
// resolving the target cluster (best-effort) for the mutation templating context.
func (c *Controller) renderDesired(ctx context.Context, rel *corev1alpha1.Release) ([]*unstructured.Unstructured, error) {
	return renderReleaseObjects(ctx, c.coordinator, c.resolver, rel, resolveClusterRef(c.clusterLister, rel))
}

// scheduleRecheck schedules the next drift re-check at the Release's sync
// interval, if one is set. With no interval, the informer resync heartbeat is the
// only re-check driver. The observer never hot-retries: drift detection is
// best-effort and periodic, so undetermined and steady states re-check on the
// same cadence.
func (c *Controller) scheduleRecheck(key string, rel *corev1alpha1.Release) {
	if d := rel.Spec.SyncPolicy.Interval.Duration; d > 0 {
		c.EnqueueAfter(key, d)
	}
}
