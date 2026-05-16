package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
)

// HelmRegistrySpec defines the desired state of HelmRegistry
type HelmRegistrySpec struct {
	// URL is the URL of the Helm registry.
	URL string `json:"url"`

	// Auth is the authentication method used to access the Helm registry.
	Auth HelmRegistryAuthSpec `json:"auth"`

	// Interval is the poll interval for ref resolution and content fetching.
	Interval metav1.Duration `json:"interval"`
}

// HelmRegistryAuthSpec defines the authentication method used to access the Helm registry.
type HelmRegistryAuthSpec struct {
	// Type is the authentication method used to access the Helm registry.
	Type authv1.AuthType `json:"type"`

	// Basic is the basic authentication used to access the Helm registry.
	Basic *authv1.BasicAuthSpec `json:"basic,omitempty"`

	// Token is the token used to access the Helm registry.
	Token *authv1.TokenAuthSpec `json:"token,omitempty"`

	// OIDC is the OIDC configuration used to access the Helm registry.
	OIDC *authv1.OIDAuthSpec `json:"oidc,omitempty"`
}

// HelmRegistryStatus reflects the observed state of HelmRegistry
type HelmRegistryStatus struct{}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:prerelease-lifecycle-gen:introduced=1.0
// +kubebuilder:object:root=true

type HelmRegistry struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HelmRegistrySpec   `json:"spec,omitempty"`
	Status HelmRegistryStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type HelmRegistryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HelmRegistry `json:"items"`
}
