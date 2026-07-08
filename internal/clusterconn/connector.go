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

package clusterconn

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/auth"
)

var (
	// ErrUnsupported marks a connection rejected because the Cluster requests a
	// connection mode or auth type this build does not implement yet. It is
	// terminal — callers surface it without requeuing.
	ErrUnsupported = errors.New("unsupported cluster configuration")
	// ErrProbeFailed marks a transient failure to build a config for / reach the
	// target cluster.
	ErrProbeFailed = errors.New("probe failed")
)

// Connector turns a Cluster into a REST config to reach its target API server.
// Connection mode is a closed switch (only three modes exist); the per-auth-type
// variation lives in the Registry, used only by the agentless mode.
type Connector struct {
	// inClusterCfg is the controller pod's own config, used for inCluster mode.
	inClusterCfg *rest.Config
	registry     Registry
	resolver     auth.SecretResolver
}

// NewConnector builds a Connector. inClusterCfg is the config the manager built
// for itself (in-cluster or --kubeconfig); registry holds the agentless
// authenticators; resolver resolves referenced credentials.
func NewConnector(inClusterCfg *rest.Config, registry Registry, resolver auth.SecretResolver) *Connector {
	return &Connector{inClusterCfg: inClusterCfg, registry: registry, resolver: resolver}
}

// Connect returns a REST config for the cluster, or an error classified as
// ErrUnsupported (terminal — mode/auth not implemented) or ErrProbeFailed
// (transient — couldn't build the config).
func (c *Connector) Connect(ctx context.Context, cluster *corev1alpha1.Cluster) (*rest.Config, error) {
	switch cluster.Spec.ConnectionMode {
	case corev1alpha1.ClusterConnectionModeInCluster:
		return c.inClusterCfg, nil

	case corev1alpha1.ClusterConnectionModeAgentless:
		if cluster.Spec.Agentless == nil {
			return nil, errors.Join(ErrProbeFailed, fmt.Errorf("agentless config is missing"))
		}
		authenticator, ok := c.registry[cluster.Spec.Agentless.Auth.Type]
		if !ok {
			return nil, errors.Join(ErrUnsupported, fmt.Errorf("auth type %q is not supported yet", cluster.Spec.Agentless.Auth.Type))
		}
		cfg, err := authenticator.RESTConfig(ctx, cluster, c.resolver)
		if err != nil {
			return nil, errors.Join(ErrProbeFailed, err)
		}
		return cfg, nil

	case corev1alpha1.ClusterConnectionModeAgent:
		// Pull mode: the control plane never connects out — status would come
		// from agent registration/heartbeat. Not implemented yet.
		return nil, errors.Join(ErrUnsupported, fmt.Errorf("connection mode %q is not supported yet", cluster.Spec.ConnectionMode))

	default:
		return nil, errors.Join(ErrUnsupported, fmt.Errorf("unknown connection mode %q", cluster.Spec.ConnectionMode))
	}
}

// DynamicClients connects to the target cluster and builds a dynamic client and a
// discovery-backed RESTMapper. Both the deployer (apply/prune) and the observer
// (dry-run compare) reach target-cluster resources through this.
func (c *Connector) DynamicClients(ctx context.Context, cluster *corev1alpha1.Cluster) (dynamic.Interface, meta.RESTMapper, error) {
	cfg, err := c.Connect(ctx, cluster)
	if err != nil {
		return nil, nil, err // clusterconn.ErrUnsupported / ErrProbeFailed
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("building dynamic client: %w", err)
	}
	disco, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("building discovery client: %w", err)
	}
	groups, err := restmapper.GetAPIGroupResources(disco)
	if err != nil {
		return nil, nil, fmt.Errorf("discovering api groups: %w", err)
	}
	return dyn, restmapper.NewDiscoveryRESTMapper(groups), nil
}
