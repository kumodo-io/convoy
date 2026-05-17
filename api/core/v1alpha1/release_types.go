package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReleaseContentType defines the type of content fetched from the source.
// +enum
type ReleaseContentType string

const (
	// ReleaseContentTypeHelm is the content type used to fetch Helm charts.
	ReleaseContentTypeHelm ReleaseContentType = "helm"

	// ReleaseContentTypeManifests is the content type used to fetch tarballs of raw manifests.
	ReleaseContentTypeManifests ReleaseContentType = "manifests"

	// ReleaseContentTypeKustomize is the content type used to fetch kustomize bundles.
	ReleaseContentTypeKustomize ReleaseContentType = "kustomize"

	// ReleaseContentTypeJsonnet is the content type used to fetch jsonnet files.
	ReleaseContentTypeJsonnet ReleaseContentType = "jsonnet"
)

// ReleaseHelmDependencyMode defines the dependency mode for Helm charts.
// +enum
type ReleaseHelmDependencyMode string

const (
	// ReleaseHelmDependencyModeResolve is the dependency mode used to resolve dependencies at render time.
	ReleaseHelmDependencyModeResolve ReleaseHelmDependencyMode = "resolve"

	// ReleaseHelmDependencyModeVendored is the dependency mode used for vendored dependencies.
	ReleaseHelmDependencyModeVendored ReleaseHelmDependencyMode = "vendored"
)

// ReleaseWaveStrategy defines the wave strategy.
// +enum
type ReleaseWaveStrategy string

const (
	// ReleaseWaveStrategyWaitForApplied is the wave strategy that waits for all resources to be applied.
	ReleaseWaveStrategyWaitForApplied ReleaseWaveStrategy = "waitForApplied"

	// ReleaseWaveStrategyWaitForHealthy is the wave strategy that waits for all resources to be healthy.
	ReleaseWaveStrategyWaitForHealthy ReleaseWaveStrategy = "waitForHealthy"
)

// ReleaseMutationType defines the type of mutation.
// +enum
type ReleaseMutationType string

const (
	// MutationTypeHelmValues is the mutation type that uses Helm values as mutation source.
	MutationTypeHelmValues ReleaseMutationType = "helmValues"

	// MutationTypeGoTemplate is the mutation type that uses Go template as mutation source.
	MutationTypeGoTemplate ReleaseMutationType = "goTemplate"

	// MutationTypeJsonPatch is the mutation type that uses JSON patch as mutation source.
	MutationTypeJsonPatch ReleaseMutationType = "jsonPatch"

	// MutationTypeStrategicMergePatch is the mutation type that uses Strategic merge patch as mutation source.
	MutationTypeStrategicMergePatch ReleaseMutationType = "strategicMergePatch"
)

// JSONPatchOperation defines the JSON patch operation.
// +enum
type JSONPatchOperation string

const (
	// JSONPatchOperationAdd is the add JSON patch operation.
	JSONPatchOperationAdd JSONPatchOperation = "add"

	// JSONPatchOperationRemove is the remove JSON patch operation.
	JSONPatchOperationRemove JSONPatchOperation = "remove"

	// JSONPatchOperationReplace is the replace JSON patch operation.
	JSONPatchOperationReplace JSONPatchOperation = "replace"

	// JSONPatchOperationMove is the move JSON patch operation.
	JSONPatchOperationMove JSONPatchOperation = "move"

	// JSONPatchOperationCopy is the copy JSON patch operation.
	JSONPatchOperationCopy JSONPatchOperation = "copy"

	// JSONPatchOperationTest is the test JSON patch operation.
	JSONPatchOperationTest JSONPatchOperation = "test"
)

// ReleasePinIntent defines the intention of pinning the release.
// +enum
type ReleasePinIntent string

const (
	// ReleasePinIntentFreeze is the intention of freezing the release.
	ReleasePinIntentFreeze ReleasePinIntent = "freeze"

	// ReleasePinIntentRollback is the intention of rolling back the release.
	ReleasePinIntentRollback ReleasePinIntent = "rollback"
)

// WorkloadsPolicySource defines the source of the deletion policy for workloads.
// +enum
type WorkloadsPolicySource string

