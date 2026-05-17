package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// DockerConfigAuthSpec defines the Docker configuration used for authentication.
type DockerConfigAuthSpec struct {
	SecretRef corev1.SecretKeySelector `json:"secretRef"`
}
