/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package core

import (
	"fmt"
	"slices"

	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ValidateCluster validates a Cluster on creation.
func ValidateCluster(cluster *Cluster) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMeta(&cluster.ObjectMeta, true, apivalidation.NameIsDNSSubdomain, field.NewPath("metadata"))
	allErrs = append(allErrs, validateClusterSpec(&cluster.Spec, field.NewPath("spec"))...)
	return allErrs
}

// ValidateClusterUpdate validates a Cluster on a spec/metadata update.
func ValidateClusterUpdate(cluster, old *Cluster) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMetaUpdate(&cluster.ObjectMeta, &old.ObjectMeta, field.NewPath("metadata"))
	allErrs = append(allErrs, validateClusterSpec(&cluster.Spec, field.NewPath("spec"))...)
	return allErrs
}

// ValidateClusterStatusUpdate validates a Cluster on a status update.
func ValidateClusterStatusUpdate(cluster, old *Cluster) field.ErrorList {
	return apivalidation.ValidateObjectMetaUpdate(&cluster.ObjectMeta, &old.ObjectMeta, field.NewPath("metadata"))
}

func validateClusterSpec(spec *ClusterSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	type clusterConnectionModeConfig struct {
		name      string
		ownerType ClusterConnectionMode
		present   bool
	}

	configs := []clusterConnectionModeConfig{
		{"agentless", ClusterConnectionModeAgentless, spec.Agentless != nil},
		{"agent", ClusterConnectionModeAgent, spec.Agent != nil},
	}

	if spec.ConnectionMode == "" {
		return append(allErrs, field.Required(fldPath.Child("connectionMode"), ""))
	}

	known := spec.ConnectionMode == ClusterConnectionModeInCluster || slices.ContainsFunc(configs, func(c clusterConnectionModeConfig) bool {
		return c.ownerType == spec.ConnectionMode
	})

	if !known {
		return append(allErrs, field.NotSupported(fldPath.Child("connectionMode"), spec.ConnectionMode, []string{
			string(ClusterConnectionModeInCluster),
			string(ClusterConnectionModeAgentless),
			string(ClusterConnectionModeAgent),
		}))
	}

	for _, c := range configs {
		switch {
		case c.ownerType == spec.ConnectionMode && !c.present:
			allErrs = append(allErrs, field.Required(fldPath.Child(c.name), fmt.Sprintf(`must be set when connectionMode is "%q"`, spec.ConnectionMode)))
		case c.ownerType != spec.ConnectionMode && c.present:
			allErrs = append(allErrs, field.Forbidden(fldPath.Child(c.name), fmt.Sprintf(`must not be set when connectionMode is "%q"`, spec.ConnectionMode)))
		}
	}

	if spec.ConnectionMode == ClusterConnectionModeAgentless && spec.Agentless != nil {
		allErrs = append(allErrs, validateClusterAuthSpec(&spec.Agentless.Auth, fldPath.Child("agentless", "auth"))...)
	}

	return allErrs
}

func validateClusterAuthSpec(spec *ClusterAuthSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	type clusterAuthConfig struct {
		name      string
		ownerType ClusterAuthType
		present   bool
	}

	configs := []clusterAuthConfig{
		{"kubeconfig", ClusterAuthTypeKubeconfig, spec.Kubeconfig != nil},
		{"token", ClusterAuthTypeToken, spec.Token != nil},
		{"clientCert", ClusterAuthTypeClientCert, spec.ClientCert != nil},
		{"execPlugin", ClusterAuthTypeExecPlugin, spec.ExecPlugin != nil},
		{"oidc", ClusterAuthTypeOIDC, spec.OIDC != nil},
	}

	// fail-fast if the auth type is not set
	if spec.Type == "" {
		return append(allErrs, field.Required(fldPath.Child("type"), ""))
	}

	// fail-fast if the auth type is not known
	known := slices.ContainsFunc(configs, func(c clusterAuthConfig) bool {
		return c.ownerType == spec.Type
	})
	if !known {
		return append(allErrs, field.NotSupported(fldPath.Child("type"), spec.Type, []string{
			string(ClusterAuthTypeKubeconfig),
			string(ClusterAuthTypeToken),
			string(ClusterAuthTypeClientCert),
			string(ClusterAuthTypeExecPlugin),
			string(ClusterAuthTypeOIDC),
		}))
	}

	for _, c := range configs {
		switch {
		case c.ownerType == spec.Type && !c.present:
			allErrs = append(allErrs, field.Required(fldPath.Child(c.name), fmt.Sprintf("must be set when auth type is %q", c.ownerType)))
		case c.ownerType != spec.Type && c.present:
			allErrs = append(allErrs, field.Forbidden(fldPath.Child(c.name), fmt.Sprintf("must not be set when auth type is %q", c.ownerType)))
		}
	}

	return allErrs
}

