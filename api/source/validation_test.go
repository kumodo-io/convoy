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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
)

func secretKeyRef(key string) corev1.SecretKeySelector {
	return corev1.SecretKeySelector{Name: "s", Key: key}
}

func gitRepo(auth GitRepositoryAuthSpec) *GitRepository {
	return &GitRepository{
		Name: "repo", Namespace: "ns",
		Spec: GitRepositorySpec{URL: "https://example.com/x.git", Auth: auth},
	}
}

func fieldErrContains(errs field.ErrorList, needle string) bool {
	for _, e := range errs {
		if strings.Contains(e.Field, needle) {
			return true
		}
	}
	return false
}

func TestValidateGitRepository_BasicValid(t *testing.T) {
	repo := gitRepo(GitRepositoryAuthSpec{
		Type:  GitRepositoryAuthTypeBasic,
		Basic: &authv1.BasicAuthSpec{Username: "u", PasswordRef: secretKeyRef("password")},
	})
	if errs := ValidateGitRepository(repo); len(errs) != 0 {
		t.Errorf("valid basic auth rejected: %v", errs)
	}
}

func TestValidateGitRepository_TokenMissingRef(t *testing.T) {
	repo := gitRepo(GitRepositoryAuthSpec{
		Type:  GitRepositoryAuthTypeToken,
		Token: &authv1.TokenAuthSpec{}, // TokenRef.Name empty
	})
	if errs := ValidateGitRepository(repo); !fieldErrContains(errs, "tokenRef") {
		t.Errorf("token auth without tokenRef not rejected: %v", errs)
	}
}

func TestValidateGitRepository_SSHRequiresKnownHosts(t *testing.T) {
	repo := gitRepo(GitRepositoryAuthSpec{
		Type:   GitRepositoryAuthTypeSSHKey,
		SSHKey: &authv1.SSHKeyAuthSpec{SSHKeyRef: secretKeyRef("id")}, // no KnownHostsRef
	})
	if errs := ValidateGitRepository(repo); !fieldErrContains(errs, "knownHostsRef") {
		t.Errorf("ssh auth without knownHostsRef not rejected: %v", errs)
	}
}

func TestValidateGitRepository_MismatchedBlockForbidden(t *testing.T) {
	// Type is token but a basic block is also present → forbidden.
	repo := gitRepo(GitRepositoryAuthSpec{
		Type:  GitRepositoryAuthTypeToken,
		Token: &authv1.TokenAuthSpec{TokenRef: secretKeyRef("token")},
		Basic: &authv1.BasicAuthSpec{Username: "u", PasswordRef: secretKeyRef("password")},
	})
	if errs := ValidateGitRepository(repo); len(errs) == 0 {
		t.Error("mismatched basic block alongside token type not rejected")
	}
}
