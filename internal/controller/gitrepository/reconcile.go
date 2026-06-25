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
	"fmt"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
)

const (
	conditionReady             = "Ready"
	conditionReasonFetched     = "Fetched"
	conditionReasonUnsupported = "Unsupported"
	conditionReasonFetchFailed = "FetchFailed"
)

// computeStatus derives the desired GitRepositoryStatus from the current object
// plus the outcome of a fetch. It is pure (no I/O, no clocks beyond the injected
// now) so it can be exhaustively unit-tested. On a fetch error the previously
// fetched facts are intentionally retained.
func computeStatus(repo *sourcev1alpha1.GitRepository, result FetchResult, fetchErr error, now metav1.Time) sourcev1alpha1.GitRepositoryStatus {
	status := *repo.Status.DeepCopy()
	status.ObservedGeneration = repo.Generation

	cond := metav1.Condition{
		Type:               conditionReady,
		ObservedGeneration: repo.Generation,
		LastTransitionTime: now,
	}

	if fetchErr != nil {
		cond.Status = metav1.ConditionFalse
		cond.Message = fetchErr.Error()
		switch {
		case errors.Is(fetchErr, ErrUnsupported):
			cond.Reason = conditionReasonUnsupported
		case errors.Is(fetchErr, ErrFetchFailed):
			cond.Reason = conditionReasonFetchFailed
		default:
			cond.Reason = conditionReasonFetchFailed
		}
		// Last-known LastFetched is retained on failure.
	} else {
		// Only stamp FetchedAt when the resolved revision actually changes, so a
		// steady unchanged repo produces no status writes between commits.
		if status.LastFetched.Revision != result.Revision {
			status.LastFetched.FetchedAt = now
		}
		status.LastFetched.Revision = result.Revision
		status.LastFetched.TreeHash = result.TreeHash
		status.LastFetched.ResolvedRef = result.ResolvedRef
		cond.Status = metav1.ConditionTrue
		cond.Reason = conditionReasonFetched
		cond.Message = fmt.Sprintf("fetched revision %s", result.Revision)
	}

	apimeta.SetStatusCondition(&status.Conditions, cond)
	return status
}

// statusChanged reports whether newStatus differs from the repo's current status
// in a way that warrants a status write.
func statusChanged(repo *sourcev1alpha1.GitRepository, newStatus sourcev1alpha1.GitRepositoryStatus) bool {
	return !apiequality.Semantic.DeepEqual(repo.Status, newStatus)
}
