package core

import (
	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ClusterConnectionMode string

const (
	ClusterConnectionModeAgentless ClusterConnectionMode = "agentless"
	ClusterConnectionModeAgent     ClusterConnectionMode = "agent"
)

type ReleasesDeletionPolicy string

const (
	ReleasesDeletionPolicyBlock  ReleasesDeletionPolicy = "block"
	ReleasesDeletionPolicyDelete ReleasesDeletionPolicy = "delete"
	ReleasesDeletionPolicyOrphan ReleasesDeletionPolicy = "orphan"
)

type WorkloadsDeletionPolicy string

const (
	WorkloadsDeletionPolicyPrune WorkloadsDeletionPolicy = "prune"
	WorkloadsDeletionPolicyLeave WorkloadsDeletionPolicy = "leave"
)

type OnUnreachableDeletionPolicy string

const (
	OnUnreachableDeletionPolicyBlock        OnUnreachableDeletionPolicy = "block"
	OnUnreachableDeletionPolicyReleasesOnly OnUnreachableDeletionPolicy = "releasesOnly"
)

type ClusterSpec struct {
	ConnectionMode ClusterConnectionMode
	Agentless      *ClusterAgentlessModeSpec
	Metadata       map[string]string
	Capabilities   ClusterCapabilitiesSpec
	DeletionPolicy ClusterDeletionPolicySpec
}

type ClusterAgentlessModeSpec struct {
	Server               string
	CAPublicCertRef      corev1.ConfigMapKeySelector
	Auth                 ClusterAuthSpec
	ConnectionParameters ClusterConnectionParametersSpec
}

type ClusterAuthSpec struct {
	Type       authv1.AuthType
	Kubeconfig *authv1.KubeconfigAuthSpec
	Token      *authv1.TokenAuthSpec
	ClientCert *authv1.ClientCertAuthSpec
	ExecPlugin *authv1.ExecPluginAuthSpec
	OIDC       *authv1.OIDAuthSpec
}

type ClusterCapabilitiesSpec struct {
	ServerSideApply bool
	APIVersions     []string
}

type ClusterDeletionPolicySpec struct {
	ReleasesDeletionPolicy      ReleasesDeletionPolicy
	WorkloadsDeletionPolicy     WorkloadsDeletionPolicy
	OnUnreachableDeletionPolicy OnUnreachableDeletionPolicy
}

type ClusterConnectionParametersSpec struct {
	QPS     int
	Burst   int
	Timeout metav1.Duration
}

type ClusterStatus struct {
	ObservedGeneration    int64
	Reachable             bool
	LastSeen              metav1.Time
	DiscoveredVersion     string
	DiscoveredAPIVersions []string
	ReleaseCount          int64
	AgentStatus           *ClusterAgentStatus
	Conditions            []metav1.Condition
}

type ClusterAgentStatus struct {
	AgentVersion  string
	RegisteredAt  metav1.Time
	LastHeartbeat metav1.Time
}

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
