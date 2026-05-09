package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type HelmRegistrySpec struct{}

type HelmRegistryStatus struct{}

type HelmRegistry struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HelmRegistrySpec   `json:"spec,omitempty"`
	Status HelmRegistryStatus `json:"status,omitempty"`
}

type HelmRegistryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HelmRegistry `json:"items"`
}