// ValidateRelease validates a Release on creation.
func ValidateRelease(release *Release) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMeta(&release.ObjectMeta, true, apivalidation.NameIsDNSSubdomain, field.NewPath("metadata"))
	allErrs = append(allErrs, validateReleaseSpec(&release.Spec, field.NewPath("spec"))...)
	allErrs = append(allErrs, validateSameNamespaceRefs(release)...)
	allErrs = append(allErrs, validateReleaseMutationList(release)...)
	allErrs = append(allErrs, ValidateReleaseLifecycle(release)...)
	return allErrs
}

// validateReleaseMutationList validates each mutation's type/content (without the
// same-namespace check, which validateSameNamespaceRefs already covers).
func validateReleaseMutationList(release *Release) field.ErrorList {
	allErrs := make(field.ErrorList, 0, len(release.Mutations))
	path := field.NewPath("mutations")
	for i := range release.Mutations {
		allErrs = append(allErrs, validateReleaseMutation(&release.Mutations[i], path.Index(i))...)
	}
	return allErrs
}

// validateSameNamespaceRefs enforces that a Release only references Clusters and
// Sources in its own namespace. The ref Namespace fields exist for a future
// cross-namespace grant model (see ROADMAP: ReferenceGrant); until that lands, a
// cross-namespace ref would let a user borrow credential-bearing Cluster/Source
// objects they cannot access — the controller resolves refs as its own identity,
// not the requester's — so reject them. An empty ref namespace defaults to the
// Release's own namespace and is always allowed.
func validateSameNamespaceRefs(release *Release) field.ErrorList {
	allErrs := field.ErrorList{}
	ns := release.Namespace
	spec := &release.Spec
	specPath := field.NewPath("spec")

	check := func(refNS string, path *field.Path) {
		if refNS != "" && refNS != ns {
			allErrs = append(allErrs, field.Forbidden(path,
				fmt.Sprintf("cross-namespace references are not permitted; must be empty or the Release namespace %q", ns)))
		}
	}

	check(spec.ClusterRef.Namespace, specPath.Child("clusterRef", "namespace"))
	if spec.Source.GitRepository != nil {
		check(spec.Source.GitRepository.Namespace, specPath.Child("source", "gitRepository", "namespace"))
	}
	if spec.Source.HelmRegistry != nil {
		check(spec.Source.HelmRegistry.Namespace, specPath.Child("source", "helmRegistry", "namespace"))
	}
	if spec.Source.OCIRegistry != nil {
		check(spec.Source.OCIRegistry.Namespace, specPath.Child("source", "ociRegistry", "namespace"))
	}
	if spec.Content.Helm != nil {
		valuesPath := specPath.Child("content", "helm", "values")
		for i := range spec.Content.Helm.Values {
			v := &spec.Content.Helm.Values[i]
			if v.GitRepository != nil && v.GitRepository.ReleaseGitRepositorySpec != nil {
				check(v.GitRepository.Namespace, valuesPath.Index(i).Child("gitRepository", "namespace"))
			}
		}
	}
	// Mutation From sources are credential-bearing refs too.
	mutationsPath := field.NewPath("mutations")
	for i := range release.Mutations {
		if from := release.Mutations[i].From; from != nil {
			check(from.Namespace, mutationsPath.Index(i).Child("from", "namespace"))
		}
	}
	return allErrs
}

