package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// SSHKeyAuthSpec defines the SSH key used for authentication.
type SSHKeyAuthSpec struct {
	// SSHKeyRef is the name and the key of the Secret containing the SSH key.
	SSHKeyRef corev1.SecretKeySelector `json:"sshKeyRef"`

	// KnownHostsRef is the name and the key of the ConfigMap containing the known hosts.
	KnownHostsRef corev1.ConfigMapKeySelector `json:"knownHostsRef"`
}
