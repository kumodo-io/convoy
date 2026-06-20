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

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

// KubeconfigAuthenticator builds a REST config from a kubeconfig stored in a
// Secret. The kubeconfig supplies the whole config (server, CA, credentials),
// so this authenticator ignores the Cluster's server/CAPublicCertRef. It is a
// cluster-only authenticator (it satisfies the cluster controller's
// Authenticator interface; there is no registry/git equivalent).
type KubeconfigAuthenticator struct{}

// RESTConfig resolves the referenced kubeconfig Secret and parses it.
func (KubeconfigAuthenticator) RESTConfig(ctx context.Context, cluster *corev1alpha1.Cluster, resolver SecretResolver) (*rest.Config, error) {
	if cluster.Spec.Agentless == nil || cluster.Spec.Agentless.Auth.Kubeconfig == nil {
		return nil, fmt.Errorf("kubeconfig auth config is missing")
	}
	ref := cluster.Spec.Agentless.Auth.Kubeconfig.SecretRef

	secret, err := resolver.Secret(ctx, cluster.Namespace, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("resolving kubeconfig secret %s/%s: %w", cluster.Namespace, ref.Name, err)
	}
	data, ok := secret.Data[ref.Key]
	if !ok {
		return nil, fmt.Errorf("key %q not found in secret %s/%s", ref.Key, cluster.Namespace, ref.Name)
	}

	cfg, err := clientcmd.RESTConfigFromKubeConfig(data)
	if err != nil {
		return nil, fmt.Errorf("parsing kubeconfig: %w", err)
	}
	return cfg, nil
}
