package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type KindHealthRuleSpec struct{}

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
