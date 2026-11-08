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
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	extensionsv1alpha1 "github.com/kumodo-io/convoy/api/extensions/v1alpha1"
	"github.com/kumodo-io/convoy/internal/health"
)

// TestAssessItem_UnknownKeepsAnchor is the KB-14 regression: a resource that could
// not be read lost its firstUnhealthy anchor, so its deadline started again.
func TestAssessItem_UnknownKeepsAnchor(t *testing.T) {
	deployments := schema.GroupVersionResource{Group: groupApps, Version: "v1", Resource: "deployments"}
	evaluator, err := health.NewEvaluator([]extensionsv1alpha1.RuleSpec{{
		ApiVersion: "apps/v1", Resources: []string{deployments.Resource}, RuleExpression: "false",
	}})
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{deployments.GroupVersion()})
	mapper.Add(deployments.GroupVersion().WithKind(deploymentKind), meta.RESTScopeNamespace)
	newDyn := func() *dynamicfake.FakeDynamicClient {
		return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
			map[schema.GroupVersionResource]string{deployments: "DeploymentList"})
	}
	timeout := newDyn()
	timeout.PrependReactor("get", deployments.Resource, func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewServerTimeout(deployments.GroupResource(), "get", 1)
	})

	item := corev1alpha1.ResourceInventoryItem{Group: groupApps, Version: "v1", Kind: deploymentKind, Namespace: "ns", Name: depName}
	unmapped := item
	unmapped.Kind = "Widget"
	first := time.Date(2026, 11, 8, 21, 59, 40, 0, time.UTC)
	now := first.Add(time.Minute)

	for _, tc := range []struct {
		name string
		item corev1alpha1.ResourceInventoryItem
		dyn  *dynamicfake.FakeDynamicClient
	}{
		{name: "mapping error", item: unmapped, dyn: newDyn()},
		{name: "not found", item: item, dyn: newDyn()},
		{name: "read error", item: item, dyn: timeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref := objectRef{gvk: schema.GroupVersionKind{Group: tc.item.Group, Version: tc.item.Version, Kind: tc.item.Kind}, namespace: tc.item.Namespace, name: tc.item.Name}
			anchors := map[string]time.Time{ref.identityID(): first}
			c := &HealthController{}

			got := c.assessItem(context.Background(), evaluator, tc.dyn, mapper, tc.item, anchors, nil, now)
			if got.state != corev1alpha1.ResourceStateUnknown {
				t.Errorf("state = %s, want Unknown", got.state)
			}
			if !got.firstUnhealthy.Equal(first) {
				t.Errorf("firstUnhealthy = %v, want the stored anchor %v", got.firstUnhealthy, first)
			}
		})
	}
}
