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

package gitrepository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
)

func repoWithRef(url string, ref sourcev1alpha1.GitRepositoryRefSpec, authType sourcev1alpha1.GitRepositoryAuthType) *sourcev1alpha1.GitRepository {
	return &sourcev1alpha1.GitRepository{
		Spec: sourcev1alpha1.GitRepositorySpec{
			URL:  url,
			Ref:  ref,
			Auth: sourcev1alpha1.GitRepositoryAuthSpec{Type: authType},
		},
	}
}

// initLocalRepo creates an on-disk git repo with one commit, a tag, and returns
// the repo path, its default branch name, the tag name, and the commit SHA.
func initLocalRepo(t *testing.T) (dir, branch, tag, commit string) {
	t.Helper()
	dir = t.TempDir()
	r, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	wt, err := r.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.yaml"), []byte("kind: ConfigMap\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := wt.Add("app.yaml"); err != nil {
		t.Fatalf("add: %v", err)
	}
	h, err := wt.Commit("init", &gogit.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example.com", When: time.Unix(1_700_000_000, 0)},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	head, err := r.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	tag = "v1.0.0"
	if _, err := r.CreateTag(tag, h, nil); err != nil {
		t.Fatalf("tag: %v", err)
	}
	return dir, head.Name().Short(), tag, h.String()
}

func TestGitFetcher_Branch(t *testing.T) {
	dir, branch, _, commit := initLocalRepo(t)
	// depth 0: the local file transport doesn't do shallow clones.
	f := &gitFetcher{depth: 0}

	got, err := f.Fetch(context.Background(), repoWithRef(dir,
		sourcev1alpha1.GitRepositoryRefSpec{Branch: &branch}, sourcev1alpha1.GitRepositoryAuthTypeNone))
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if got.Revision != commit {
		t.Errorf("Revision = %q, want %q", got.Revision, commit)
	}
	if got.TreeHash == "" {
		t.Errorf("TreeHash is empty")
	}
	if got.ResolvedRef != branch {
		t.Errorf("ResolvedRef = %q, want %q", got.ResolvedRef, branch)
	}
}

func TestGitFetcher_Tag(t *testing.T) {
	dir, _, tag, commit := initLocalRepo(t)
	f := &gitFetcher{depth: 0}

	got, err := f.Fetch(context.Background(), repoWithRef(dir,
		sourcev1alpha1.GitRepositoryRefSpec{Tag: &tag}, sourcev1alpha1.GitRepositoryAuthTypeNone))
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if got.Revision != commit {
		t.Errorf("Revision = %q, want %q", got.Revision, commit)
	}
	if got.ResolvedRef != tag {
		t.Errorf("ResolvedRef = %q, want %q", got.ResolvedRef, tag)
	}
}

func TestGitFetcher_UnsupportedAuthType(t *testing.T) {
	f := NewGitFetcher(nil)
	_, err := f.Fetch(context.Background(), repoWithRef(dummyGitRepositoryURL,
		sourcev1alpha1.GitRepositoryRefSpec{Branch: new("main")}, sourcev1alpha1.GitRepositoryAuthTypeToken))
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestGitFetcher_SemverUnsupported(t *testing.T) {
	f := NewGitFetcher(nil)
	_, err := f.Fetch(context.Background(), repoWithRef(dummyGitRepositoryURL,
		sourcev1alpha1.GitRepositoryRefSpec{Semver: new("^1.0.0")}, sourcev1alpha1.GitRepositoryAuthTypeNone))
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}
