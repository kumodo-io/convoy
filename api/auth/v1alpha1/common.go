package v1alpha1

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
)

// OIDCAuthType defines the OIDC authentication method type.
// +enum
type OIDCAuthType string

const (
	// OIDCAuthTypeClientSecret is the OIDC authentication method using a client secret.
	OIDCAuthTypeClientSecret OIDCAuthType = "clientSecret"

	// OIDCAuthTypePrivateKeyJWT is the OIDC authentication method using a private key JWT.
	OIDCAuthTypePrivateKeyJWT OIDCAuthType = "privateKeyJWT"

	// OIDCAuthTypeTLSClientAuth is the OIDC authentication method using TLS client authentication.
	OIDCAuthTypeTLSClientAuth OIDCAuthType = "tlsClientAuth"
)
