package core

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
)

type ClusterConnectionMode string

const (
	ClusterConnectionModeAgentless ClusterConnectionMode = "agentless"
	ClusterConnectionModeAgent     ClusterConnectionMode = "agent"
)

type ReleasesDeletionPolicy string

const (
	ReleasesDeletionPolicyBlock  ReleasesDeletionPolicy = "block"
	ReleasesDeletionPolicyDelete ReleasesDeletionPolicy = "delete"
	ReleasesDeletionPolicyOrphan ReleasesDeletionPolicy = "orphan"
)

type WorkloadsDeletionPolicy string

const (
	WorkloadsDeletionPolicyPrune WorkloadsDeletionPolicy = "prune"
	WorkloadsDeletionPolicyLeave WorkloadsDeletionPolicy = "leave"
)

type OnUnreachableDeletionPolicy string

const (
	OnUnreachableDeletionPolicyBlock        OnUnreachableDeletionPolicy = "block"
	OnUnreachableDeletionPolicyReleasesOnly OnUnreachableDeletionPolicy = "releasesOnly"
)

type ClusterSpec struct {
	ConnectionMode ClusterConnectionMode
	Agentless      *ClusterAgentlessModeSpec
	Metadata       map[string]string
	Capabilities   ClusterCapabilitiesSpec
	DeletionPolicy ClusterDeletionPolicySpec
}

type ClusterAgentlessModeSpec struct {
	Server               string
	CAPublicCertRef      corev1.ConfigMapKeySelector
	Auth                 ClusterAuthSpec
	ConnectionParameters ClusterConnectionParametersSpec
}

