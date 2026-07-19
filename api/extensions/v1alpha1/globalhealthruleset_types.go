package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// GlobalHealthRuleSetSpec defines the desired state of GlobalHealthRuleSet
type GlobalHealthRuleSetSpec struct {
	Rules []RuleSpec `json:"rules"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:prerelease-lifecycle-gen:introduced=1.0
// +kubebuilder:object:root=true

type GlobalHealthRuleSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec GlobalHealthRuleSetSpec `json:"spec,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type GlobalHealthRuleSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GlobalHealthRuleSet `json:"items"`
}
