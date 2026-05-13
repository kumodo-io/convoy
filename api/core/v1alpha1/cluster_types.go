package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterConnectionMode defines the connection mode between the control plane
// and the target cluster.
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

// ClusterAuthType defines the authentication mode used to connect to the
// target cluster in the agentless connection mode.
type ClusterAuthType string

const (
	// ClusterAuthModeKubeconfig defines the authentication mode using a kubeconfig file.
	ClusterAuthModeKubeconfig ClusterAuthType = "kubeconfig"

	// ClusterAuthModeToken defines the authentication mode using a token.
	ClusterAuthModeToken ClusterAuthType = "token"

	// ClusterAuthModeClientCert defines the authentication mode using a client certificate.
	ClusterAuthModeClientCert ClusterAuthType = "clientCert"

	// ClusterAuthModeExecPlugin defines the authentication mode using an exec plugin.
	ClusterAuthModeExecPlugin ClusterAuthType = "execPlugin"

	// ClusterAuthModeWorkloadIdentity defines the authentication mode using a workload identity.
	ClusterAuthModeWorkloadIdentity ClusterAuthType = "workloadIdentity"

	// ClusterAuthModeOIDCClientCredentials defines the authentication mode using OIDC client credentials.
	ClusterAuthModeOIDCClientCredentials ClusterAuthType = "oidcClientCredentails"
)

// ReleasesDeletionPolicy defines the deletion policy for the releases
// in case of the cluster deletion.
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
type WorkloadsDeletionPolicy string

const (
	// WorkloadsDeletionPolicyPrune deletes deployed manifests from the cluster.
	WorkloadsDeletionPolicyPrune WorkloadsDeletionPolicy = "prune"

	// WorkloadsDeletionPolicyLeave leaves deployed manifests in the cluster.
	WorkloadsDeletionPolicyLeave WorkloadsDeletionPolicy = "leave"
)

// OnUnreachableDeletionPolicy defines the deletion policy for the cluster
// in case the cluster is unreachable.
type OnUnreachableDeletionPolicy string

const (
	// OnUnreachableDeletionPolicyBlock blocks the cluster deletion
	// until reachability is restored (default).
	OnUnreachableDeletionPolicyBlock OnUnreachableDeletionPolicy = "block"

	// OnUnreachableDeletionPolicyReleasesOnly cleans up the Release objects (cluster is gone).
	OnUnreachableDeletionPolicyReleasesOnly OnUnreachableDeletionPolicy = "releasesOnly"
)

// OIDCClientAuthType defines the authentication method used by the OIDC client.
type OIDCClientAuthType string

const (
	// OIDCClientAuthTypeClientSecret is the authentication method using a client secret.
	OIDCClientAuthTypeClientSecret OIDCClientAuthType = "clientSecret"

	// OIDCClientAuthTypeTLSClientAuth is the authentication method using TLS client authentication.
	OIDCClientAuthTypeTLSClientAuth OIDCClientAuthType = "tlsClientAuth"

	// OIDCClientAuthTypePrivateKeyJWT is the authentication method using a private key JWT.
	OIDCClientAuthTypePrivateKeyJWT OIDCClientAuthType = "privateKeyJWT"
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
	// Possible values are:
	// - kubeconfig
	// - token
	// - clientCert
	// - execPlugin
	// - workloadIdentity
	// - oidcClientCredentials
	// The workload identity authentication mode does not require specific
	// configuration as it uses the identity of the controller's pod.
	Type ClusterAuthType `json:"type"`

	// Kubeconfig is the authentication mode using a kubeconfig file.
	Kubeconfig *AuthModeKubeconfigSpec `json:"kubeconfig,omitempty"`

	// Token is the authentication mode using a token.
	Token *AuthModeTokenSpec `json:"token,omitempty"`

	// ClientCert is the authentication mode using a client certificate.
	ClientCert *AuthModeClientCertSpec `json:"clientCert,omitempty"`

	// ExecPlugin is the authentication mode using an exec plugin.
	ExecPlugin *AuthModeExecPluginSpec `json:"execPlugin,omitempty"`

	// OIDCClientCredentials is the authentication mode using OIDC client credentials.
	OIDCClientCredentials *AuthModeOIDCClientCredentialsSpec `json:"oidcClientCredentials,omitempty"`
}

// AuthModeKubeconfigSpec defines the authentication mode using a kubeconfig file.
type AuthModeKubeconfigSpec struct {
	// SecretRef is the name and the key of the Secret containing the kubeconfig file.
	SecretRef corev1.SecretKeySelector `json:"secretRef"`
}

// AuthModeTokenSpec defines the authentication mode using a token.
type AuthModeTokenSpec struct {
	// SecretRef is the name and the key of the Secret containing the token.
	SecretRef corev1.SecretKeySelector `json:"secretRef"`
}

// AuthModeClientCertSpec defines the authentication mode using a client certificate.
type AuthModeClientCertSpec struct {
	// CertRef is the name and the key of the Secret containing the client certificate.
	CertRef corev1.SecretKeySelector `json:"certRef"`

	// KeyRef is the name and the key of the Secret containing the client certificate key.
	KeyRef corev1.SecretKeySelector `json:"keyRef"`
}

