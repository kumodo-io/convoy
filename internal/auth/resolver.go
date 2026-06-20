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

// Package auth holds the credential-resolution building blocks shared by the
// convoy controllers. It defines the shared vocabulary (SecretResolver,
// RegistryCredential) and the concrete authenticators; the per-resource
// Authenticator interfaces live in their consumer packages so this package
// stays a leaf (it imports only API types and client-go, never a controller).
package auth

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// SecretResolver reads Secrets and ConfigMaps from the cluster convoy runs in.
// Authenticators use it to resolve credential material referenced by an object's
// auth spec (the references are namespace-local to the object).
type SecretResolver interface {
	Secret(ctx context.Context, namespace, name string) (*corev1.Secret, error)
	ConfigMap(ctx context.Context, namespace, name string) (*corev1.ConfigMap, error)
}

// clientSecretResolver resolves via a Kubernetes clientset. A lister-backed
// implementation can replace it later without touching the authenticators.
type clientSecretResolver struct {
	kube kubernetes.Interface
}

// NewSecretResolver returns a SecretResolver backed by the given clientset.
func NewSecretResolver(kube kubernetes.Interface) SecretResolver {
	return &clientSecretResolver{kube: kube}
}

func (r *clientSecretResolver) Secret(ctx context.Context, namespace, name string) (*corev1.Secret, error) {
	return r.kube.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
}

func (r *clientSecretResolver) ConfigMap(ctx context.Context, namespace, name string) (*corev1.ConfigMap, error) {
	return r.kube.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
}
