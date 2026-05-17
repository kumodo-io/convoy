package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// AuthType defines the authentication method used to access the Helm registry.
// +enum
type AuthType string

const (
	// AuthTypeNone is the authentication method using no authentication.
	AuthTypeNone AuthType = "none"

	// AuthTypeBasic is the authentication method using basic authentication.
	AuthTypeBasic AuthType = "basic"

	// AuthTypeToken is the authentication method using a token.
	AuthTypeToken AuthType = "token"

	// AuthTypeSSHKey is the authentication method using SSH key authentication.
	AuthTypeSSHKey AuthType = "sshKey"

	// AuthTypeGithubApp is the authentication method using a GitHub App.
	AuthTypeGithubApp AuthType = "githubApp"

	// AuthTypeOIDC is the authentication method using OIDC.
	AuthTypeOIDC AuthType = "oidc"

	// AuthTypeKubeconfig is the authentication method using a kubeconfig file.
	AuthTypeKubeconfig AuthType = "kubeconfig"

	// AuthTypeExecPlugin is the authentication method using an exec plugin.
	AuthTypeExecPlugin AuthType = "execPlugin"

	// AuthTypeClientCert is the authentication method using a client certificate.
	AuthTypeClientCert AuthType = "clientCert"

	// AuthTypeWorkloadIdentity is the authentication method using a workload identity.
	AuthTypeWorkloadIdentity AuthType = "workloadIdentity"

	// AuthTypeDockerConfig is the authentication method using a docker config.
	AuthTypeDockerConfig AuthType = "dockerConfig"
)

// TLSSpec defines the TLS configuration used to verify given endpoint.
type TLSSpec struct {
	// CAPublicCertRef is the name and the key of the ConfigMap containing the CA public certificate.
	// The CA certificate is used to verify the IdP server's certificate.
	CAPublicCertRef corev1.ConfigMapKeySelector `json:"caPublicCertRef"`

	// InsecureSkipVerify is a flag to skip the certificate verification.
	InsecureSkipVerify bool `json:"insecureSkipVerify"`
}
