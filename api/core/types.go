package core

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
)

// ---------- Cluster types ----------

type ClusterConnectionMode string

const (
	ClusterConnectionModeInCluster ClusterConnectionMode = "inCluster"
	ClusterConnectionModeAgentless ClusterConnectionMode = "agentless"
	ClusterConnectionModeAgent     ClusterConnectionMode = "agent"
)

type ClusterAuthType authv1.AuthType

const (
	ClusterAuthTypeKubeconfig = ClusterAuthType(authv1.AuthTypeKubeconfig)
	ClusterAuthTypeToken      = ClusterAuthType(authv1.AuthTypeToken)
	ClusterAuthTypeClientCert = ClusterAuthType(authv1.AuthTypeClientCert)
	ClusterAuthTypeExecPlugin = ClusterAuthType(authv1.AuthTypeExecPlugin)
	ClusterAuthTypeOIDC       = ClusterAuthType(authv1.AuthTypeOIDC)
)

type ClusterSpec struct {
	ConnectionMode ClusterConnectionMode
	Agent          *ClusterAgentModeSpec
	Agentless      *ClusterAgentlessModeSpec
	Metadata       map[string]string
	Capabilities   ClusterCapabilitiesSpec
	DeletionPolicy ClusterDeletionPolicy
}

type ClusterAgentModeSpec struct{}

type ClusterAgentlessModeSpec struct {
	Server               string
	CAPublicCertRef      corev1.ConfigMapKeySelector
	Auth                 ClusterAuthSpec
	ConnectionParameters ClusterConnectionParametersSpec
}

type ClusterAuthSpec struct {
	Type       ClusterAuthType
	Kubeconfig *authv1.KubeconfigAuthSpec
	Token      *authv1.TokenAuthSpec
	ClientCert *authv1.ClientCertAuthSpec
	ExecPlugin *authv1.ExecPluginAuthSpec
	OIDC       *authv1.OIDAuthSpec
}

type ClusterCapabilitiesSpec struct {
	ServerSideApply bool
	APIVersions     []string
}

type ClusterDeletionPolicy struct {
	PruneReleases bool
}

type ClusterConnectionParametersSpec struct {
	QPS     int
	Burst   int
	Timeout metav1.Duration
}

type ClusterStatus struct {
	ObservedGeneration    int64
	Reachable             bool
	LastSeen              metav1.Time
	DiscoveredVersion     string
	DiscoveredAPIVersions []string
	ReleaseCount          int64
	AgentStatus           *ClusterAgentStatus
	Conditions            []metav1.Condition
}

