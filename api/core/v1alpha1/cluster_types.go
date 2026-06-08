package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
)

// ClusterConnectionMode defines the connection mode between the control plane
// and the target cluster.
// +enum
type ClusterConnectionMode string

const (
	// ClusterConnectionModeAgentless defines the connection mode when the
	// control plane pushes the workload manifests to the cluster connecting
	// to the target cluster's API server.
	ClusterConnectionModeAgentless ClusterConnectionMode = "agentless"

	// ClusterConnectionModeAgent defines the connection mode when the
	// agent is running on the target cluster, pulls the workload manifests
	// and applies them locally.
	ClusterConnectionModeAgent ClusterConnectionMode = "agent"
)

// ReleasesDeletionPolicy defines the deletion policy for the releases
// in case of the cluster deletion.
// +enum
type ReleasesDeletionPolicy string

const (
	// ReleasesDeletionPolicyBlock blocks the cluster deletion until
	// no Release objects reference the cluster (default, recommended).
	ReleasesDeletionPolicyBlock ReleasesDeletionPolicy = "block"

	// ReleasesDeletionPolicyDelete deletes the Release objects (cascading).
	ReleasesDeletionPolicyDelete ReleasesDeletionPolicy = "delete"

	// ReleasesDeletionPolicyOrphan leaves the Release objects in the cluster.
	// The Release objects are orphaned and enter the "ClusterMissing" state.
	ReleasesDeletionPolicyOrphan ReleasesDeletionPolicy = "orphan"
)

// WorkloadsDeletionPolicy defines the deletion policy for the workloads
// in case of the cluster deletion.
// +enum
type WorkloadsDeletionPolicy string

const (
	// WorkloadsDeletionPolicyPrune deletes deployed manifests from the cluster.
	WorkloadsDeletionPolicyPrune WorkloadsDeletionPolicy = "prune"

	// WorkloadsDeletionPolicyLeave leaves deployed manifests in the cluster.
	WorkloadsDeletionPolicyLeave WorkloadsDeletionPolicy = "leave"
)

// OnUnreachableDeletionPolicy defines the deletion policy for the cluster
// in case the cluster is unreachable.
// +enum
type OnUnreachableDeletionPolicy string

const (
	// OnUnreachableDeletionPolicyBlock blocks the cluster deletion
	// until reachability is restored (default).
	OnUnreachableDeletionPolicyBlock OnUnreachableDeletionPolicy = "block"

	// OnUnreachableDeletionPolicyReleasesOnly cleans up the Release objects (cluster is gone).
	OnUnreachableDeletionPolicyReleasesOnly OnUnreachableDeletionPolicy = "releasesOnly"
)

// ClusterSpec defines the desired state of the Cluster.
type ClusterSpec struct {
	// ConnectionMode is the connection mode between the control plane and the target cluster.
	// Possible values are:
	// - agentless
	// - agent
	ConnectionMode ClusterConnectionMode `json:"connectionMode"`

	// Agentless is the configuration of the agentless connection mode.
	Agentless *ClusterAgentlessModeSpec `json:"agentless,omitempty"`

	// Metadata is the metadata of the cluster that is used for the templating
	// of the workload manifests defined in the corresponding Release objects.
	Metadata map[string]string `json:"metadata,omitempty"`

	// Capabilities is the capabilities of the cluster.
	Capabilities ClusterCapabilitiesSpec `json:"capabilities,omitempty"`

	// DeletionPolicy is the deletion policy of the cluster.
	DeletionPolicy ClusterDeletionPolicySpec `json:"deletionPolicy,omitempty"`
}

// ClusterAgentlessModeSpec defines the configuration of the agentless connection mode.
type ClusterAgentlessModeSpec struct {
	// Server is the address of the Kubernetes API server.
	Server string `json:"server"`

	// CAPublicCertRef is the name and the key of the ConfigMap containing the CA public certificate.
	// The CA certificate is used to verify the Kubernetes API server's certificate.
	CAPublicCertRef corev1.ConfigMapKeySelector `json:"caPublicCertRef"`

	// Auth is the authentication configuration for the agentless connection mode.
	Auth ClusterAuthSpec `json:"auth"`

	ConnectionParameters ClusterConnectionParametersSpec `json:"connectionParameters"`
}

