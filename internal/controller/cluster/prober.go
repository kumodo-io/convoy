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

package cluster

import (
	"context"
	"fmt"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	authv1alpha1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

// ProbeResult is the observed state of a target cluster after a successful probe.
type ProbeResult struct {
	// Version is the discovered Kubernetes server version (git version).
	Version string
	// APIVersions is the set of group/versions the target cluster serves.
	APIVersions []string
}

// Prober connects to the cluster described by a Cluster object and reports what
// it observes. It is the single seam between the reconcile loop and real
// network I/O, so the controller can be unit-tested with a fake.
type Prober interface {
	Probe(ctx context.Context, cluster *corev1alpha1.Cluster) (ProbeResult, error)
}

// ErrUnsupported is returned by a Prober when the Cluster requests a connection
// mode or auth type this build does not implement yet. Reconcile surfaces it as
// a non-Ready condition rather than retrying forever.
type ErrUnsupported struct{ Reason string }

func (e *ErrUnsupported) Error() string { return e.Reason }

// inClusterProber implements Prober for the workload-identity case: it connects
// using the controller pod's own identity (the supplied in-cluster rest.Config)
// and runs discovery against it. This is the only auth mode supported in this
// slice; other modes resolve credentials from referenced Secrets/ConfigMaps and
// are not implemented yet.
type inClusterProber struct {
	cfg *rest.Config
}

// NewInClusterProber returns a Prober that probes via the controller's own
// in-cluster identity.
func NewInClusterProber(cfg *rest.Config) Prober {
	return &inClusterProber{cfg: cfg}
}

func (p *inClusterProber) Probe(ctx context.Context, cluster *corev1alpha1.Cluster) (ProbeResult, error) {
	if cluster.Spec.ConnectionMode != corev1alpha1.ClusterConnectionModeAgentless {
		return ProbeResult{}, &ErrUnsupported{Reason: fmt.Sprintf("connection mode %q is not supported yet (only %q)", cluster.Spec.ConnectionMode, corev1alpha1.ClusterConnectionModeAgentless)}
	}
	if cluster.Spec.Agentless == nil || cluster.Spec.Agentless.Auth.Type != authv1alpha1.AuthTypeWorkloadIdentity {
		return ProbeResult{}, &ErrUnsupported{Reason: fmt.Sprintf("auth type is not supported yet (only %q)", authv1alpha1.AuthTypeWorkloadIdentity)}
	}

	dc, err := discovery.NewDiscoveryClientForConfig(p.cfg)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("building discovery client: %w", err)
	}

	info, err := dc.ServerVersion()
	if err != nil {
		return ProbeResult{}, fmt.Errorf("fetching server version: %w", err)
	}

	groups, err := dc.ServerGroups()
	if err != nil {
		return ProbeResult{}, fmt.Errorf("fetching server groups: %w", err)
	}

	var apiVersions []string
	for _, g := range groups.Groups {
		for _, v := range g.Versions {
			apiVersions = append(apiVersions, v.GroupVersion)
		}
	}

	return ProbeResult{Version: info.GitVersion, APIVersions: apiVersions}, nil
}
