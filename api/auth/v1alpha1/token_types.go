package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// TokenAuthSpec defines the token used for authentication.
type TokenAuthSpec struct {
	// TokenRef is the name and the key of the Secret containing the token.
	TokenRef corev1.SecretKeySelector `json:"tokenRef"`
}