// ValidateReleaseUpdate validates a Release on a spec/metadata update, enforcing
// the immutability of the identity fields (source, content type, cluster).
func ValidateReleaseUpdate(release, old *Release) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMetaUpdate(&release.ObjectMeta, &old.ObjectMeta, field.NewPath("metadata"))
	allErrs = append(allErrs, validateReleaseSpec(&release.Spec, field.NewPath("spec"))...)
	allErrs = append(allErrs, validateSameNamespaceRefs(release)...)
	allErrs = append(allErrs, validateReleaseMutationList(release)...)
	allErrs = append(allErrs, ValidateReleaseLifecycle(release)...)

	// source configuration validation
	if release.Spec.Source.Type != old.Spec.Source.Type {
		return append(allErrs, field.Invalid(field.NewPath("spec", "source", "type"), release.Spec.Source.Type, "field is immutable"))
	}
	// git repository configuration validation
	if release.Spec.Source.GitRepository != nil && (release.Spec.Source.GitRepository.Name != old.Spec.Source.GitRepository.Name) {
		return append(allErrs, field.Invalid(field.NewPath("spec", "source", "gitRepository", "name"), release.Spec.Source.GitRepository.Name, "field is immutable"))
	}
	if release.Spec.Source.GitRepository != nil && (release.Spec.Source.GitRepository.Namespace != old.Spec.Source.GitRepository.Namespace) {
		return append(allErrs, field.Invalid(field.NewPath("spec", "source", "gitRepository", "namespace"), release.Spec.Source.GitRepository.Namespace, "field is immutable"))
	}

	// helm registry configuration validation
	if release.Spec.Source.HelmRegistry != nil && (release.Spec.Source.HelmRegistry.Name != old.Spec.Source.HelmRegistry.Name) {
		return append(allErrs, field.Invalid(field.NewPath("spec", "source", "helmRegistry", "name"), release.Spec.Source.HelmRegistry.Name, "field is immutable"))
	}
	if release.Spec.Source.HelmRegistry != nil && (release.Spec.Source.HelmRegistry.Namespace != old.Spec.Source.HelmRegistry.Namespace) {
		return append(allErrs, field.Invalid(field.NewPath("spec", "source", "helmRegistry", "namespace"), release.Spec.Source.HelmRegistry.Namespace, "field is immutable"))
	}

	// oci registry configuration validation
	if release.Spec.Source.OCIRegistry != nil && (release.Spec.Source.OCIRegistry.Name != old.Spec.Source.OCIRegistry.Name) {
		return append(allErrs, field.Invalid(field.NewPath("spec", "source", "ociRegistry", "name"), release.Spec.Source.OCIRegistry.Name, "field is immutable"))
	}
	if release.Spec.Source.OCIRegistry != nil && (release.Spec.Source.OCIRegistry.Namespace != old.Spec.Source.OCIRegistry.Namespace) {
		return append(allErrs, field.Invalid(field.NewPath("spec", "source", "ociRegistry", "namespace"), release.Spec.Source.OCIRegistry.Namespace, "field is immutable"))
	}

	if release.Spec.Content.Type != old.Spec.Content.Type {
		return append(allErrs, field.Invalid(field.NewPath("spec", "content", "type"), release.Spec.Content.Type, "field is immutable"))
	}
	if release.Spec.ClusterRef != old.Spec.ClusterRef {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "clusterRef"), release.Spec.ClusterRef, "field is immutable"))
	}

	return allErrs
}

// ValidateReleaseStatusUpdate validates a Release on a status update.
func ValidateReleaseStatusUpdate(release, old *Release) field.ErrorList {
	return apivalidation.ValidateObjectMetaUpdate(&release.ObjectMeta, &old.ObjectMeta, field.NewPath("metadata"))
}

// ValidateReleaseMutations validates the Release's post-render mutation pipeline
// (the /mutations subresource). Each mutation must declare a known type and carry
// the content that type requires. It also re-checks the same-namespace guard on
// mutation From references.
func ValidateReleaseMutations(release *Release) field.ErrorList {
	allErrs := make(field.ErrorList, 0, len(release.Mutations))
	path := field.NewPath("mutations")
	for i := range release.Mutations {
		allErrs = append(allErrs, validateReleaseMutation(&release.Mutations[i], path.Index(i))...)
	}
	allErrs = append(allErrs, validateSameNamespaceRefs(release)...)
	return allErrs
}