// AuthModeExecPluginSpec defines the authentication mode using an exec plugin.
type AuthModeExecPluginSpec struct {
	// Command is the command to execute.
	Command string `json:"command"`

	// Args are the arguments to pass to the command.
	// +listType=atomic
	Args []string `json:"args,omitempty"`

	// Env are the environment variables to pass to the command.
	// +listType=map
	// +listMapKey=name
	Env []corev1.EnvVar `json:"env,omitempty"`

	// CredentialsRef is the optional reference to credentials the plugin needs (e.g., AWS IAM role).
	// Defined by the reference to the Secret and the key of the Secret containing the credentials.
	CredentialsRef corev1.SecretReference `json:"credentialsRef,omitempty"`
}

// AuthModeOIDCClientCredentialsSpec defines the authentication mode using OIDC client credentials.
type AuthModeOIDCClientCredentialsSpec struct {
	// IssuerURL is the OIDC issuer URL.
	IssuerURL string `json:"issuerURL"`

	// ClientID is the OIDC client ID.
	ClientID string `json:"clientID"`

	// ClientAuth is the OIDC client authentication method and configuration.
	ClientAuth OIDCClientAuthSpec `json:"clientAuth"`

	// Scopes are the OIDC scopes.
	// +listType=atomic
	Scopes []string `json:"scopes,omitempty"`

	// Audience is the OIDC audience.
	Audience string `json:"audience,omitempty"`

	// TLS is the IdP TLS configuration.
	TLS *OIDCTLSSpec `json:"tls,omitempty"`
}

// OIDCClientAuthSpec defines the OIDC client authentication method and configuration.
type OIDCClientAuthSpec struct {
	// Type is the OIDC client authentication method.
	Type OIDCClientAuthType `json:"type"`

	// ClientSecret is the reference to the Secret and the key of the
	// Secret containing the OIDC client secret.
	ClientSecret *OIDCClientAuthClientSecretSpec `json:"clientSecret,omitempty"`

	// TLSClientAuth is the reference to the Secrets and the keys of the Secret(s) containing the
	// OIDC client certificate and key.
	TLSClientAuth *OIDCClientAuthTLSClientAuthSpec `json:"tlsClientAuth,omitempty"`

	// PrivateKeyJWT is the reference to the Secret and the key of the Secret containing the OIDC private key JWT.
	PrivateKeyJWT *OIDCClientAuthPrivateKeyJWTSpec `json:"privateKeyJWT,omitempty"`
}

// OIDCClientAuthClientSecretSpec defines the reference to the Secret
// and the key of the Secret containing the OIDC client secret.
type OIDCClientAuthClientSecretSpec struct {
	// ClientSecretRef is the name and the key of the Secret containing the OIDC client secret.
	ClientSecretRef corev1.SecretKeySelector `json:"clientSecretRef"`
}

// OIDCClientAuthTLSClientAuthSpec defines the reference to the Secrets
// and the keys of the Secret(s) containing the OIDC client certificate and key.
type OIDCClientAuthTLSClientAuthSpec struct {
	// CertRef is the name and the key of the Secret containing the OIDC client certificate.
	CertRef corev1.SecretKeySelector `json:"certRef"`

	// KeyRef is the name and the key of the Secret containing the OIDC client certificate key.
	KeyRef corev1.SecretKeySelector `json:"keyRef"`
}

// OIDCClientAuthPrivateKeyJWTSpec defines the reference to the Secret
// and the key of the Secret containing the OIDC private key JWT.
type OIDCClientAuthPrivateKeyJWTSpec struct {
	// PrivateKeyRef is the name and the key of the Secret containing the OIDC private key JWT.
	PrivateKeyRef corev1.SecretKeySelector `json:"privateKeyRef"`
}

// ClusterCapabilitiesSpec defines the capabilities the cluster supports — informational,
// used by the renderer. If omitted, controller discovers these on first successful connection.
type ClusterCapabilitiesSpec struct {
	ServerSideApply bool `json:"serverSideApply"`

	// APIVersions is the list of specific API versions the renderer can rely on.
	// +listType=atomic
	APIVersions []string `json:"apiVersions,omitempty"`
}

type OIDCTLSSpec struct {
	// CAPublicCertRef is the name and the key of the ConfigMap containing the CA public certificate.
	// The CA certificate is used to verify the IdP server's certificate.
	CAPublicCertRef corev1.ConfigMapKeySelector `json:"caPublicCertRef"`

	// InsecureSkipVerify is a flag to skip the certificate verification.
	InsecureSkipVerify bool `json:"insecureSkipVerify"`
}

// ClusterDeletionPolicySpec defines the deletion policy of the cluster.
type ClusterDeletionPolicySpec struct {
	// ReleasesDeletionPolicy is the deletion policy for the Release objects in case of the cluster deletion.
	// Possible values are:
	// - block (default, recommended)
	// - delete
	// - orphan
	ReleasesDeletionPolicy ReleasesDeletionPolicy `json:"releasesDeletionPolicy"`

	// WorkloadsDeletionPolicy is the deletion policy for the workloads in case of the cluster deletion.
	// Possible values are:
	// - prune (default)
	// - leave
	WorkloadsDeletionPolicy WorkloadsDeletionPolicy `json:"workloadsDeletionPolicy"`

	// OnUnreachableDeletionPolicy is the deletion policy for the cluster in case the cluster is unreachable.
	// Possible values are:
	// - block (default)
	// - releasesOnly
	OnUnreachableDeletionPolicy OnUnreachableDeletionPolicy `json:"onUnreachableDeletionPolicy"`
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

type ClusterStatus struct{}

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
