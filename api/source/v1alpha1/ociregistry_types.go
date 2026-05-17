package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
)

// ContentType defines the type of content fetched from the OCI registry.
// +enum
type ContentType string

const (
	// ContentTypeHelm is the content type used to fetch Helm charts.
	ContentTypeHelm ContentType = "helm"

	// ContentTypeManifests is the content type used to fetch tarballs of raw manifests.
	ContentTypeManifests ContentType = "manifests"

	// ContentTypeKustomize is the content type used to fetch kustomize bundles.
	ContentTypeKustomize ContentType = "kustomize"
)

// OCIRegistrySpec defines the desired state of OCIRegistry
type OCIRegistrySpec struct {
	// URL is the URL of the OCI registry.
	URL string `json:"url"`

	// Auth is the authentication method used to access the OCI registry.
	Auth OCIRegistryAuthSpec `json:"auth"`

	// Interval is the poll interval for ref resolution and content fetching.
	Interval metav1.Duration `json:"interval"`

	// TLS is the TLS configuration used to verify the OCI registry.
	TLS *authv1.TLSSpec `json:"tls,omitempty"`

	// DefaultContentType is the default content type used to fetch content from the OCI registry.
	DefaultContentType ContentType `json:"defaultContentType"`
}

// OCIRegistryAuthSpec defines the authentication method used to access the OCI registry.
type OCIRegistryAuthSpec struct {
	// Type is the authentication method used to access the OCI registry.
	Type authv1.AuthType `json:"type"`

	// Basic is the basic authentication used to access the OCI registry.
	Basic *authv1.BasicAuthSpec `json:"basic,omitempty"`

	// Token is the token used to access the OCI registry.
	Token *authv1.TokenAuthSpec `json:"token,omitempty"`

	// DockerConfig is the Docker config used to access the OCI registry.
	DockerConfig *authv1.DockerConfigAuthSpec `json:"dockerConfig,omitempty"`

	// OIDC is the OIDC configuration used to access the OCI registry.
	OIDC *authv1.OIDAuthSpec `json:"oidc,omitempty"`
}

// OCIRegistryWebhookSpec defines the desired state of OCIRegistryWebhook.
type OCIRegistryWebhookSpec struct {
	// Name is the name of the webhook.
	Name string `json:"name"`

	// Provider is the name of the webhook provider.
	Provider string `json:"provider"`

	// SecretRef is the name and the key of the Secret containing the webhook secret.
	SecretRef corev1.SecretKeySelector `json:"secretRef"`

	// Events is the list of events that trigger the webhook.
	// +listType=atomic
	Events []string `json:"events"`

	// Filters is the list of repositories and tags that trigger the webhook.
	Filters *OCIRegistryWebhookFilterSpec `json:"filters,omitempty"`
}

// OCIRegistryWebhookFilterSpec defines the list of repositories that trigger the webhook.
type OCIRegistryWebhookFilterSpec struct {
	// Repositories is the list of repositories that trigger the webhook.
	// +listType=atomic
	Repositories []string `json:"repositories"`

	// Tags is the list of tags that trigger the webhook.
	// +listType=atomic
	Tags []string `json:"tags"`
}

// OCIRegistryStatus reflects the observed state of OCIRegistry
type OCIRegistryStatus struct {
	// ObservedGeneration is the last generation that was observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// LastChecked is the last time the OCI registry was checked.
	LastChecked OCIRegistryLastCheckedStatus `json:"lastChecked,omitempty"`

	// ReleaseCount is the number of Release referencing the OCIRegistry.
	ReleaseCount int64 `json:"releaseCount,omitempty"`

	// Webhooks is the list of statuses of webhooks that are configured for the OCIRegistry.
	// +listType=map
	// +listMapKey=name
	Webhooks []WebhookStatus `json:"webhooks,omitempty"`

	// Conditions is the list of conditions related to the HelmRegistry.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// OCIRegistryLastCheckedStatus reflects the last time the OCI registry was checked.
type OCIRegistryLastCheckedStatus struct {
	// CheckedAt is the time the OCI registry was last checked.
	CheckedAt metav1.Time `json:"checkedAt,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:prerelease-lifecycle-gen:introduced=1.0
// +kubebuilder:object:root=true

type OCIRegistry struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec OCIRegistrySpec `json:"spec,omitempty"`
	// +listType=map
	// +listMapKey=name
	Webhook []OCIRegistryWebhookSpec `json:"webhook,omitempty"`
	Status  OCIRegistryStatus        `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type OCIRegistryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OCIRegistry `json:"items"`
}
