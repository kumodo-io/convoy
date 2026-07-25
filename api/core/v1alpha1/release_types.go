package v1alpha1

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ConvoySyncWaveAnnotation is set on the rendered resource(s) to
	// define the order resource(s) will be applied in. If not set,
	// then it's treated as wave 0.
	ConvoySyncWaveAnnotation = "convoy.kumodo.io/sync-wave"

	// ConvoySyncStrategyAnnotation is set on the rendered resource(s) to
	// define the strategy used to resolve the order and resource(s) readiness.
	// If not set then it's treated as "waitForApply"
	ConvoySyncStrategyAnnotation = "convoy.kumodo.io/sync-strategy"

	// ConvoySyncTimeoutAnnotation is set on the rendered resource(s) to
	// define the timeout the controller will wait for. If not set,
	// then it's treated as 30s. Only applicable for "waitForHealthyWithTimeout"
	// strategy.
	ConvoySyncTimeoutAnnotation = "convoy.kumodo.io/sync-timeout"
)

// ReleaseSourceType defines the type of the source.
// +enum
type ReleaseSourceType string

const (
	// ReleaseSourceTypeGitRepository is the source type used for Git repositories.
	ReleaseSourceTypeGitRepository ReleaseSourceType = "gitRepository"

	// ReleaseSourceTypeHelmRegistry is the source type used for Helm registries.
	ReleaseSourceTypeHelmRegistry ReleaseSourceType = "helmRegistry"

	// ReleaseSourceTypeOCIRegistry is the source type used for OCI registries.
	ReleaseSourceTypeOCIRegistry ReleaseSourceType = "ociRegistry"
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

	// ReleaseContentTypeCue is the content type used to fetch cue files.
	ReleaseContentTypeCue ReleaseContentType = "cue"
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

// ValuesSourceType defines the type of the values source
// +enum
type ValuesSourceType string

const (
	// ValuesSourceTypeGitRepository is the values source type to fetch Helm chart
	// values from the [github.com/kumodo-io/convoy/api/source/v1alpha1.GitRepository]
	ValuesSourceTypeGitRepository ValuesSourceType = "gitRepository"

	// ValuesSourceTypeConfigMap is the values source type to get values from ConfigMap
	ValuesSourceTypeConfigMap ValuesSourceType = "configMap"

	// ValuesSourceTypeSecret is the values source type to get values from Secret
	ValuesSourceTypeSecret ValuesSourceType = "secret"

	// ValuesSourceTypeInline is the values source type to get inlined values
	ValuesSourceTypeInline ValuesSourceType = "inline"
)

// ReleaseWaveStrategy defines the wave strategy.
// +enum
type ReleaseWaveStrategy string

const (
	// ReleaseWaveStrategyWaitForApplied is the wave strategy that waits for all resources to be applied.
	ReleaseWaveStrategyWaitForApplied ReleaseWaveStrategy = "waitForApplied"

	// ReleaseWaveStrategyWaitForHealthy is the wave strategy that waits for all resources to be healthy.
	ReleaseWaveStrategyWaitForHealthy ReleaseWaveStrategy = "waitForHealthy"

	// ReleaseWaveStrategyWaitForHealthyWithTimeout is the wave strategy that waits for all resources to be healthy with a timeout.
	ReleaseWaveStrategyWaitForHealthyWithTimeout ReleaseWaveStrategy = "waitForHealthyWithTimeout"
)

// ReleaseMutationType defines the type of mutation.
// +enum
type ReleaseMutationType string

const (
	// MutationTypeGoTemplate is the mutation type that uses Go template as mutation source.
	MutationTypeGoTemplate ReleaseMutationType = "goTemplate"

	// MutationTypeJsonPatch is the mutation type that uses JSON patch as mutation source.
	MutationTypeJsonPatch ReleaseMutationType = "jsonPatch"

	// MutationTypeStrategicMergePatch is the mutation type that uses Strategic merge patch as mutation source.
	MutationTypeStrategicMergePatch ReleaseMutationType = "strategicMergePatch"

	// MutationTypeCelTransform is the mutation type that uses CEL as mutation source.
	MutationTypeCelTransform ReleaseMutationType = "celTransform"

	// MutationTypeSyncWave is a special mutation type which is used to set
	// the sync-related annotations on the rendered resources. These annotations
	// could still be set using other mutation types, but it's typos-prone
	// approach which is hard to debug. Dedicated mutation type in opposite
	// provides the validated, targeted and typed way to produce those annotations.
	MutationTypeSyncWave ReleaseMutationType = "syncWave"
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

	// ReleaseRenderAttemptStageHookFailed is the stage of a release render attempt that failed due to a hook failure.
	ReleaseRenderAttemptStageHookFailed ReleaseRenderAttemptStage = "HookFailed"
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

	// ResourceWaveStatePending is the state of a resource wave that is pending.
	ResourceWaveStatePending ResourceWaveState = "Pending"

	// ResourceWaveStateTimedOut is the state of a resource wave that has timed out.
	ResourceWaveStateTimedOut ResourceWaveState = "TimedOut"
)

// LifecycleHookPhase defines the phase of a lifecycle hook.
// +enum
type LifecycleHookPhase string

const (
	// LifecycleHookPhasePreSync is the phase of a lifecycle hook that runs before the sync.
	LifecycleHookPhasePreSync LifecycleHookPhase = "PreSync"

	// LifecycleHookPhasePostSync is the phase of a lifecycle hook that runs after the sync.
	LifecycleHookPhasePostSync LifecycleHookPhase = "PostSync"

	// LifecycleHookPhasePreDelete is the phase of a lifecycle hook that runs before the delete.
	LifecycleHookPhasePreDelete LifecycleHookPhase = "PreDelete"

	// LifecycleHookPhasePostDelete is the phase of a lifecycle hook that runs after the delete.
	LifecycleHookPhasePostDelete LifecycleHookPhase = "PostDelete"

	// LifecycleHookPhaseSyncFailed is the phase of a lifecycle hook that runs when the sync fails.
	LifecycleHookPhaseSyncFailed LifecycleHookPhase = "SyncFailed"

	// LifecycleHookPhasePreRender is the phase of a lifecycle hook that runs before the render.
	LifecycleHookPhasePreRender LifecycleHookPhase = "PreRender"

	// LifecycleHookPhasePostRender is the phase of a lifecycle hook that runs after the render.
	LifecycleHookPhasePostRender LifecycleHookPhase = "PostRender"
)

// LifecycleHookType defines the type of a lifecycle hook.
// +enum
type LifecycleHookType string

const (
	// LifecycleHookTypePod is the type of a lifecycle hook that runs as a pod.
	LifecycleHookTypePod LifecycleHookType = "Pod"

	// LifecycleHookTypeJob is the type of a lifecycle hook that runs as a job.
	LifecycleHookTypeJob LifecycleHookType = "Job"
)

// LifecycleHookOnFailureAction defines the action to take when a lifecycle hook fails.
// +enum
type LifecycleHookOnFailureAction string

const (
	// LifecycleHookOnFailureActionContinue is the action to take when a
	// lifecycle hook fails and the Release processing should continue.
	LifecycleHookOnFailureActionContinue LifecycleHookOnFailureAction = "Continue"

	// LifecycleHookOnFailureActionFail is the action to take when a
	// lifecycle hook fails and the Release processing should fail.
	LifecycleHookOnFailureActionFail LifecycleHookOnFailureAction = "Fail"
)

// LifecycleHookCleanup defines the condition to perform a cleanup action when a lifecycle hook completes.
// +enum
type LifecycleHookCleanup string

const (
	// LifecycleHookCleanupOnSuccess is the condition to perform a
	// cleanup action when a lifecycle hook succeeds.
	LifecycleHookCleanupOnSuccess LifecycleHookCleanup = "OnSuccess"

	// LifecycleHookCleanupOnCompletion is the condition to perform a
	// cleanup action when a lifecycle hook completes with any result.
	LifecycleHookCleanupOnCompletion LifecycleHookCleanup = "OnCompletion"

	// LifecycleHookCleanupNever is the condition to never perform a
	// cleanup action when a lifecycle hook completes.
	LifecycleHookCleanupNever LifecycleHookCleanup = "Never"
)

// LifecycleHookSource defines the source of a lifecycle hook.
// +enum
type LifecycleHookSource string

const (
	// LifecycleHookSourceLifecycle is the source of a lifecycle hook that is defined in the Release.
	LifecycleHookSourceLifecycle LifecycleHookSource = "lifecycle"

	// LifecycleHookSourceChart is the source of a lifecycle hook that is defined in the Helm chart.
	LifecycleHookSourceChart LifecycleHookSource = "chart"
)

// LifecycleHookState defines the state of a lifecycle hook.
// +enum
type LifecycleHookState string

const (
	// LifecycleHookStateSkipped is the state of a lifecycle hook that has been skipped.
	LifecycleHookStateSkipped LifecycleHookState = "Skipped"

	// LifecycleHookStateRunning is the state of a lifecycle hook that is running.
	LifecycleHookStateRunning LifecycleHookState = "Running"

	// LifecycleHookStateSucceeded is the state of a lifecycle hook that has succeeded.
	LifecycleHookStateSucceeded LifecycleHookState = "Succeeded"

	// LifecycleHookStateFailed is the state of a lifecycle hook that has failed.
	LifecycleHookStateFailed LifecycleHookState = "Failed"

	// LifecycleHookStateTimedOut is the state of a lifecycle hook that has timed out.
	LifecycleHookStateTimedOut LifecycleHookState = "TimedOut"
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

	// DeletionPolicy defines the deletion policy for Release.
	DeletionPolicy ReleaseDeletionPolicy `json:"deletionPolicy"`

	// HealthAssessment defines the health assessment configuration
	// for resources delivered by the Release.
	HealthAssessment *ReleaseHealthAssessment `json:"healthAssessment,omitempty"`
}

// ReleaseSourceSpec defines the source for the release.
type ReleaseSourceSpec struct {
	// Type is the type of the source.
	Type ReleaseSourceType `json:"type"`

	// GitRepository is the Git repository source for the release.
	GitRepository *ReleaseGitRepositorySpec `json:"gitRepository,omitempty"`

	// HelmRegistry is the Helm registry source for the release.
	HelmRegistry *ReleaseHelmRegistrySpec `json:"helmRegistry,omitempty"`

	// OCIRegistry is the OCI registry source for the release.
	OCIRegistry *ReleaseOCIRegistrySpec `json:"ociRegistry,omitempty"`
}

// ReleaseGitRepositorySpec defines the Git repository source for the release.
type ReleaseGitRepositorySpec struct {
	// Name is the name of the [github.com/kumodo-io/convoy/api/source/v1alpha1.GitRepository] object.
	Name string `json:"name"`

	// Namespace is the namespace of the [github.com/kumodo-io/convoy/api/source/v1alpha1.GitRepository] object.
	Namespace string `json:"namespace,omitempty"`

	// Ref is the reference to the target revision of the Git repository.
	Ref *ReleaseGitRepositoryRefSpec `json:"ref,omitempty"`
}

// ReleaseHelmRegistrySpec defines the Helm registry source for the release.
type ReleaseHelmRegistrySpec struct {
	// Name is the name of the [github.com/kumodo-io/convoy/api/source/v1alpha1.HelmRegistry] object.
	Name string `json:"name"`

	// Namespace is the namespace of the [github.com/kumodo-io/convoy/api/source/v1alpha1.HelmRegistry] object.
	Namespace string `json:"namespace,omitempty"`

	// Chart is the definition of the Helm chart to be installed.
	Chart *ReleaseHelmRegistryChartSpec `json:"chart,omitempty"`
}

// ReleaseOCIRegistrySpec defines the OCI registry source for the release.
type ReleaseOCIRegistrySpec struct {
	// Name is the name of the [github.com/kumodo-io/convoy/api/source/v1alpha1.OCIRegistry] object.
	Name string `json:"name"`

	// Namespace is the namespace of the [github.com/kumodo-io/convoy/api/source/v1alpha1.OCIRegistry] object.
	Namespace string `json:"namespace,omitempty"`

	// Artifact is the definition of the OCI artifact to be installed.
	Artifact *ReleaseOCIRegistryArtifactSpec `json:"artifact,omitempty"`
}

// ReleaseGitRepositoryRefSpec defines the desired state of the Git repository revision.
type ReleaseGitRepositoryRefSpec struct {
	// Branch is the branch to track in the Git repository.
	Branch *string `json:"branch,omitempty"`

	// Tag is the tag to track in the Git repository.
	Tag *string `json:"tag,omitempty"`

	// Commit is the commit to track in the Git repository.
	Commit *string `json:"commit,omitempty"`

	// Semver is the tag-based semantic version expression to track in the Git repository.
	Semver *string `json:"semver,omitempty"`
}

// ReleaseHelmRegistryChartSpec defines the desired state of the Helm registry chart.
type ReleaseHelmRegistryChartSpec struct {
	// Name is the name of the Helm chart.
	Name string `json:"name"`

	// Version is the version of the Helm chart.
	Version string `json:"version"`
}

// ReleaseOCIRegistryArtifactSpec defines the desired state of the OCI registry artifact.
type ReleaseOCIRegistryArtifactSpec struct {
	// Name is the name of the OCI artifact.
	Name string `json:"name"`

	// Version is the version of the OCI artifact.
	Version string `json:"version"`
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
	// ReleaseName is the name of the Helm release.
	// Defaults to the Helm chart name if not specified.
	ReleaseName string `json:"releaseName,omitempty"`

	// ReleaseNamespace is the namespace of the Helm release.
	// Defaults to 'default' namespace if not specified.
	ReleaseNamespace string `json:"releaseNamespace,omitempty"`

	// Path is the path to the Helm chart within the source.
	// Ignored if the source is a Helm registry.
	Path string `json:"path,omitempty"`

	// DependencyMode is the dependency mode for the Helm chart.
	DependencyMode ReleaseHelmDependencyMode `json:"dependencyMode"`

	// Values contains a list of values sources to be applied to the Helm chart.
	Values []ValuesSourceSpec `json:"values,omitempty"`
}

// ReleaseContentManifestsSpec defines the manifests content of the release.
type ReleaseContentManifestsSpec struct {
	// TargetNamespace is the namespace to target for the manifests.
	// Defaults to 'default' namespace if not specified.
	TargetNamespace string `json:"targetNamespace,omitempty"`

	// Paths is the list of paths to the manifests within the source.
	// +listType=atomic
	Paths []string `json:"paths"`
}

// ReleaseContentKustomizeSpec defines the kustomize content of the release.
type ReleaseContentKustomizeSpec struct {
	// TargetNamespace is the namespace to target for the kustomize bundle.
	// Defaults to 'default' namespace if not specified.
	TargetNamespace string `json:"targetNamespace,omitempty"`

	// Path is the path to the kustomize bundle within the source.
	Path string `json:"path"`
}

// ReleaseContentJsonnetSpec defines the jsonnet content of the release.
type ReleaseContentJsonnetSpec struct {
	// TargetNamespace is the namespace to target for the jsonnet bundle.
	// Defaults to 'default' namespace if not specified.
	TargetNamespace string `json:"targetNamespace,omitempty"`

	// Path is the path to the jsonnet file within the source.
	Path string `json:"path"`

	// TLA is the list of top-level arguments to pass to the jsonnet file.
	// +listType=map
	// +listMapKey=name
	TLA []corev1.EnvVar `json:"tla,omitempty"`
}

// ValuesSourceSpec defines the source of the Helm chart values
type ValuesSourceSpec struct {
	// Type defines the type of values source.
	Type ValuesSourceType `json:"type"`

	// GitRepository defines the configuration of the git repository used as values source.
	GitRepository *ValuesSourceGitRepositorySpec `json:"gitRepository,omitempty"`

	// ConfigMap defines the config map and the key containing the values.
	ConfigMap *corev1.ConfigMapKeySelector `json:"configMap,omitempty"`

	// Secret defines the secret and the key containing the values.
	Secret *corev1.SecretKeySelector `json:"secret,omitempty"`

	// Inline contains inlined values.
	Inline *apiextensionsv1.JSON `json:"inline,omitempty"`
}

// ValuesSourceGitRepositorySpec defines the Git repository and the location of the values files.
type ValuesSourceGitRepositorySpec struct {
	*ReleaseGitRepositorySpec `json:",inline"`

	// Paths is the list of paths to values files in the git repository.
	Paths []string `json:"paths"`
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

	// Prune controls steady-state garbage collection of resources dropped from the render.
	Prune PruneSpec `json:"prune"`

	// Interval is the poll interval for drift detection.
	Interval metav1.Duration `json:"interval"`

	// RetryBackoff defines the backoff policy for retrying failed syncs.
	RetryBackoff ReleaseRetryBackoffSpec `json:"retryBackoff"`

	// WaveStrategy defines the wave strategy.
	WaveStrategy ReleaseWaveStrategySpec `json:"waveStrategy"`
}

// PruneSpec defines the garbage collection policy for Release.
type PruneSpec struct {
	// Orphaned defines whether to delete resources this Release previously applied but no longer renders.
	Orphaned bool `json:"orphaned"`

	// IncludeCRDs defines whether to prune orphaned CustomResourceDefinitions (dangerous — cascades to all CRs).
	IncludeCRDs bool `json:"includeCRDs"`

	// IncludeNamespaces defines whether to prune orphaned Namespaces (dangerous).
	IncludeNamespaces bool `json:"includeNamespaces"`
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

	// Wave is the wave number.
	Wave int `json:"wave"`

	// Timeout is the timeout for the wave strategy. If not set, controller will wait indefinitely.
	Timeout *metav1.Duration `json:"timeout,omitempty"`
}

// ReleaseRetentionPolicySpec defines the desired state of the release retention policy.
type ReleaseRetentionPolicySpec struct {
	// KeepHistory defines how many historical (rendered, applied) records to retain in S3.
	// Drives /history pagination depth and rollback range.
	KeepHistory int `json:"keepHistory"`
}

// ReleaseDeletionPolicy defines a per-Release deletion behaviour:
// what happens when the specific Release is being deleted.
type ReleaseDeletionPolicy struct {
	// KeepWorkloads flag defines whether to cleanup deployed resources
	// on the Release object deletion.
	KeepWorkloads bool `json:"keepWorkloads"`

	// BlockOnClusterUnreachable flag defines whether to block Release object
	// deletion in case the KeepWorkloads flag is set to false and the target
	// Cluster is unreachable.
	BlockOnClusterUnreachable bool `json:"blockOnClusterUnreachable"`
}

// ReleaseHealthAssessment defines selectors to get the health rules along
// with assessment deadline anchor.
type ReleaseHealthAssessment struct {
	GlobalRuleSetSelector *metav1.LabelSelector `json:"globalRuleSetSelector,omitempty"`
	LocalRuleSetSelector  *metav1.LabelSelector `json:"localRuleSetSelector,omitempty"`
	Deadline              *metav1.Duration      `json:"deadline,omitempty"`
}

// ReleaseMutation defines the desired state of the ReleaseMutation.
type ReleaseMutation struct {
	// Type is the type of the mutation.
	Type ReleaseMutationType `json:"type"`

	// Target define mutation target.
	Target ReleaseMutationTarget `json:"target"`

	// Inline is the inline mutation.
	// +listType=atomic
	Inline []string `json:"inline,omitempty"`

	// From is the source of the mutation.
	From *ReleaseMutationFromSpec `json:"from,omitempty"`

	// Patches is the list of JSON patches to apply to the release.
	// +listType=atomic
	Patches []ReleaseMutationPatchSpec `json:"patches,omitempty"`

	// WaveStrategy is the sync-wave configuration applied as annotations
	// to the defined target. Only applicable for "syncWave" mutation type.
	WaveStrategy *ReleaseWaveStrategySpec `json:"waveStrategy,omitempty"`
}

// ReleaseMutationTarget defines the target the mutation should be applied to.
// - If the name is empty, then the mutation will be applied to all rendered
// objects in the given namespace.
// - If the namespace is empty, the mutation will be applied to all rendered
// objects with matching name.
// - If both name and namespace are empty, the the mutation will be applied
// to all rendered objects.
type ReleaseMutationTarget struct {
	// Group defines the API group of the target.
	Group string `json:"group"`

	// Version defines the API version of the target.
	Version string `json:"version"`

	// Kind defines the kind of the target.
	Kind string `json:"kind"`

	// Name defines the name of the target.
	Name string `json:"name,omitempty"`

	// Namespace defines the namespace of the target.
	Namespace string `json:"namespace,omitempty"`
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

	// Ref is the reference for GitRepository used as a mutation source.
	Ref *ReleaseGitRepositoryRefSpec `json:"ref,omitempty"`
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

// ReleaseLifecycle defines the desired state of the ReleaseLifecycle.
type ReleaseLifecycle struct {
	// PreRender are the pre-render hooks.
	// +listType=atomic
	PreRender []LifecycleHookSpec `json:"preRender,omitempty"`

	// PostRender are the post-render hooks.
	// +listType=atomic
	PostRender []LifecycleHookSpec `json:"postRender,omitempty"`

	// PreSync are the pre-sync hooks.
	// +listType=atomic
	PreSync []LifecycleHookSpec `json:"preSync,omitempty"`

	// SyncFailed are the sync-failed hooks.
	// +listType=atomic
	SyncFailed []LifecycleHookSpec `json:"syncFailed,omitempty"`

	// PostSync are the post-sync hooks.
	// +listType=atomic
	PostSync []LifecycleHookSpec `json:"postSync,omitempty"`

	// PreDelete are the pre-delete hooks.
	// +listType=atomic
	PreDelete []LifecycleHookSpec `json:"preDelete,omitempty"`

	// PostDelete are the post-delete hooks.
	// +listType=atomic
	PostDelete []LifecycleHookSpec `json:"postDelete,omitempty"`
}

// LifecycleHookSpec defines the desired state of the LifecycleHook.
type LifecycleHookSpec struct {
	// Name is the name of the hook.
	Name string `json:"name"`

	// Type is the type of the hook.
	Type LifecycleHookType `json:"type"`

	// When is the hook execution condition written in CEL expression format.
	When string `json:"when,omitempty"`

	// Weight is the weight of the hook.
	Weight int32 `json:"weight"`

	// Timeout is the timeout for the hook. If not set, controller will wait indefinitely.
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// OnFailure is the action to take when the hook fails.
	OnFailure LifecycleHookOnFailureAction `json:"onFailure"`

	// DeleteBeforeCreation is true if the hook should delete the resources before they are created.
	DeleteBeforeCreation bool `json:"deleteBeforeCreation,omitempty"`

	// Cleanup is the action to take when the hook completes.
	Cleanup LifecycleHookCleanup `json:"cleanup"`

	// PodTemplate is the pod template for the hook. Mutual exclusive with JobTemplate.
	PodTemplate *corev1.PodTemplateSpec `json:"podTemplate,omitempty"`

	// JobTemplate is the job template for the hook. Mutual exclusive with PodTemplate.
	JobTemplate *batchv1.JobTemplateSpec `json:"jobTemplate,omitempty"`
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
	Pin *ReleasePinStatus `json:"pin,omitempty"`

	// ResolvedRetentionPolicy is the resolved retention policy of the release.
	ResolvedRetentionPolicy ResolvedRetentionPolicyStatus `json:"resolvedRetentionPolicy,omitempty"`

	// LastSuccessful is the last successful sync attempt.
	LastSuccessful *ReleaseAttemptedRenderStatus `json:"lastSuccessful,omitempty"`

	// LastAttempted is the last attempted sync attempt.
	LastAttempted *ReleaseAttemptedRenderStatus `json:"lastAttempted,omitempty"`

	// Waves reflects the order in which resources were applied and their health status.
	// Each wave entry represents a group of resources applied together, with a state indicating
	// whether they have been fully applied and healthy, are in progress, or have failed.
	// +listType=map
	// +listMapKey=number
	Waves []ResourceWaveStatus `json:"waves,omitempty"`

	// ReconciledAt is the time when the release was reconciled.
	ReconciledAt *metav1.Time `json:"reconciledAt,omitempty"`

	// HistoryTruncated is true if the release history is truncated.
	HistoryTruncated bool `json:"historyTruncated"`

	// Lifecycle reflects the observed state of the release lifecycle hooks.
	Lifecycle LifecycleStatus `json:"lifecycle,omitempty"`

	// HealthSummary reflects the health status of the release.
	HealthSummary ReleaseHealthSummary `json:"healthSummary,omitempty"`

	// LastHandledSyncAt reflects the timestamp when the last sync was handled
	LastHandledSyncAt *metav1.Time `json:"lastHandledSyncAt,omitempty"`
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

	// Ref is the ref of the source if applicable, e.g. branch, tag, commit.
	Ref string `json:"ref,omitempty"`

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

	// WaveNumber is the wave number of the failure.
	WaveNumber int32 `json:"waveNumber,omitempty"`

	// HookName is the name of the hook that failed.
	HookName string `json:"hookName,omitempty"`

	// HookPhase is the phase of the hook that failed.
	HookPhase *LifecycleHookPhase `json:"hookPhase,omitempty"`
}

// ResourceWaveStatus reflects the status of a wave of resources.
type ResourceWaveStatus struct {
	// Number is the resources wave weight.
	Number int64 `json:"number"`

	// Applied reflects whether all resources within the wave are applied.
	Applied *bool `json:"applied,omitempty"`

	// Healthy reflects whether all resources within the wave are healthy.
	Healthy *bool `json:"healthy,omitempty"`

	// Resources is the number of resources in the wave.
	Resources int `json:"resources,omitempty"`

	// State is the state of the wave.
	State ResourceWaveState `json:"state,omitempty"`

	// StartedAt is the time when the wave started.
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// CompletedAt is the time when the wave completed.
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// FailedAt is the time when the wave failed.
	FailedAt *metav1.Time `json:"failedAt,omitempty"`

	// Message is the failure message.
	Message string `json:"message,omitempty"`
}

type LifecycleStatus struct {
	// PreRender are the pre-render hooks.
	// +listType=atomic
	PreRender []LifecycleHookStatus `json:"preRender,omitempty"`

	// PostRender are the post-render hooks.
	// +listType=atomic
	PostRender []LifecycleHookStatus `json:"postRender,omitempty"`

	// PreSync are the pre-sync hooks.
	// +listType=atomic
	PreSync []LifecycleHookStatus `json:"preSync,omitempty"`

	// SyncFailed are the sync-failed hooks.
	// +listType=atomic
	SyncFailed []LifecycleHookStatus `json:"syncFailed,omitempty"`

	// PostSync are the post-sync hooks.
	// +listType=atomic
	PostSync []LifecycleHookStatus `json:"postSync,omitempty"`

	// PreDelete are the pre-delete hooks.
	// +listType=atomic
	PreDelete []LifecycleHookStatus `json:"preDelete,omitempty"`

	// PostDelete are the post-delete hooks.
	// +listType=atomic
	PostDelete []LifecycleHookStatus `json:"postDelete,omitempty"`
}

// LifecycleHookStatus reflects the status of a LifecycleHook.
type LifecycleHookStatus struct {
	// Name is the name of the hook.
	Name string `json:"name"`

	// Source is the source of the hook.
	Source LifecycleHookSource `json:"source"`

	// Kind is the kind of the resource created by the hook.
	Kind string `json:"kind"`

	// ApiVersion is the API version of the resource created by the hook.
	ApiVersion string `json:"apiVersion"`

	// ResourceName is the name of the resource created by the hook.
	ResourceName string `json:"resourceName"`

	// ResourceNamespace is the namespace of the resource created by the hook.
	ResourceNamespace string `json:"resourceNamespace"`

	// State is the state of the hook.
	State LifecycleHookState `json:"state"`

	// Weight is the weight of the hook.
	Weight int32 `json:"weight"`

	// StartedAt is the time when the hook started.
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// CompletedAt is the time when the hook completed.
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// CleanedUpAt is the time when the hook was cleaned up.
	CleanedUpAt *metav1.Time `json:"cleanedUpAt,omitempty"`

	// Message is the human-readable message containing the failure reason, if any.
	Message string `json:"message,omitempty"`

	// FollowUpAction is the action to take after the hook failed.
	FollowUpAction *LifecycleHookOnFailureAction `json:"followUpAction,omitempty"`
}

// ReleaseHealthSummary reflects the health summary of the Release.
type ReleaseHealthSummary struct {
	// Total is the total number of resources in the release.
	Total int32 `json:"total"`

	// Healthy is the number of healthy resources in the release.
	Healthy int32 `json:"healthy"`

	// Progressing is the number of progressing resources in the release.
	Progressing int32 `json:"progressing"`

	// Degraded is the number of degraded resources in the release.
	Degraded int32 `json:"degraded"`

	// Suspended is the number of suspended resources in the release.
	Suspended int32 `json:"suspended"`

	// Unknown is the number of resources in an unknown state.
	Unknown int32 `json:"unknown"`
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
	Lifecycle *ReleaseLifecycle `json:"lifecycle,omitempty"`
	Pin       *ReleasePin       `json:"pin,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ReleaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Release `json:"items"`
}
