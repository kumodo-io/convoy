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

package ociregistry

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
)

type authFakeResolver struct {
	secrets map[string]*corev1.Secret
}

func (f authFakeResolver) Secret(_ context.Context, _, name string) (*corev1.Secret, error) {
	if s, ok := f.secrets[name]; ok {
		return s, nil
	}
	return nil, apierrors.NewNotFound(corev1.Resource("secrets"), name)
}

func (f authFakeResolver) ConfigMap(_ context.Context, _, name string) (*corev1.ConfigMap, error) {
	return nil, apierrors.NewNotFound(corev1.Resource("configmaps"), name)
}

func secretKeyRef(name, key string) corev1.SecretKeySelector {
	return corev1.SecretKeySelector{Name: name, Key: key}
}

func regWithAuth(a sourcev1alpha1.OCIRegistryAuthSpec) *sourcev1alpha1.OCIRegistry {
	return &sourcev1alpha1.OCIRegistry{
		Name: "reg", Namespace: "ns",
		Spec: sourcev1alpha1.OCIRegistrySpec{URL: "oci://ghcr.io/org", Auth: a},
	}
}

func TestOCIResolveAuth_Basic(t *testing.T) {
	f := &ociFetcher{resolver: authFakeResolver{secrets: map[string]*corev1.Secret{"c": {Data: map[string][]byte{"password": []byte("p")}}}}}
	cred, err := f.resolveAuth(context.Background(), regWithAuth(sourcev1alpha1.OCIRegistryAuthSpec{
		Type:  sourcev1alpha1.OCIRegistryAuthTypeBasic,
		Basic: &authv1.BasicAuthSpec{Username: "u", PasswordRef: secretKeyRef("c", "password")},
	}))
	if err != nil {
		t.Fatalf("resolveAuth: %v", err)
	}
	if cred.username != "u" || cred.password != "p" || !cred.set() {
		t.Errorf("cred = %#v, want basic u/p", cred)
	}
}

func TestOCIResolveAuth_Token(t *testing.T) {
	f := &ociFetcher{resolver: authFakeResolver{secrets: map[string]*corev1.Secret{"c": {Data: map[string][]byte{"token": []byte("t")}}}}}
	cred, err := f.resolveAuth(context.Background(), regWithAuth(sourcev1alpha1.OCIRegistryAuthSpec{
		Type:  sourcev1alpha1.OCIRegistryAuthTypeToken,
		Token: &authv1.TokenAuthSpec{TokenRef: secretKeyRef("c", "token")},
	}))
	if err != nil {
		t.Fatalf("resolveAuth: %v", err)
	}
	// Token becomes the basic-auth password under the placeholder username.
	if cred.username != ociTokenUsername || cred.password != "t" {
		t.Errorf("cred = %#v, want %s/t", cred, ociTokenUsername)
	}
}

func TestOCIResolveAuth_DockerConfigUnsupported(t *testing.T) {
	f := &ociFetcher{resolver: authFakeResolver{}}
	if _, err := f.resolveAuth(context.Background(), regWithAuth(sourcev1alpha1.OCIRegistryAuthSpec{Type: sourcev1alpha1.OCIRegistryAuthTypeDockerConfig})); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported for dockerConfig", err)
	}
}
