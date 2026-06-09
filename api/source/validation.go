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

package source

import (
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ---------- GitRepository ----------

// ValidateGitRepository validates a GitRepository on creation.
func ValidateGitRepository(repo *GitRepository) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMeta(&repo.ObjectMeta, true, apivalidation.NameIsDNSSubdomain, field.NewPath("metadata"))
	allErrs = append(allErrs, validateGitRepositorySpec(&repo.Spec, field.NewPath("spec"))...)
	return allErrs
}

// ValidateGitRepositoryUpdate validates a GitRepository spec/metadata update.
func ValidateGitRepositoryUpdate(repo, old *GitRepository) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMetaUpdate(&repo.ObjectMeta, &old.ObjectMeta, field.NewPath("metadata"))
	allErrs = append(allErrs, validateGitRepositorySpec(&repo.Spec, field.NewPath("spec"))...)
	return allErrs
}

// ValidateGitRepositoryStatusUpdate validates a GitRepository status update.
func ValidateGitRepositoryStatusUpdate(repo, old *GitRepository) field.ErrorList {
	return apivalidation.ValidateObjectMetaUpdate(&repo.ObjectMeta, &old.ObjectMeta, field.NewPath("metadata"))
}

func validateGitRepositorySpec(spec *GitRepositorySpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if spec.URL == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("url"), ""))
	}

	set := 0
	for _, ref := range []*string{spec.Ref.Branch, spec.Ref.Tag, spec.Ref.Commit, spec.Ref.Semver} {
		if ref != nil {
			set++
		}
	}
	if set != 1 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("ref"), spec.Ref, "exactly one of branch, tag, commit, or semver must be set"))
	}

	return allErrs
}

// ---------- HelmRegistry ----------

// ValidateHelmRegistry validates a HelmRegistry on creation.
func ValidateHelmRegistry(reg *HelmRegistry) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMeta(&reg.ObjectMeta, true, apivalidation.NameIsDNSSubdomain, field.NewPath("metadata"))
	allErrs = append(allErrs, validateHelmRegistrySpec(&reg.Spec, field.NewPath("spec"))...)
	return allErrs
}

// ValidateHelmRegistryUpdate validates a HelmRegistry spec/metadata update.
func ValidateHelmRegistryUpdate(reg, old *HelmRegistry) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMetaUpdate(&reg.ObjectMeta, &old.ObjectMeta, field.NewPath("metadata"))
	allErrs = append(allErrs, validateHelmRegistrySpec(&reg.Spec, field.NewPath("spec"))...)
	return allErrs
}

// ValidateHelmRegistryStatusUpdate validates a HelmRegistry status update.
func ValidateHelmRegistryStatusUpdate(reg, old *HelmRegistry) field.ErrorList {
	return apivalidation.ValidateObjectMetaUpdate(&reg.ObjectMeta, &old.ObjectMeta, field.NewPath("metadata"))
}

func validateHelmRegistrySpec(spec *HelmRegistrySpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if spec.URL == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("url"), ""))
	}
	return allErrs
}

// ---------- OCIRegistry ----------

// ValidateOCIRegistry validates an OCIRegistry on creation.
func ValidateOCIRegistry(reg *OCIRegistry) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMeta(&reg.ObjectMeta, true, apivalidation.NameIsDNSSubdomain, field.NewPath("metadata"))
	allErrs = append(allErrs, validateOCIRegistrySpec(&reg.Spec, field.NewPath("spec"))...)
	return allErrs
}

// ValidateOCIRegistryUpdate validates an OCIRegistry spec/metadata update.
func ValidateOCIRegistryUpdate(reg, old *OCIRegistry) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMetaUpdate(&reg.ObjectMeta, &old.ObjectMeta, field.NewPath("metadata"))
	allErrs = append(allErrs, validateOCIRegistrySpec(&reg.Spec, field.NewPath("spec"))...)
	return allErrs
}

// ValidateOCIRegistryStatusUpdate validates an OCIRegistry status update.
func ValidateOCIRegistryStatusUpdate(reg, old *OCIRegistry) field.ErrorList {
	return apivalidation.ValidateObjectMetaUpdate(&reg.ObjectMeta, &old.ObjectMeta, field.NewPath("metadata"))
}

func validateOCIRegistrySpec(spec *OCIRegistrySpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if spec.URL == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("url"), ""))
	}
	switch spec.DefaultContentType {
	case "", ContentTypeHelm, ContentTypeManifests, ContentTypeKustomize:
		// empty is allowed; the controller defaults it
	default:
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("defaultContentType"), spec.DefaultContentType, []string{
			string(ContentTypeHelm),
			string(ContentTypeManifests),
			string(ContentTypeKustomize),
		}))
	}
	return allErrs
}
