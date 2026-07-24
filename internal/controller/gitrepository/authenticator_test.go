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

package gitrepository

import (
	"context"
	"errors"
	"testing"

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
)

// authFakeResolver serves canned Secrets/ConfigMaps by name.
type authFakeResolver struct {
	secrets    map[string]*corev1.Secret
	configMaps map[string]*corev1.ConfigMap
}

func (f authFakeResolver) Secret(_ context.Context, _, name string) (*corev1.Secret, error) {
	if s, ok := f.secrets[name]; ok {
		return s, nil
	}
	return nil, apierrors.NewNotFound(corev1.Resource("secrets"), name)
}

func (f authFakeResolver) ConfigMap(_ context.Context, _, name string) (*corev1.ConfigMap, error) {
	if c, ok := f.configMaps[name]; ok {
		return c, nil
	}
	return nil, apierrors.NewNotFound(corev1.Resource("configmaps"), name)
}

func secretKeyRef(name, key string) corev1.SecretKeySelector {
	return corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: name}, Key: key}
}

func gitRepoWithAuth(auth sourcev1alpha1.GitRepositoryAuthSpec) *sourcev1alpha1.GitRepository {
	return &sourcev1alpha1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "repo", Namespace: "ns"},
		Spec:       sourcev1alpha1.GitRepositorySpec{URL: "https://example.com/x.git", Auth: auth},
	}
}

func TestAuthMethod_None(t *testing.T) {
	m, err := authMethod(context.Background(), authFakeResolver{}, gitRepoWithAuth(sourcev1alpha1.GitRepositoryAuthSpec{Type: sourcev1alpha1.GitRepositoryAuthTypeNone}))
	if err != nil {
		t.Fatalf("authMethod: %v", err)
	}
	if m != nil {
		t.Errorf("none auth = %v, want nil (anonymous)", m)
	}
}

func TestAuthMethod_Basic(t *testing.T) {
	repo := gitRepoWithAuth(sourcev1alpha1.GitRepositoryAuthSpec{
		Type:  sourcev1alpha1.GitRepositoryAuthTypeBasic,
		Basic: &authv1.BasicAuthSpec{Username: "alice", PasswordRef: secretKeyRef("creds", "password")},
	})
	r := authFakeResolver{secrets: map[string]*corev1.Secret{"creds": {Data: map[string][]byte{"password": []byte("s3cr3t")}}}}

	m, err := authMethod(context.Background(), r, repo)
	if err != nil {
		t.Fatalf("authMethod: %v", err)
	}
	ba, ok := m.(*githttp.BasicAuth)
	if !ok || ba.Username != "alice" || ba.Password != "s3cr3t" {
		t.Errorf("basic auth = %#v, want alice/s3cr3t", m)
	}
}

func TestAuthMethod_Token(t *testing.T) {
	repo := gitRepoWithAuth(sourcev1alpha1.GitRepositoryAuthSpec{
		Type:  sourcev1alpha1.GitRepositoryAuthTypeToken,
		Token: &authv1.TokenAuthSpec{TokenRef: secretKeyRef("creds", "token")},
	})
	r := authFakeResolver{secrets: map[string]*corev1.Secret{"creds": {Data: map[string][]byte{"token": []byte("ghp_x")}}}}

	m, err := authMethod(context.Background(), r, repo)
	if err != nil {
		t.Fatalf("authMethod: %v", err)
	}
	// Token is sent as the basic-auth password with a placeholder username.
	ba, ok := m.(*githttp.BasicAuth)
	if !ok || ba.Username != tokenAuthUsername || ba.Password != "ghp_x" {
		t.Errorf("token auth = %#v, want %s/ghp_x", m, tokenAuthUsername)
	}
}

func TestAuthMethod_UnsupportedType(t *testing.T) {
	repo := gitRepoWithAuth(sourcev1alpha1.GitRepositoryAuthSpec{Type: sourcev1alpha1.GitRepositoryAuthTypeGithubApp})
	if _, err := authMethod(context.Background(), authFakeResolver{}, repo); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported for githubApp", err)
	}
}

func TestAuthMethod_SSHInvalidKey(t *testing.T) {
	repo := gitRepoWithAuth(sourcev1alpha1.GitRepositoryAuthSpec{
		Type:   sourcev1alpha1.GitRepositoryAuthTypeSSHKey,
		SSHKey: &authv1.SSHKeyAuthSpec{SSHKeyRef: secretKeyRef("ssh", "id")},
	})
	r := authFakeResolver{secrets: map[string]*corev1.Secret{"ssh": {Data: map[string][]byte{"id": []byte("not-a-key")}}}}
	if _, err := authMethod(context.Background(), r, repo); err == nil {
		t.Error("authMethod accepted an invalid ssh key, want error")
	}
}

func TestAuthMethod_MissingSecret(t *testing.T) {
	repo := gitRepoWithAuth(sourcev1alpha1.GitRepositoryAuthSpec{
		Type:  sourcev1alpha1.GitRepositoryAuthTypeToken,
		Token: &authv1.TokenAuthSpec{TokenRef: secretKeyRef("absent", "token")},
	})
	if _, err := authMethod(context.Background(), authFakeResolver{}, repo); err == nil {
		t.Error("authMethod succeeded with a missing secret, want error")
	}
}
