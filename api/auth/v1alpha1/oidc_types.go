package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// OIDAuthSpec defines the OIDC configuration used for authentication.
type OIDAuthSpec struct {
	// IssuerURL is the URL of the OIDC issuer.
	IssuerURL string `json:"issuerURL"`

	// ClientID is the OIDC client ID.
	ClientID string `json:"clientID"`

	// ClientAuth is the OIDC authentication method used for authentication.
	ClientAuth OIDCClientAuthSpec `json:"clientAuth"`

	// Scopes are the OIDC scopes.
	// +listType=atomic
	Scopes []string `json:"scopes,omitempty"`

	// Audience is the OIDC audience.
	Audience string `json:"audience,omitempty"`

	// TLS is the issuer TLS configuration.
	TLS *OIDCTLSSpec `json:"tls,omitempty"`
}

// OIDCClientAuthSpec defines the OIDC authentication method configuration used for authentication.
type OIDCClientAuthSpec struct {
	// Type is the OIDC authentication method type used for authentication.
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
