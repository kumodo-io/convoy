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
	return allErrs
}

// ValidateReleaseUpdate validates a Release on a spec/metadata update, enforcing
// the immutability of the identity fields (source, content type, cluster).
func ValidateReleaseUpdate(release, old *Release) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMetaUpdate(&release.ObjectMeta, &old.ObjectMeta, field.NewPath("metadata"))
	allErrs = append(allErrs, validateReleaseSpec(&release.Spec, field.NewPath("spec"))...)
	allErrs = append(allErrs, validateSameNamespaceRefs(release)...)

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

	return allErrs
}
