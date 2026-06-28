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

package helmregistry

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	clientset "github.com/kumodo-io/convoy/api/generated/clientset/versioned"
	informers "github.com/kumodo-io/convoy/api/generated/informers/externalversions/source/v1alpha1"
	listers "github.com/kumodo-io/convoy/api/generated/listers/source/v1alpha1"
	"github.com/kumodo-io/convoy/internal/controller/base"
)

// Controller reconciles HelmRegistry objects. The work-queue, workers, and retry
// machinery live in the embedded base.Controller; this type supplies only the
// HelmRegistry-specific reconcile logic.
type Controller struct {
	*base.Controller
	client  clientset.Interface
	lister  listers.HelmRegistryLister
	fetcher Fetcher
	// now is injectable for tests; defaults to metav1.Now.
	now func() metav1.Time
}

// NewController wires a Controller to a HelmRegistry informer. It verifies
// reachability via the fetcher; it does not fetch content.
func NewController(client clientset.Interface, informer informers.HelmRegistryInformer, fetcher Fetcher) *Controller {
	c := &Controller{
		client:  client,
		lister:  informer.Lister(),
		fetcher: fetcher,
		now:     metav1.Now,
	}
	c.Controller = base.NewController("helmregistry", c.syncHandler)
	utilruntime.Must(c.WatchInformer(informer.Informer()))
	return c
}

// syncHandler reconciles a single HelmRegistry: verify reachability and write status.
func (c *Controller) syncHandler(ctx context.Context, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key %q: %w", key, err))
		return nil // unrecoverable; don't requeue
	}

	reg, err := c.lister.HelmRegistries(namespace).Get(name)
	if apierrors.IsNotFound(err) {
		return nil // deleted; nothing to do
	}
	if err != nil {
		return err
	}

	verifyErr := c.fetcher.Verify(ctx, reg)
	newStatus := computeStatus(reg, verifyErr, c.now())

	if statusChanged(reg, newStatus) {
		updated := reg.DeepCopy()
		updated.Status = newStatus
		if _, err := c.client.SourceV1alpha1().HelmRegistries(namespace).UpdateStatus(ctx, updated, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("updating status: %w", err)
		}
		klog.V(4).InfoS("updated helmregistry status", "helmregistry", key)
	}

	// A transient verify failure is reported as an error so the rate-limited queue
	// retries it. Periodic re-verification comes from the informer's resync.
	if verifyErr != nil {
		if errors.Is(verifyErr, ErrUnsupported) {
			return nil // terminal; don't hot-loop
		}
		return verifyErr
	}
	return nil
}
