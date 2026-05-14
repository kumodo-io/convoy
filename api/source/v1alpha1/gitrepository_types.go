package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GitRepositoryAuthType defines the authentication method used to access the Git repository.
// +enum
type GitRepositoryAuthType string

const (
	// GitRepositoryAuthTypeNone is the authentication method using no authentication.
	GitRepositoryAuthTypeNone GitRepositoryAuthType = "none"

	// GitRepositoryAuthTypeSSHKey is the authentication method using SSH key authentication.
	GitRepositoryAuthTypeSSHKey GitRepositoryAuthType = "sshKey"

	// GitRepositoryAuthTypeBasic is the authentication method using basic authentication.
	GitRepositoryAuthTypeBasic GitRepositoryAuthType = "basic"

	// GitRepositoryAuthTypeToken is the authentication method using a token.
	GitRepositoryAuthTypeToken GitRepositoryAuthType = "token"

	// GitRepositoryAithTypeGithubApp is the authentication method using a GitHub App.
	GitRepositoryAithTypeGithubApp GitRepositoryAuthType = "githubApp"
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
	Type GitRepositoryAuthType `json:"type"`

	// SSHKey is the SSH key used to access the Git repository.
	SSHKey *GitRepositoryAuthSSHKeySpec `json:"sshKey,omitempty"`

	// Basic is the basic authentication used to access the Git repository.
	Basic *GitRepositoryAuthBasicSpec `json:"basic,omitempty"`

	// Token is the token used to access the Git repository.
	Token *GitRepositoryAuthTokenSpec `json:"token,omitempty"`

	// GithubApp is the GitHub App configuration used to access the Git repository.
	GithubApp *GitRepositoryAuthGithubAppSpec `json:"githubApp,omitempty"`
}

// GitRepositoryAuthSSHKeySpec defines the SSH key used to access the Git repository.
type GitRepositoryAuthSSHKeySpec struct {
	// SSHKeyRef is the name and the key of the Secret containing the SSH key.
	SSHKeyRef corev1.SecretKeySelector `json:"sshKeyRef"`

	// KnownHostsRef is the name and the key of the ConfigMap containing the known hosts.
	KnownHostsRef corev1.ConfigMapKeySelector `json:"knownHostsRef"`
}

// GitRepositoryAuthBasicSpec defines the basic authentication used to access the Git repository.
type GitRepositoryAuthBasicSpec struct {
	// Username is the username used to access the Git repository.
	Username string `json:"username"`

	// PasswordRef is the name and the key of the Secret containing the password.
	PasswordRef corev1.SecretKeySelector `json:"passwordRef"`
}

// GitRepositoryAuthTokenSpec defines the token used to access the Git repository.
type GitRepositoryAuthTokenSpec struct {
	// TokenRef is the name and the key of the Secret containing the token.
	TokenRef corev1.SecretKeySelector `json:"tokenRef"`
}

// GitRepositoryAuthGithubAppSpec defines the GitHub App configuration used to access the Git repository.
type GitRepositoryAuthGithubAppSpec struct {
	// AppID is the ID of the GitHub App.
	AppID int64 `json:"appID"`

	// InstallationID is the ID of the GitHub App installation.
	InstallationID int64 `json:"installationID"`

	// PrivateKeyRef is the name and the key of the Secret containing the private key.
	PrivateKeyRef corev1.SecretKeySelector `json:"privateKey"`
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
	Webhooks []GitRepositoryWebhookStatus `json:"webhooks,omitempty"`

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

// GitRepositoryWebhookStatus reflects the status of a webhook.
type GitRepositoryWebhookStatus struct {
	// Name is the name of the webhook.
	Name string `json:"name"`

	// URL is the URL of the webhook.
	URL string `json:"url"`

	// LastReceived is the last time the webhook was received.
	LastReceived *metav1.Time `json:"lastReceived,omitempty"`

	// LastTriggered is the last time the webhook was triggered.
	LastTriggered *metav1.Time `json:"lastTriggered,omitempty"`

	// ReceivedCount is the number of times the webhook was received.
	ReceivedCount int64 `json:"receivedCount,omitempty"`

	// TriggersCount is the number of times the webhook was triggered.
	TriggersCount int64 `json:"triggersCount,omitempty"`

	// RejectedCount is the number of times the webhook was rejected.
	RejectedCount int64 `json:"rejectedCount,omitempty"`

	// FilteredCount is the number of times the webhook was filtered.
	FilteredCount int64 `json:"filteredCount,omitempty"`

	// LastRejection is the last time the webhook was rejected.
	LastRejection *GitRepositoryWebhookRejectionStatus `json:"lastRejection,omitempty"`
}

// GitRepositoryWebhookRejectionStatus reflects the last time the webhook was rejected.
type GitRepositoryWebhookRejectionStatus struct {
	// RejectedAt is the time the webhook was rejected.
	RejectedAt *metav1.Time `json:"rejectedAt,omitempty"`

	// Reason is the reason the webhook was rejected.
	Reason string `json:"reason,omitempty"`

	// Message is the message of the webhook rejection.
	Message string `json:"message,omitempty"`
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
