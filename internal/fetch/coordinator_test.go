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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	sourcelisters "github.com/kumodo-io/convoy/api/generated/listers/source/v1alpha1"
	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
	"github.com/kumodo-io/convoy/internal/artifact"
	"github.com/kumodo-io/convoy/internal/controller/gitrepository"
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
		ObjectMeta: metav1.ObjectMeta{Name: repoName, Namespace: repoNamespace},
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

func TestEnsure_FetchSucceedsAndCaches(t *testing.T) {
	fetcher := &fakeFetcher{result: gitrepository.FetchResult{
		Revision: "abc123",
		Files:    map[string][]byte{"app.yaml": []byte("kind: ConfigMap")},
	}}
	store := artifact.NewMemoryStore()
	c := NewCoordinator(listerWith(repo()), fetcher, store)

	res, err := c.Ensure(context.Background(), branchSrc(), repoNamespace)
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
	c := NewCoordinator(listerWith(repo()), fetcher, store)

	src := &corev1alpha1.ReleaseGitRepositorySpec{
		Name: repoName,
		Ref:  &corev1alpha1.ReleaseGitRepositoryRefSpec{Commit: new("deadbeef")},
	}
	res, err := c.Ensure(context.Background(), src, repoNamespace)
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
	c := NewCoordinator(listerWith(), &fakeFetcher{}, artifact.NewMemoryStore())

	res, err := c.Ensure(context.Background(), branchSrc(), repoNamespace)
	if err == nil {
		t.Fatalf("Ensure returned nil error, want transient error for missing repo")
	}
	if res.Ready {
		t.Errorf("Result.Ready = true, want false")
	}
}

func TestEnsure_NilSourceIsTerminal(t *testing.T) {
	c := NewCoordinator(listerWith(repo()), &fakeFetcher{}, artifact.NewMemoryStore())

	res, err := c.Ensure(context.Background(), nil, repoNamespace)
	if err != nil {
		t.Fatalf("Ensure returned error %v, want nil (terminal)", err)
	}
	if res.Ready {
		t.Errorf("Result.Ready = true, want false")
	}
}

func TestEnsure_NilRefIsTerminal(t *testing.T) {
	c := NewCoordinator(listerWith(repo()), &fakeFetcher{}, artifact.NewMemoryStore())

	res, err := c.Ensure(context.Background(), &corev1alpha1.ReleaseGitRepositorySpec{Name: repoName}, repoNamespace)
	if err != nil {
		t.Fatalf("Ensure returned error %v, want nil (terminal)", err)
	}
	if res.Ready {
		t.Errorf("Result.Ready = true, want false")
	}
}

func TestEnsure_UnsupportedFetchIsTerminal(t *testing.T) {
	fetcher := &fakeFetcher{err: errors.Join(gitrepository.ErrUnsupported, errors.New("token auth"))}
	c := NewCoordinator(listerWith(repo()), fetcher, artifact.NewMemoryStore())

	res, err := c.Ensure(context.Background(), branchSrc(), repoNamespace)
	if err != nil {
		t.Fatalf("Ensure returned error %v, want nil (terminal)", err)
	}
	if res.Ready {
		t.Errorf("Result.Ready = true, want false")
	}
}

func TestEnsure_FetchErrorIsTransient(t *testing.T) {
	fetcher := &fakeFetcher{err: errors.New("connection refused")}
	c := NewCoordinator(listerWith(repo()), fetcher, artifact.NewMemoryStore())

	res, err := c.Ensure(context.Background(), branchSrc(), repoNamespace)
	if err == nil {
		t.Fatalf("Ensure returned nil error, want transient error")
	}
	if res.Ready {
		t.Errorf("Result.Ready = true, want false")
	}
}