// ClusterAuthSpec defines the authentication mode used to connect to
// the target cluster in the agentless connection mode.
type ClusterAuthSpec struct {
	// Type is the authentication mode used to connect to the target cluster.
	// The workload identity authentication mode does not require specific
	// configuration as it uses the identity of the controller's pod.
	Type authv1.AuthType `json:"type"`

	// Kubeconfig is the authentication mode using a kubeconfig file.
	Kubeconfig *authv1.KubeconfigAuthSpec `json:"kubeconfig,omitempty"`

	// Token is the authentication mode using a token.
	Token *authv1.TokenAuthSpec `json:"token,omitempty"`

	// ClientCert is the authentication mode using a client certificate.
	ClientCert *authv1.ClientCertAuthSpec `json:"clientCert,omitempty"`

	// ExecPlugin is the authentication mode using an exec plugin.
	ExecPlugin *authv1.ExecPluginAuthSpec `json:"execPlugin,omitempty"`

	// OIDC is the authentication mode using OIDC client credentials.
	OIDC *authv1.OIDAuthSpec `json:"oidc,omitempty"`
}

// ClusterCapabilitiesSpec defines the capabilities the cluster supports — informational,
// used by the renderer. If omitted, controller discovers these on first successful connection.
type ClusterCapabilitiesSpec struct {
	ServerSideApply bool `json:"serverSideApply"`

	// APIVersions is the list of specific API versions the renderer can rely on.
	// +listType=atomic
	APIVersions []string `json:"apiVersions,omitempty"`
}

// ClusterDeletionPolicySpec defines the deletion policy of the cluster.
type ClusterDeletionPolicySpec struct {
	// Releases is the deletion policy for the Release objects in case of the cluster deletion.
	// Possible values are:
	// - block (default, recommended)
	// - delete
	// - orphan
	Releases ReleasesDeletionPolicy `json:"releasesDeletionPolic"`

	// Workloads is the deletion policy for the workloads in case of the cluster deletion.
	// Possible values are:
	// - prune (default)
	// - leave
	Workloads WorkloadsDeletionPolicy `json:"workloads"`

	// OnUnreachable is the deletion policy for the cluster in case the cluster is unreachable.
	// Possible values are:
	// - block (default)
	// - releasesOnly
	OnUnreachable OnUnreachableDeletionPolicy `json:"onUnreachable"`
}

// ClusterConnectionParametersSpec defines the connection parameters of the cluster.
type ClusterConnectionParametersSpec struct {
	// QPS is the maximum number of queries per second (QPS) to the Kubernetes API server.
	QPS int `json:"qps"`

	// Burst is the maximum number of burst queries per second (QPS) to the Kubernetes API server.
	Burst int `json:"burst"`

	// Timeout is the timeout for the Kubernetes API server connection.
	Timeout metav1.Duration `json:"timeout"`
}

// ClusterStatus reflects the observed state of the Cluster.
type ClusterStatus struct {
	// ObservedGeneration is the last generation that was observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Reachable is a flag indicating whether the cluster is reachable.
	Reachable bool `json:"reachable"`

	// LastSeen is the last time the cluster was seen by the controller.
	LastSeen metav1.Time `json:"lastSeen,omitempty"`

	// DiscoveredVersion is the discovered Kubernetes version of the cluster.
	DiscoveredVersion string `json:"discoveredVersion,omitempty"`

	// DiscoveredAPIVersions is the list of discovered API versions of the cluster.
	// +listType=atomic
	DiscoveredAPIVersions []string `json:"discoveredAPIVersions,omitempty"`

	// ReleaseCount is the number of Release objects in the cluster.
	ReleaseCount int64 `json:"releaseCount,omitempty"`

	// AgentStatus is the status of the agent running on the cluster.
	// Applicable only if the cluster connection mode is agent.
	AgentStatus *ClusterAgentStatus `json:"agentStatus,omitempty"`

	// Conditions is the list of conditions related to the cluster.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ClusterAgentStatus reflects the observed state of the agent running on the cluster.
type ClusterAgentStatus struct {
	// AgentVersion is the version of the agent running on the cluster.
	AgentVersion string `json:"agentVersion"`

	// RegisteredAt is the time when the agent was registered with the cluster.
	RegisteredAt metav1.Time `json:"registeredAt"`

	// LastHeartbeat is the time when the agent last sent a heartbeat.
	LastHeartbeat metav1.Time `json:"lastHeartbeat,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:prerelease-lifecycle-gen:introduced=1.0
// +kubebuilder:object:root=true

type Cluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterSpec   `json:"spec,omitempty"`
	Status ClusterStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cluster `json:"items"`
}
