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
	"fmt"
	"slices"

	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
)

const (
	basicAuthType         = "basic"
	tokenAuthType         = "token"
	sshKeyAuthType        = "sshKey"
	githubAppAuthType     = "githubApp"
	oidcAuthType          = "oidc"
	dockerConfigAuthType  = "dockerConfig"
	cloudIdentityAuthType = "cloudIdentity"
)

type sourceAuthConfig struct {
	name      string
	ownerType authv1.AuthType
	present   bool
}

func validateAuthConfig(configs []sourceAuthConfig, fldPath *field.Path, ownerType authv1.AuthType) field.ErrorList {
	allErrs := field.ErrorList{}
	for _, c := range configs {
		switch {
		case c.ownerType == ownerType && !c.present:
			allErrs = append(allErrs, field.Required(fldPath.Child("auth", c.name), fmt.Sprintf(`must be set when auth type is "%q"`, ownerType)))
		case c.ownerType != ownerType && c.present:
			allErrs = append(allErrs, field.Forbidden(fldPath.Child("auth", c.name), fmt.Sprintf(`must not be set when auth type is "%q"`, ownerType)))
		}
	}
	return allErrs
}

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

	configs := []sourceAuthConfig{
		{basicAuthType, authv1.AuthType(GitRepositoryAuthTypeBasic), spec.Auth.Basic != nil},
		{tokenAuthType, authv1.AuthType(GitRepositoryAuthTypeToken), spec.Auth.Token != nil},
		{sshKeyAuthType, authv1.AuthType(GitRepositoryAuthTypeSSHKey), spec.Auth.SSHKey != nil},
		{githubAppAuthType, authv1.AuthType(GitRepositoryAuthTypeGithubApp), spec.Auth.GithubApp != nil},
		{oidcAuthType, authv1.AuthType(GitRepositoryAuthTypeOIDC), spec.Auth.OIDC != nil},
		{cloudIdentityAuthType, authv1.AuthType(GitRepositoryAuthTypeCloudIdentity), spec.Auth.CloudIdentity != nil},
	}

	if spec.Auth.Type == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("auth", "type"), ""))
	}

	known := spec.Auth.Type == GitRepositoryAuthTypeNone || slices.ContainsFunc(configs, func(c sourceAuthConfig) bool {
		return c.ownerType == authv1.AuthType(spec.Auth.Type)
	})

	if !known {
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("auth", "type"), spec.Auth.Type, []string{
			string(GitRepositoryAuthTypeNone),
			string(GitRepositoryAuthTypeBasic),
			string(GitRepositoryAuthTypeToken),
			string(GitRepositoryAuthTypeSSHKey),
			string(GitRepositoryAuthTypeGithubApp),
			string(GitRepositoryAuthTypeOIDC),
			string(GitRepositoryAuthTypeCloudIdentity),
		}))
	}

	allErrs = append(allErrs, validateAuthConfig(configs, fldPath, authv1.AuthType(spec.Auth.Type))...)
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

	configs := []sourceAuthConfig{
		{basicAuthType, authv1.AuthType(HelmRegistryAuthTypeBasic), spec.Auth.Basic != nil},
		{tokenAuthType, authv1.AuthType(HelmRegistryAuthTypeToken), spec.Auth.Token != nil},
		{oidcAuthType, authv1.AuthType(HelmRegistryAuthTypeOIDC), spec.Auth.OIDC != nil},
		{cloudIdentityAuthType, authv1.AuthType(HelmRegistryAuthTypeCloudIdentity), spec.Auth.CloudIdentity != nil},
	}

	if spec.Auth.Type == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("auth", "type"), ""))
	}

	known := spec.Auth.Type == HelmRegistryAuthTypeNone || slices.ContainsFunc(configs, func(c sourceAuthConfig) bool {
		return c.ownerType == authv1.AuthType(spec.Auth.Type)
	})

	if !known {
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("auth", "type"), spec.Auth.Type, []string{
			string(HelmRegistryAuthTypeNone),
			string(HelmRegistryAuthTypeBasic),
			string(HelmRegistryAuthTypeToken),
			string(HelmRegistryAuthTypeOIDC),
			string(HelmRegistryAuthTypeCloudIdentity),
		}))
	}

	allErrs = append(allErrs, validateAuthConfig(configs, fldPath, authv1.AuthType(spec.Auth.Type))...)

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

	configs := []sourceAuthConfig{
		{basicAuthType, authv1.AuthType(OCIRegistryAuthTypeBasic), spec.Auth.Basic != nil},
		{tokenAuthType, authv1.AuthType(OCIRegistryAuthTypeToken), spec.Auth.Token != nil},
		{oidcAuthType, authv1.AuthType(OCIRegistryAuthTypeOIDC), spec.Auth.OIDC != nil},
		{dockerConfigAuthType, authv1.AuthType(OCIRegistryAuthTypeDockerConfig), spec.Auth.DockerConfig != nil},
		{cloudIdentityAuthType, authv1.AuthType(OCIRegistryAuthTypeCloudIdentity), spec.Auth.CloudIdentity != nil},
	}

	if spec.Auth.Type == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("auth", "type"), ""))
	}

	known := spec.Auth.Type == OCIRegistryAuthTypeNone || slices.ContainsFunc(configs, func(c sourceAuthConfig) bool {
		return c.ownerType == authv1.AuthType(spec.Auth.Type)
	})

	if !known {
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("auth", "type"), spec.Auth.Type, []string{
			string(OCIRegistryAuthTypeNone),
			string(OCIRegistryAuthTypeBasic),
			string(OCIRegistryAuthTypeToken),
			string(OCIRegistryAuthTypeOIDC),
			string(OCIRegistryAuthTypeDockerConfig),
			string(OCIRegistryAuthTypeCloudIdentity),
		}))
	}

	allErrs = append(allErrs, validateAuthConfig(configs, fldPath, authv1.AuthType(spec.Auth.Type))...)

	if spec.URL == "" {
		allErrs = append(allErrs, field.Required(fldPath.Child("url"), ""))
	}
	switch spec.DefaultContentType {
	case ContentTypeHelm, ContentTypeManifests, ContentTypeKustomize:
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
