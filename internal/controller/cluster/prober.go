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
	"github.com/kumodo-io/convoy/internal/clusterconn"
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

// connectorProber implements Prober as the composition of the (varying)
// connection step and the (fixed) discovery step: connect → discover. The
// connect-layer error sentinels live in the clusterconn package.
type connectorProber struct {
	connect *clusterconn.Connector
}

// NewProber returns a Prober that builds a config via the connector and probes
// it with discovery.
func NewProber(connect *clusterconn.Connector) Prober {
	return &connectorProber{connect: connect}
}

func (p *connectorProber) Probe(ctx context.Context, cluster *corev1alpha1.Cluster) (ProbeResult, error) {
	cfg, err := p.connect.Connect(ctx, cluster)
	if err != nil {
		return ProbeResult{}, err // already classified (ErrUnsupported / ErrProbeFailed)
	}
	return discover(ctx, cfg)
}

// discover runs discovery against any config — the part that does not vary by
// connection mode or auth type.
func discover(_ context.Context, cfg *rest.Config) (ProbeResult, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return ProbeResult{}, errors.Join(clusterconn.ErrProbeFailed, fmt.Errorf("building discovery client: %w", err))
	}

	info, err := dc.ServerVersion()
	if err != nil {
		return ProbeResult{}, errors.Join(clusterconn.ErrProbeFailed, fmt.Errorf("fetching server version: %w", err))
	}

	groups, err := dc.ServerGroups()
	if err != nil {
		return ProbeResult{}, errors.Join(clusterconn.ErrProbeFailed, fmt.Errorf("fetching server groups: %w", err))
	}

	var apiVersions []string
	for _, g := range groups.Groups {
		for _, v := range g.Versions {
			apiVersions = append(apiVersions, v.GroupVersion)
		}
	}

	return ProbeResult{Version: info.GitVersion, APIVersions: apiVersions}, nil
}