const (
	// WorkloadsPolicySourceRelease is used when the workloads deletion policy is defined in the Release.
	WorkloadsPolicySourceRelease WorkloadsPolicySource = "release"

	// WorkloadsPolicySourceCluster is used when the workloads deletion policy is defined in the Cluster.
	WorkloadsPolicySourceCluster WorkloadsPolicySource = "cluster"
)

// ReleaseRenderAttemptPhase defines the phase of a release render attempt.
// +enum
type ReleaseRenderAttemptPhase string

const (
	// ReleaseRenderAttemptPhaseSyncing is the phase of a release render attempt that is syncing.
	ReleaseRenderAttemptPhaseSyncing ReleaseRenderAttemptPhase = "Syncing"

	// ReleaseRenderAttemptPhaseRendering is the phase of a release render attempt that is rendering.
	ReleaseRenderAttemptPhaseRendering ReleaseRenderAttemptPhase = "Rendering"

	// ReleaseRenderAttemptPhaseSucceeded is the phase of a release render attempt that succeeded.
	ReleaseRenderAttemptPhaseSucceeded ReleaseRenderAttemptPhase = "Succeeded"

	// ReleaseRenderAttemptPhaseFailed is the phase of a release render attempt that failed.
	ReleaseRenderAttemptPhaseFailed ReleaseRenderAttemptPhase = "Failed"
)

// ReleaseRenderAttemptStage defines the stage of a release render attempt.
// +enum
type ReleaseRenderAttemptStage string

const (
	// ReleaseRenderAttemptStageSyncing is the stage of a release render attempt that is syncing.
	ReleaseRenderAttemptStageSyncing ReleaseRenderAttemptStage = "Syncing"

	// ReleaseRenderAttemptStageRendering is the stage of a release render attempt that is rendering.
	ReleaseRenderAttemptStageRendering ReleaseRenderAttemptStage = "Rendering"

	// ReleaseRenderAttemptStageSourceResolution is the stage of a release render attempt that is resolving the source.
	ReleaseRenderAttemptStageSourceResolution ReleaseRenderAttemptStage = "SourceResolution"

	// ReleaseRenderAttemptStageClusterResolution is the stage of a release render attempt that is resolving the cluster.
	ReleaseRenderAttemptStageClusterResolution ReleaseRenderAttemptStage = "ClusterResolution"

	// ReleaseRenderAttemptStageMutationResolution is the stage of a release render attempt that is resolving the mutations.
	ReleaseRenderAttemptStageMutationResolution ReleaseRenderAttemptStage = "MutationResolution"
)

// ResourceWaveState defines the state of a resource wave.
// +enum
type ResourceWaveState string

const (
	// ResourceWaveStateCompleted is the state of a resource wave that has been completed.
	ResourceWaveStateCompleted ResourceWaveState = "Completed"

	// ResourceWaveStateInProgress is the state of a resource wave that is in progress.
	ResourceWaveStateInProgress ResourceWaveState = "InProgress"

	// ResourceWaveStateFailed is the state of a resource wave that has failed.
	ResourceWaveStateFailed ResourceWaveState = "Failed"
)

// ReleaseSpec defines the desired state of the Release.
type ReleaseSpec struct {
	// Source is the content source of the release.
	// Immutable after creation. To re-target, create a new Release.
	Source ReleaseSourceSpec `json:"source"`

	// Content is the content of the release.
	Content ReleaseContentSpec `json:"content"`

	// ClusterRef is the reference to the cluster where the release will be deployed.
	// Immutable after creation.
	ClusterRef ReleaseClusterRefSpec `json:"clusterRef"`

	// SyncPolicy is the sync policy for the release.
	SyncPolicy ReleaseSyncPolicySpec `json:"syncPolicy"`

	// RetentionPolicy is the retention policy for the release.
	RetentionPolicy ReleaseRetentionPolicySpec `json:"retentionPolicy"`

	// DeletionPolicyOverride is the override for the deletion policy of the cluster Release is bound to.
	DeletionPolicyOverride *ReleaseDeletionPolicyOverrideSpec `json:"deletionPolicyOverride,omitempty"`
}

