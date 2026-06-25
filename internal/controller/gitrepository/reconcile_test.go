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
	"errors"
	"testing"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
)

func gitRepo(generation int64, status sourcev1alpha1.GitRepositoryStatus) *sourcev1alpha1.GitRepository {
	return &sourcev1alpha1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "g1", Namespace: "ns", Generation: generation},
		Status:     status,
	}
}

func TestComputeStatus_Fetched(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC))
	repo := gitRepo(5, sourcev1alpha1.GitRepositoryStatus{})
	result := FetchResult{Revision: dummyGitRepositoryRevision, TreeHash: dummyGitRepositoryTreeHash, ResolvedRef: dummyGitRepositoryBranch}

	got := computeStatus(repo, result, nil, now)

	if got.ObservedGeneration != 5 {
		t.Errorf("ObservedGeneration = %d, want 5", got.ObservedGeneration)
	}
	if got.LastFetched.Revision != dummyGitRepositoryRevision || got.LastFetched.TreeHash != dummyGitRepositoryTreeHash || got.LastFetched.ResolvedRef != dummyGitRepositoryBranch {
		t.Errorf("LastFetched = %+v, want abc123/tree456/main", got.LastFetched)
	}
	if !got.LastFetched.FetchedAt.Equal(&now) {
		t.Errorf("FetchedAt = %v, want %v", got.LastFetched.FetchedAt, now)
	}
	cond := apimeta.FindStatusCondition(got.Conditions, conditionReady)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != conditionReasonFetched {
		t.Errorf("Ready condition = %+v, want True/Fetched", cond)
	}
}

func TestComputeStatus_FetchFailedRetainsLast(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC))
	earlier := metav1.NewTime(time.Date(2026, 6, 20, 11, 0, 0, 0, time.UTC))
	repo := gitRepo(3, sourcev1alpha1.GitRepositoryStatus{
		LastFetched: sourcev1alpha1.GitRepositoryLastFetchedStatus{Revision: "old", FetchedAt: earlier},
	})

	got := computeStatus(repo, FetchResult{}, errors.Join(ErrFetchFailed, errors.New("connection refused")), now)

	if got.LastFetched.Revision != "old" {
		t.Errorf("LastFetched.Revision = %q, want retained %q", got.LastFetched.Revision, "old")
	}
	if !got.LastFetched.FetchedAt.Equal(&earlier) {
		t.Errorf("FetchedAt = %v, want retained %v", got.LastFetched.FetchedAt, earlier)
	}
	cond := apimeta.FindStatusCondition(got.Conditions, conditionReady)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != conditionReasonFetchFailed {
		t.Errorf("Ready condition = %+v, want False/FetchFailed", cond)
	}
}

func TestComputeStatus_Unsupported(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC))

	got := computeStatus(gitRepo(1, sourcev1alpha1.GitRepositoryStatus{}), FetchResult{},
		errors.Join(ErrUnsupported, errors.New("semver not supported")), now)

	cond := apimeta.FindStatusCondition(got.Conditions, conditionReady)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != conditionReasonUnsupported {
		t.Errorf("Ready condition = %+v, want False/Unsupported", cond)
	}
}

func TestStatusChanged(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC))
	repo := gitRepo(2, sourcev1alpha1.GitRepositoryStatus{})
	result := FetchResult{Revision: dummyGitRepositoryRevision, TreeHash: dummyGitRepositoryTreeHash, ResolvedRef: dummyGitRepositoryBranch}

	first := computeStatus(repo, result, nil, now)
	if !statusChanged(repo, first) {
		t.Fatalf("statusChanged = false on first reconcile, want true")
	}

	settled := gitRepo(2, first)
	again := computeStatus(settled, result, nil, now)
	if statusChanged(settled, again) {
		t.Errorf("statusChanged = true for identical fetch, want false")
	}
}
