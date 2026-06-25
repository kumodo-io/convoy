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

// Package clusterconn turns a Cluster object into a client config for its target
// API server. It is the shared connection layer used by the cluster controller
// (to probe) and the release controller (to apply) — neither imports the other.
package clusterconn

import (
	"context"

	"k8s.io/client-go/rest"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/auth"
)

// Authenticator builds a REST config to reach a Cluster's target API server for
// one auth type. The concrete implementations live in the auth package and
// satisfy this interface structurally. Each implementation reads its own
// sub-spec from the Cluster and produces a complete config (e.g. kubeconfig
// supplies server + CA + credentials itself, whereas token-style auth builds
// from the agentless server/CA plus resolved credentials).
type Authenticator interface {
	RESTConfig(ctx context.Context, cluster *corev1alpha1.Cluster, secrets auth.SecretResolver) (*rest.Config, error)
}

// Registry maps a cluster auth type to its Authenticator. The manager populates
// it; the connector dispatches on it for the agentless connection mode.
type Registry map[corev1alpha1.ClusterAuthType]Authenticator