// ReleaseSourceSpec defines the source for the release.
type ReleaseSourceSpec struct {
	// Kind is the kind of the source.
	Kind string `json:"kind"`

	// Name is the name of the source.
	Name string `json:"name"`

	// Namespace is the namespace of the source.
	Namespace string `json:"namespace,omitempty"`
}

// ReleaseContentSpec defines the content of the release.
type ReleaseContentSpec struct {
	// Type is the type of the content.
	// Immutable after creation.
	Type ReleaseContentType `json:"type"`

	// Helm is the Helm chart content of the release.
	Helm *ReleaseContentHelmSpec `json:"helm,omitempty"`

	// Manifests is the manifests content of the release.
	Manifests *ReleaseContentManifestsSpec `json:"manifests,omitempty"`

	// Kustomize is the kustomize content of the release.
	Kustomize *ReleaseContentKustomizeSpec `json:"kustomize,omitempty"`

	// Jsonnet is the jsonnet content of the release.
	Jsonnet *ReleaseContentJsonnetSpec `json:"jsonnet,omitempty"`
}

// ReleaseContentHelmSpec defines the Helm chart content of the release.
type ReleaseContentHelmSpec struct {
	// Path is the path to the Helm chart within the source.
	// Ignored if the source is a Helm registry.
	Path string `json:"path,omitempty"`

	// Chart is the name of the Helm chart.
	// Ignored if the source is a Git repository.
	Chart string `json:"chart,omitempty"`

	// Version is the version of the Helm chart.
	Version string `json:"version,omitempty"`

	// DependencyMode is the dependency mode for the Helm chart.
	DependencyMode ReleaseHelmDependencyMode `json:"dependencyMode"`
}

// ReleaseContentManifestsSpec defines the manifests content of the release.
type ReleaseContentManifestsSpec struct {
	// Paths is the list of paths to the manifests within the source.
	// +listType=atomic
	Paths []string `json:"paths"`
}

// ReleaseContentKustomizeSpec defines the kustomize content of the release.
type ReleaseContentKustomizeSpec struct {
	// Path is the path to the kustomize bundle within the source.
	Path string `json:"path"`
}

// ReleaseContentJsonnetSpec defines the jsonnet content of the release.
type ReleaseContentJsonnetSpec struct {
	// Path is the path to the jsonnet file within the source.
	Path string `json:"path"`

	// TLA is the list of top-level arguments to pass to the jsonnet file.
	// +listType=map
	// +listMapKey=name
	TLA []corev1.EnvVar `json:"tla,omitempty"`
}

// ReleaseClusterRefSpec defines the desired state of the ReleaseClusterRef.
type ReleaseClusterRefSpec struct {
	// Name is the name of the cluster.
	Name string `json:"name"`

	// Namespace is the namespace of the cluster.
	Namespace string `json:"namespace,omitempty"`
}

// ReleaseSyncPolicySpec defines the desired state of the ReleaseSyncPolicy.
type ReleaseSyncPolicySpec struct {
	// Automatic defines whether the release should be automatically synced.
	// Manual syncing requires the POST /sync to trigger the sync.
	Automatic bool `json:"automatic"`

	// PruneOrphaned defines whether to prune resources that are no longer referenced by the release.
	// This may happen in case the content configuration changes. If set to false,
	// state of such resources will be left untouched and won't be tracked by the controller.
	PruneOrphaned bool `json:"pruneOrphaned"`

	// Interval is the poll interval for drift detection.
	Interval metav1.Duration `json:"interval"`

	// RetryBackoff defines the backoff policy for retrying failed syncs.
	RetryBackoff ReleaseRetryBackoffSpec `json:"retryBackoff"`

	// WaveStrategy defines the wave strategy.
	WaveStrategy ReleaseWaveStrategySpec `json:"waveStrategy"`
}

// ReleaseRetryBackoffSpec defines the desired state of the backoff policy for retrying failed syncs.
type ReleaseRetryBackoffSpec struct {
	// Initial is the initial backoff duration.
	Initial metav1.Duration `json:"initial"`

	// Max is the maximum backoff duration.
	Max metav1.Duration `json:"max"`

	// Factor is the backoff factor.
	Factor float64 `json:"factor"`
}

