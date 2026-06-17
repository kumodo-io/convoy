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
	"errors"
	"fmt"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

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

var (
	// ErrUnsupported marks a probe rejected because the Cluster requests a
	// connection mode or auth type this build does not implement yet. It is
	// terminal — reconcile surfaces it as a non-Ready condition without
	// requeuing. Probers join it with a detail error describing what was asked.
	ErrUnsupported = errors.New("unsupported cluster configuration")
	// ErrProbeFailed marks a transient failure to reach the target cluster.
	// Probers join it with the underlying connection/discovery error.
	ErrProbeFailed = errors.New("probe failed")
)

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
	if cluster.Spec.ConnectionMode != corev1alpha1.ClusterConnectionModeInCluster {
		return ProbeResult{}, errors.Join(ErrUnsupported, fmt.Errorf("connection mode %q is not supported yet (only %q)", cluster.Spec.ConnectionMode, corev1alpha1.ClusterConnectionModeInCluster))
	}

	dc, err := discovery.NewDiscoveryClientForConfig(p.cfg)
	if err != nil {
		return ProbeResult{}, errors.Join(ErrProbeFailed, fmt.Errorf("building discovery client: %w", err))
	}

	info, err := dc.ServerVersion()
	if err != nil {
		return ProbeResult{}, errors.Join(ErrProbeFailed, fmt.Errorf("fetching server version: %w", err))
	}

	groups, err := dc.ServerGroups()
	if err != nil {
		return ProbeResult{}, errors.Join(ErrProbeFailed, fmt.Errorf("fetching server groups: %w", err))
	}

	var apiVersions []string
	for _, g := range groups.Groups {
		for _, v := range g.Versions {
			apiVersions = append(apiVersions, v.GroupVersion)
		}
	}

	return ProbeResult{Version: info.GitVersion, APIVersions: apiVersions}, nil
}
