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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/clusterconn"
)

// fieldManager identifies convoy as the owner of the fields it applies.
const fieldManager = "convoy"

// Deployer applies rendered objects to a target cluster. It is the seam between
// the reconcile loop and the target cluster, so the controller can be
// unit-tested with a fake.
type Deployer interface {
	Deploy(ctx context.Context, cluster *corev1alpha1.Cluster, objects []*unstructured.Unstructured) error
}

// connectorDeployer connects via the shared connector and server-side-applies.
type connectorDeployer struct {
	connector *clusterconn.Connector
}

// NewDeployer returns a Deployer that connects through the cluster connector.
func NewDeployer(connector *clusterconn.Connector) Deployer {
	return &connectorDeployer{connector: connector}
}

func (d *connectorDeployer) Deploy(ctx context.Context, cluster *corev1alpha1.Cluster, objects []*unstructured.Unstructured) error {
	cfg, err := d.connector.Connect(ctx, cluster)
	if err != nil {
		return err // clusterconn.ErrUnsupported / ErrProbeFailed
	}

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("building dynamic client: %w", err)
	}
	disco, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return fmt.Errorf("building discovery client: %w", err)
	}
	groups, err := restmapper.GetAPIGroupResources(disco)
	if err != nil {
		return fmt.Errorf("discovering api groups: %w", err)
	}
	mapper := restmapper.NewDiscoveryRESTMapper(groups)

	for _, obj := range objects {
		if err := apply(ctx, dyn, mapper, obj); err != nil {
			return fmt.Errorf("applying %s %q: %w", obj.GetKind(), obj.GetName(), err)
		}
	}
	return nil
}

// apply server-side-applies a single object, resolving its GVK to a resource and
// honoring namespaced vs cluster-scoped placement.
func apply(ctx context.Context, dyn dynamic.Interface, mapper meta.RESTMapper, obj *unstructured.Unstructured) error {
	gvk := obj.GroupVersionKind()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("rest mapping for %s: %w", gvk, err)
	}

	var ri dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		namespace := obj.GetNamespace()
		if namespace == "" {
			namespace = metav1.NamespaceDefault
		}
		ri = dyn.Resource(mapping.Resource).Namespace(namespace)
	} else {
		ri = dyn.Resource(mapping.Resource)
	}

	data, err := obj.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshaling object: %w", err)
	}
	_, err = ri.Patch(ctx, obj.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: fieldManager,
		Force:        new(true),
	})
	return err
}