// ReleaseWaveStrategySpec defines the desired state of the wave strategy.
type ReleaseWaveStrategySpec struct {
	// Strategy is the wave strategy.
	Strategy ReleaseWaveStrategy `json:"strategy"`

	// Timeout is the timeout for the wave strategy. If not set, controller will wait indefinitely.
	Timeout *metav1.Duration `json:"timeout,omitempty"`
}

// ReleaseRetentionPolicySpec defines the desired state of the release retention policy.
type ReleaseRetentionPolicySpec struct {
	// KeepHistory defines how many historical (rendered, applied) records to retain in S3.
	// Drives /history pagination depth and rollback range.
	KeepHistory int `json:"keepHistory"`
}

// ReleaseDeletionPolicyOverrideSpec is used to override the deletion policy of the cluster
// Release is bound to.
type ReleaseDeletionPolicyOverrideSpec struct {
	// Workloads is the deletion policy for workloads.
	Workloads WorkloadsDeletionPolicy `json:"workloads"`
}

// ReleaseMutation defines the desired state of the ReleaseMutation.
type ReleaseMutation struct {
	// Type is the type of the mutation.
	Type ReleaseMutationType `json:"type"`

	// Inline is the inline mutation.
	// +listType=atomic
	Inline []string `json:"inline,omitempty"`

	// From is the source of the mutation.
	From *ReleaseMutationFromSpec `json:"from,omitempty"`

	// Patches is the list of JSON patches to apply to the release.
	// +listType=atomic
	Patches []ReleaseMutationPatchSpec `json:"patches,omitempty"`
}

// ReleaseMutationFromSpec defines the source of the mutation.
type ReleaseMutationFromSpec struct {
	// Kind is the kind of the source.
	Kind string `json:"kind"`

	// Name is the name of the source.
	Name string `json:"name"`

	// Namespace is the namespace of the source.
	Namespace string `json:"namespace,omitempty"`

	// Path is the path to the mutation within the source.
	Path string `json:"path"`
}

// ReleaseMutationPatchSpec defines the JSON patch to apply to the release within the mutation.
type ReleaseMutationPatchSpec struct {
	// Op is the JSON patch operation.
	Op JSONPatchOperation `json:"op"`

	// Path is the path to the field within the release to patch.
	Path string `json:"path"`

	// From is the path to the field within the release to copy or move from.
	// Applicable only for move and copy operations.
	From string `json:"from,omitempty"`

	// Value is the value to patch.
	Value *apiextensionsv1.JSON `json:"value,omitempty"`
}

// ReleasePin defines the desired state of the ReleasePin.
type ReleasePin struct {
	// RenderRef is the reference to the rendered state that is pinned.
	RenderRef string `json:"renderRef"`

	// PinnedAt is the time when the release was pinned.
	PinnedAt metav1.Time `json:"pinnedAt"`

	// PinnedBy is the name of the user that pinned the release.
	PinnedBy string `json:"pinnedBy,omitempty"`

	// Reason is the reason for pinning the release.
	Reason string `json:"reason,omitempty"`
}

// ReleaseStatus reflects the observed state of the Release.
type ReleaseStatus struct {
	// ObservedGeneration is the last generation that was observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions is the list of conditions related to the release.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Pin is the status of the release pin.
	Pin ReleasePinStatus `json:"pin,omitempty"`

	// ResolvedDeletionPolicy is the resolved deletion policy of the release.
	ResolvedDeletionPolicy ResolvedDeletionPolicyStatus `json:"resolvedDeletionPolicy,omitempty"`

	// ResolvedRetentionPolicy is the resolved retention policy of the release.
	ResolvedRetentionPolicy ResolvedRetentionPolicyStatus `json:"resolvedRetentionPolicy,omitempty"`

	// LastSuccessful is the last successful sync attempt.
	LastSuccessful *ReleaseAttemptedRenderStatus `json:"lastSuccessful,omitempty"`

	// LastAttempted is the last attempted sync attempt.
	LastAttempted *ReleaseAttemptedRenderStatus `json:"lastAttempted,omitempty"`

	// Waves reflects the order in which resources were applied and their health status.
	// Each wave entry represents a group of resources applied together, with a state indicating
	// whether they have been fully applied and healthy, are in progress, or have failed.
	// +listType=atomic
	Waves []ResourceWaveStatus `json:"waves,omitempty"`

	// ReconciledAt is the time when the release was reconciled.
	ReconciledAt *metav1.Time `json:"reconciledAt,omitempty"`

	// HistoryTruncated is true if the release history is truncated.
	HistoryTruncated bool `json:"historyTruncated"`
}

