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
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-billy/v5/util"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/memory"

	authv1 "github.com/kumodo-io/convoy/api/auth/v1alpha1"
	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
	"github.com/kumodo-io/convoy/internal/auth"
)

// FetchResult is what a Fetcher observes for a GitRepository at its tracked ref.
type FetchResult struct {
	// Revision is the resolved commit SHA.
	Revision string
	// TreeHash is the commit's tree SHA.
	TreeHash string
	// ResolvedRef is the human-readable ref that was resolved (branch/tag/commit).
	ResolvedRef string
	// Files is a snapshot of the checked-out worktree (repo-relative path → content).
	Files map[string][]byte
}

// Fetcher resolves a GitRepository's tracked ref to a concrete revision and
// snapshots its content. It is the single seam between the reconcile loop and
// real network I/O, so the controller can be unit-tested with a fake.
type Fetcher interface {
	Fetch(ctx context.Context, repo *sourcev1alpha1.GitRepository) (FetchResult, error)
}

var (
	// ErrUnsupported marks a fetch rejected because the GitRepository requests an
	// auth type or ref kind this build does not implement yet. Terminal: reconcile
	// surfaces it as a non-Ready condition without requeuing.
	ErrUnsupported = errors.New("unsupported git repository configuration")
	// ErrFetchFailed marks a transient failure to reach or read the repository.
	ErrFetchFailed = errors.New("git fetch failed")
)

// gitFetcher fetches over the network with go-git. It currently supports only
// the "none" auth type (anonymous, public repos); other types return
// ErrUnsupported. The clone path already threads a transport.AuthMethod, so a
// future Authenticator registry plugs in without touching the fetch logic.
type gitFetcher struct {
	// depth bounds clone history; 1 (shallow) by default. Exposed so tests can
	// clone a local repo with full history when shallow isn't supported there.
	depth int

	resolver auth.SecretResolver
}

// NewGitFetcher returns a Fetcher that clones public repositories anonymously.
func NewGitFetcher(resolver auth.SecretResolver) Fetcher {
	return &gitFetcher{
		depth:    1,
		resolver: resolver,
	}
}

func (f *gitFetcher) Fetch(ctx context.Context, repo *sourcev1alpha1.GitRepository) (FetchResult, error) {
	if repo.Spec.Auth.Type != sourcev1alpha1.GitRepositoryAuthTypeNone {
		return FetchResult{}, errors.Join(ErrUnsupported, fmt.Errorf("auth type %q is not supported yet (only %q)", repo.Spec.Auth.Type, sourcev1alpha1.GitRepositoryAuthTypeNone))
	}
	// auth "none" → anonymous (nil transport auth).
	return f.clone(ctx, repo, nil)
}

func (f *gitFetcher) clone(ctx context.Context, repo *sourcev1alpha1.GitRepository, authMethod transport.AuthMethod) (FetchResult, error) {
	ref := repo.Spec.Ref
	switch {
	case ref.Branch != nil:
		return f.cloneRef(ctx, repo.Spec.URL, plumbing.NewBranchReferenceName(*ref.Branch), *ref.Branch, authMethod, repo.Spec.TLS)
	case ref.Tag != nil:
		return f.cloneRef(ctx, repo.Spec.URL, plumbing.NewTagReferenceName(*ref.Tag), *ref.Tag, authMethod, repo.Spec.TLS)
	case ref.Commit != nil:
		return f.cloneCommit(ctx, repo.Spec.URL, *ref.Commit, authMethod, repo.Spec.TLS)
	case ref.Semver != nil:
		return FetchResult{}, errors.Join(ErrUnsupported, fmt.Errorf("semver ref resolution is not supported yet"))
	default:
		return FetchResult{}, errors.Join(ErrUnsupported, fmt.Errorf("no ref specified"))
	}
}

