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

package v1alpha1

// SyncRequestedAtAnnotation is stamped (with an RFC3339 timestamp) by a user
// /sync request and by /pin. The controller enqueues on changes to this annotation
// and, in manual mode (syncPolicy.automatic=false), applies when its value is newer
// than status.lastHandledSyncAt.
const SyncRequestedAtAnnotation = "convoy.kumodo.io/sync-requested-at"

// ReconcileRequestedAtAnnotation is stamped (with an RFC3339 timestamp) by writes
// that only wake the controller: /mutations, /lifecycle and /sync?wake=true (the
// observer). In manual mode it never counts as a request to apply.
const ReconcileRequestedAtAnnotation = "convoy.kumodo.io/reconcile-requested-at"

// SyncForceRequestedAtAnnotation is stamped (with an RFC3339 timestamp) by the
// /sync subresource for a forced sync (/sync?force=true). When its value is newer
// than status.lastHandledForceSyncAt, the controller bypasses the pre-apply dry-run
// and clears a held Failed/RolledBack rollout, re-driving the render. The normal
// SyncRequestedAtAnnotation is stamped alongside it to wake the controller.
const SyncForceRequestedAtAnnotation = "convoy.kumodo.io/sync-force-requested-at"

const (
	// ReleaseKind is a string value for release kind
	ReleaseKind = "Release"

	// ClusterKind is a string value for cluster kind
	ClusterKind = "Cluster"
)
