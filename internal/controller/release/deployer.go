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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/clusterconn"
	"github.com/kumodo-io/convoy/internal/inventory"
)

// fieldManager identifies convoy as the owner of the fields it applies.
const fieldManager = "convoy"

// Deployer applies rendered objects to a target cluster and prunes resources by
// reference. It is the seam between the reconcile loop and the target cluster,
// so the controller can be unit-tested with a fake.
type Deployer interface {
	// Deploy server-side-applies objects and returns the refs it applied, with
	// namespaces resolved exactly as placed (so the returned set is authoritative
	// for inventory + pruning).
	Deploy(ctx context.Context, cluster *corev1alpha1.Cluster, objects []*unstructured.Unstructured) ([]inventory.Ref, error)
	// Prune deletes the given resources (ignoring already-absent ones).
	Prune(ctx context.Context, cluster *corev1alpha1.Cluster, refs []inventory.Ref) error
}

// connectorDeployer connects via the shared connector and server-side-applies.
type connectorDeployer struct {
	connector *clusterconn.Connector
}

// NewDeployer returns a Deployer that connects through the cluster connector.
func NewDeployer(connector *clusterconn.Connector) Deployer {
	return &connectorDeployer{connector: connector}
}

func (d *connectorDeployer) Deploy(ctx context.Context, cluster *corev1alpha1.Cluster, objects []*unstructured.Unstructured) ([]inventory.Ref, error) {
	dyn, mapper, err := d.clients(ctx, cluster)
	if err != nil {
		return nil, err
	}

	refs := make([]inventory.Ref, 0, len(objects))
	for _, obj := range objects {
		ref, err := apply(ctx, dyn, mapper, obj)
		if err != nil {
			return nil, fmt.Errorf("applying %s %q: %w", obj.GetKind(), obj.GetName(), err)
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func (d *connectorDeployer) Prune(ctx context.Context, cluster *corev1alpha1.Cluster, refs []inventory.Ref) error {
	if len(refs) == 0 {
		return nil
	}
	dyn, mapper, err := d.clients(ctx, cluster)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if err := prune(ctx, dyn, mapper, ref); err != nil {
			return fmt.Errorf("pruning %s %q: %w", ref.Kind, ref.Name, err)
		}
	}
	return nil
}

// clients connects to the target cluster and builds a dynamic client + RESTMapper.
func (d *connectorDeployer) clients(ctx context.Context, cluster *corev1alpha1.Cluster) (dynamic.Interface, meta.RESTMapper, error) {
	return d.connector.DynamicClients(ctx, cluster)
}

// resourceFor resolves obj's GVK to a namespaced-or-cluster resource client and
// returns the inventory.Ref describing where it was placed.
func resourceFor(dyn dynamic.Interface, mapper meta.RESTMapper, gvk schema.GroupVersionKind, namespace, name string) (dynamic.ResourceInterface, inventory.Ref, error) {
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, inventory.Ref{}, fmt.Errorf("rest mapping for %s: %w", gvk, err)
	}
	ref := inventory.Ref{Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind, Name: name}
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if namespace == "" {
			namespace = metav1.NamespaceDefault
		}
		ref.Namespace = namespace
		return dyn.Resource(mapping.Resource).Namespace(namespace), ref, nil
	}
	return dyn.Resource(mapping.Resource), ref, nil
}

// apply server-side-applies a single object and returns the ref it placed.
func apply(ctx context.Context, dyn dynamic.Interface, mapper meta.RESTMapper, obj *unstructured.Unstructured) (inventory.Ref, error) {
	ri, ref, err := resourceFor(dyn, mapper, obj.GroupVersionKind(), obj.GetNamespace(), obj.GetName())
	if err != nil {
		return inventory.Ref{}, err
	}
	data, err := obj.MarshalJSON()
	if err != nil {
		return inventory.Ref{}, fmt.Errorf("marshaling object: %w", err)
	}
	applied, err := ri.Patch(ctx, obj.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: fieldManager,
		Force:        new(true),
	})
	if err != nil {
		return inventory.Ref{}, err
	}
	ref.UID = applied.GetUID()
	return ref, nil
}

// prune deletes a single resource, treating an already-absent object as success.
func prune(ctx context.Context, dyn dynamic.Interface, mapper meta.RESTMapper, ref inventory.Ref) error {
	gvk := schema.GroupVersionKind{Group: ref.Group, Version: ref.Version, Kind: ref.Kind}
	ri, _, err := resourceFor(dyn, mapper, gvk, ref.Namespace, ref.Name)
	if err != nil {
		return err
	}
	if err := ri.Delete(ctx, ref.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}