func validateReleaseMutation(m *ReleaseMutation, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	switch m.Type {
	case MutationTypeJsonPatch:
		if len(m.Patches) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("patches"), "jsonPatch requires at least one patch"))
		}
		for j := range m.Patches {
			if !knownJSONPatchOp(m.Patches[j].Op) {
				allErrs = append(allErrs, field.NotSupported(fldPath.Child("patches").Index(j).Child("op"), m.Patches[j].Op,
					[]string{"add", "remove", "replace", "move", "copy", "test"}))
			}
			if m.Patches[j].Path == "" {
				allErrs = append(allErrs, field.Required(fldPath.Child("patches").Index(j).Child("path"), ""))
			}
		}
	case MutationTypeStrategicMergePatch, MutationTypeGoTemplate, MutationTypeCelTransform:
		if len(m.Inline) == 0 && m.From == nil {
			allErrs = append(allErrs, field.Required(fldPath, fmt.Sprintf("%s requires inline content or a from source", m.Type)))
		}
	case MutationTypeSyncWave:
		if m.WaveStrategy == nil {
			allErrs = append(allErrs, field.Required(fldPath.Child("waveStrategy"), "syncWave requires waveStrategy"))
		} else if s := m.WaveStrategy.Strategy; s != "" && !knownWaveStrategy(s) {
			allErrs = append(allErrs, field.NotSupported(fldPath.Child("waveStrategy", "strategy"), s,
				[]string{string(ReleaseWaveStrategyWaitForApplied), string(ReleaseWaveStrategyWaitForHealthy), string(ReleaseWaveStrategyWaitForHealthyWithTimeout)}))
		}
	default:
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("type"), m.Type,
			[]string{string(MutationTypeJsonPatch), string(MutationTypeStrategicMergePatch), string(MutationTypeGoTemplate), string(MutationTypeCelTransform), string(MutationTypeSyncWave)}))
	}
	return allErrs
}

func knownWaveStrategy(s ReleaseWaveStrategy) bool {
	switch s {
	case ReleaseWaveStrategyWaitForApplied, ReleaseWaveStrategyWaitForHealthy, ReleaseWaveStrategyWaitForHealthyWithTimeout:
		return true
	default:
		return false
	}
}

func knownRollbackPolicy(p RollbackPolicy) bool {
	switch p {
	case RollbackPolicyNever, RollbackPolicyOnWaveFailure, RollbackPolicyOnReleaseFailure:
		return true
	default:
		return false
	}
}

// validateFailurePolicy validates spec.failurePolicy. rollback must be a known policy.
// A rollback other than Never requires spec.healthAssessment.deadline: the Degraded
// verdict is the only thing that fails a rollout, and a resource escalates to Degraded
// only once that deadline elapses — without it a failing rollout would wait forever and
// the policy could never act, so an enabled-looking policy that can never fire is a
// configuration error, not a silent no-op. See https://convoy.kumodo.io/documentation/architecture/rollout-state-machine/.
func validateFailurePolicy(spec *ReleaseSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	fp := spec.FailurePolicy
	if fp == nil {
		return allErrs
	}
	if !knownRollbackPolicy(fp.Rollback) {
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("rollback"), fp.Rollback, []string{
			string(RollbackPolicyNever),
			string(RollbackPolicyOnWaveFailure),
			string(RollbackPolicyOnReleaseFailure),
		}))
	}
	if fp.Rollback != RollbackPolicyNever {
		if ha := spec.HealthAssessment; ha == nil || ha.Deadline == nil {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "healthAssessment", "deadline"),
				"is required when spec.failurePolicy.rollback is not Never"))
		}
	}
	return allErrs
}

func knownJSONPatchOp(op JSONPatchOperation) bool {
	switch op {
	case JSONPatchOperationAdd, JSONPatchOperationRemove, JSONPatchOperationReplace,
		JSONPatchOperationMove, JSONPatchOperationCopy, JSONPatchOperationTest:
		return true
	default:
		return false
	}
}

// ValidateReleaseLifecycle validates the Release's lifecycle hooks (the top-level
// lifecycle field, written via the /lifecycle subresource). It performs structural
// validation only: hook name presence + per-phase uniqueness, type↔template
// consistency, and known onFailure/cleanup enums. CEL `when` expressions are
// compiled by the hook runner, not here.
func ValidateReleaseLifecycle(release *Release) field.ErrorList {
	allErrs := field.ErrorList{}
	if release.Lifecycle == nil {
		return allErrs
	}
	lc := release.Lifecycle
	base := field.NewPath("lifecycle")
	phases := []struct {
		name  string
		hooks []LifecycleHookSpec
	}{
		{"preSync", lc.PreSync},
		{"syncFailed", lc.SyncFailed},
		{"postSync", lc.PostSync},
		{"preDelete", lc.PreDelete},
		{"postDelete", lc.PostDelete},
	}
	for _, p := range phases {
		allErrs = append(allErrs, validateLifecyclePhase(p.hooks, base.Child(p.name), p.name)...)
	}
	return allErrs
}

