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
	"github.com/kumodo-io/convoy/internal/artifact"
	"github.com/kumodo-io/convoy/internal/clusterconn"
	"github.com/kumodo-io/convoy/internal/controller/base"
)

// sourceKindGitRepository is the only source kind supported for now.
const sourceKindGitRepository = "GitRepository"

// Controller reconciles Release objects: render source content → apply to the
// target cluster → record status.
type Controller struct {
	*base.Controller
	client        clientset.Interface
	releaseLister corelisters.ReleaseLister
	clusterLister corelisters.ClusterLister
	store         artifact.Store
	deployer      Deployer
	// now is injectable for tests; defaults to metav1.Now.
	now func() metav1.Time
}

// NewController wires a Controller to a Release informer. It reads source
// artifacts from store and target clusters from clusterLister.
func NewController(
	client clientset.Interface,
	informer coreinformers.ReleaseInformer,
	clusterLister corelisters.ClusterLister,
	store artifact.Store,
	deployer Deployer,
) *Controller {
	c := &Controller{
		client:        client,
		releaseLister: informer.Lister(),
		clusterLister: clusterLister,
		store:         store,
		deployer:      deployer,
		now:           metav1.Now,
	}
	c.Controller = base.NewController("release", c.syncHandler)
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

	ready, reason, message, recErr := c.reconcile(ctx, rel)
	newStatus := computeStatus(rel, c.now(), ready, reason, message)

	if statusChanged(rel, newStatus) {
		updated := rel.DeepCopy()
		updated.Status = newStatus
		updated.Status.ReconciledAt = new(c.now())
		if _, err := c.client.CoreV1alpha1().Releases(namespace).UpdateStatus(ctx, updated, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("updating status: %w", err)
		}
		klog.V(4).InfoS("updated release status", "release", key, "ready", ready, "reason", reason)
	}

	// recErr != nil → transient, rate-limited retry. recErr == nil → terminal
	// (success or an Unsupported configuration), let resync re-check.
	return recErr
}

// reconcile runs the render-and-apply steps and returns the Ready outcome plus a
// requeue error (non-nil = transient, retry).
func (c *Controller) reconcile(ctx context.Context, rel *corev1alpha1.Release) (ready bool, reason, message string, err error) {
	if rel.Spec.Source.Kind != sourceKindGitRepository {
		return false, conditionReasonUnsupported, fmt.Sprintf("source kind %q is not supported yet (only %q)", rel.Spec.Source.Kind, sourceKindGitRepository), nil
	}

	sourceNamespace := orDefault(rel.Spec.Source.Namespace, rel.Namespace)
	art, ok := c.store.Get(artifact.Key(sourceKindGitRepository, sourceNamespace, rel.Spec.Source.Name))
	if !ok {
		return false, conditionReasonSourceNotReady, "waiting for source artifact", fmt.Errorf("source artifact for %s/%s not available yet", sourceNamespace, rel.Spec.Source.Name)
	}

	clusterNamespace := orDefault(rel.Spec.ClusterRef.Namespace, rel.Namespace)
	cluster, err := c.clusterLister.Clusters(clusterNamespace).Get(rel.Spec.ClusterRef.Name)
	if apierrors.IsNotFound(err) {
		return false, conditionReasonClusterNotReady, fmt.Sprintf("target cluster %s/%s not found", clusterNamespace, rel.Spec.ClusterRef.Name), fmt.Errorf("cluster not found")
	}
	if err != nil {
		return false, conditionReasonClusterNotReady, err.Error(), err
	}
	if !cluster.Status.Reachable {
		return false, conditionReasonClusterNotReady, "target cluster is not reachable", fmt.Errorf("cluster %s not reachable", cluster.Name)
	}

	objects, err := render(rel.Spec.Content, art)
	if err != nil {
		if errors.Is(err, ErrUnsupported) {
			return false, conditionReasonUnsupported, err.Error(), nil // terminal
		}
		return false, conditionReasonDeployFailed, err.Error(), err
	}

	if err := c.deployer.Deploy(ctx, cluster, objects); err != nil {
		if errors.Is(err, ErrUnsupported) || errors.Is(err, clusterconn.ErrUnsupported) {
			return false, conditionReasonUnsupported, err.Error(), nil // terminal
		}
		return false, conditionReasonDeployFailed, err.Error(), err
	}

	return true, conditionReasonApplied, fmt.Sprintf("applied %d object(s) from revision %s", len(objects), art.Revision), nil
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
