package source

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
)

// ---------- Common types ----------

type WebhookStatus struct {
	Name          string
	URL           string
	LastReceived  *metav1.Time
	LastTriggered *metav1.Time
	ReceivedCount int64
	TriggersCount int64
	RejectedCount int64
	FilteredCount int64
	LastRejection *LastRejectionStatus
}

type LastRejectionStatus struct {
	RejectedAt *metav1.Time
	Reason     string
	Message    string
}

// ---------- GitRepository types ----------

type GitRepositoryAuthType authv1.AuthType

const (
	GitRepositoryAuthTypeNone          = GitRepositoryAuthType(authv1.AuthTypeNone)
	GitRepositoryAuthTypeBasic         = GitRepositoryAuthType(authv1.AuthTypeBasic)
	GitRepositoryAuthTypeToken         = GitRepositoryAuthType(authv1.AuthTypeToken)
	GitRepositoryAuthTypeSSHKey        = GitRepositoryAuthType(authv1.AuthTypeSSHKey)
	GitRepositoryAuthTypeGithubApp     = GitRepositoryAuthType(authv1.AuthTypeGithubApp)
	GitRepositoryAuthTypeOIDC          = GitRepositoryAuthType(authv1.AuthTypeOIDC)
	GitRepositoryAuthTypeCloudIdentity = GitRepositoryAuthType(authv1.AuthTypeCloudIdentity)
)

type GitRepositorySpec struct {
	URL             string
	Auth            GitRepositoryAuthSpec
	Interval        metav1.Duration
	MinSyncInterval metav1.Duration
	IncludePaths    []string
	IgnorePaths     []string
	TLS             *authv1.TLSSpec
}

type GitRepositoryAuthSpec struct {
	Type          GitRepositoryAuthType
	SSHKey        *authv1.SSHKeyAuthSpec
	Basic         *authv1.BasicAuthSpec
	Token         *authv1.TokenAuthSpec
	GithubApp     *authv1.GithubAppAuthSpec
	OIDC          *authv1.OIDAuthSpec
	CloudIdentity *authv1.CloudIdentitySpec
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
	ObservedGeneration    int64
	LastSuccessfulProbeAt *metav1.Time
	ReleaseCount          int64
	Webhooks              []WebhookStatus
	Conditions            []metav1.Condition
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

type HelmRegistryAuthType authv1.AuthType

const (
	HelmRegistryAuthTypeNone          = HelmRegistryAuthType(authv1.AuthTypeNone)
	HelmRegistryAuthTypeBasic         = HelmRegistryAuthType(authv1.AuthTypeBasic)
	HelmRegistryAuthTypeToken         = HelmRegistryAuthType(authv1.AuthTypeToken)
	HelmRegistryAuthTypeOIDC          = HelmRegistryAuthType(authv1.AuthTypeOIDC)
	HelmRegistryAuthTypeCloudIdentity = HelmRegistryAuthType(authv1.AuthTypeCloudIdentity)
)

type HelmRegistrySpec struct {
	URL             string
	Auth            HelmRegistryAuthSpec
	Interval        metav1.Duration
	MinSyncInterval metav1.Duration
	TLS             *authv1.TLSSpec
}

type HelmRegistryAuthSpec struct {
	Type          HelmRegistryAuthType
	Basic         *authv1.BasicAuthSpec
	Token         *authv1.TokenAuthSpec
	OIDC          *authv1.OIDAuthSpec
	CloudIdentity *authv1.CloudIdentitySpec
}

type HelmRegistryWebhookSpec struct {
	Name      string
	Provider  string
	SecretRef corev1.SecretKeySelector
	Events    []string
	Filters   *HelmRegistryWebhookFilterSpec
}

type HelmRegistryWebhookFilterSpec struct {
	Charts []string
}

type HelmRegistryStatus struct {
	ObservedGeneration    int64
	LastSuccessfulProbeAt *metav1.Time
	ReleaseCount          int64
	Webhooks              []WebhookStatus
	Conditions            []metav1.Condition
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type HelmRegistry struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec    HelmRegistrySpec
	Webhook []HelmRegistryWebhookSpec
	Status  HelmRegistryStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type HelmRegistryList struct {
	metav1.TypeMeta
	metav1.ListMeta

	Items []HelmRegistry
}

// ---------- OCIRegistry types ---------

type ContentType string

const (
	ContentTypeHelm      ContentType = "helm"
	ContentTypeManifests ContentType = "manifests"
	ContentTypeKustomize ContentType = "kustomize"
)

type OCIRegistryAuthType authv1.AuthType

const (
	OCIRegistryAuthTypeNone          = OCIRegistryAuthType(authv1.AuthTypeNone)
	OCIRegistryAuthTypeDockerConfig  = OCIRegistryAuthType(authv1.AuthTypeDockerConfig)
	OCIRegistryAuthTypeBasic         = OCIRegistryAuthType(authv1.AuthTypeBasic)
	OCIRegistryAuthTypeToken         = OCIRegistryAuthType(authv1.AuthTypeToken)
	OCIRegistryAuthTypeOIDC          = OCIRegistryAuthType(authv1.AuthTypeOIDC)
	OCIRegistryAuthTypeCloudIdentity = OCIRegistryAuthType(authv1.AuthTypeCloudIdentity)
)

type OCIRegistrySpec struct {
	URL                string
	Auth               OCIRegistryAuthSpec
	Interval           metav1.Duration
	MinSyncInterval    metav1.Duration
	TLS                *authv1.TLSSpec
	DefaultContentType ContentType
}

type OCIRegistryAuthSpec struct {
	Type          OCIRegistryAuthType
	Basic         *authv1.BasicAuthSpec
	Token         *authv1.TokenAuthSpec
	DockerConfig  *authv1.DockerConfigAuthSpec
	OIDC          *authv1.OIDAuthSpec
	CloudIdentity *authv1.CloudIdentitySpec
}

type OCIRegistryWebhookSpec struct {
	Name      string
	Provider  string
	SecretRef corev1.SecretKeySelector
	Events    []string
	Filters   *OCIRegistryWebhookFilterSpec
}

type OCIRegistryWebhookFilterSpec struct {
	Repositories []string
	Tags         []string
}

type OCIRegistryStatus struct {
	ObservedGeneration    int64
	LastSuccessfulProbeAt *metav1.Time
	ReleaseCount          int64
	Webhooks              []WebhookStatus
	Conditions            []metav1.Condition
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type OCIRegistry struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec    OCIRegistrySpec
	Webhook []OCIRegistryWebhookSpec
	Status  OCIRegistryStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type OCIRegistryList struct {
	metav1.TypeMeta
	metav1.ListMeta

	Items []OCIRegistry
}
