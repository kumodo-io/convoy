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

package release

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/clusterconn"
)

// HookRunner creates and deletes lifecycle-hook workloads on a target cluster. It
// is the seam between the reconcile loop and the target for hook execution (the
// controller creates; the observer polls), so the controller can be unit-tested
// with a fake. Cleanup/deleteBeforeCreation delete by label selector because
// cross-cluster ownerRefs don't resolve.
type HookRunner interface {
	// Create creates the hook workload and returns the created object with the
	// server-resolved namespace/UID (so the controller can record where it landed).
	// An already-existing workload (idempotent re-arm within a render) is fetched
	// and returned rather than erroring.
	Create(ctx context.Context, cluster *corev1alpha1.Cluster, workload *unstructured.Unstructured) (*unstructured.Unstructured, error)
	// Delete deletes a single workload by name, tolerating an already-absent one.
	Delete(ctx context.Context, cluster *corev1alpha1.Cluster, gvk schema.GroupVersionKind, namespace, name string) error
	// DeleteMatching deletes every workload of the GVK matching the label selector
	// (deleteBeforeCreation across renders), tolerating none-present.
	DeleteMatching(ctx context.Context, cluster *corev1alpha1.Cluster, gvk schema.GroupVersionKind, namespace, labelSelector string) error
}

// connectorHookRunner reaches the target cluster through the shared connector.
type connectorHookRunner struct {
	connector *clusterconn.Connector
}

// NewHookRunner returns a HookRunner that connects through the cluster connector.
func NewHookRunner(connector *clusterconn.Connector) HookRunner {
	return &connectorHookRunner{connector: connector}
}

func (r *connectorHookRunner) Create(ctx context.Context, cluster *corev1alpha1.Cluster, workload *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	dyn, mapper, err := r.connector.DynamicClients(ctx, cluster)
	if err != nil {
		return nil, err
	}
	ri, _, err := resourceFor(dyn, mapper, workload.GroupVersionKind(), workload.GetNamespace(), workload.GetName())
	if err != nil {
		return nil, err
	}
	created, err := ri.Create(ctx, workload, metav1.CreateOptions{FieldManager: fieldManager})
	if apierrors.IsAlreadyExists(err) {
		return ri.Get(ctx, workload.GetName(), metav1.GetOptions{})
	}
	if err != nil {
		return nil, fmt.Errorf("creating hook workload %s %q: %w", workload.GetKind(), workload.GetName(), err)
	}
	return created, nil
}

func (r *connectorHookRunner) Delete(ctx context.Context, cluster *corev1alpha1.Cluster, gvk schema.GroupVersionKind, namespace, name string) error {
	dyn, mapper, err := r.connector.DynamicClients(ctx, cluster)
	if err != nil {
		return err
	}
	ri, _, err := resourceFor(dyn, mapper, gvk, namespace, name)
	if err != nil {
		return err
	}
	background := metav1.DeletePropagationBackground
	if err := ri.Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &background}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting hook workload %s %q: %w", gvk.Kind, name, err)
	}
	return nil
}

func (r *connectorHookRunner) DeleteMatching(ctx context.Context, cluster *corev1alpha1.Cluster, gvk schema.GroupVersionKind, namespace, labelSelector string) error {
	dyn, mapper, err := r.connector.DynamicClients(ctx, cluster)
	if err != nil {
		return err
	}
	ri, _, err := resourceFor(dyn, mapper, gvk, namespace, "")
	if err != nil {
		return err
	}
	background := metav1.DeletePropagationBackground
	if err := ri.DeleteCollection(ctx,
		metav1.DeleteOptions{PropagationPolicy: &background},
		metav1.ListOptions{LabelSelector: labelSelector},
	); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting hook workloads %s matching %q: %w", gvk.Kind, labelSelector, err)
	}
	return nil
}