// ReleasePinStatus reflects the observed state of the ReleasePin.
type ReleasePinStatus struct {
	// Intent is the intention of pinning the release. Derives from the
	// Release.Spec.Pin.RenderRef: if the renderRef matches the latest
	// Release state, then the intent is "freeze". Otherwise, it's "rollback".
	Intent ReleasePinIntent `json:"intent"`

	// RenderInputs is the inputs used to render the release.
	RenderInputs *ReleasePinRenderInputsStatus `json:"renderInputs,omitempty"`
}

// ReleasePinRenderInputsStatus reflects the observed state of the release pin.
type ReleasePinRenderInputsStatus struct {
	// SourceKind is the kind of the source.
	SourceKind string `json:"sourceKind"`

	// SourceName is the name of the source.
	SourceName string `json:"sourceName"`

	// SourceNamespace is the namespace of the source.
	SourceNamespace string `json:"sourceNamespace,omitempty"`

	// SourceRevision is the revision of the source.
	SourceRevision string `json:"sourceRevision,omitempty"`

	// ContentType is the type of the content.
	ContentType ReleaseContentType `json:"contentType"`

	// ContentPath is the path to the content within the source.
	ContentPath string `json:"contentPath,omitempty"`

	// MutationsHash is the hash of the mutations applied to the content.
	MutationsHash string `json:"mutationsHash,omitempty"`
}

// ResolvedDeletionPolicyStatus reflects the resolved deletion policy
// of the Release considering the deletion policy overrides.
type ResolvedDeletionPolicyStatus struct {
	// WorkloadsPolicySource is the source of the deletion policy for workloads.
	WorkloadsPolicySource WorkloadsPolicySource `json:"workloadsPolicySource"`

	// Policy is the resolved deletion policy.
	Policy ResultingDeletionPolicy `json:"policy"`

	// SourceClusterGeneration is the generation of the source cluster.
	SourceClusterGeneration int64 `json:"sourceClusterGeneration,omitempty"`
}

// ResultingDeletionPolicy reflects the resolved deletion policy
// of the Release considering the deletion policy overrides.
type ResultingDeletionPolicy struct {
	// Workloads is the resolved deletion policy for workloads.
	Workloads WorkloadsDeletionPolicy `json:"workloads"`

	// Releases is the resolved deletion policy for releases.
	Releases ReleasesDeletionPolicy `json:"releases"`

	// OnUnreachable is the resolved deletion policy for unreachable clusters.
	OnUnreachable OnUnreachableDeletionPolicy `json:"onUnreachable"`
}

// ResolvedRetentionPolicyStatus reflects the resulting retention policy.
type ResolvedRetentionPolicyStatus struct {
	// KeepHistory is the resulting retention policy for the release.
	KeepHistory int `json:"keepHistory"`

	// HistoryCount is the observed history count.
	HistoryCount int `json:"historyCount"`
}

// ReleaseAttemptedRenderStatus reflects the status of a release render attempt.
type ReleaseAttemptedRenderStatus struct {
	// ResolvedSource is the resolved source of the release.
	ResolvedSource ResolvedSource `json:"resolvedSource"`

	// ResolvedCluster is the resolved cluster of the release.
	ResolvedCluster ResolvedCluster `json:"resolvedCluster"`

	// ResolvedMutations is the resolved mutations of the release.
	// +listType=atomic
	ResolvedMutations []ResolvedMutation `json:"resolvedMutations"`

	// RenderRef is the reference to the rendered state.
	RenderRef string `json:"renderRef"`

	// AttemptedAt is the time when the Release sync was attempted.
	AttemptedAt *metav1.Time `json:"attemptedAt,omitempty"`

	// Phase is the phase of the release render attempt.
	Phase ReleaseRenderAttemptPhase `json:"phase,omitempty"`

	// SyncedAt is the time when the Release was synced.
	SyncedAt *metav1.Time `json:"syncedAt,omitempty"`

	// SyncedResources is the number of resources synced.
	SyncedResources int64 `json:"syncedResources,omitempty"`

	// Failure is the failure status of the release render attempt.
	Failure *ReleaseAttemptedRenderFailureStatus `json:"failure,omitempty"`
}

