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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/kumodo-io/convoy/internal/inventory"
)

const (
	// conditionDeleted reports a Release deletion that waits on its target cluster.
	conditionDeleted               = "Deleted"
	conditionReasonDeletionBlocked = "DeletionBlocked"

	verbList = "list"
)

// unmanaged describes the objects in objs that are not in managed and not owned,
// through an ownerReference chain, by an object in managed.
func unmanaged(_ []unstructured.Unstructured, _ []inventory.Ref) []string {
	return nil
}

// listNamespaced lists the objects of every listable resource in lists within
// namespace. Each resource it cannot list becomes a blocker.
func listNamespaced(_ context.Context, _ dynamic.Interface, _ []*metav1.APIResourceList, _ string) ([]unstructured.Unstructured, []string) {
	return nil, nil
}

// listCustomResources lists, in all namespaces, the custom resources of the CRD
// named crdName. A failed read or list becomes a blocker.
func listCustomResources(_ context.Context, _ dynamic.Interface, _ string) ([]unstructured.Unstructured, []string) {
	return nil, nil
}

// blockedMessage explains why target was not deleted.
func blockedMessage(_ inventory.Ref, _ []string) string {
	return ""
}
