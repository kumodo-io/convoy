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
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	clientset "github.com/kumodo-io/convoy/api/generated/clientset/versioned"
	coreinformers "github.com/kumodo-io/convoy/api/generated/informers/externalversions/core/v1alpha1"
	corelisters "github.com/kumodo-io/convoy/api/generated/listers/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/auth"
	"github.com/kumodo-io/convoy/internal/clusterconn"
	"github.com/kumodo-io/convoy/internal/controller/base"
	"github.com/kumodo-io/convoy/internal/fetch"
	"github.com/kumodo-io/convoy/internal/render"
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
	// now is injectable for tests; defaults to metav1.Now.
	now func() metav1.Time
	// assessFn and syncFn are seams: they default to the real drift assessment and
	// /sync request, and are stubbed in tests so syncHandler can be exercised
	// without a live target cluster.
	assessFn func(context.Context, *corev1alpha1.Release) (bool, error)
	syncFn   func(context.Context, *corev1alpha1.Release) error
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
) *Controller {
	c := &Controller{
		client:        client,
		releaseLister: informer.Lister(),
		clusterLister: clusterLister,
		coordinator:   coordinator,
		resolver:      resolver,
		connector:     connector,
		now:           metav1.Now,
	}
	c.assessFn = c.assess
	c.syncFn = c.requestSync
	c.Controller = base.NewController("observer", c.syncHandler)
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
	if !rel.DeletionTimestamp.IsZero() {
		return nil // being finalized; nothing to observe
	}

	drifted, recErr := c.assessFn(ctx, rel)
	if recErr != nil {
		// Couldn't determine drift (transient or config problem): mark Unknown and
		// let the requeue retry. Undetermined is not treated as drift, so no /sync.
		if writeErr := c.writeInSync(ctx, rel, metav1.ConditionUnknown, reasonUndetermined, recErr.Error()); writeErr != nil {
			return writeErr
		}
		return c.requeue(key, rel, recErr)
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

	return c.requeue(key, rel, nil)
}

// assess renders the desired state and compares it to the live cluster. A non-nil
// error means drift could not be determined (transient fetch/connect, or a
// terminal config problem); the boolean is only meaningful when err is nil.
func (c *Controller) assess(ctx context.Context, rel *corev1alpha1.Release) (bool, error) {
	res, err := c.coordinator.Ensure(ctx, rel.Spec.Source, rel.Namespace)
	if err != nil {
		return false, err
	}
	if !res.Ready {
		return false, fmt.Errorf("source not ready: %s", res.Message)
	}

	clusterNamespace := rel.Spec.ClusterRef.Namespace
	if clusterNamespace == "" {
		clusterNamespace = rel.Namespace
	}
	cluster, err := c.clusterLister.Clusters(clusterNamespace).Get(rel.Spec.ClusterRef.Name)
	if err != nil {
		return false, fmt.Errorf("resolving target cluster: %w", err)
	}
	if !cluster.Status.Reachable {
		return false, fmt.Errorf("target cluster %s is not reachable", cluster.Name)
	}

	valueLayers, err := c.resolveValueLayers(ctx, rel)
	if err != nil {
		return false, err
	}
	objects, err := render.Render(rel.Spec.Content, res.Artifact, valueLayers)
	if err != nil {
		return false, err
	}

	dyn, mapper, err := c.connector.DynamicClients(ctx, cluster)
	if err != nil {
		return false, err
	}
	return detectDrift(ctx, dyn, mapper, objects)
}

// resolveValueLayers resolves the Helm value sources to ordered raw-YAML layers.
func (c *Controller) resolveValueLayers(ctx context.Context, rel *corev1alpha1.Release) ([][]byte, error) {
	if rel.Spec.Content.Type != corev1alpha1.ReleaseContentTypeHelm || rel.Spec.Content.Helm == nil {
		return nil, nil
	}
	return render.ResolveValues(ctx, rel.Spec.Content.Helm.Values, rel.Namespace, c.resolver, c.coordinator)
}

// requeue schedules the next drift re-check at the Release's sync interval. On a
// transient error it returns the error so the base rate limiter retries instead.
func (c *Controller) requeue(key string, rel *corev1alpha1.Release, recErr error) error {
	if recErr != nil {
		if errors.Is(recErr, render.ErrUnsupported) || errors.Is(recErr, clusterconn.ErrUnsupported) {
			return nil // terminal config problem: don't hammer; resync re-checks
		}
		return recErr // transient: rate-limited retry
	}
	if d := rel.Spec.SyncPolicy.Interval.Duration; d > 0 {
		c.EnqueueAfter(key, d)
	}
	return nil
}