// cloneRef shallow-clones a single branch or tag and reads its HEAD commit.
func (f *gitFetcher) cloneRef(
	ctx context.Context,
	url string,
	refName plumbing.ReferenceName,
	resolved string,
	authMethod transport.AuthMethod,
	tlsOpts *authv1.TLSSpec,
) (FetchResult, error) {
	worktree := memfs.New()
	gitOpts := gogit.CloneOptions{
		URL:           url,
		ReferenceName: refName,
		SingleBranch:  true,
		Depth:         f.depth,
		Auth:          authMethod,
		Tags:          gogit.NoTags,
	}
	applyTLS(&gitOpts, tlsOpts)

	r, err := gogit.CloneContext(ctx, memory.NewStorage(), worktree, new(gitOpts))
	if err != nil {
		return FetchResult{}, errors.Join(ErrFetchFailed, fmt.Errorf("cloning %s at %s: %w", url, refName, err))
	}
	head, err := r.Head()
	if err != nil {
		return FetchResult{}, errors.Join(ErrFetchFailed, fmt.Errorf("reading HEAD of %s: %w", url, err))
	}
	return result(r, head.Hash(), resolved, worktree)
}

// cloneCommit clones the repository, checks out an explicit commit SHA, and
// snapshots the worktree at that commit.
func (f *gitFetcher) cloneCommit(
	ctx context.Context,
	url, commit string,
	authMethod transport.AuthMethod,
	tlsOpts *authv1.TLSSpec,
) (FetchResult, error) {
	worktree := memfs.New()
	gitOpts := gogit.CloneOptions{
		URL:  url,
		Auth: authMethod,
	}
	applyTLS(&gitOpts, tlsOpts)

	r, err := gogit.CloneContext(ctx, memory.NewStorage(), worktree, new(gitOpts))
	if err != nil {
		return FetchResult{}, errors.Join(ErrFetchFailed, fmt.Errorf("cloning %s: %w", url, err))
	}
	wt, err := r.Worktree()
	if err != nil {
		return FetchResult{}, errors.Join(ErrFetchFailed, fmt.Errorf("opening worktree of %s: %w", url, err))
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{Hash: plumbing.NewHash(commit)}); err != nil {
		return FetchResult{}, errors.Join(ErrFetchFailed, fmt.Errorf("checking out commit %s: %w", commit, err))
	}
	return result(r, plumbing.NewHash(commit), commit, worktree)
}

// applyTLS folds the GitRepository's TLS settings into the clone options.
func applyTLS(opts *gogit.CloneOptions, tlsOpts *authv1.TLSSpec) {
	if tlsOpts == nil {
		return
	}
	// todo: resolve the CA ConfigMap and pass it as opts.CABundle ([]byte).
	opts.InsecureSkipTLS = tlsOpts.InsecureSkipVerify
}

// result reads the commit metadata and snapshots the worktree.
func result(r *gogit.Repository, hash plumbing.Hash, resolved string, worktree billy.Filesystem) (FetchResult, error) {
	c, err := r.CommitObject(hash)
	if err != nil {
		return FetchResult{}, errors.Join(ErrFetchFailed, fmt.Errorf("reading commit %s: %w", hash, err))
	}
	files, err := snapshotWorktree(worktree)
	if err != nil {
		return FetchResult{}, errors.Join(ErrFetchFailed, fmt.Errorf("snapshotting worktree: %w", err))
	}
	return FetchResult{
		Revision:    c.Hash.String(),
		TreeHash:    c.TreeHash.String(),
		ResolvedRef: resolved,
		Files:       files,
	}, nil
}

// snapshotWorktree reads every file in the checked-out worktree into a map keyed
// by repo-relative path.
func snapshotWorktree(fsys billy.Filesystem) (map[string][]byte, error) {
	files := make(map[string][]byte)
	err := util.Walk(fsys, "/", func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		file, err := fsys.Open(p)
		if err != nil {
			return err
		}
		defer func() {
			_ = file.Close()
		}()
		data, err := io.ReadAll(file)
		if err != nil {
			return err
		}
		files[strings.TrimPrefix(p, "/")] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
