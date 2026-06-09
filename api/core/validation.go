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

	switch spec.ConnectionMode {
	case ClusterConnectionModeAgentless:
		if spec.Agentless == nil {
			allErrs = append(allErrs, field.Required(fldPath.Child("agentless"), `must be set when connectionMode is "agentless"`))
		}
	case ClusterConnectionModeAgent:
		// Agent (pull) mode carries no control-plane-side configuration today;
		// it is documented but not yet implemented.
	case "":
		allErrs = append(allErrs, field.Required(fldPath.Child("connectionMode"), ""))
	default:
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("connectionMode"), spec.ConnectionMode, []string{
			string(ClusterConnectionModeAgentless),
			string(ClusterConnectionModeAgent),
		}))
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
