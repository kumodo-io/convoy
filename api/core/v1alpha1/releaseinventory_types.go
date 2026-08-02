package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ResourceState is a single resource's health verdict.
// +enum
type ResourceState string

const (
	// ResourceStateHealthy — all applicable rules pass (or none apply).
	ResourceStateHealthy ResourceState = "Healthy"

	// ResourceStateProgressing — a rule fails but the deadline has not elapsed.
	ResourceStateProgressing ResourceState = "Progressing"

	// ResourceStateDegraded — a rule fails and the deadline has elapsed.
	ResourceStateDegraded ResourceState = "Degraded"

	// ResourceStateUnknown — the resource could not be assessed (unreachable / eval error).
	ResourceStateUnknown ResourceState = "Unknown"
)

// ResourceInventoryItem defines the inventoried resource.
type ResourceInventoryItem struct {
	// Group reflects the API group of the inventoried resource.
	Group string `json:"group"`

	// Version reflects the API version of the inventoried resource.
	Version string `json:"version,omitempty"`

	// Kind reflects the kind of the inventoried resource.
	Kind string `json:"kind"`

	// Namespace reflects the namespace of the inventoried resource if any.
	Namespace string `json:"namespace"`

	// Name reflects the name of the inventoried resource.
	Name string `json:"name"`

	// RenderRef is the hash of the release render inventoried resource belongs to.
	RenderRef string `json:"renderRef,omitempty"`

	// AssessedRenderRef is the hash of the release render that was assessed.
	AssessedRenderRef string `json:"assessedRenderRef,omitempty"`

	// UID reflects the uid of the inventoried resource.
	UID *types.UID `json:"uid,omitempty"`

	// Wave reflects the sync wave inventoried resource belongs to.
	Wave *int `json:"wave,omitempty"`

	// State reflects the state of the inventoried resource.
	State *ResourceState `json:"state,omitempty"`

	// FirstUnhealthy contains the timestamp inventoried resource entered
	// unhealthy state for the first time.
	FirstUnhealthy *metav1.Time `json:"firstUnhealthy,omitempty"`

	// Message contains the message explaining the inventoried
	// resource state (if applicable).
	Message string `json:"message,omitempty"`
}

// ReleaseLifecycleHookItem defines the inventoried lifecycle hook.
type ReleaseLifecycleHookItem struct {
	// Name is the name of the lifecycle hook.
	Name string `json:"name"`

	// Phase is the lifecycle hook phase.
	Phase LifecycleHookPhase `json:"phase"`

	// RenderRef is the hash of the release render. This is used
	// as a gate for decision whether to re-run hook.
	RenderRef string `json:"renderRef"`

	// Weight is the weight of the hook.
	Weight int32 `json:"weight"`

	// Timeout is the timeout for the hook.
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// Group reflects the API group of the lifecycle hook workload.
	Group string `json:"group"`

	// Version reflects the API version of the lifecycle hook workload.
	Version string `json:"version,omitempty"`

	// Kind reflects the kind of the lifecycle hook workload.
	Kind string `json:"kind"`

	// WorkloadNamespace reflects the namespace of the ilifecycle hook workload if any.
	WorkloadNamespace string `json:"workloadnamespace,omitempty"`

	// WorkloadName reflects the name of the lifecycle hook workload.
	WorkloadName string `json:"workloadname"`

	// CreatedAt is the timestamp when the lifecycle hook workload was created.
	CreatedAt *metav1.Time `json:"createdAt,omitempty"`

	// FinishedAt is the timestamp when the lifecycle hook workload finished the job.
	FinishedAt *metav1.Time `json:"finishedAt,omitempty"`

	// State is the lifecycle hook state.
	State *LifecycleHookState `json:"state,omitempty"`

	// Message is the human-readable message containing the failure reason, if any.
	Message string `json:"message,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:prerelease-lifecycle-gen:introduced=1.0
// +kubebuilder:object:root=true

type ReleaseInventory struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +listType=map
	// +listMapKey=group
	// +listMapKey=kind
	// +listMapKey=namespace
	// +listMapKey=name
	Inventory []ResourceInventoryItem `json:"inventory,omitempty"`

	// +listType=map
	// +listMapKey=name
	// +listMapKey=phase
	Hooks []ReleaseLifecycleHookItem `json:"hooks,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ReleaseInventoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ReleaseInventory `json:"items"`
}
