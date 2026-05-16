package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// ClientCertAuthSpec defines the authentication mode using a client certificate.
type ClientCertAuthSpec struct {
	// CertRef is the name and the key of the Secret containing the client certificate.
	CertRef corev1.SecretKeySelector `json:"certRef"`

	// KeyRef is the name and the key of the Secret containing the client certificate key.
	KeyRef corev1.SecretKeySelector `json:"keyRef"`
}
