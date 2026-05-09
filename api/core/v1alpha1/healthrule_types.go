package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type HealthRuleSpec struct{}

type HealthRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec HealthRuleSpec `json:"spec,omitempty"`
}

type HealthRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HealthRule `json:"items"`
}
