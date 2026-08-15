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

package cluster_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	genericregistrytest "k8s.io/apiserver/pkg/registry/generic/testing"
	"k8s.io/apiserver/pkg/registry/rest"
	etcd3testing "k8s.io/apiserver/pkg/storage/etcd3/testing"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
	"github.com/kumodo-io/convoy/api/core"
	coreinstall "github.com/kumodo-io/convoy/api/core/install"
	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	clusterstorage "github.com/kumodo-io/convoy/internal/registry/core/cluster"
	"github.com/kumodo-io/convoy/internal/registry/registrytest"
)

const (
	dummyClusterName = "dummy-cluster"
	dummyServerURL   = "https://dummy.example.com:6443"
)

// newStorage builds Cluster REST storage backed by an in-process etcd test server.
func newStorage(t *testing.T) (*genericregistry.Store, *clusterstorage.StatusREST, *etcd3testing.EtcdTestServer) {
	scheme, restOptions, server := registrytest.NewEtcdStorage(t, coreinstall.Install, corev1alpha1.SchemeGroupVersion, core.Resource("clusters"))

	restStorage, statusStorage, err := clusterstorage.NewREST(scheme, restOptions, 4)
	if err != nil {
		server.Terminate(t)
		t.Fatalf("unexpected error from NewREST: %v", err)
	}
	return restStorage.Store, statusStorage, server
}

func validNewCluster(namespace string) *core.Cluster {
	return &core.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyClusterName,
			Namespace: namespace,
		},
		Spec: core.ClusterSpec{
			ConnectionMode: core.ClusterConnectionModeInCluster,
		},
	}
}

func invalidNewClusterNoConnectionMode(namespace string) *core.Cluster {
	return &core.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyClusterName,
			Namespace: namespace,
		},
		Spec: core.ClusterSpec{
			ConnectionMode: "",
		},
	}
}

func invalidNewClusterNoConnectionModeConfig(namespace string) *core.Cluster {
	return &core.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyClusterName,
			Namespace: namespace,
		},
		Spec: core.ClusterSpec{
			ConnectionMode: core.ClusterConnectionModeAgentless,
		},
	}
}

func invalidNewClusterNoAuthSpec(namespace string) *core.Cluster {
	return &core.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyClusterName,
			Namespace: namespace,
		},
		Spec: core.ClusterSpec{
			ConnectionMode: core.ClusterConnectionModeAgentless,
			Agentless: &core.ClusterAgentlessModeSpec{
				Server: dummyServerURL,
			},
		},
	}
}

func invalidNewClusterNoAuthType(namespace string) *core.Cluster {
	return &core.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyClusterName,
			Namespace: namespace,
		},
		Spec: core.ClusterSpec{
			ConnectionMode: core.ClusterConnectionModeAgentless,
			Agentless: &core.ClusterAgentlessModeSpec{
				Server: dummyServerURL,
				Auth:   core.ClusterAuthSpec{},
			},
		},
	}
}

func invalidNewClusterUnknownAuthType(namespace string) *core.Cluster {
	return &core.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyClusterName,
			Namespace: namespace,
		},
		Spec: core.ClusterSpec{
			ConnectionMode: core.ClusterConnectionModeAgentless,
			Agentless: &core.ClusterAgentlessModeSpec{
				Server: dummyServerURL,
				Auth: core.ClusterAuthSpec{
					Type: "bar",
				},
			},
		},
	}
}

func invalidNewClusterNoAuthConfig(namespace string) *core.Cluster {
	return &core.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyClusterName,
			Namespace: namespace,
		},
		Spec: core.ClusterSpec{
			ConnectionMode: core.ClusterConnectionModeAgentless,
			Agentless: &core.ClusterAgentlessModeSpec{
				Server: dummyServerURL,
				Auth: core.ClusterAuthSpec{
					Type: core.ClusterAuthTypeKubeconfig,
				},
			},
		},
	}
}

