package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type KindHealthRuleSpec struct{}

type KindHealthRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec KindHealthRuleSpec `json:"spec,omitempty"`
}

type KindHealthRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KindHealthRule `json:"items"`
}
