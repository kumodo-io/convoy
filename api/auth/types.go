package auth

import (
	corev1 "k8s.io/api/core/v1"
)

// ---------- Common types ----------

type AuthType string

const (
	AuthTypeNone             AuthType = "none"
	AuthTypeBasic            AuthType = "basic"
	AuthTypeToken            AuthType = "token"
	AuthTypeSSHKey           AuthType = "sshKey"
	AuthTypeGithubApp        AuthType = "githubApp"
	AuthTypeOIDC             AuthType = "oidc"
	AuthTypeKubeconfig       AuthType = "kubeconfig"
	AuthTypeExecPlugin       AuthType = "execPlugin"
	AuthTypeClientCert       AuthType = "clientCert"
	AuthTypeWorkloadIdentity AuthType = "workloadIdentity"
)

type TLSSpec struct {
	CAPublicCertRef    corev1.ConfigMapKeySelector
	InsecureSkipVerify bool
}

// ---------- Basic types ----------

type BasicAuthSpec struct {
	Username    string
	PasswordRef corev1.SecretKeySelector
}

// ---------- Token types ----------

type TokenAuthSpec struct {
	TokenRef corev1.SecretKeySelector
}

// ---------- SSHKey types ----------

type SSHKeyAuthSpec struct {
	SSHKeyRef     corev1.SecretKeySelector
	KnownHostsRef corev1.ConfigMapKeySelector
}

// ---------- GithubApp types ---------

type GithubAppAuthSpec struct {
	AppID          int64
	InstallationID int64
	PrivateKeyRef  corev1.SecretKeySelector
}

// ---------- Kubeconfig types ---------

type KubeconfigAuthSpec struct {
	SecretRef corev1.SecretKeySelector
}

// ---------- ClientCert types ---------

type ClientCertAuthSpec struct {
	CertRef corev1.SecretKeySelector
	KeyRef  corev1.SecretKeySelector
}

// ---------- ExecPlugin types ----------

type ExecPluginAuthSpec struct {
	Command        string
	Args           []string
	Env            []corev1.EnvVar
	CredentialsRef corev1.SecretReference
}

// ---------- OIDC types ----------

type OIDCAuthType string

const (
	OIDCAuthTypeClientSecret  OIDCAuthType = "clientSecret"
	OIDCAuthTypePrivateKeyJWT OIDCAuthType = "privateKeyJWT"
	OIDCAuthTypeTLSClientAuth OIDCAuthType = "tlsClientAuth"
)

type OIDAuthSpec struct {
	IssuerURL  string
	ClientID   string
	ClientAuth OIDCClientAuthSpec
	Scopes     []string
	Audience   string
	TLS        *TLSSpec
}

type OIDCClientAuthSpec struct {
	Type          OIDCAuthType
	ClientSecret  *OIDCAuthClientSecretSpec
	TLSClientAuth *OIDCAuthTLSClientAuthSpec
	PrivateKeyJWT *OIDCAuthPrivateKeyJWTSpec
}

type OIDCAuthClientSecretSpec struct {
	ClientSecretRef corev1.SecretKeySelector
}

type OIDCAuthTLSClientAuthSpec struct {
	CertRef corev1.SecretKeySelector
	KeyRef  corev1.SecretKeySelector
}

type OIDCAuthPrivateKeyJWTSpec struct {
	PrivateKeyRef corev1.SecretKeySelector
}

type OIDCTLSSpec struct {
	CAPublicCertRef    corev1.ConfigMapKeySelector
	InsecureSkipVerify bool
}
