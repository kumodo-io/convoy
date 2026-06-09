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