type ClusterAuthSpec struct {
	Type       authv1.AuthType
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

type ClusterDeletionPolicySpec struct {
	Releases      ReleasesDeletionPolicy
	Workloads     WorkloadsDeletionPolicy
	OnUnreachable OnUnreachableDeletionPolicy
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

type ReleaseWaveStrategy string

const (
	ReleaseWaveStrategyWaitForApplied            ReleaseWaveStrategy = "waitForApplied"
	ReleaseWaveStrategyWaitForHealthy            ReleaseWaveStrategy = "waitForHealthy"
	ReleaseWaveStrategyWaitForHealthyWithTimeout ReleaseWaveStrategy = "waitForHealthyWithTimeout"
)

type ReleaseMutationType string

const (
	MutationTypeHelmValues          ReleaseMutationType = "helmValues"
	MutationTypeGoTemplate          ReleaseMutationType = "goTemplate"
	MutationTypeJsonPatch           ReleaseMutationType = "jsonPatch"
	MutationTypeStrategicMergePatch ReleaseMutationType = "strategicMergePatch"
	MutationTypeCelTransform        ReleaseMutationType = "celTransform"
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
)

type ResourceWaveState string

const (
	ResourceWaveStateCompleted  ResourceWaveState = "Completed"
	ResourceWaveStateInProgress ResourceWaveState = "InProgress"
	ResourceWaveStateFailed     ResourceWaveState = "Failed"
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
	Source                 ReleaseSourceSpec
	Content                ReleaseContentSpec
	ClusterRef             ReleaseClusterRefSpec
	SyncPolicy             ReleaseSyncPolicySpec
	RetentionPolicy        ReleaseRetentionPolicySpec
	DeletionPolicyOverride *ReleaseDeletionPolicyOverrideSpec
}

type ReleaseSourceSpec struct {
	Kind      string
	Name      string
	Namespace string
}

type ReleaseContentSpec struct {
	Type      ReleaseContentType
	Helm      *ReleaseContentHelmSpec
	Manifests *ReleaseContentManifestsSpec
	Kustomize *ReleaseContentKustomizeSpec
	Jsonnet   *ReleaseContentJsonnetSpec
}

type ReleaseContentHelmSpec struct {
	Path           string
	Chart          string
	Version        string
	DependencyMode ReleaseHelmDependencyMode
}

type ReleaseContentManifestsSpec struct {
	Paths []string
}

type ReleaseContentKustomizeSpec struct {
	Path string
}

type ReleaseContentJsonnetSpec struct {
	Path string
	TLA  []corev1.EnvVar
}

type ReleaseClusterRefSpec struct {
	Name      string
	Namespace string
}

type ReleaseSyncPolicySpec struct {
	Automatic     bool
	PruneOrphaned bool
	Interval      metav1.Duration
	RetryBackoff  ReleaseRetryBackoffSpec
	WaveStrategy  ReleaseWaveStrategySpec
}

type ReleaseRetryBackoffSpec struct {
	Initial metav1.Duration
	Max     metav1.Duration
	Factor  float64
}

type ReleaseWaveStrategySpec struct {
	Strategy ReleaseWaveStrategy
	Timeout  *metav1.Duration
}

type ReleaseRetentionPolicySpec struct {
	KeepHistory int
}

type ReleaseDeletionPolicyOverrideSpec struct {
	Workloads WorkloadsDeletionPolicy
}

type ReleaseMutation struct {
	Type    ReleaseMutationType
	Inline  []string
	From    *ReleaseMutationFromSpec
	Patches []ReleaseMutationPatchSpec
}

type ReleaseMutationFromSpec struct {
	Kind      string
	Name      string
	Namespace string
	Path      string
}

type ReleaseMutationPatchSpec struct {
	Op    JSONPatchOperation
	Path  string
	From  string
	Value *apiextensionsv1.JSON
}

type ReleaseLifecycle struct {
	PreRenderHooks  []LifecycleHookSpec
	PostRenderHooks []LifecycleHookSpec
	PreSyncHooks    []LifecycleHookSpec
	SyncFailedHooks []LifecycleHookSpec
	PostSyncHooks   []LifecycleHookSpec
	PreDeleteHooks  []LifecycleHookSpec
	PostDeleteHooks []LifecycleHookSpec
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
	PodTemplate          *corev1.PodTemplateSpec
	JobTemplate          *batchv1.JobTemplateSpec
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
	Pin                     ReleasePinStatus
	ResolvedDeletionPolicy  ResolvedDeletionPolicyStatus
	ResolvedRetentionPolicy ResolvedRetentionPolicyStatus
	LastSuccessful          *ReleaseAttemptedRenderStatus
	LastAttempted           *ReleaseAttemptedRenderStatus
	Waves                   []ResourceWaveStatus
	ReconciledAt            *metav1.Time
	HistoryTruncated        bool
	Lifecycle               LifecycleStatus
	HealthSummary           ReleaseHealthSummary
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

type ResolvedDeletionPolicyStatus struct {
	WorkloadsPolicySource   WorkloadsPolicySource
	Policy                  ResultingDeletionPolicy
	SourceClusterGeneration int64
}

type ResultingDeletionPolicy struct {
	Workloads     WorkloadsDeletionPolicy
	Releases      ReleasesDeletionPolicy
	OnUnreachable OnUnreachableDeletionPolicy
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
}

type ResourceWaveStatus struct {
	Number      int64
	Resources   int
	State       ResourceWaveState
	StartedAt   *metav1.Time
	CompletedAt *metav1.Time
	FailedAt    *metav1.Time
	Message     string
}

type LifecycleStatus struct {
	PreRenderHooks  []LifecycleHookStatus
	PostRenderHooks []LifecycleHookStatus
	PreSyncHooks    []LifecycleHookStatus
	SyncFailedHooks []LifecycleHookStatus
	PostSyncHooks   []LifecycleHookStatus
	PreDeleteHooks  []LifecycleHookStatus
	PostDeleteHooks []LifecycleHookStatus
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
	Lifecycle ReleaseLifecycle
	Pin       ReleasePin
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ReleaseList struct {
	metav1.TypeMeta
	metav1.ListMeta

	Items []Release
}
