package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// KubeconfigAuthSpec defines the authentication mode using a kubeconfig file.
type KubeconfigAuthSpec struct {
	// SecretRef is the name and the key of the Secret containing the kubeconfig file.
	SecretRef corev1.SecretKeySelector `json:"secretRef"`
}
