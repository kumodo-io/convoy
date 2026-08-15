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

// Package gitrepository implements the controller that reconciles
// [github.com/kumodo-io/convoy/api/source/v1alpha1.GitRepository] objects:
// it verifies the repository is reachable and authenticated and records
// that in the object's status. Content for a specific ref is fetched on
// demand by the release controller's fetch coordinator (the ref lives on
// the Release, not the GitRepository).
package gitrepository

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

// Controller reconciles GitRepository objects. The work-queue, workers, and
// retry machinery live in the embedded base.Controller; this type supplies only
// the GitRepository-specific reconcile logic.
type Controller struct {
	*base.Controller
	client  clientset.Interface
	lister  listers.GitRepositoryLister
	fetcher Fetcher
	// now is injectable for tests; defaults to metav1.Now.
	now func() metav1.Time
}

// NewController wires a Controller to a GitRepository informer. It verifies
// reachability/auth via the fetcher; it does not fetch content.
func NewController(client clientset.Interface, informer informers.GitRepositoryInformer, fetcher Fetcher, opts ...base.Option) *Controller {
	c := &Controller{
		client:  client,
		lister:  informer.Lister(),
		fetcher: fetcher,
		now:     metav1.Now,
	}
	c.Controller = base.NewController("gitrepository", c.syncHandler, opts...)
	utilruntime.Must(c.WatchInformer(informer.Informer()))
	return c
}

// syncHandler reconciles a single GitRepository: verify reachability/auth and
// write status.
func (c *Controller) syncHandler(ctx context.Context, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key %q: %w", key, err))
		return nil // unrecoverable; don't requeue
	}

	repo, err := c.lister.GitRepositories(namespace).Get(name)
	if apierrors.IsNotFound(err) {
		return nil // deleted; nothing to do
	}
	if err != nil {
		return err
	}

	verifyErr := c.fetcher.Verify(ctx, repo)
	newStatus := computeStatus(repo, verifyErr, c.now())

	if statusChanged(repo, newStatus) {
		updated := repo.DeepCopy()
		updated.Status = newStatus
		if _, err := c.client.SourceV1alpha1().GitRepositories(namespace).UpdateStatus(ctx, updated, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("updating status: %w", err)
		}
		klog.V(4).InfoS("updated gitrepository status", "gitrepository", key)
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
