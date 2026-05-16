package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HelmRegistryAuthType defines the authentication method used to access the Helm registry.
// +enum
type HelmRegistryAuthType string

const (
	// HelmRegistryAuthTypeNone is the authentication method using no authentication.
	HelmRegistryAuthTypeNone HelmRegistryAuthType = "none"

	// HelmRegistryAuthTypeBasic is the authentication method using basic authentication.
	HelmRegistryAuthTypeBasic HelmRegistryAuthType = "basic"

	// HelmRegistryAuthTypeToken is the authentication method using a token.
	HelmRegistryAuthTypeToken HelmRegistryAuthType = "token"

	// HelmRegistryAuthTypeOIDC is the authentication method using OIDC.
	HelmRegistryAuthTypeOIDC HelmRegistryAuthType = "oidc"
)

// OIDCAuthType defines the OIDC authentication method type used to access the Helm registry.
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

// HelmRegistrySpec defines the desired state of HelmRegistry
type HelmRegistrySpec struct {
	// URL is the URL of the Helm registry.
	URL string `json:"url"`

	// Auth is the authentication method used to access the Helm registry.
	Auth HelmRegistryAuthSpec `json:"auth"`

	// Interval is the poll interval for ref resolution and content fetching.
	Interval metav1.Duration `json:"interval"`
}

// HelmRegistryAuthSpec defines the authentication method used to access the Helm registry.
type HelmRegistryAuthSpec struct {
	// Type is the authentication method used to access the Helm registry.
	Type HelmRegistryAuthType `json:"type"`

	// Basic is the basic authentication used to access the Helm registry.
	Basic *HelmRegistryAuthBasicSpec `json:"basic,omitempty"`

	// Token is the token used to access the Helm registry.
	Token *HelmRegistryAuthTokenSpec `json:"token,omitempty"`

	// OIDC is the OIDC configuration used to access the Helm registry.
	OIDC *HelmRegistryAuthOIDCSpec `json:"oidc,omitempty"`
}

// HelmRegistryAuthBasicSpec defines the basic authentication used to access the Helm registry.
type HelmRegistryAuthBasicSpec struct {
	// Username is the username used to access the Helm registry.
	Username string `json:"username"`

	// PasswordRef is the name and the key of the Secret containing the password.
	PasswordRef corev1.SecretKeySelector `json:"passwordRef"`
}

// HelmRegistryAuthTokenSpec defines the token used to access the Helm registry.
type HelmRegistryAuthTokenSpec struct {
	// TokenRef is the name and the key of the Secret containing the token.
	TokenRef corev1.SecretKeySelector `json:"tokenRef"`
}

// HelmRegistryAuthOIDCSpec defines the OIDC configuration used to access the Helm registry.
type HelmRegistryAuthOIDCSpec struct {
	// IssuerURL is the URL of the OIDC issuer.
	IssuerURL string `json:"issuerURL"`

	// ClientID is the OIDC client ID.
	ClientID string `json:"clientID"`

	// ClientAuth is the OIDC authentication method used to access the Helm registry.
	ClientAuth OIDCClientAuthSpec `json:"clientAuth"`

	// Scopes are the OIDC scopes.
	// +listType=atomic
	Scopes []string `json:"scopes,omitempty"`

	// Audience is the OIDC audience.
	Audience string `json:"audience,omitempty"`

	// TLS is the IdP TLS configuration.
	TLS *OIDCTLSSpec `json:"tls,omitempty"`
}

// OIDCClientAuthSpec defines the OIDC authentication method type used to access the Helm registry.
type OIDCClientAuthSpec struct {
	// Type is the OIDC authentication method type used to access the Helm registry.
	Type OIDCAuthType `json:"type"`

	// ClientSecret is the reference to the Secret and the key of the Secret containing the OIDC client secret.
	ClientSecret *OIDCAuthClientSecretSpec `json:"clientSecret,omitempty"`

	// TLSClientAuth is the reference to the Secrets and the keys of the Secret(s) containing the OIDC client certificate and key.
	TLSClientAuth *OIDCAuthTLSClientAuthSpec `json:"tlsClientAuth,omitempty"`

	// PrivateKeyJWT is the reference to the Secret and the key of the Secret containing the OIDC private key JWT.
	PrivateKeyJWT *OIDCAuthPrivateKeyJWTSpec `json:"privateKeyJWT,omitempty"`
}

// OIDCAuthClientSecretSpec defines the reference to the Secret
// and the key of the Secret containing the OIDC client secret.
type OIDCAuthClientSecretSpec struct {
	// ClientSecretRef is the name and the key of the Secret containing the OIDC client secret.
	ClientSecretRef corev1.SecretKeySelector `json:"clientSecretRef"`
}

// OIDCAuthTLSClientAuthSpec defines the reference to the Secrets
// and the keys of the Secret(s) containing the OIDC client certificate and key.
type OIDCAuthTLSClientAuthSpec struct {
	// CertRef is the name and the key of the Secret containing the OIDC client certificate.
	CertRef corev1.SecretKeySelector `json:"certRef"`

	// KeyRef is the name and the key of the Secret containing the OIDC client certificate key.
	KeyRef corev1.SecretKeySelector `json:"keyRef"`
}

// OIDCAuthPrivateKeyJWTSpec defines the reference to the Secret
// and the key of the Secret containing the OIDC private key JWT.
type OIDCAuthPrivateKeyJWTSpec struct {
	// PrivateKeyRef is the name and the key of the Secret containing the OIDC private key JWT.
	PrivateKeyRef corev1.SecretKeySelector `json:"privateKeyRef"`
}

type OIDCTLSSpec struct {
	// CAPublicCertRef is the name and the key of the ConfigMap containing the CA public certificate.
	// The CA certificate is used to verify the IdP server's certificate.
	CAPublicCertRef corev1.ConfigMapKeySelector `json:"caPublicCertRef"`

	// InsecureSkipVerify is a flag to skip the certificate verification.
	InsecureSkipVerify bool `json:"insecureSkipVerify"`
}

// HelmRegistryStatus reflects the observed state of HelmRegistry
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
