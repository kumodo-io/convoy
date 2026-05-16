package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// GithubAppAuthSpec defines the GitHub App configuration used for authentication.
type GithubAppAuthSpec struct {
	// AppID is the ID of the GitHub App.
	AppID int64 `json:"appID"`

	// InstallationID is the ID of the GitHub App installation.
	InstallationID int64 `json:"installationID"`

	// PrivateKeyRef is the name and the key of the Secret containing the private key.
	PrivateKeyRef corev1.SecretKeySelector `json:"privateKey"`
}
