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

package helmregistry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
)

const (
	demoChartName = "demo"
)

// chartTGZ builds a chart .tgz with a "demo/" top-level dir from chart-relative
// files.
func chartTGZ(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		hdr := &tar.Header{Name: "demo/" + name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// repoServer serves an index.yaml referencing a single demo-0.1.0 chart and the
// chart tarball itself.
func repoServer(t *testing.T) *httptest.Server {
	t.Helper()
	tgz := chartTGZ(t, map[string]string{
		"Chart.yaml":        "apiVersion: v2\nname: demo\nversion: 0.1.0\n",
		"templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/charts/demo-0.1.0.tgz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(tgz)
	})
	srv := httptest.NewServer(mux)
	mux.HandleFunc("/index.yaml", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "apiVersion: v1\nentries:\n  demo:\n    - version: 0.1.0\n      urls:\n        - %s/charts/demo-0.1.0.tgz\n", srv.URL)
	})
	t.Cleanup(srv.Close)
	return srv
}

func noneRegistry(url string) *sourcev1alpha1.HelmRegistry {
	return &sourcev1alpha1.HelmRegistry{
		Spec: sourcev1alpha1.HelmRegistrySpec{
			URL:  url,
			Auth: sourcev1alpha1.HelmRegistryAuthSpec{Type: sourcev1alpha1.HelmRegistryAuthTypeNone},
		},
	}
}

func TestHelmFetcher_FetchReRootsChart(t *testing.T) {
	srv := repoServer(t)
	f := NewHelmFetcher(nil)

	got, err := f.Fetch(context.Background(), noneRegistry(srv.URL), Ref{Name: demoChartName, Version: "0.1.0"})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if got.Revision != "demo-0.1.0" {
		t.Errorf("Revision = %q, want demo-0.1.0", got.Revision)
	}
	// Files must be re-rooted: Chart.yaml at the top, not under demo/.
	if _, ok := got.Files["Chart.yaml"]; !ok {
		t.Errorf("Chart.yaml not at chart root; files = %v", keys(got.Files))
	}
	if _, ok := got.Files["templates/cm.yaml"]; !ok {
		t.Errorf("templates/cm.yaml missing; files = %v", keys(got.Files))
	}
}

func TestHelmFetcher_VerifyReachable(t *testing.T) {
	srv := repoServer(t)
	if err := NewHelmFetcher(nil).Verify(context.Background(), noneRegistry(srv.URL)); err != nil {
		t.Errorf("Verify returned error: %v", err)
	}
}

func TestHelmFetcher_VerifyUnreachable(t *testing.T) {
	err := NewHelmFetcher(nil).Verify(context.Background(), noneRegistry("http://127.0.0.1:1"))
	if !errors.Is(err, ErrFetchFailed) {
		t.Errorf("err = %v, want ErrFetchFailed", err)
	}
}

func TestHelmFetcher_UnknownVersion(t *testing.T) {
	srv := repoServer(t)
	_, err := NewHelmFetcher(nil).Fetch(context.Background(), noneRegistry(srv.URL), Ref{Name: demoChartName, Version: "9.9.9"})
	if !errors.Is(err, ErrFetchFailed) {
		t.Errorf("err = %v, want ErrFetchFailed", err)
	}
}

func TestHelmFetcher_UnsupportedAuth(t *testing.T) {
	reg := noneRegistry("https://example.com")
	reg.Spec.Auth.Type = sourcev1alpha1.HelmRegistryAuthTypeBasic
	_, err := NewHelmFetcher(nil).Fetch(context.Background(), reg, Ref{Name: demoChartName, Version: "0.1.0"})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
