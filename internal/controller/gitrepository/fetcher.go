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

	"github.com/go-git/go-billy/v5/memfs"
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
}

// Fetcher resolves a GitRepository's tracked ref to a concrete revision. It is
// the single seam between the reconcile loop and real network I/O, so the
// controller can be unit-tested with a fake.
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
	gitOpts := gogit.CloneOptions{
		URL:           url,
		ReferenceName: refName,
		SingleBranch:  true,
		Depth:         f.depth,
		Auth:          authMethod,
		Tags:          gogit.NoTags,
	}
	if tlsOpts != nil {
		// todo: get config map containing CA bundle and pass it to CloneOptions.CABundle as []byte
		gitOpts.InsecureSkipTLS = tlsOpts.InsecureSkipVerify
		// gitOpts.CABundle = TBD
	}
	r, err := gogit.CloneContext(ctx, memory.NewStorage(), memfs.New(), new(gitOpts))
	if err != nil {
		return FetchResult{}, errors.Join(ErrFetchFailed, fmt.Errorf("cloning %s at %s: %w", url, refName, err))
	}
	head, err := r.Head()
	if err != nil {
		return FetchResult{}, errors.Join(ErrFetchFailed, fmt.Errorf("reading HEAD of %s: %w", url, err))
	}
	return commitResult(r, head.Hash(), resolved)
}

// cloneCommit clones the repository and resolves an explicit commit SHA.
func (f *gitFetcher) cloneCommit(
	ctx context.Context,
	url, commit string,
	authMethod transport.AuthMethod,
	tlsOpts *authv1.TLSSpec,
) (FetchResult, error) {
	gitOpts := gogit.CloneOptions{
		URL:  url,
		Auth: authMethod,
	}
	if tlsOpts != nil {
		// todo: get config map containing CA bundle and pass it to CloneOptions.CABundle as []byte
		gitOpts.InsecureSkipTLS = tlsOpts.InsecureSkipVerify
		// gitOpts.CABundle = TBD
	}
	r, err := gogit.CloneContext(ctx, memory.NewStorage(), memfs.New(), new(gitOpts))
	if err != nil {
		return FetchResult{}, errors.Join(ErrFetchFailed, fmt.Errorf("cloning %s: %w", url, err))
	}
	return commitResult(r, plumbing.NewHash(commit), commit)
}

func commitResult(r *gogit.Repository, hash plumbing.Hash, resolved string) (FetchResult, error) {
	c, err := r.CommitObject(hash)
	if err != nil {
		return FetchResult{}, errors.Join(ErrFetchFailed, fmt.Errorf("reading commit %s: %w", hash, err))
	}
	return FetchResult{
		Revision:    c.Hash.String(),
		TreeHash:    c.TreeHash.String(),
		ResolvedRef: resolved,
	}, nil
}
