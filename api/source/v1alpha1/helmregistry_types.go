package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type HelmRegistrySpec struct{}

type HelmRegistryStatus struct{}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:prerelease-lifecycle-gen:introduced=1.0
// +kubebuilder:object:root=true

type HelmRegistry struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HelmRegistrySpec   `json:"spec,omitempty"`
	Status HelmRegistryStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type HelmRegistryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HelmRegistry `json:"items"`
}
