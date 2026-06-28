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
	"github.com/kumodo-io/convoy/internal/controller/helmregistry"
	"github.com/kumodo-io/convoy/internal/controller/ociregistry"
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
	// Ensure makes the content for the given source available, dispatching on
	// src.Type to the matching fetch path. A non-nil error means a transient
	// failure (caller should requeue). A nil error with Ready=false means a
	// terminal configuration problem (do not requeue), e.g. an unsupported
	// source type. Ready=true carries the resolved revision and artifact.
	Ensure(ctx context.Context, src corev1alpha1.ReleaseSourceSpec, namespace string) (Result, error)
}

// Deps are the listers, fetchers, and store the coordinator dispatches across.
// One pair (lister + fetcher) per supported source type.
type Deps struct {
	Store          artifact.Store
	GitRepos       sourcelisters.GitRepositoryLister
	GitFetcher     gitrepository.Fetcher
	HelmRegistries sourcelisters.HelmRegistryLister
	HelmFetcher    helmregistry.Fetcher
	OCIRegistries  sourcelisters.OCIRegistryLister
	OCIFetcher     ociregistry.Fetcher
}

// coordinator is the in-process, synchronous implementation.
type coordinator struct {
	store          artifact.Store
	gitRepos       sourcelisters.GitRepositoryLister
	gitFetcher     gitrepository.Fetcher
	helmRegistries sourcelisters.HelmRegistryLister
	helmFetcher    helmregistry.Fetcher
	ociRegistries  sourcelisters.OCIRegistryLister
	ociFetcher     ociregistry.Fetcher
}

// NewCoordinator builds the in-process coordinator from its dependencies.
func NewCoordinator(deps Deps) Coordinator {
	return &coordinator{
		store:          deps.Store,
		gitRepos:       deps.GitRepos,
		gitFetcher:     deps.GitFetcher,
		helmRegistries: deps.HelmRegistries,
		helmFetcher:    deps.HelmFetcher,
		ociRegistries:  deps.OCIRegistries,
		ociFetcher:     deps.OCIFetcher,
	}
}

func (c *coordinator) Ensure(ctx context.Context, src corev1alpha1.ReleaseSourceSpec, namespace string) (Result, error) {
	switch src.Type {
	case corev1alpha1.ReleaseSourceTypeGitRepository:
		return c.ensureGitRepository(ctx, src.GitRepository, namespace)
	case corev1alpha1.ReleaseSourceTypeHelmRegistry:
		return c.ensureHelmRegistry(ctx, src.HelmRegistry, namespace)
	case corev1alpha1.ReleaseSourceTypeOCIRegistry:
		return c.ensureOCIRegistry(ctx, src.OCIRegistry, namespace)
	default:
		return Result{Message: fmt.Sprintf("source type %q is not supported yet", src.Type)}, nil // terminal
	}
}

func (c *coordinator) ensureGitRepository(ctx context.Context, src *corev1alpha1.ReleaseGitRepositorySpec, namespace string) (Result, error) {
	if src == nil {
		return Result{Message: "gitRepository source is not set"}, nil // terminal
	}
	ns := src.Namespace
	if ns == "" {
		ns = namespace
	}

	repo, err := c.gitRepos.GitRepositories(ns).Get(src.Name)
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

	fr, err := c.gitFetcher.Fetch(ctx, repo, toFetcherRef(src.Ref))
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

func (c *coordinator) ensureHelmRegistry(ctx context.Context, src *corev1alpha1.ReleaseHelmRegistrySpec, namespace string) (Result, error) {
	if src == nil {
		return Result{Message: "helmRegistry source is not set"}, nil // terminal
	}
	if src.Chart == nil || src.Chart.Name == "" || src.Chart.Version == "" {
		return Result{Message: "helmRegistry chart name and version are required"}, nil // terminal
	}
	ns := src.Namespace
	if ns == "" {
		ns = namespace
	}

	reg, err := c.helmRegistries.HelmRegistries(ns).Get(src.Name)
	if apierrors.IsNotFound(err) {
		// Source may be created later — transient.
		return Result{Message: fmt.Sprintf("HelmRegistry %s/%s not found", ns, src.Name)}, fmt.Errorf("source HelmRegistry %s/%s not found", ns, src.Name)
	}
	if err != nil {
		return Result{Message: err.Error()}, err
	}

	// Chart versions are immutable, so content is addressable up front: serve from
	// the store if already fetched.
	revision := src.Chart.Name + "-" + src.Chart.Version
	if a, ok := c.store.Get(artifact.Key(reg.Spec.URL, revision)); ok {
		return Result{Ready: true, Revision: a.Revision, Artifact: a}, nil
	}

	fr, err := c.helmFetcher.Fetch(ctx, reg, helmregistry.Ref{Name: src.Chart.Name, Version: src.Chart.Version})
	if err != nil {
		if errors.Is(err, helmregistry.ErrUnsupported) {
			return Result{Message: err.Error()}, nil // terminal
		}
		return Result{Message: err.Error()}, err // transient (fetch failed)
	}

	a := artifact.Artifact{Revision: fr.Revision, Files: fr.Files}
	c.store.Put(artifact.Key(reg.Spec.URL, fr.Revision), a)
	return Result{Ready: true, Revision: fr.Revision, Artifact: a}, nil
}

func (c *coordinator) ensureOCIRegistry(ctx context.Context, src *corev1alpha1.ReleaseOCIRegistrySpec, namespace string) (Result, error) {
	if src == nil {
		return Result{Message: "ociRegistry source is not set"}, nil // terminal
	}
	if src.Artifact == nil || src.Artifact.Name == "" || src.Artifact.Version == "" {
		return Result{Message: "ociRegistry artifact name and version are required"}, nil // terminal
	}
	ns := src.Namespace
	if ns == "" {
		ns = namespace
	}

	reg, err := c.ociRegistries.OCIRegistries(ns).Get(src.Name)
	if apierrors.IsNotFound(err) {
		// Source may be created later — transient.
		return Result{Message: fmt.Sprintf("OCIRegistry %s/%s not found", ns, src.Name)}, fmt.Errorf("source OCIRegistry %s/%s not found", ns, src.Name)
	}
	if err != nil {
		return Result{Message: err.Error()}, err
	}

	// Artifact versions are immutable, so content is addressable up front: serve
	// from the store if already fetched.
	revision := src.Artifact.Name + "-" + src.Artifact.Version
	if a, ok := c.store.Get(artifact.Key(reg.Spec.URL, revision)); ok {
		return Result{Ready: true, Revision: a.Revision, Artifact: a}, nil
	}

	fr, err := c.ociFetcher.Fetch(ctx, reg, ociregistry.Ref{Name: src.Artifact.Name, Version: src.Artifact.Version})
	if err != nil {
		if errors.Is(err, ociregistry.ErrUnsupported) {
			return Result{Message: err.Error()}, nil // terminal
		}
		return Result{Message: err.Error()}, err // transient (fetch failed)
	}

	a := artifact.Artifact{Revision: fr.Revision, Files: fr.Files}
	c.store.Put(artifact.Key(reg.Spec.URL, fr.Revision), a)
	return Result{Ready: true, Revision: fr.Revision, Artifact: a}, nil
}

func toFetcherRef(r *corev1alpha1.ReleaseGitRepositoryRefSpec) gitrepository.Ref {
	if r == nil {
		return gitrepository.Ref{}
	}
	return gitrepository.Ref{Branch: r.Branch, Tag: r.Tag, Commit: r.Commit, Semver: r.Semver}
}