// lifecyclePhasePostSync is the field name of the postSync phase — the only phase
// on which runAfter is allowed.
const lifecyclePhasePostSync = "postSync"

// validateLifecyclePhase validates one phase's hook list: names are unique within
// the phase, and each hook is individually valid.
func validateLifecyclePhase(hooks []LifecycleHookSpec, fldPath *field.Path, phaseName string) field.ErrorList {
	allErrs := field.ErrorList{}
	seen := make(map[string]struct{}, len(hooks))
	for i := range hooks {
		h := &hooks[i]
		hookPath := fldPath.Index(i)
		if h.Name == "" {
			allErrs = append(allErrs, field.Required(hookPath.Child("name"), "hook name is required"))
		} else if _, dup := seen[h.Name]; dup {
			allErrs = append(allErrs, field.Duplicate(hookPath.Child("name"), h.Name))
		} else {
			seen[h.Name] = struct{}{}
		}
		allErrs = append(allErrs, validateLifecycleHook(h, hookPath, phaseName)...)
	}
	return allErrs
}

// validateLifecycleHook validates a single hook's type↔template consistency, its
// onFailure/cleanup enums, and its runAfter (postSync-only). phaseName is the
// phase's field name (e.g. "postSync").
func validateLifecycleHook(h *LifecycleHookSpec, fldPath *field.Path, phaseName string) field.ErrorList {
	allErrs := field.ErrorList{}

	// Exactly one template, matching the declared type.
	switch h.Type {
	case LifecycleHookTypePod:
		if h.PodTemplate == nil {
			allErrs = append(allErrs, field.Required(fldPath.Child("podTemplate"), `podTemplate is required when type is "Pod"`))
		}
		if h.JobTemplate != nil {
			allErrs = append(allErrs, field.Forbidden(fldPath.Child("jobTemplate"), `must not be set when type is "Pod"`))
		}
	case LifecycleHookTypeJob:
		if h.JobTemplate == nil {
			allErrs = append(allErrs, field.Required(fldPath.Child("jobTemplate"), `jobTemplate is required when type is "Job"`))
		}
		if h.PodTemplate != nil {
			allErrs = append(allErrs, field.Forbidden(fldPath.Child("podTemplate"), `must not be set when type is "Job"`))
		}
	default:
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("type"), h.Type,
			[]string{string(LifecycleHookTypePod), string(LifecycleHookTypeJob)}))
	}

	// Enums: empty is allowed (the runner defaults per phase); a set value must be known.
	if s := h.OnFailure; s != "" && !knownHookOnFailure(s) {
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("onFailure"), s,
			[]string{string(LifecycleHookOnFailureActionContinue), string(LifecycleHookOnFailureActionFail)}))
	}
	if c := h.Cleanup; c != "" && !knownHookCleanup(c) {
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("cleanup"), c,
			[]string{string(LifecycleHookCleanupOnSuccess), string(LifecycleHookCleanupOnCompletion), string(LifecycleHookCleanupNever)}))
	}

	// runAfter is a postSync-only trigger: it gates when the hook fires (after the
	// release is Applied — the default — or Healthy). On any other phase it is
	// meaningless (those phases have no post-apply health milestone to wait on).
	if h.RunAfter != nil {
		if phaseName != lifecyclePhasePostSync {
			allErrs = append(allErrs, field.Forbidden(fldPath.Child("runAfter"), "runAfter is only allowed on postSync hooks"))
		} else if !knownHookRunAfter(*h.RunAfter) {
			allErrs = append(allErrs, field.NotSupported(fldPath.Child("runAfter"), *h.RunAfter,
				[]string{string(LifecycleHookRunAfterApplied), string(LifecycleHookRunAfterHealthy)}))
		}
	}
	return allErrs
}

// knownHookRunAfter reports whether a is a recognized runAfter value.
func knownHookRunAfter(a LifecycleHookRunAfter) bool {
	switch a {
	case LifecycleHookRunAfterApplied, LifecycleHookRunAfterHealthy:
		return true
	default:
		return false
	}
}

func knownHookOnFailure(a LifecycleHookOnFailureAction) bool {
	switch a {
	case LifecycleHookOnFailureActionContinue, LifecycleHookOnFailureActionFail:
		return true
	default:
		return false
	}
}

