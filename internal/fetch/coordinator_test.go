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

package fetch

import (
	"context"
	"errors"
	"testing"

	"k8s.io/client-go/tools/cache"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	sourcelisters "github.com/kumodo-io/convoy/api/generated/listers/source/v1alpha1"
	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
	"github.com/kumodo-io/convoy/internal/artifact"
	"github.com/kumodo-io/convoy/internal/controller/gitrepository"
	"github.com/kumodo-io/convoy/internal/controller/helmregistry"
	"github.com/kumodo-io/convoy/internal/controller/ociregistry"
)

const (
	repoNamespace = "ns"
	repoName      = "repo"
	repoURL       = "https://example.com/org/repo.git"
)

// fakeFetcher records its calls and returns canned results.
type fakeFetcher struct {
	result gitrepository.FetchResult
	err    error
	calls  int
}

func (f *fakeFetcher) Fetch(context.Context, *sourcev1alpha1.GitRepository, gitrepository.Ref) (gitrepository.FetchResult, error) {
	f.calls++
	return f.result, f.err
}

func (f *fakeFetcher) Verify(context.Context, *sourcev1alpha1.GitRepository) error { return nil }

func listerWith(repos ...*sourcev1alpha1.GitRepository) sourcelisters.GitRepositoryLister {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	for _, r := range repos {
		_ = indexer.Add(r)
	}
	return sourcelisters.NewGitRepositoryLister(indexer)
}

func repo() *sourcev1alpha1.GitRepository {
	return &sourcev1alpha1.GitRepository{
		Name: repoName, Namespace: repoNamespace,
		Spec: sourcev1alpha1.GitRepositorySpec{
			URL:  repoURL,
			Auth: sourcev1alpha1.GitRepositoryAuthSpec{Type: sourcev1alpha1.GitRepositoryAuthTypeNone},
		},
	}
}

func branchSrc() *corev1alpha1.ReleaseGitRepositorySpec {
	return &corev1alpha1.ReleaseGitRepositorySpec{
		Name: repoName,
		Ref:  &corev1alpha1.ReleaseGitRepositoryRefSpec{Branch: new("main")},
	}
}

// gitSource wraps a git source block in the discriminated ReleaseSourceSpec.
func gitSource(s *corev1alpha1.ReleaseGitRepositorySpec) corev1alpha1.ReleaseSourceSpec {
	return corev1alpha1.ReleaseSourceSpec{Type: corev1alpha1.ReleaseSourceTypeGitRepository, GitRepository: s}
}

// gitCoordinator builds a coordinator wired with only the git dependencies.
func gitCoordinator(lister sourcelisters.GitRepositoryLister, fetcher gitrepository.Fetcher, store artifact.Store) Coordinator {
	return NewCoordinator(Deps{Store: store, GitRepos: lister, GitFetcher: fetcher})
}

func TestEnsure_FetchSucceedsAndCaches(t *testing.T) {
	fetcher := &fakeFetcher{result: gitrepository.FetchResult{
		Revision: "abc123",
		Files:    map[string][]byte{"app.yaml": []byte("kind: ConfigMap")},
	}}
	store := artifact.NewMemoryStore()
	c := gitCoordinator(listerWith(repo()), fetcher, store)

	res, err := c.Ensure(context.Background(), gitSource(branchSrc()), repoNamespace)
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if !res.Ready || res.Revision != "abc123" {
		t.Fatalf("Result = %+v, want Ready with revision abc123", res)
	}
	if _, ok := store.Get(artifact.Key(repoURL, "abc123")); !ok {
		t.Errorf("artifact not stored under (url, revision) key")
	}
}