type ClusterAgentStatus struct {
	AgentVersion  string
	RegisteredAt  metav1.Time
	LastHeartbeat metav1.Time
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Cluster struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec   ClusterSpec
	Status ClusterStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ClusterList struct {
	metav1.TypeMeta
	metav1.ListMeta

	Items []Cluster
}

// ---------- Release types ----------

type ReleaseSourceType string

const (
	ReleaseSourceTypeGitRepository ReleaseSourceType = "gitRepository"
	ReleaseSourceTypeHelmRegistry  ReleaseSourceType = "helmRegistry"
	ReleaseSourceTypeOCIRegistry   ReleaseSourceType = "ociRegistry"
)

type ReleaseContentType string

const (
	ReleaseContentTypeHelm      ReleaseContentType = "helm"
	ReleaseContentTypeManifests ReleaseContentType = "manifests"
	ReleaseContentTypeKustomize ReleaseContentType = "kustomize"
	ReleaseContentTypeJsonnet   ReleaseContentType = "jsonnet"
	ReleaseContentTypeCue       ReleaseContentType = "cue"
)

type ReleaseHelmDependencyMode string

const (
	ReleaseHelmDependencyModeResolve  ReleaseHelmDependencyMode = "resolve"
	ReleaseHelmDependencyModeVendored ReleaseHelmDependencyMode = "vendored"
)

type ValuesSourceType string

const (
	ValuesSourceTypeGitRepository ValuesSourceType = "gitRepository"
	ValuesSourceTypeConfigMap     ValuesSourceType = "configMap"
	ValuesSourceTypeSecret        ValuesSourceType = "secret"
	ValuesSourceTypeInline        ValuesSourceType = "inline"
)

type ReleaseWaveStrategy string

const (
	ReleaseWaveStrategyWaitForApplied            ReleaseWaveStrategy = "waitForApplied"
	ReleaseWaveStrategyWaitForHealthy            ReleaseWaveStrategy = "waitForHealthy"
	ReleaseWaveStrategyWaitForHealthyWithTimeout ReleaseWaveStrategy = "waitForHealthyWithTimeout"
)

type ReleaseMutationType string

const (
	MutationTypeGoTemplate          ReleaseMutationType = "goTemplate"
	MutationTypeJsonPatch           ReleaseMutationType = "jsonPatch"
	MutationTypeStrategicMergePatch ReleaseMutationType = "strategicMergePatch"
	MutationTypeCelTransform        ReleaseMutationType = "celTransform"
	MutationTypeSyncWave            ReleaseMutationType = "syncWave"
)

type JSONPatchOperation string

const (
	JSONPatchOperationAdd     JSONPatchOperation = "add"
	JSONPatchOperationRemove  JSONPatchOperation = "remove"
	JSONPatchOperationReplace JSONPatchOperation = "replace"
	JSONPatchOperationMove    JSONPatchOperation = "move"
	JSONPatchOperationCopy    JSONPatchOperation = "copy"
	JSONPatchOperationTest    JSONPatchOperation = "test"
)

type ReleasePinIntent string

const (
	ReleasePinIntentFreeze   ReleasePinIntent = "freeze"
	ReleasePinIntentRollback ReleasePinIntent = "rollback"
)

type WorkloadsPolicySource string

const (
	WorkloadsPolicySourceRelease WorkloadsPolicySource = "release"
	WorkloadsPolicySourceCluster WorkloadsPolicySource = "cluster"
)

type ReleaseRenderAttemptPhase string

const (
	ReleaseRenderAttemptPhaseSyncing   ReleaseRenderAttemptPhase = "Syncing"
	ReleaseRenderAttemptPhaseRendering ReleaseRenderAttemptPhase = "Rendering"
	ReleaseRenderAttemptPhaseSucceeded ReleaseRenderAttemptPhase = "Succeeded"
	ReleaseRenderAttemptPhaseFailed    ReleaseRenderAttemptPhase = "Failed"
)

type ReleaseRenderAttemptStage string

const (
	ReleaseRenderAttemptStageSyncing            ReleaseRenderAttemptStage = "Syncing"
	ReleaseRenderAttemptStageRendering          ReleaseRenderAttemptStage = "Rendering"
	ReleaseRenderAttemptStageSourceResolution   ReleaseRenderAttemptStage = "SourceResolution"
	ReleaseRenderAttemptStageClusterResolution  ReleaseRenderAttemptStage = "ClusterResolution"
	ReleaseRenderAttemptStageMutationResolution ReleaseRenderAttemptStage = "MutationResolution"
	ReleaseRenderAttemptStageHookFailed         ReleaseRenderAttemptStage = "HookFailed"
)

type ResourceWaveState string

const (
	ResourceWaveStateCompleted  ResourceWaveState = "Completed"
	ResourceWaveStateInProgress ResourceWaveState = "InProgress"
	ResourceWaveStateFailed     ResourceWaveState = "Failed"
)

type LifecycleHookPhase string

const (
	LifecycleHookPhasePreSync    LifecycleHookPhase = "PreSync"
	LifecycleHookPhasePostSync   LifecycleHookPhase = "PostSync"
	LifecycleHookPhasePreDelete  LifecycleHookPhase = "PreDelete"
	LifecycleHookPhasePostDelete LifecycleHookPhase = "PostDelete"
	LifecycleHookPhaseSyncFailed LifecycleHookPhase = "SyncFailed"
	LifecycleHookPhasePreRender  LifecycleHookPhase = "PreRender"
	LifecycleHookPhasePostRender LifecycleHookPhase = "PostRender"
)

type LifecycleHookType string

const (
	LifecycleHookTypePod LifecycleHookType = "Pod"
	LifecycleHookTypeJob LifecycleHookType = "Job"
)

type LifecycleHookOnFailureAction string

const (
	LifecycleHookOnFailureActionContinue LifecycleHookOnFailureAction = "Continue"
	LifecycleHookOnFailureActionFail     LifecycleHookOnFailureAction = "Fail"
)

type LifecycleHookCleanup string

const (
	LifecycleHookCleanupOnSuccess    LifecycleHookCleanup = "OnSuccess"
	LifecycleHookCleanupOnCompletion LifecycleHookCleanup = "OnCompletion"
	LifecycleHookCleanupNever        LifecycleHookCleanup = "Never"
)

type LifecycleHookSource string

const (
	LifecycleHookSourceLifecycle LifecycleHookSource = "lifecycle"
	LifecycleHookSourceChart     LifecycleHookSource = "chart"
)

type LifecycleHookState string

const (
	LifecycleHookStateSkipped   LifecycleHookState = "Skipped"
	LifecycleHookStateRunning   LifecycleHookState = "Running"
	LifecycleHookStateSucceeded LifecycleHookState = "Succeeded"
	LifecycleHookStateFailed    LifecycleHookState = "Failed"
	LifecycleHookStateTimedOut  LifecycleHookState = "TimedOut"
)

type ReleaseSpec struct {
	Source           ReleaseSourceSpec
	Content          ReleaseContentSpec
	ClusterRef       ReleaseClusterRefSpec
	SyncPolicy       ReleaseSyncPolicySpec
	RetentionPolicy  ReleaseRetentionPolicySpec
	DeletionPolicy   ReleaseDeletionPolicy
	HealthAssessment *ReleaseHealthAssessment
}

type ReleaseSourceSpec struct {
	Type          ReleaseSourceType
	GitRepository *ReleaseGitRepositorySpec
	HelmRegistry  *ReleaseHelmRegistrySpec
	OCIRegistry   *ReleaseOCIRegistrySpec
}

type ReleaseGitRepositorySpec struct {
	Name      string
	Namespace string
	Ref       *ReleaseGitRepositoryRefSpec
}

type ReleaseHelmRegistrySpec struct {
	Name      string
	Namespace string
	Chart     *ReleaseHelmRegistryChartSpec
}

type ReleaseOCIRegistrySpec struct {
	Name      string
	Namespace string
	Artifact  *ReleaseOCIRegistryArtifactSpec
}

type ReleaseGitRepositoryRefSpec struct {
	Branch *string
	Tag    *string
	Commit *string
	Semver *string
}

type ReleaseHelmRegistryChartSpec struct {
	Name    string
	Version string
}

type ReleaseOCIRegistryArtifactSpec struct {
	Name    string
	Version string
}

type ReleaseContentSpec struct {
	Type      ReleaseContentType
	Helm      *ReleaseContentHelmSpec
	Manifests *ReleaseContentManifestsSpec
	Kustomize *ReleaseContentKustomizeSpec
	Jsonnet   *ReleaseContentJsonnetSpec
}

type ReleaseContentHelmSpec struct {
	ReleaseName      string
	ReleaseNamespace string
	Path             string
	DependencyMode   ReleaseHelmDependencyMode
	Values           []ValuesSourceSpec
}

type ReleaseContentManifestsSpec struct {
	TargetNamespace string
	Paths           []string
}

type ReleaseContentKustomizeSpec struct {
	TargetNamespace string
	Path            string
}

type ReleaseContentJsonnetSpec struct {
	TargetNamespace string
	Path            string
	TLA             []corev1.EnvVar
}

type ValuesSourceSpec struct {
	Type          ValuesSourceType
	GitRepository *ValuesSourceGitRepositorySpec
	ConfigMap     *corev1.ConfigMapKeySelector
	Secret        *corev1.SecretKeySelector
	Inline        *apiextensionsv1.JSON
}

type ValuesSourceGitRepositorySpec struct {
	*ReleaseGitRepositorySpec
	Paths []string
}

type ReleaseClusterRefSpec struct {
	Name      string
	Namespace string
}

type ReleaseSyncPolicySpec struct {
	Automatic    bool
	Prune        PruneSpec
	Interval     metav1.Duration
	RetryBackoff ReleaseRetryBackoffSpec
	WaveStrategy ReleaseWaveStrategySpec
}

type PruneSpec struct {
	Orphaned          bool
	IncludeCRDs       bool
	IncludeNamespaces bool
}

type ReleaseRetryBackoffSpec struct {
	Initial metav1.Duration
	Max     metav1.Duration
	Factor  float64
}

type ReleaseWaveStrategySpec struct {
	Strategy ReleaseWaveStrategy
	Wave     int
	Timeout  *metav1.Duration
}

type ReleaseRetentionPolicySpec struct {
	KeepHistory int
}

type ReleaseDeletionPolicy struct {
	KeepWorkloads             bool
	BlockOnClusterUnreachable bool
}

type ReleaseHealthAssessment struct {
	GlobalRuleSetSelector *metav1.LabelSelector
	LocalRuleSetSelector  *metav1.LabelSelector
	Deadline              *metav1.Duration
}

type ReleaseMutation struct {
	Type         ReleaseMutationType
	Target       ReleaseMutationTarget
	Inline       []string
	From         *ReleaseMutationFromSpec
	Patches      []ReleaseMutationPatchSpec
	WaveStrategy *ReleaseWaveStrategySpec
}

type ReleaseMutationTarget struct {
	Group     string
	Version   string
	Kind      string
	Name      string
	Namespace string
}

type ReleaseMutationFromSpec struct {
	Kind      string
	Name      string
	Namespace string
	Path      string
	Ref       *ReleaseGitRepositoryRefSpec
}

type ReleaseMutationPatchSpec struct {
	Op    JSONPatchOperation
	Path  string
	From  string
	Value *apiextensionsv1.JSON
}

type ReleaseLifecycle struct {
	PreRender  []LifecycleHookSpec
	PostRender []LifecycleHookSpec
	PreSync    []LifecycleHookSpec
	SyncFailed []LifecycleHookSpec
	PostSync   []LifecycleHookSpec
	PreDelete  []LifecycleHookSpec
	PostDelete []LifecycleHookSpec
}

type LifecycleHookSpec struct {
	Name                 string
	Type                 LifecycleHookType
	When                 string
	Weight               int32
	Timeout              *metav1.Duration
	OnFailure            LifecycleHookOnFailureAction
	DeleteBeforeCreation bool
	Cleanup              LifecycleHookCleanup
	PodTemplate          *LifecycleHookPodTemplate
	JobTemplate          *LifecycleHookJobTemplate
}

type LifecycleHookJobTemplate struct {
	Metadata PartialMetadata
	Spec     batchv1.JobSpec
}

type LifecycleHookPodTemplate struct {
	Metadata PartialMetadata
	Spec     corev1.PodSpec
}

type PartialMetadata struct {
	Namespace   string
	Labels      map[string]string
	Annotations map[string]string
}

type ReleasePin struct {
	RenderRef string
	PinnedAt  metav1.Time
	PinnedBy  string
	Reason    string
}

type ReleaseStatus struct {
	ObservedGeneration      int64
	Conditions              []metav1.Condition
	Pin                     *ReleasePinStatus
	ResolvedRetentionPolicy ResolvedRetentionPolicyStatus
	LastSuccessful          *ReleaseAttemptedRenderStatus
	LastAttempted           *ReleaseAttemptedRenderStatus
	Waves                   []ResourceWaveStatus
	ReconciledAt            *metav1.Time
	HistoryTruncated        bool
	Lifecycle               LifecycleStatus
	HealthSummary           ReleaseHealthSummary
	LastHandledSyncAt       *metav1.Time
}

type ReleasePinStatus struct {
	Intent       ReleasePinIntent
	RenderInputs *ReleasePinRenderInputsStatus
}

type ReleasePinRenderInputsStatus struct {
	SourceKind      string
	SourceName      string
	SourceNamespace string
	SourceRevision  string
	ContentType     ReleaseContentType
	ContentPath     string
	MutationsHash   string
}

type ResolvedRetentionPolicyStatus struct {
	KeepHistory  int
	HistoryCount int
}

type ReleaseAttemptedRenderStatus struct {
	ResolvedSource    ResolvedSource
	ResolvedCluster   ResolvedCluster
	ResolvedMutations []ResolvedMutation
	RenderRef         string
	AttemptedAt       *metav1.Time
	Phase             ReleaseRenderAttemptPhase
	SyncedAt          *metav1.Time
	SyncedResources   int64
	Failure           *ReleaseAttemptedRenderFailureStatus
}

type ResolvedSource struct {
	Kind               string
	Name               string
	Namespace          string
	UID                string
	ObservedGeneration int64
	URL                string
	Ref                string
	Revision           string
	ContentType        ReleaseContentType
	Path               string
}

type ResolvedCluster struct {
	Name               string
	Namespace          string
	UID                string
	ObservedGeneration int64
	Server             string
	MetadataSnapshot   map[string]string
}

type ResolvedMutation struct {
	Index          int
	Type           ReleaseMutationType
	SourceRef      ReleaseMutationFromSpec
	SourceRevision string
	ContentHash    string
}

type ReleaseAttemptedRenderFailureStatus struct {
	Stage         ReleaseRenderAttemptStage
	MutationIndex int
	Message       string
	WaveNumber    int32
	HookName      string
	HookPhase     *LifecycleHookPhase
}

type ResourceWaveStatus struct {
	Number      int64
	Applied     *bool
	Healthy     *bool
	Resources   int
	State       ResourceWaveState
	StartedAt   *metav1.Time
	CompletedAt *metav1.Time
	FailedAt    *metav1.Time
	Message     string
}

type LifecycleStatus struct {
	PreRender  []LifecycleHookStatus
	PostRender []LifecycleHookStatus
	PreSync    []LifecycleHookStatus
	SyncFailed []LifecycleHookStatus
	PostSync   []LifecycleHookStatus
	PreDelete  []LifecycleHookStatus
	PostDelete []LifecycleHookStatus
}

type LifecycleHookStatus struct {
	Name              string
	Source            LifecycleHookSource
	Kind              string
	ApiVersion        string
	ResourceName      string
	ResourceNamespace string
	State             LifecycleHookState
	Weight            int32
	StartedAt         *metav1.Time
	CompletedAt       *metav1.Time
	CleanedUpAt       *metav1.Time
	Message           string
	FollowUpAction    *LifecycleHookOnFailureAction
}

type ReleaseHealthSummary struct {
	Total       int32
	Healthy     int32
	Progressing int32
	Degraded    int32
	Suspended   int32
	Unknown     int32
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Release struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec   ReleaseSpec
	Status ReleaseStatus

	Mutations []ReleaseMutation
	Lifecycle *ReleaseLifecycle
	Pin       *ReleasePin
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ReleaseList struct {
	metav1.TypeMeta
	metav1.ListMeta

	Items []Release
}

// ---------- ReleaseInventory types ----------

type ResourceState string

const (
	ResourceStateHealthy     ResourceState = "Healthy"
	ResourceStateProgressing ResourceState = "Progressing"
	ResourceStateDegraded    ResourceState = "Degraded"
	ResourceStateUnknown     ResourceState = "Unknown"
)

type ResourceInventoryItem struct {
	Group          string
	Version        string
	Kind           string
	Namespace      string
	Name           string
	UID            *types.UID
	Wave           *int
	State          *ResourceState
	FirstUnhealthy *metav1.Time
	Message        string
}

type ReleaseLifecycleHookItem struct {
	Name              string
	Phase             LifecycleHookPhase
	RenderRef         string
	Weight            int32
	Timeout           *metav1.Duration
	Group             string
	Version           string
	Kind              string
	WorkloadNamespace string
	WorkloadName      string
	CreatedAt         *metav1.Time
	FinishedAt        *metav1.Time
	State             *LifecycleHookState
	Message           string
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ReleaseInventory struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Inventory []ResourceInventoryItem
	Hooks     []ReleaseLifecycleHookItem
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ReleaseInventoryList struct {
	metav1.TypeMeta
	metav1.ListMeta
	Items []ReleaseInventory
}
