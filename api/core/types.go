package core

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type ClusterSpec struct{}

type ClusterStatus struct{}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Cluster struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec   ClusterSpec
	Status ClusterStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ClusterList struct {
	metav1.TypeMeta
	metav1.ListMeta

	Items []Cluster
}

type ReleaseSpec struct{}

type ReleaseStatus struct{}

type ReleaseMutations struct{}

type ReleasePin struct{}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Release struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec   ReleaseSpec
	Status ReleaseStatus

	Mutations ReleaseMutations
	Pin       ReleasePin
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ReleaseList struct {
	metav1.TypeMeta
	metav1.ListMeta

	Items []Release
}
