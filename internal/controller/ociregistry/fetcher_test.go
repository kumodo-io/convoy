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

package ociregistry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
)

const (
	refName    = "demo"
	refVersion = "0.1.0"
)

// chartTGZ builds a chart .tgz with a "demo/" top-level dir from chart-relative files.
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

func registryWithURL(url string) *sourcev1alpha1.OCIRegistry {
	return &sourcev1alpha1.OCIRegistry{
		Spec: sourcev1alpha1.OCIRegistrySpec{
			URL:  url,
			Auth: sourcev1alpha1.OCIRegistryAuthSpec{Type: sourcev1alpha1.OCIRegistryAuthTypeNone},
		},
	}
}

func TestOCIRef(t *testing.T) {
	cases := []struct{ base, name, version, want string }{
		{"oci://ghcr.io/stefanprodan/charts", "podinfo", "6.5.0", "ghcr.io/stefanprodan/charts/podinfo:6.5.0"},
		{"ghcr.io/org/charts/", "app", "1.0.0", "ghcr.io/org/charts/app:1.0.0"},
	}
	for _, c := range cases {
		if got := ociRef(c.base, c.name, c.version); got != c.want {
			t.Errorf("ociRef(%q,%q,%q) = %q, want %q", c.base, c.name, c.version, got, c.want)
		}
	}
}

func TestFetch_PullsAndReRoots(t *testing.T) {
	tgz := chartTGZ(t, map[string]string{
		"Chart.yaml":        "apiVersion: v2\nname: demo\nversion: 0.1.0\n",
		"templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n",
	})
	var gotRef string
	f := &ociFetcher{pull: func(ref string, _ *authv1.TLSSpec) ([]byte, error) {
		gotRef = ref
		return tgz, nil
	}}

	got, err := f.Fetch(context.Background(), registryWithURL("oci://reg.example.com/charts"), Ref{Name: refName, Version: refVersion})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if gotRef != "reg.example.com/charts/demo:0.1.0" {
		t.Errorf("pull ref = %q, want reg.example.com/charts/demo:0.1.0", gotRef)
	}
	if got.Revision != "demo-0.1.0" {
		t.Errorf("Revision = %q, want demo-0.1.0", got.Revision)
	}
	if _, ok := got.Files["Chart.yaml"]; !ok {
		t.Errorf("Chart.yaml not re-rooted; files = %v", keys(got.Files))
	}
	if _, ok := got.Files["templates/cm.yaml"]; !ok {
		t.Errorf("templates/cm.yaml missing; files = %v", keys(got.Files))
	}
}

func TestFetch_PullError(t *testing.T) {
	f := &ociFetcher{pull: func(string, *authv1.TLSSpec) ([]byte, error) {
		return nil, errors.Join(ErrFetchFailed, errors.New("manifest unknown"))
	}}
	_, err := f.Fetch(context.Background(), registryWithURL("oci://reg.example.com/charts"), Ref{Name: refName, Version: refVersion})
	if !errors.Is(err, ErrFetchFailed) {
		t.Errorf("err = %v, want ErrFetchFailed", err)
	}
}

func TestFetch_UnsupportedAuth(t *testing.T) {
	reg := registryWithURL("oci://reg.example.com/charts")
	reg.Spec.Auth.Type = sourcev1alpha1.OCIRegistryAuthTypeBasic
	f := &ociFetcher{pull: func(string, *authv1.TLSSpec) ([]byte, error) { return nil, nil }}
	_, err := f.Fetch(context.Background(), reg, Ref{Name: refName, Version: refVersion})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestFetch_MissingVersion(t *testing.T) {
	f := &ociFetcher{pull: func(string, *authv1.TLSSpec) ([]byte, error) { return nil, nil }}
	_, err := f.Fetch(context.Background(), registryWithURL("oci://reg.example.com/charts"), Ref{Name: refName})
	if !errors.Is(err, ErrFetchFailed) {
		t.Errorf("err = %v, want ErrFetchFailed", err)
	}
}

func TestVerify_Reachable(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	reg := registryWithURL("oci://" + strings.TrimPrefix(srv.URL, "https://"))
	reg.Spec.TLS = &authv1.TLSSpec{InsecureSkipVerify: true}
	if err := NewOCIFetcher(nil).Verify(context.Background(), reg); err != nil {
		t.Errorf("Verify returned error: %v", err)
	}
}

func TestVerify_Unreachable(t *testing.T) {
	err := NewOCIFetcher(nil).Verify(context.Background(), registryWithURL("oci://127.0.0.1:1"))
	if !errors.Is(err, ErrFetchFailed) {
		t.Errorf("err = %v, want ErrFetchFailed", err)
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