func TestEnsure_CommitServedFromStore(t *testing.T) {
	fetcher := &fakeFetcher{}
	store := artifact.NewMemoryStore()
	store.Put(artifact.Key(repoURL, "deadbeef"), artifact.Artifact{Revision: "deadbeef"})
	c := gitCoordinator(listerWith(repo()), fetcher, store)

	src := &corev1alpha1.ReleaseGitRepositorySpec{
		Name: repoName,
		Ref:  &corev1alpha1.ReleaseGitRepositoryRefSpec{Commit: new("deadbeef")},
	}
	res, err := c.Ensure(context.Background(), gitSource(src), repoNamespace)
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if !res.Ready || res.Revision != "deadbeef" {
		t.Fatalf("Result = %+v, want Ready from cache", res)
	}
	if fetcher.calls != 0 {
		t.Errorf("fetcher was called %d times, want 0 (cache hit)", fetcher.calls)
	}
}

func TestEnsure_RepoNotFoundIsTransient(t *testing.T) {
	c := gitCoordinator(listerWith(), &fakeFetcher{}, artifact.NewMemoryStore())

	res, err := c.Ensure(context.Background(), gitSource(branchSrc()), repoNamespace)
	if err == nil {
		t.Fatalf("Ensure returned nil error, want transient error for missing repo")
	}
	if res.Ready {
		t.Errorf("Result.Ready = true, want false")
	}
}

func TestEnsure_NilSourceIsTerminal(t *testing.T) {
	c := gitCoordinator(listerWith(repo()), &fakeFetcher{}, artifact.NewMemoryStore())

	res, err := c.Ensure(context.Background(), gitSource(nil), repoNamespace)
	if err != nil {
		t.Fatalf("Ensure returned error %v, want nil (terminal)", err)
	}
	if res.Ready {
		t.Errorf("Result.Ready = true, want false")
	}
}

func TestEnsure_UnsupportedSourceTypeIsTerminal(t *testing.T) {
	c := gitCoordinator(listerWith(repo()), &fakeFetcher{}, artifact.NewMemoryStore())

	res, err := c.Ensure(context.Background(), corev1alpha1.ReleaseSourceSpec{Type: corev1alpha1.ReleaseSourceTypeHelmRegistry}, repoNamespace)
	if err != nil {
		t.Fatalf("Ensure returned error %v, want nil (terminal)", err)
	}
	if res.Ready {
		t.Errorf("Result.Ready = true, want false")
	}
}

func TestEnsure_NilRefIsTerminal(t *testing.T) {
	c := gitCoordinator(listerWith(repo()), &fakeFetcher{}, artifact.NewMemoryStore())

	res, err := c.Ensure(context.Background(), gitSource(&corev1alpha1.ReleaseGitRepositorySpec{Name: repoName}), repoNamespace)
	if err != nil {
		t.Fatalf("Ensure returned error %v, want nil (terminal)", err)
	}
	if res.Ready {
		t.Errorf("Result.Ready = true, want false")
	}
}

func TestEnsure_UnsupportedFetchIsTerminal(t *testing.T) {
	fetcher := &fakeFetcher{err: errors.Join(gitrepository.ErrUnsupported, errors.New("token auth"))}
	c := gitCoordinator(listerWith(repo()), fetcher, artifact.NewMemoryStore())

	res, err := c.Ensure(context.Background(), gitSource(branchSrc()), repoNamespace)
	if err != nil {
		t.Fatalf("Ensure returned error %v, want nil (terminal)", err)
	}
	if res.Ready {
		t.Errorf("Result.Ready = true, want false")
	}
}

func TestEnsure_FetchErrorIsTransient(t *testing.T) {
	fetcher := &fakeFetcher{err: errors.New("connection refused")}
	c := gitCoordinator(listerWith(repo()), fetcher, artifact.NewMemoryStore())

	res, err := c.Ensure(context.Background(), gitSource(branchSrc()), repoNamespace)
	if err == nil {
		t.Fatalf("Ensure returned nil error, want transient error")
	}
	if res.Ready {
		t.Errorf("Result.Ready = true, want false")
	}
}

// --- HelmRegistry source ---

const (
	helmRegName = "charts"
	helmRegURL  = "https://charts.example.com"

	revision     = "podinfo-6.5.0"
	chartName    = "podinfo"
	chartVersion = "6.5.0"
)

type fakeHelmFetcher struct {
	result helmregistry.FetchResult
	err    error
	calls  int
}

