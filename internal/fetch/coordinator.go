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

// Package fetch coordinates fetching source content on behalf of Releases. The
// Release controller asks the Coordinator to Ensure that the content for a
// (source, ref) is available; the Coordinator resolves the source, fetches by
// ref, and caches the result in the content-addressed artifact store.
//
// Coordinator is an interface so the in-process implementation here can later be
// swapped for an out-of-process transport (gRPC/bus) without touching callers.
// The current implementation is synchronous; an async work-queue/dedup variant
// is a follow-up that preserves the same interface.
package fetch

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	sourcelisters "github.com/kumodo-io/convoy/api/generated/listers/source/v1alpha1"
	"github.com/kumodo-io/convoy/internal/artifact"
	"github.com/kumodo-io/convoy/internal/controller/gitrepository"
)

// Result reports the outcome of an Ensure call.
type Result struct {
	// Ready is true when the artifact is available.
	Ready bool
	// Revision is the resolved source revision (set when Ready).
	Revision string
	// Artifact is the fetched content (set when Ready).
	Artifact artifact.Artifact
	// Message describes a not-ready outcome (for the consumer's status).
	Message string
}

// Coordinator ensures source content is fetched and available.
type Coordinator interface {
	// Ensure makes the content for the given GitRepository source + ref available.
	// A non-nil error means a transient failure (caller should requeue). A nil
	// error with Ready=false means a terminal configuration problem (do not
	// requeue). Ready=true carries the resolved revision and artifact.
	Ensure(ctx context.Context, src *corev1alpha1.ReleaseGitRepositorySpec, namespace string) (Result, error)
}

// coordinator is the in-process, synchronous implementation.
type coordinator struct {
	repos   sourcelisters.GitRepositoryLister
	fetcher gitrepository.Fetcher
	store   artifact.Store
}

// NewCoordinator builds the in-process coordinator.
func NewCoordinator(repos sourcelisters.GitRepositoryLister, fetcher gitrepository.Fetcher, store artifact.Store) Coordinator {
	return &coordinator{repos: repos, fetcher: fetcher, store: store}
}

func (c *coordinator) Ensure(ctx context.Context, src *corev1alpha1.ReleaseGitRepositorySpec, namespace string) (Result, error) {
	if src == nil {
		return Result{Message: "gitRepository source is not set"}, nil // terminal
	}
	ns := src.Namespace
	if ns == "" {
		ns = namespace
	}

	repo, err := c.repos.GitRepositories(ns).Get(src.Name)
	if apierrors.IsNotFound(err) {
		// Source may be created later — transient.
		return Result{Message: fmt.Sprintf("GitRepository %s/%s not found", ns, src.Name)}, fmt.Errorf("source GitRepository %s/%s not found", ns, src.Name)
	}
	if err != nil {
		return Result{Message: err.Error()}, err
	}
	if src.Ref == nil {
		return Result{Message: "source ref is not set"}, nil // terminal
	}

	// Immutable commit refs are content-addressable up front: serve from the
	// store if already fetched. Branch/tag/semver must re-resolve.
	if src.Ref.Commit != nil {
		if a, ok := c.store.Get(artifact.Key(repo.Spec.URL, *src.Ref.Commit)); ok {
			return Result{Ready: true, Revision: a.Revision, Artifact: a}, nil
		}
	}

	fr, err := c.fetcher.Fetch(ctx, repo, toFetcherRef(src.Ref))
	if err != nil {
		if errors.Is(err, gitrepository.ErrUnsupported) {
			return Result{Message: err.Error()}, nil // terminal
		}
		return Result{Message: err.Error()}, err // transient (fetch failed)
	}

	a := artifact.Artifact{Revision: fr.Revision, Files: fr.Files}
	c.store.Put(artifact.Key(repo.Spec.URL, fr.Revision), a)
	return Result{Ready: true, Revision: fr.Revision, Artifact: a}, nil
}

func toFetcherRef(r *corev1alpha1.ReleaseGitRepositoryRefSpec) gitrepository.Ref {
	if r == nil {
		return gitrepository.Ref{}
	}
	return gitrepository.Ref{Branch: r.Branch, Tag: r.Tag, Commit: r.Commit, Semver: r.Semver}
}
