package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// BasicAuthSpec defines the basic authentication.
type BasicAuthSpec struct {
	// Username is the username used for authentication.
	Username string `json:"username"`

	// PasswordRef is the name and the key of the Secret containing the password.
	PasswordRef corev1.SecretKeySelector `json:"passwordRef"`
}
