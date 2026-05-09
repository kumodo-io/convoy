package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type ReleaseSpec struct{}

type ReleaseStatus struct{}

type ReleaseMutations struct{}

type ReleasePin struct{}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:prerelease-lifecycle-gen:introduced=1.0
// +kubebuilder:object:root=true

type Release struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ReleaseSpec   `json:"spec,omitempty"`
	Status ReleaseStatus `json:"status,omitempty"`

	Mutations ReleaseMutations `json:"mutations,omitempty"`
	Pin       ReleasePin       `json:"pin,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ReleaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Release `json:"items"`
}
