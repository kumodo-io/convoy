package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ResourceHealthRuleSpec defines the desired state of ResourceHealthRule.
type ResourceHealthRuleSpec struct {
	HealthRule `json:",inline"`

	// Target is the target resource.
	Target ResourceTarget `json:"target"`
}

type ResourceTarget struct {
	// Group is the group of the target resource.
	Group string `json:"group"`

	// Kind is the kind of the target resource.
	Kind string `json:"kind"`

	// Version is the API version of the target resource.
	Version string `json:"version"`

	// Name is the name of the target resource.
	Name string `json:"name"`

	// Namespace is the namespace of the target resource.
	Namespace string `json:"namespace"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:prerelease-lifecycle-gen:introduced=1.0
// +kubebuilder:object:root=true

type ResourceHealthRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ResourceHealthRuleSpec `json:"spec,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ResourceHealthRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourceHealthRule `json:"items"`
}
