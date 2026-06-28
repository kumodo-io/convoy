package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
)

// HelmRegistryAuthType is a subset of authv1.AuthType.
// It defines only those authentication modes that are valid for the HelmRegistry controller.
// +enum
type HelmRegistryAuthType authv1.AuthType

const (
	// HelmRegistryAuthTypeNone is the authentication method using no authentication.
	HelmRegistryAuthTypeNone = HelmRegistryAuthType(authv1.AuthTypeNone)

	// HelmRegistryAuthTypeBasic is the authentication method using basic authentication.
	HelmRegistryAuthTypeBasic = HelmRegistryAuthType(authv1.AuthTypeBasic)

	// HelmRegistryAuthTypeToken is the authentication method using a token.
	HelmRegistryAuthTypeToken = HelmRegistryAuthType(authv1.AuthTypeToken)

	// HelmRegistryAuthTypeOIDC is the authentication method using OIDC.
	HelmRegistryAuthTypeOIDC = HelmRegistryAuthType(authv1.AuthTypeOIDC)

	// HelmRegistryAuthTypeCloudIdentity is the authentication method using a cloud identity.
	HelmRegistryAuthTypeCloudIdentity = HelmRegistryAuthType(authv1.AuthTypeCloudIdentity)
)

// HelmRegistrySpec defines the desired state of HelmRegistry
type HelmRegistrySpec struct {
	// URL is the URL of the Helm registry.
	URL string `json:"url"`

	// Auth is the authentication method used to access the Helm registry.
	Auth HelmRegistryAuthSpec `json:"auth"`

	// Interval is the poll interval for ref resolution and content fetching.
	Interval metav1.Duration `json:"interval"`

	// MinSyncInterval is the minimum interval between syncs. Used to rate limit syncs.
	MinSyncInterval metav1.Duration `json:"minSyncInterval"`

	// TLS is the TLS configuration used to verify the Helm registry.
	TLS *authv1.TLSSpec `json:"tls,omitempty"`
}

// HelmRegistryAuthSpec defines the authentication method used to access the Helm registry.
type HelmRegistryAuthSpec struct {
	// Type is the authentication method used to access the Helm registry.
	Type HelmRegistryAuthType `json:"type"`

	// Basic is the basic authentication used to access the Helm registry.
	Basic *authv1.BasicAuthSpec `json:"basic,omitempty"`

	// Token is the token used to access the Helm registry.
	Token *authv1.TokenAuthSpec `json:"token,omitempty"`

	// OIDC is the OIDC configuration used to access the Helm registry.
	OIDC *authv1.OIDAuthSpec `json:"oidc,omitempty"`

	// CloudIdentity is the cloud identity configuration used to access the Helm registry.
	CloudIdentity *authv1.CloudIdentitySpec `json:"cloudIdentity,omitempty"`
}

// HelmRegistryWebhookSpec defines the desired state of HelmRegistryWebhook.
type HelmRegistryWebhookSpec struct {
	// Name is the name of the webhook.
	Name string `json:"name"`

	// Provider is the name of the webhook provider.
	Provider string `json:"provider"`

	// SecretRef is the name and the key of the Secret containing the webhook secret.
	SecretRef corev1.SecretKeySelector `json:"secretRef"`

	// Events is the list of events that trigger the webhook.
	// +listType=atomic
	Events []string `json:"events"`

	// Filters is the list of charts that trigger the webhook.
	Filters *HelmRegistryWebhookFilterSpec `json:"filters,omitempty"`
}

// HelmRegistryWebhookFilterSpec defines the list of charts that trigger the webhook.
type HelmRegistryWebhookFilterSpec struct {
	// Charts is the list of charts that trigger the webhook.
	// +listType=atomic
	Charts []string `json:"charts"`
}

// HelmRegistryStatus reflects the observed state of HelmRegistry
type HelmRegistryStatus struct {
	// ObservedGeneration is the last generation that was observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ReleaseCount is the number of Release referencing the HelmRegistry.
	ReleaseCount int64 `json:"releaseCount,omitempty"`

	// Webhooks is the list of statuses of webhooks that are configured for the HelmRegistry.
	// +listType=map
	// +listMapKey=name
	Webhooks []WebhookStatus `json:"webhooks,omitempty"`

	// Conditions is the list of conditions related to the HelmRegistry.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:prerelease-lifecycle-gen:introduced=1.0
// +kubebuilder:object:root=true

type HelmRegistry struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec HelmRegistrySpec `json:"spec,omitempty"`
	// +listType=map
	// +listMapKey=name
	Webhook []HelmRegistryWebhookSpec `json:"webhook,omitempty"`
	Status  HelmRegistryStatus        `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type HelmRegistryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HelmRegistry `json:"items"`
}
