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
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

// objectRef identifies a managed object for the /health payload and for matching
// its ReleaseInventory entry.
type objectRef struct {
	gvk       schema.GroupVersionKind
	namespace string
	name      string
}

// identityID is the version-independent identity key (group|kind|namespace|name),
// matching inventory.Ref.ID() so a health entry lines up with the controller's
// ReleaseInventory entry for the same object regardless of apiVersion.
func (r objectRef) identityID() string {
	return r.gvk.Group + "|" + r.gvk.Kind + "|" + r.namespace + "|" + r.name
}

// healthEntry is a single managed object's assessed health. FirstUnhealthy
// anchors the progress deadline (when the object first failed in the current
// unhealthy streak); zero when Healthy.
type healthEntry struct {
	ref            objectRef
	state          corev1alpha1.ResourceState
	message        string
	firstUnhealthy time.Time
}
