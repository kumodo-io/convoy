package source

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type GitRepositorySpec struct{}

type GitRepositoryStatus struct{}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type GitRepository struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec   GitRepositorySpec
	Status GitRepositoryStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type GitRepositoryList struct {
	metav1.TypeMeta
	metav1.ListMeta

	Items []GitRepository
}

type HelmRegistrySpec struct{}

type HelmRegistryStatus struct{}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type HelmRegistry struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec   HelmRegistrySpec
	Status HelmRegistryStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type HelmRegistryList struct {
	metav1.TypeMeta
	metav1.ListMeta

	Items []HelmRegistry
}

type OCIRegistrySpec struct{}

type OCIRegistryStatus struct{}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type OCIRegistry struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec   OCIRegistrySpec
	Status OCIRegistryStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type OCIRegistryList struct {
	metav1.TypeMeta
	metav1.ListMeta

	Items []OCIRegistry
}