func (f *fakeHelmFetcher) Fetch(context.Context, *sourcev1alpha1.HelmRegistry, helmregistry.Ref) (helmregistry.FetchResult, error) {
	f.calls++
	return f.result, f.err
}

func (f *fakeHelmFetcher) Verify(context.Context, *sourcev1alpha1.HelmRegistry) error { return nil }

func helmListerWith(regs ...*sourcev1alpha1.HelmRegistry) sourcelisters.HelmRegistryLister {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	for _, r := range regs {
		_ = indexer.Add(r)
	}
	return sourcelisters.NewHelmRegistryLister(indexer)
}

func helmReg() *sourcev1alpha1.HelmRegistry {
	return &sourcev1alpha1.HelmRegistry{
		Name: helmRegName, Namespace: repoNamespace,
		Spec: sourcev1alpha1.HelmRegistrySpec{
			URL:  helmRegURL,
			Auth: sourcev1alpha1.HelmRegistryAuthSpec{Type: sourcev1alpha1.HelmRegistryAuthTypeNone},
		},
	}
}

func helmSource(chart, version string) corev1alpha1.ReleaseSourceSpec {
	return corev1alpha1.ReleaseSourceSpec{
		Type: corev1alpha1.ReleaseSourceTypeHelmRegistry,
		HelmRegistry: &corev1alpha1.ReleaseHelmRegistrySpec{
			Name:  helmRegName,
			Chart: &corev1alpha1.ReleaseHelmRegistryChartSpec{Name: chart, Version: version},
		},
	}
}

func helmCoordinator(lister sourcelisters.HelmRegistryLister, fetcher helmregistry.Fetcher, store artifact.Store) Coordinator {
	return NewCoordinator(Deps{Store: store, HelmRegistries: lister, HelmFetcher: fetcher})
}

func TestEnsure_HelmFetchSucceedsAndCaches(t *testing.T) {
	fetcher := &fakeHelmFetcher{result: helmregistry.FetchResult{
		Revision: revision,
		Files:    map[string][]byte{"Chart.yaml": []byte("name: podinfo")},
	}}
	store := artifact.NewMemoryStore()
	c := helmCoordinator(helmListerWith(helmReg()), fetcher, store)

	res, err := c.Ensure(context.Background(), helmSource(chartName, chartVersion), repoNamespace)
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if !res.Ready || res.Revision != revision {
		t.Fatalf("Result = %+v, want Ready with revision podinfo-6.5.0", res)
	}
	if _, ok := store.Get(artifact.Key(helmRegURL, revision)); !ok {
		t.Errorf("artifact not stored under (url, revision) key")
	}
}

func TestEnsure_HelmServedFromStore(t *testing.T) {
	fetcher := &fakeHelmFetcher{}
	store := artifact.NewMemoryStore()
	store.Put(artifact.Key(helmRegURL, revision), artifact.Artifact{Revision: revision})
	c := helmCoordinator(helmListerWith(helmReg()), fetcher, store)

	res, err := c.Ensure(context.Background(), helmSource(chartName, chartVersion), repoNamespace)
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if !res.Ready || res.Revision != revision {
		t.Fatalf("Result = %+v, want Ready from cache", res)
	}
	if fetcher.calls != 0 {
		t.Errorf("fetcher was called %d times, want 0 (version is immutable, cache hit)", fetcher.calls)
	}
}

func TestEnsure_HelmMissingCoordinateIsTerminal(t *testing.T) {
	c := helmCoordinator(helmListerWith(helmReg()), &fakeHelmFetcher{}, artifact.NewMemoryStore())

	// chart name set but version empty → terminal config error, no requeue.
	res, err := c.Ensure(context.Background(), helmSource(chartName, ""), repoNamespace)
	if err != nil {
		t.Fatalf("Ensure returned error %v, want nil (terminal)", err)
	}
	if res.Ready {
		t.Errorf("Result.Ready = true, want false")
	}
}

// --- OCIRegistry source ---