func invalidNewClusterAuthConfigMismatch(namespace string) *core.Cluster {
	return &core.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dummyClusterName,
			Namespace: namespace,
		},
		Spec: core.ClusterSpec{
			ConnectionMode: core.ClusterConnectionModeAgentless,
			Agentless: &core.ClusterAgentlessModeSpec{
				Server: dummyServerURL,
				Auth: core.ClusterAuthSpec{
					Type:  core.ClusterAuthTypeKubeconfig,
					Token: &authv1.TokenAuthSpec{},
				},
			},
		},
	}
}

func TestCreate(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	ns := test.TestNamespace()
	test.TestCreate(
		validNewCluster(ns),
		invalidNewClusterNoConnectionMode(ns),
		invalidNewClusterNoConnectionModeConfig(ns),
		invalidNewClusterNoAuthSpec(ns),
		invalidNewClusterNoAuthType(ns),
		invalidNewClusterUnknownAuthType(ns),
		invalidNewClusterNoAuthConfig(ns),
		invalidNewClusterAuthConfigMismatch(ns),
	)
}

func TestUpdate(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestUpdate(
		validNewCluster(test.TestNamespace()),
		// valid update: add a label
		func(obj runtime.Object) runtime.Object {
			c := obj.(*core.Cluster)
			if c.Labels == nil {
				c.Labels = map[string]string{}
			}
			c.Labels["updated"] = "true"
			return c
		},
		// invalid update: clears the required connectionMode
		func(obj runtime.Object) runtime.Object {
			c := obj.(*core.Cluster)
			c.Spec.ConnectionMode = ""
			c.Spec.Agentless = nil
			return c
		},
	)
}

func TestGet(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestGet(validNewCluster(test.TestNamespace()))
}

func TestList(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestList(validNewCluster(test.TestNamespace()))
}

func TestDelete(t *testing.T) {
	storage, _, server := newStorage(t)
	defer server.Terminate(t)
	test := genericregistrytest.New(t, storage)
	test.TestDelete(validNewCluster(test.TestNamespace()))
}

// TestStatusUpdate verifies the /status subresource updates status while leaving
// spec untouched.
func TestStatusUpdate(t *testing.T) {
	storage, statusStorage, server := newStorage(t)
	defer server.Terminate(t)

	ctx := genericapirequest.WithNamespace(genericapirequest.NewContext(), metav1.NamespaceDefault)

	if _, err := storage.Create(ctx, validNewCluster(metav1.NamespaceDefault), rest.ValidateAllObjectFunc, &metav1.CreateOptions{}); err != nil {
		t.Fatalf("unexpected error creating cluster: %v", err)
	}

	obj, err := statusStorage.Get(ctx, dummyClusterName, &metav1.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected error getting cluster: %v", err)
	}
	cluster := obj.(*core.Cluster)

	cluster.Status.Reachable = true
	cluster.Status.DiscoveredVersion = "v1.29.4"
	cluster.Status.ObservedGeneration = cluster.Generation
	// A spec change submitted through /status must be ignored.
	cluster.Spec.Metadata = map[string]string{"ignored": "true"}

	updatedObj, _, err := statusStorage.Update(
		ctx,
		cluster.Name,
		rest.DefaultUpdatedObjectInfo(cluster),
		rest.ValidateAllObjectFunc,
		rest.ValidateAllObjectUpdateFunc,
		false,
		&metav1.UpdateOptions{},
	)
	if err != nil {
		t.Fatalf("unexpected error updating status: %v", err)
	}

	updated := updatedObj.(*core.Cluster)
	if !updated.Status.Reachable {
		t.Errorf("expected status.reachable to be true after status update")
	}
	if updated.Status.DiscoveredVersion != "v1.29.4" {
		t.Errorf("expected status.discoveredVersion to persist, got %q", updated.Status.DiscoveredVersion)
	}
	if updated.Spec.Metadata != nil {
		t.Errorf("expected spec change to be ignored by /status, got %v", updated.Spec.Metadata)
	}
}