// ResolvedSource reflects the resolved source of the Release.
type ResolvedSource struct {
	// Kind is the kind of the source.
	Kind string `json:"kind"`

	// Name is the name of the source.
	Name string `json:"name"`

	// Namespace is the namespace of the source.
	Namespace string `json:"namespace,omitempty"`

	// UID is the UID of the source.
	UID string `json:"uid"`

	// ObservedGeneration is the observed generation of the source.
	ObservedGeneration int64 `json:"observedGeneration"`

	// URL is the URL of the source.
	URL string `json:"url"`

	// Revision is the revision of the source.
	Revision string `json:"revision"`

	// ContentType is the type of the content.
	ContentType ReleaseContentType `json:"contentType"`

	// Path is the path to the content within the source.
	Path string `json:"path,omitempty"`
}

// ResolvedCluster reflects the resolved cluster of the Release.
type ResolvedCluster struct {
	// Name is the name of the cluster.
	Name string `json:"name"`

	// Namespace is the namespace of the cluster.
	Namespace string `json:"namespace,omitempty"`

	// UID is the UID of the cluster.
	UID string `json:"uid"`

	// ObservedGeneration is the observed generation of the cluster.
	ObservedGeneration int64 `json:"observedGeneration"`

	// Server is the server URL of the cluster.
	Server string `json:"server"`

	// MetadataSnapshot is the snapshot of the cluster metadata.
	MetadataSnapshot map[string]string `json:"metadataSnapshot,omitempty"`
}

// ResolvedMutation reflects the resolved mutation of the Release.
type ResolvedMutation struct {
	// Index is the index of the mutation.
	Index int `json:"index"`

	// Type is the type of the mutation.
	Type ReleaseMutationType `json:"type"`

	// SourceRef is the source reference of the mutation.
	SourceRef ReleaseMutationFromSpec `json:"sourceRef"`

	// SourceRevision is the source revision of the mutation.
	SourceRevision string `json:"sourceRevision"`

	// ContentHash is the content hash of the mutation.
	ContentHash string `json:"contentHash"`
}

// ReleaseAttemptedRenderFailureStatus reflects the failure status of a Release render attempt.
type ReleaseAttemptedRenderFailureStatus struct {
	// Stage is the stage of the failure.
	Stage ReleaseRenderAttemptStage `json:"stage"`

	// MutationIndex is the index of the mutation that failed.
	MutationIndex int `json:"mutationIndex,omitempty"`

	// Message is the failure message.
	Message string `json:"message"`
}

// ResourceWaveStatus reflects the status of a wave of resources.
type ResourceWaveStatus struct {
	// Number is the resources wave weight.
	Number int64 `json:"number"`

	// Resources is the number of resources in the wave.
	Resources int `json:"resources"`

	// State is the state of the wave.
	State ResourceWaveState `json:"state"`

	// StartedAt is the time when the wave started.
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// CompletedAt is the time when the wave completed.
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// FailedAt is the time when the wave failed.
	FailedAt *metav1.Time `json:"failedAt,omitempty"`

	// Message is the failure message.
	Message string `json:"message,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:prerelease-lifecycle-gen:introduced=1.0
// +kubebuilder:object:root=true

type Release struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ReleaseSpec   `json:"spec,omitempty"`
	Status ReleaseStatus `json:"status,omitempty"`

	// +listType=atomic
	Mutations []ReleaseMutation `json:"mutations,omitempty"`
	Pin       ReleasePin        `json:"pin,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ReleaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Release `json:"items"`
}
