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

// Package ociregistry implements the controller that reconciles source.convoy
// OCIRegistry objects: it verifies the registry is reachable. Chart content for a
// specific (name, version) coordinate is pulled on demand by the release
// controller's fetch coordinator (the coordinate lives on the Release, not the
// OCIRegistry). This build supports Helm charts stored as OCI artifacts.
package ociregistry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	cryptotls "crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"helm.sh/helm/v3/pkg/registry"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
	"github.com/kumodo-io/convoy/internal/auth"
)

// FetchResult is the content a Fetcher resolves for an artifact coordinate.
type FetchResult struct {
	// Revision identifies the fetched content (chart "name-version").
	Revision string
	// Files is the chart tree, re-rooted so Chart.yaml is at the top.
	Files map[string][]byte
}

// Ref is the artifact coordinate to fetch. Kept local so the fetcher stays
// decoupled from the core API.
type Ref struct {
	Name    string
	Version string
}

// Fetcher pulls an OCI artifact for a coordinate and verifies a registry is
// reachable. It is the seam between the fetch coordinator and network I/O.
type Fetcher interface {
	// Fetch pulls the chart artifact at ref and returns its content.
	Fetch(ctx context.Context, reg *sourcev1alpha1.OCIRegistry, ref Ref) (FetchResult, error)
	// Verify checks the registry's distribution API is reachable.
	Verify(ctx context.Context, reg *sourcev1alpha1.OCIRegistry) error
}

var (
	// ErrUnsupported marks a fetch rejected because the OCIRegistry requests an
	// auth type this build does not implement yet. Terminal.
	ErrUnsupported = errors.New("unsupported oci registry configuration")
	// ErrFetchFailed marks a transient failure to reach or read the registry.
	ErrFetchFailed = errors.New("oci registry fetch failed")
)

// pullFunc pulls a Helm chart artifact and returns the chart .tgz bytes. It is a
// field on the fetcher so tests can inject a fake without a live registry. Note:
// Helm's registry client takes no context, so pulls are not cancellable.
type pullFunc func(ref string, tls *authv1.TLSSpec) ([]byte, error)

// ociFetcher pulls Helm charts stored as OCI artifacts. It currently supports
// only the "none" auth type (anonymous pulls from public registries).
type ociFetcher struct {
	resolver auth.SecretResolver
	pull     pullFunc
}

// NewOCIFetcher returns a Fetcher that pulls charts from public OCI registries.
func NewOCIFetcher(resolver auth.SecretResolver) Fetcher {
	return &ociFetcher{resolver: resolver, pull: defaultPull}
}

func (f *ociFetcher) Fetch(_ context.Context, reg *sourcev1alpha1.OCIRegistry, ref Ref) (FetchResult, error) {
	if reg.Spec.Auth.Type != sourcev1alpha1.OCIRegistryAuthTypeNone {
		return FetchResult{}, errors.Join(ErrUnsupported, fmt.Errorf("auth type %q is not supported yet (only %q)", reg.Spec.Auth.Type, sourcev1alpha1.OCIRegistryAuthTypeNone))
	}
	if ref.Version == "" {
		return FetchResult{}, errors.Join(ErrFetchFailed, fmt.Errorf("artifact version is required"))
	}

	tgz, err := f.pull(ociRef(reg.Spec.URL, ref.Name, ref.Version), reg.Spec.TLS)
	if err != nil {
		return FetchResult{}, err
	}
	files, err := expandChartArchive(tgz)
	if err != nil {
		return FetchResult{}, errors.Join(ErrFetchFailed, fmt.Errorf("expanding chart archive: %w", err))
	}
	return FetchResult{Revision: ref.Name + "-" + ref.Version, Files: files}, nil
}

func (f *ociFetcher) Verify(ctx context.Context, reg *sourcev1alpha1.OCIRegistry) error {
	if reg.Spec.Auth.Type != sourcev1alpha1.OCIRegistryAuthTypeNone {
		return errors.Join(ErrUnsupported, fmt.Errorf("auth type %q is not supported yet (only %q)", reg.Spec.Auth.Type, sourcev1alpha1.OCIRegistryAuthTypeNone))
	}
	// The distribution API base (/v2/) is the standard liveness probe. A healthy
	// registry returns 200 (open) or 401 (auth required) — both mean reachable.
	pingURL := registryPingURL(reg.Spec.URL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pingURL, nil)
	if err != nil {
		return errors.Join(ErrFetchFailed, err)
	}
	resp, err := httpClient(reg.Spec.TLS).Do(req)
	if err != nil {
		return errors.Join(ErrFetchFailed, fmt.Errorf("GET %s: %w", pingURL, err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized {
		return nil
	}
	return errors.Join(ErrFetchFailed, fmt.Errorf("GET %s: status %s", pingURL, resp.Status))
}

// defaultPull pulls a Helm chart artifact via Helm's OCI registry client.
func defaultPull(ref string, tls *authv1.TLSSpec) ([]byte, error) {
	var opts []registry.ClientOption
	if tls != nil && tls.InsecureSkipVerify {
		opts = append(opts, registry.ClientOptHTTPClient(httpClient(tls)))
	}
	client, err := registry.NewClient(opts...)
	if err != nil {
		return nil, errors.Join(ErrFetchFailed, fmt.Errorf("building registry client: %w", err))
	}
	res, err := client.Pull(ref, registry.PullOptWithChart(true))
	if err != nil {
		return nil, errors.Join(ErrFetchFailed, fmt.Errorf("pull %s: %w", ref, err))
	}
	if res.Chart == nil || len(res.Chart.Data) == 0 {
		return nil, errors.Join(ErrFetchFailed, fmt.Errorf("no chart layer in %s", ref))
	}
	return res.Chart.Data, nil
}

// ociRef builds an OCI reference "host/path/name:version" from the registry base
// URL (which may carry an oci:// scheme) and the artifact coordinate.
func ociRef(base, name, version string) string {
	b := strings.TrimPrefix(base, "oci://")
	b = strings.TrimSuffix(b, "/")
	return fmt.Sprintf("%s/%s:%s", b, name, version)
}

// registryPingURL returns the distribution API base URL for the registry host.
func registryPingURL(base string) string {
	b := strings.TrimPrefix(base, "oci://")
	host := b
	if i := strings.IndexByte(b, '/'); i >= 0 {
		host = b[:i]
	}
	return "https://" + host + "/v2/"
}

func httpClient(tls *authv1.TLSSpec) *http.Client {
	tr := &http.Transport{}
	if tls != nil && tls.InsecureSkipVerify {
		tr.TLSClientConfig = &cryptotls.Config{InsecureSkipVerify: true}
	}
	return &http.Client{Transport: tr}
}

// expandChartArchive unpacks a chart .tgz, stripping the single top-level chart
// directory so Chart.yaml sits at the root of the returned tree.
func expandChartArchive(data []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()

	files := map[string][]byte{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		i := strings.IndexByte(name, '/')
		if i < 0 {
			continue // file outside a chart dir; skip
		}
		rel := name[i+1:]
		if rel == "" {
			continue
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		files[rel] = b
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("empty chart archive")
	}
	return files, nil
}