func knownHookCleanup(c LifecycleHookCleanup) bool {
	switch c {
	case LifecycleHookCleanupOnSuccess, LifecycleHookCleanupOnCompletion, LifecycleHookCleanupNever:
		return true
	default:
		return false
	}
}

func validateReleaseSpec(spec *ReleaseSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	type releaseSourceConfig struct {
		name      string
		ownerType ReleaseSourceType
		present   bool
	}

	sourceConfigs := []releaseSourceConfig{
		{"ref", ReleaseSourceTypeGitRepository, spec.Source.GitRepository != nil},
		{"chart", ReleaseSourceTypeHelmRegistry, spec.Source.HelmRegistry != nil},
		{"artifact", ReleaseSourceTypeOCIRegistry, spec.Source.OCIRegistry != nil},
	}

	if spec.Source.Type == "" {
		return append(allErrs, field.Required(fldPath.Child("source", "type"), ""))
	}

	known := slices.ContainsFunc(sourceConfigs, func(c releaseSourceConfig) bool {
		return c.ownerType == spec.Source.Type
	})
	if !known {
		return append(allErrs, field.NotSupported(fldPath.Child("source", "type"), spec.Source.Type, []string{
			string(ReleaseSourceTypeGitRepository),
			string(ReleaseSourceTypeHelmRegistry),
			string(ReleaseSourceTypeOCIRegistry),
		}))
	}

	for _, c := range sourceConfigs {
		switch {
		case c.ownerType == spec.Source.Type && !c.present:
			allErrs = append(allErrs, field.Required(fldPath.Child(c.name), fmt.Sprintf(`must be set when connectionMode is "%q"`, spec.Source.Type)))
		case c.ownerType != spec.Source.Type && c.present:
			allErrs = append(allErrs, field.Forbidden(fldPath.Child(c.name), fmt.Sprintf(`must not be set when connectionMode is "%q"`, spec.Source.Type)))
		}
	}

	type releaseContentConfig struct {
		name      string
		ownerType ReleaseContentType
		present   bool
	}

	contentConfigs := []releaseContentConfig{
		{"helm", ReleaseContentTypeHelm, spec.Content.Helm != nil},
		{"manifests", ReleaseContentTypeManifests, spec.Content.Manifests != nil},
		{"kustomize", ReleaseContentTypeKustomize, spec.Content.Kustomize != nil},
		{"jsonnet", ReleaseContentTypeJsonnet, spec.Content.Jsonnet != nil},
		// {"cue", ReleaseContentTypeCue, spec.Content.Cue != nil},
	}

	if spec.Content.Type == "" {
		return append(allErrs, field.Required(fldPath.Child("content", "type"), ""))
	}

	known = slices.ContainsFunc(contentConfigs, func(c releaseContentConfig) bool {
		return c.ownerType == spec.Content.Type
	})
	if !known {
		return append(allErrs, field.NotSupported(fldPath.Child("content", "type"), spec.Content.Type, []string{
			string(ReleaseContentTypeHelm),
			string(ReleaseContentTypeManifests),
			string(ReleaseContentTypeKustomize),
			string(ReleaseContentTypeJsonnet),
		}))
	}

	for _, c := range contentConfigs {
		switch {
		case c.ownerType == spec.Content.Type && !c.present:
			allErrs = append(allErrs, field.Required(fldPath.Child(c.name), fmt.Sprintf(`must be set when content type is "%q"`, spec.Content.Type)))
		case c.ownerType != spec.Content.Type && c.present:
			allErrs = append(allErrs, field.Forbidden(fldPath.Child(c.name), fmt.Sprintf(`must not be set when content type is "%q"`, spec.Content.Type)))
		}
	}

	if spec.ClusterRef.Name == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("clusterRef", "name"), ""))
	}

	// Axis B cluster-wave strategy (spec.syncPolicy.waveStrategy). Empty defaults to
	// waitForApplied; any set value must be a known strategy.
	if s := spec.SyncPolicy.WaveStrategy.Strategy; s != "" && !knownWaveStrategy(s) {
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("syncPolicy", "waveStrategy", "strategy"), s,
			[]string{
				string(ReleaseWaveStrategyWaitForApplied),
				string(ReleaseWaveStrategyWaitForHealthy),
				string(ReleaseWaveStrategyWaitForHealthyWithTimeout),
			}))
	}

	allErrs = append(allErrs, validateFailurePolicy(spec, fldPath.Child("failurePolicy"))...)

	return allErrs
}
