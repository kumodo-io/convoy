package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// ExecPluginAuthSpec defines the authentication mode using an exec plugin.
type ExecPluginAuthSpec struct {
	// Command is the command to execute.
	Command string `json:"command"`

	// Args are the arguments to pass to the command.
	// +listType=atomic
	Args []string `json:"args,omitempty"`

	// Env are the environment variables to pass to the command.
	// +listType=map
	// +listMapKey=name
	Env []corev1.EnvVar `json:"env,omitempty"`

	// CredentialsRef is the optional reference to credentials the plugin needs (e.g., AWS IAM role).
	// Defined by the reference to the Secret and the key of the Secret containing the credentials.
	CredentialsRef corev1.SecretReference `json:"credentialsRef"`
}
