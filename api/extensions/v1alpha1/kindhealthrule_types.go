package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// KindHealthRuleSpec defines the desired state of KindHealthRule
type KindHealthRuleSpec struct {
	HealthRule `json:",inline"`

	// Target is the target kind.
	Target KindTarget `json:"target"`
}

// KindTarget defines the target kind by its group, kind and version.
type KindTarget struct {
	// Group is the group of the target kind.
	Group string `json:"group"`

	// Kind is the target kind.
	Kind string `json:"kind"`

	// Version is the API version of the target kind.
	Version string `json:"version"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:prerelease-lifecycle-gen:introduced=1.0
// +kubebuilder:object:root=true

type KindHealthRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec KindHealthRuleSpec `json:"spec,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type KindHealthRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KindHealthRule `json:"items"`
}
