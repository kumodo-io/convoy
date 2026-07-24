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

package helmregistry

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	return corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: name}, Key: key}
}

func regWithAuth(a sourcev1alpha1.HelmRegistryAuthSpec) *sourcev1alpha1.HelmRegistry {
	return &sourcev1alpha1.HelmRegistry{
		ObjectMeta: metav1.ObjectMeta{Name: "reg", Namespace: "ns"},
		Spec:       sourcev1alpha1.HelmRegistrySpec{URL: "https://charts.example.com", Auth: a},
	}
}

func TestHelmResolveAuth_Basic(t *testing.T) {
	f := &helmFetcher{resolver: authFakeResolver{secrets: map[string]*corev1.Secret{"c": {Data: map[string][]byte{"password": []byte("p")}}}}}
	cred, err := f.resolveAuth(context.Background(), regWithAuth(sourcev1alpha1.HelmRegistryAuthSpec{
		Type:  sourcev1alpha1.HelmRegistryAuthTypeBasic,
		Basic: &authv1.BasicAuthSpec{Username: "u", PasswordRef: secretKeyRef("c", "password")},
	}))
	if err != nil {
		t.Fatalf("resolveAuth: %v", err)
	}
	if cred.username != "u" || cred.password != "p" || cred.bearer != "" {
		t.Errorf("cred = %#v, want basic u/p", cred)
	}
}

func TestHelmResolveAuth_Token(t *testing.T) {
	f := &helmFetcher{resolver: authFakeResolver{secrets: map[string]*corev1.Secret{"c": {Data: map[string][]byte{"token": []byte("t")}}}}}
	cred, err := f.resolveAuth(context.Background(), regWithAuth(sourcev1alpha1.HelmRegistryAuthSpec{
		Type:  sourcev1alpha1.HelmRegistryAuthTypeToken,
		Token: &authv1.TokenAuthSpec{TokenRef: secretKeyRef("c", "token")},
	}))
	if err != nil {
		t.Fatalf("resolveAuth: %v", err)
	}
	if cred.bearer != "t" || cred.username != "" {
		t.Errorf("cred = %#v, want bearer t", cred)
	}
}

func TestHelmResolveAuth_Unsupported(t *testing.T) {
	f := &helmFetcher{resolver: authFakeResolver{}}
	if _, err := f.resolveAuth(context.Background(), regWithAuth(sourcev1alpha1.HelmRegistryAuthSpec{Type: sourcev1alpha1.HelmRegistryAuthTypeOIDC})); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported for oidc", err)
	}
}
