package source

import (
	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ---------- GitRepository types ----------

type GitRepositorySpec struct {
	URL          string
	Ref          GitRepositoryRefSpec
	Auth         GitRepositoryAuthSpec
	Interval     metav1.Duration
	IncludePaths []string
	IgnorePaths  []string
}

type GitRepositoryRefSpec struct {
	Branch *string
	Tag    *string
	Commit *string
	Semver *string
}

type GitRepositoryAuthSpec struct {
	Type      authv1.AuthType
	SSHKey    *authv1.SSHKeyAuthSpec
	Basic     *authv1.BasicAuthSpec
	Token     *authv1.TokenAuthSpec
	GithubApp *authv1.GithubAppAuthSpec
	OIDC      *authv1.OIDAuthSpec
}

type GitRepositoryWebhookSpec struct {
	Name      string
	Provider  string
	SecretRef corev1.SecretKeySelector
	Events    []string
	Filters   *GitRepositoryWebhookFilterSpec
}

type GitRepositoryWebhookFilterSpec struct {
	Refs  []string
	Paths []string
}

type GitRepositoryStatus struct {
	ObservedGeneration int64
	LastFetched        GitRepositoryLastFetchedStatus
	ReleaseCount       int64
	Webhooks           []GitRepositoryWebhookStatus
	Conditions         []metav1.Condition
}

type GitRepositoryLastFetchedStatus struct {
	Revision    string
	TreeHash    string
	FetchedAt   metav1.Time
	ResolvedRef string
}

type GitRepositoryWebhookStatus struct {
	Name          string
	URL           string
	LastReceived  *metav1.Time
	LastTriggered *metav1.Time
	ReceivedCount int64
	TriggersCount int64
	RejectedCount int64
	FilteredCount int64
	LastRejection *GitRepositoryWebhookRejectionStatus
}

type GitRepositoryWebhookRejectionStatus struct {
	RejectedAt *metav1.Time
	Reason     string
	Message    string
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type GitRepository struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec    GitRepositorySpec
	Webhook []GitRepositoryWebhookSpec
	Status  GitRepositoryStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type GitRepositoryList struct {
	metav1.TypeMeta
	metav1.ListMeta

	Items []GitRepository
}

// ---------- HelmRegistry types ----------

type HelmRegistrySpec struct {
	URL      string
	Auth     HelmRegistryAuthSpec
	Interval metav1.Duration
}

type HelmRegistryAuthSpec struct {
	Type  authv1.AuthType
	Basic *authv1.BasicAuthSpec
	Token *authv1.TokenAuthSpec
	OIDC  *authv1.OIDAuthSpec
}

type HelmRegistryStatus struct{}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type HelmRegistry struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec   HelmRegistrySpec
	Status HelmRegistryStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type HelmRegistryList struct {
	metav1.TypeMeta
	metav1.ListMeta

	Items []HelmRegistry
}

type OCIRegistrySpec struct{}

type OCIRegistryStatus struct{}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type OCIRegistry struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec   OCIRegistrySpec
	Status OCIRegistryStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type OCIRegistryList struct {
	metav1.TypeMeta
	metav1.ListMeta

	Items []OCIRegistry
}
