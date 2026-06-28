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

// Package helmregistry implements the controller that reconciles source.convoy
// HelmRegistry objects: it verifies the registry is reachable. Chart content for
// a specific (name, version) coordinate is fetched on demand by the release
// controller's fetch coordinator (the coordinate lives on the Release, not the
// HelmRegistry).
package helmregistry

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
	"net/url"
	"strings"

	"sigs.k8s.io/yaml"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
	"github.com/kumodo-io/convoy/internal/auth"
)

// helmIndex is the subset of a Helm repository index.yaml we need to resolve a
// chart coordinate to a download URL. Parsed with a local type to avoid pulling
// helm.sh/helm/v3/pkg/repo, which transitively drags in the kube/ORAS tree.
type helmIndex struct {
	Entries map[string][]helmChartVersion `json:"entries"`
}

type helmChartVersion struct {
	Version string   `json:"version"`
	URLs    []string `json:"urls"`
}

// get returns the chart version matching name and an exact version string.
func (i *helmIndex) get(name, version string) (*helmChartVersion, error) {
	versions, ok := i.Entries[name]
	if !ok {
		return nil, fmt.Errorf("no chart named %q in index", name)
	}
	want := strings.TrimPrefix(version, "v")
	for idx := range versions {
		if strings.TrimPrefix(versions[idx].Version, "v") == want {
			return &versions[idx], nil
		}
	}
	return nil, fmt.Errorf("no version %q of chart %q in index", version, name)
}

// FetchResult is the content a Fetcher resolves for a chart coordinate.
type FetchResult struct {
	// Revision identifies the fetched content (chart "name-version").
	Revision string
	// Files is the chart tree, re-rooted so Chart.yaml is at the top.
	Files map[string][]byte
}

// Ref is the chart coordinate to fetch. Kept local so the fetcher stays
// decoupled from the core API (the coordinator maps the Release chart onto this).
type Ref struct {
	Name    string
	Version string
}

// Fetcher resolves a chart coordinate to its content and verifies a registry is
// reachable. It is the seam between the fetch coordinator and network I/O.
type Fetcher interface {
	// Fetch downloads the chart at ref and returns its content.
	Fetch(ctx context.Context, reg *sourcev1alpha1.HelmRegistry, ref Ref) (FetchResult, error)
	// Verify checks the registry is reachable and serves a valid index.
	Verify(ctx context.Context, reg *sourcev1alpha1.HelmRegistry) error
}

var (
	// ErrUnsupported marks a fetch rejected because the HelmRegistry requests an
	// auth type this build does not implement yet. Terminal.
	ErrUnsupported = errors.New("unsupported helm registry configuration")
	// ErrFetchFailed marks a transient failure to reach or read the registry.
	ErrFetchFailed = errors.New("helm registry fetch failed")
)

// helmFetcher fetches charts from classic HTTP Helm repositories. It currently
// supports only the "none" auth type (anonymous, public repos).
type helmFetcher struct {
	resolver auth.SecretResolver
}

// NewHelmFetcher returns a Fetcher that pulls charts from public HTTP repos.
func NewHelmFetcher(resolver auth.SecretResolver) Fetcher {
	return &helmFetcher{resolver: resolver}
}

func (f *helmFetcher) Fetch(ctx context.Context, reg *sourcev1alpha1.HelmRegistry, ref Ref) (FetchResult, error) {
	if reg.Spec.Auth.Type != sourcev1alpha1.HelmRegistryAuthTypeNone {
		return FetchResult{}, errors.Join(ErrUnsupported, fmt.Errorf("auth type %q is not supported yet (only %q)", reg.Spec.Auth.Type, sourcev1alpha1.HelmRegistryAuthTypeNone))
	}
	if ref.Version == "" {
		return FetchResult{}, errors.Join(ErrFetchFailed, fmt.Errorf("chart version is required"))
	}

	idx, err := f.index(ctx, reg)
	if err != nil {
		return FetchResult{}, err
	}
	cv, err := idx.get(ref.Name, ref.Version)
	if err != nil {
		return FetchResult{}, errors.Join(ErrFetchFailed, fmt.Errorf("chart %s-%s not found in %s: %w", ref.Name, ref.Version, reg.Spec.URL, err))
	}
	if len(cv.URLs) == 0 {
		return FetchResult{}, errors.Join(ErrFetchFailed, fmt.Errorf("chart %s-%s has no download URL", ref.Name, ref.Version))
	}

	chartURL, err := resolveChartURL(reg.Spec.URL, cv.URLs[0])
	if err != nil {
		return FetchResult{}, errors.Join(ErrFetchFailed, fmt.Errorf("resolving chart URL: %w", err))
	}
	tgz, err := f.get(ctx, chartURL, reg.Spec.TLS)
	if err != nil {
		return FetchResult{}, err
	}
	files, err := expandChartArchive(tgz)
	if err != nil {
		return FetchResult{}, errors.Join(ErrFetchFailed, fmt.Errorf("expanding chart archive: %w", err))
	}
	return FetchResult{Revision: ref.Name + "-" + ref.Version, Files: files}, nil
}

func (f *helmFetcher) Verify(ctx context.Context, reg *sourcev1alpha1.HelmRegistry) error {
	if reg.Spec.Auth.Type != sourcev1alpha1.HelmRegistryAuthTypeNone {
		return errors.Join(ErrUnsupported, fmt.Errorf("auth type %q is not supported yet (only %q)", reg.Spec.Auth.Type, sourcev1alpha1.HelmRegistryAuthTypeNone))
	}
	_, err := f.index(ctx, reg)
	return err
}

// index downloads and parses the repository's index.yaml.
func (f *helmFetcher) index(ctx context.Context, reg *sourcev1alpha1.HelmRegistry) (*helmIndex, error) {
	indexURL := strings.TrimSuffix(reg.Spec.URL, "/") + "/index.yaml"
	data, err := f.get(ctx, indexURL, reg.Spec.TLS)
	if err != nil {
		return nil, err
	}
	idx := &helmIndex{}
	if err := yaml.Unmarshal(data, idx); err != nil {
		return nil, errors.Join(ErrFetchFailed, fmt.Errorf("parsing index.yaml from %s: %w", reg.Spec.URL, err))
	}
	return idx, nil
}

// get performs an anonymous HTTP GET honoring the registry's TLS settings.
func (f *helmFetcher) get(ctx context.Context, rawURL string, tls *authv1.TLSSpec) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.Join(ErrFetchFailed, err)
	}
	resp, err := httpClient(tls).Do(req)
	if err != nil {
		return nil, errors.Join(ErrFetchFailed, fmt.Errorf("GET %s: %w", rawURL, err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Join(ErrFetchFailed, fmt.Errorf("GET %s: status %s", rawURL, resp.Status))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Join(ErrFetchFailed, fmt.Errorf("reading %s: %w", rawURL, err))
	}
	return body, nil
}

func httpClient(tls *authv1.TLSSpec) *http.Client {
	tr := &http.Transport{}
	if tls != nil && tls.InsecureSkipVerify {
		tr.TLSClientConfig = &cryptotls.Config{InsecureSkipVerify: true}
	}
	return &http.Client{Transport: tr}
}

// resolveChartURL resolves a chart's (possibly relative) download URL against the
// repository base URL. Mirrors Helm's relative-URL handling.
func resolveChartURL(base, ref string) (string, error) {
	r, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	if r.IsAbs() {
		return ref, nil
	}
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	// ResolveReference needs a trailing slash on the base path to keep it.
	b.Path = strings.TrimSuffix(b.Path, "/") + "/"
	return b.ResolveReference(r).String(), nil
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
