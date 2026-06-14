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
	return allErrs
}

// ValidateReleaseUpdate validates a Release on a spec/metadata update, enforcing
// the immutability of the identity fields (source, content type, cluster).
func ValidateReleaseUpdate(release, old *Release) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMetaUpdate(&release.ObjectMeta, &old.ObjectMeta, field.NewPath("metadata"))
	allErrs = append(allErrs, validateReleaseSpec(&release.Spec, field.NewPath("spec"))...)

	if release.Spec.Source != old.Spec.Source {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "source"), release.Spec.Source, "field is immutable"))
	}
	if release.Spec.Content.Type != old.Spec.Content.Type {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "content", "type"), release.Spec.Content.Type, "field is immutable"))
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

	if spec.Source.Kind == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("source", "kind"), ""))
	}
	if spec.Source.Name == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("source", "name"), ""))
	}

	switch spec.Content.Type {
	case ReleaseContentTypeHelm, ReleaseContentTypeManifests, ReleaseContentTypeKustomize, ReleaseContentTypeJsonnet, ReleaseContentTypeCue:
		// supported
	case "":
		allErrs = append(allErrs, field.Required(fldPath.Child("content", "type"), ""))
	default:
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("content", "type"), spec.Content.Type, []string{
			string(ReleaseContentTypeHelm),
			string(ReleaseContentTypeManifests),
			string(ReleaseContentTypeKustomize),
			string(ReleaseContentTypeJsonnet),
			string(ReleaseContentTypeCue),
		}))
	}

	if spec.ClusterRef.Name == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("clusterRef", "name"), ""))
	}

	return allErrs
}
