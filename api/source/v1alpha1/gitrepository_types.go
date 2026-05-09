package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type GitRepositorySpec struct{}

type GitRepositoryStatus struct{}

type GitRepository struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GitRepositorySpec   `json:"spec,omitempty"`
	Status GitRepositoryStatus `json:"status,omitempty"`
}

type GitRepositoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GitRepository `json:"items"`
}
