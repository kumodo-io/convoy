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

package auth

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	authv1alpha1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

const (
	dummyNamespace  = "dummy-ns"
	dummySecretName = "dummy-secret"
	dummySecretKey  = "dummy-key"
)

// fakeSecretResolver serves Secrets/ConfigMaps from in-memory maps keyed by
// "namespace/name".
type fakeSecretResolver struct {
	secrets    map[string]*corev1.Secret
	configMaps map[string]*corev1.ConfigMap
}

func (f fakeSecretResolver) Secret(_ context.Context, ns, name string) (*corev1.Secret, error) {
	if s, ok := f.secrets[ns+"/"+name]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("secret %s/%s not found", ns, name)
}

func (f fakeSecretResolver) ConfigMap(_ context.Context, ns, name string) (*corev1.ConfigMap, error) {
	if c, ok := f.configMaps[ns+"/"+name]; ok {
		return c, nil
	}
	return nil, fmt.Errorf("configmap %s/%s not found", ns, name)
}

const sampleServer = "https://target.example.com:6443"

var sampleKubeconfig = []byte(`apiVersion: v1
kind: Config
clusters:
- name: target
  cluster:
    server: ` + sampleServer + `
    insecure-skip-tls-verify: true
contexts:
- name: target
  context:
    cluster: target
    user: target
current-context: target
users:
- name: target
  user:
    token: deadbeef
`)

func kubeconfigCluster() *corev1alpha1.Cluster {
	return &corev1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: dummyNamespace},
		Spec: corev1alpha1.ClusterSpec{
			ConnectionMode: corev1alpha1.ClusterConnectionModeAgentless,
			Agentless: &corev1alpha1.ClusterAgentlessModeSpec{
				Auth: corev1alpha1.ClusterAuthSpec{
					Type: corev1alpha1.ClusterAuthTypeKubeconfig,
					Kubeconfig: &authv1alpha1.KubeconfigAuthSpec{
						SecretRef: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: dummySecretName},
							Key:                  dummySecretKey,
						},
					},
				},
			},
		},
	}
}

func TestKubeconfigAuthenticator_RESTConfig(t *testing.T) {
	resolver := fakeSecretResolver{
		secrets: map[string]*corev1.Secret{
			fmt.Sprintf("%s/%s", dummyNamespace, dummySecretName): {Data: map[string][]byte{dummySecretKey: sampleKubeconfig}},
		},
	}

	cfg, err := KubeconfigAuthenticator{}.RESTConfig(context.Background(), kubeconfigCluster(), resolver)
	if err != nil {
		t.Fatalf("RESTConfig returned error: %v", err)
	}
	if cfg.Host != sampleServer {
		t.Errorf("cfg.Host = %q, want %q", cfg.Host, sampleServer)
	}
}

func TestKubeconfigAuthenticator_Errors(t *testing.T) {
	tests := []struct {
		name     string
		resolver fakeSecretResolver
		cluster  *corev1alpha1.Cluster
	}{
		{
			name:     "secret missing",
			resolver: fakeSecretResolver{secrets: map[string]*corev1.Secret{}},
			cluster:  kubeconfigCluster(),
		},
		{
			name: "key missing",
			resolver: fakeSecretResolver{secrets: map[string]*corev1.Secret{
				dummySecretName: {Data: map[string][]byte{"other": sampleKubeconfig}},
			}},
			cluster: kubeconfigCluster(),
		},
		{
			name: "malformed kubeconfig",
			resolver: fakeSecretResolver{secrets: map[string]*corev1.Secret{
				dummySecretName: {Data: map[string][]byte{dummySecretKey: []byte("not a kubeconfig")}},
			}},
			cluster: kubeconfigCluster(),
		},
		{
			name:     "kubeconfig block missing",
			resolver: fakeSecretResolver{},
			cluster: &corev1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: dummyNamespace},
				Spec: corev1alpha1.ClusterSpec{
					ConnectionMode: corev1alpha1.ClusterConnectionModeAgentless,
					Agentless: &corev1alpha1.ClusterAgentlessModeSpec{
						Auth: corev1alpha1.ClusterAuthSpec{Type: corev1alpha1.ClusterAuthTypeKubeconfig},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := (KubeconfigAuthenticator{}).RESTConfig(context.Background(), tt.cluster, tt.resolver); err == nil {
				t.Errorf("RESTConfig returned nil error, want error")
			}
		})
	}
}