const (
	ociRegName = "oci-charts"
	ociRegURL  = "oci://reg.example.com/charts"
)

type fakeOCIFetcher struct {
	result ociregistry.FetchResult
	err    error
	calls  int
}

func (f *fakeOCIFetcher) Fetch(context.Context, *sourcev1alpha1.OCIRegistry, ociregistry.Ref) (ociregistry.FetchResult, error) {
	f.calls++
	return f.result, f.err
}

func (f *fakeOCIFetcher) Verify(context.Context, *sourcev1alpha1.OCIRegistry) error { return nil }

func ociListerWith(regs ...*sourcev1alpha1.OCIRegistry) sourcelisters.OCIRegistryLister {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	for _, r := range regs {
		_ = indexer.Add(r)
	}
	return sourcelisters.NewOCIRegistryLister(indexer)
}

func ociReg() *sourcev1alpha1.OCIRegistry {
	return &sourcev1alpha1.OCIRegistry{
		Name: ociRegName, Namespace: repoNamespace,
		Spec: sourcev1alpha1.OCIRegistrySpec{
			URL:  ociRegURL,
			Auth: sourcev1alpha1.OCIRegistryAuthSpec{Type: sourcev1alpha1.OCIRegistryAuthTypeNone},
		},
	}
}

func ociSource(name, version string) corev1alpha1.ReleaseSourceSpec {
	return corev1alpha1.ReleaseSourceSpec{
		Type: corev1alpha1.ReleaseSourceTypeOCIRegistry,
		OCIRegistry: &corev1alpha1.ReleaseOCIRegistrySpec{
			Name:     ociRegName,
			Artifact: &corev1alpha1.ReleaseOCIRegistryArtifactSpec{Name: name, Version: version},
		},
	}
}

func ociCoordinator(lister sourcelisters.OCIRegistryLister, fetcher ociregistry.Fetcher, store artifact.Store) Coordinator {
	return NewCoordinator(Deps{Store: store, OCIRegistries: lister, OCIFetcher: fetcher})
}

func TestEnsure_OCIFetchSucceedsAndCaches(t *testing.T) {
	fetcher := &fakeOCIFetcher{result: ociregistry.FetchResult{
		Revision: revision,
		Files:    map[string][]byte{"Chart.yaml": []byte("name: podinfo")},
	}}
	store := artifact.NewMemoryStore()
	c := ociCoordinator(ociListerWith(ociReg()), fetcher, store)

	res, err := c.Ensure(context.Background(), ociSource(chartName, chartVersion), repoNamespace)
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if !res.Ready || res.Revision != revision {
		t.Fatalf("Result = %+v, want Ready with revision podinfo-6.5.0", res)
	}
	if _, ok := store.Get(artifact.Key(ociRegURL, revision)); !ok {
		t.Errorf("artifact not stored under (url, revision) key")
	}
}

func TestEnsure_OCIServedFromStore(t *testing.T) {
	fetcher := &fakeOCIFetcher{}
	store := artifact.NewMemoryStore()
	store.Put(artifact.Key(ociRegURL, revision), artifact.Artifact{Revision: revision})
	c := ociCoordinator(ociListerWith(ociReg()), fetcher, store)

	res, err := c.Ensure(context.Background(), ociSource(chartName, chartVersion), repoNamespace)
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if !res.Ready || res.Revision != revision {
		t.Fatalf("Result = %+v, want Ready from cache", res)
	}
	if fetcher.calls != 0 {
		t.Errorf("fetcher was called %d times, want 0 (version is immutable, cache hit)", fetcher.calls)
	}
}

func TestEnsure_OCIMissingCoordinateIsTerminal(t *testing.T) {
	c := ociCoordinator(ociListerWith(ociReg()), &fakeOCIFetcher{}, artifact.NewMemoryStore())

	res, err := c.Ensure(context.Background(), ociSource(chartName, ""), repoNamespace)
	if err != nil {
		t.Fatalf("Ensure returned error %v, want nil (terminal)", err)
	}
	if res.Ready {
		t.Errorf("Result.Ready = true, want false")
	}
}
