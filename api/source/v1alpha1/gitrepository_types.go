package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
)

// GitRepositorySpec defines the desired state of the GitRepository.
type GitRepositorySpec struct {
	// URL is the URL of the Git repository.
	URL string `json:"url"`

	// Ref is what to track in the Git repository.
	Ref GitRepositoryRefSpec `json:"ref"`

	// Auth is the authentication method used to access the Git repository.
	Auth GitRepositoryAuthSpec `json:"auth"`

	// Interval is the poll interval for ref resolution and content fetching.
	Interval metav1.Duration `json:"interval"`

	// IncludePaths is a list of paths to include in the Git repository.
	// +listType=atomic
	IncludePaths []string `json:"includePaths,omitempty"`

	// IgnorePaths is a list of paths to ignore in the Git repository.
	// +listType=atomic
	IgnorePaths []string `json:"ignorePaths,omitempty"`

	// TLS is the TLS configuration used to verify the Git repository.
	TLS *authv1.TLSSpec `json:"tls,omitempty"`
}

// GitRepositoryRefSpec defines the desired state of the GitRepositoryRef.
type GitRepositoryRefSpec struct {
	// Branch is the branch to track in the Git repository.
	Branch *string `json:"branch,omitempty"`

	// Tag is the tag to track in the Git repository.
	Tag *string `json:"tag,omitempty"`

	// Commit is the commit to track in the Git repository.
	Commit *string `json:"commit,omitempty"`

	// Semver is the tag-based semantic version expression to track in the Git repository.
	Semver *string `json:"semver,omitempty"`
}

// GitRepositoryAuthSpec defines the authentication method used to access the Git repository.
type GitRepositoryAuthSpec struct {
	// Type is the authentication method used to access the Git repository.
	Type authv1.AuthType `json:"type"`

	// SSHKey is the SSH key used to access the Git repository.
	SSHKey *authv1.SSHKeyAuthSpec `json:"sshKey,omitempty"`

	// Basic is the basic authentication used to access the Git repository.
	Basic *authv1.BasicAuthSpec `json:"basic,omitempty"`

	// Token is the token used to access the Git repository.
	Token *authv1.TokenAuthSpec `json:"token,omitempty"`

	// GithubApp is the GitHub App configuration used to access the Git repository.
	GithubApp *authv1.GithubAppAuthSpec `json:"githubApp,omitempty"`

	// OIDC is the OIDC configuration used to access the Git repository.
	OIDC *authv1.OIDAuthSpec `json:"oidc,omitempty"`
}

// GitRepositoryWebhookSpec defines the desired state of the GitRepositoryWebhook.
type GitRepositoryWebhookSpec struct {
	// Name is the name of the webhook.
	Name string `json:"name"`

	// Provider is the name of the webhook provider.
	Provider string `json:"provider"`

	// SecretRef is the name and the key of the Secret containing the webhook secret.
	SecretRef corev1.SecretKeySelector `json:"secretRef"`

	// Events is the list of events that trigger the webhook.
	// +listType=atomic
	Events []string `json:"events"`

	// Filters is the list of paths and branches that trigger the webhook.
	Filters *GitRepositoryWebhookFilterSpec `json:"filters,omitempty"`
}

// GitRepositoryWebhookFilterSpec defines the list of paths and branches that trigger the webhook.
type GitRepositoryWebhookFilterSpec struct {
	// Refs is the list of branches that trigger the webhook.
	// +listType=atomic
	Refs []string `json:"refs"`

	// Paths is the list of paths that trigger the webhook.
	// +listType=atomic
	Paths []string `json:"paths"`
}

// GitRepositoryStatus reflects the observed state of the GitRepository.
type GitRepositoryStatus struct {
	// ObservedGeneration is the last generation that was observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// LastFetched is the last time the Git repository was fetched.
	LastFetched GitRepositoryLastFetchedStatus `json:"lastFetched,omitempty"`

	// ReleaseCount is the number of Release referencing the GitRepository.
	ReleaseCount int64 `json:"releaseCount,omitempty"`

	// Webhooks is the list of statuses of webhooks that are configured for the GitRepository.
	// +listType=map
	// +listMapKey=name
	Webhooks []WebhookStatus `json:"webhooks,omitempty"`

	// Conditions is the list of conditions related to the GitRepository.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// GitRepositoryLastFetchedStatus reflects the last time the Git repository was fetched.
type GitRepositoryLastFetchedStatus struct {
	// Revision is the revision of the Git repository.
	Revision string `json:"revision,omitempty"`

	// TreeHash is the tree hash of the Git repository.
	TreeHash string `json:"treeHash,omitempty"`

	// FetchedAt is the time the Git repository was fetched.
	FetchedAt metav1.Time `json:"fetchedAt,omitempty"`

	// ResolvedRef is the resolved reference of the Git repository.
	ResolvedRef string `json:"resolvedRef,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:prerelease-lifecycle-gen:introduced=1.0
// +kubebuilder:object:root=true

type GitRepository struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec GitRepositorySpec `json:"spec,omitempty"`
	// +listType=map
	// +listMapKey=name
	Webhook []GitRepositoryWebhookSpec `json:"webhook,omitempty"`
	Status  GitRepositoryStatus        `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type GitRepositoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GitRepository `json:"items"`
}
