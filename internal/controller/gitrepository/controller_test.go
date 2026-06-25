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
	"testing"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	fakeclient "github.com/kumodo-io/convoy/api/generated/clientset/versioned/fake"
	informers "github.com/kumodo-io/convoy/api/generated/informers/externalversions"
	sourcev1alpha1 "github.com/kumodo-io/convoy/api/source/v1alpha1"
	"github.com/kumodo-io/convoy/internal/artifact"
)

const (
	dummyGitRepositoryName      = "dummy-gitrepository"
	dummyGitRepositoryNamespace = "dummy-namespace"
	dummyGitRepositoryURL       = "https://dummy.example.com/service.git"
	dummyGitRepositoryBranch    = "main"
	dummyGitRepositoryRevision  = "abc123"
	dummyGitRepositoryTreeHash  = "tree123"
)

type fakeFetcher struct {
	result FetchResult
	err    error
}

func (f fakeFetcher) Fetch(context.Context, *sourcev1alpha1.GitRepository) (FetchResult, error) {
	return f.result, f.err
}

func noneRepo() *sourcev1alpha1.GitRepository {
	return &sourcev1alpha1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:       dummyGitRepositoryName,
			Namespace:  dummyGitRepositoryNamespace,
			Generation: 1,
		},
		Spec: sourcev1alpha1.GitRepositorySpec{
			URL:  dummyGitRepositoryURL,
			Ref:  sourcev1alpha1.GitRepositoryRefSpec{Branch: new(dummyGitRepositoryBranch)},
			Auth: sourcev1alpha1.GitRepositoryAuthSpec{Type: sourcev1alpha1.GitRepositoryAuthTypeNone},
		},
	}
}

func newTestController(t *testing.T, repo *sourcev1alpha1.GitRepository, fetcher Fetcher) (*Controller, *fakeclient.Clientset, cache.Store, artifact.Store) {
	t.Helper()
	client := fakeclient.NewSimpleClientset(repo)
	factory := informers.NewSharedInformerFactory(client, 0)
	informer := factory.Source().V1alpha1().GitRepositories()
	store := informer.Informer().GetStore()
	if err := store.Add(repo); err != nil {
		t.Fatalf("seeding informer store: %v", err)
	}
	artifactStore := artifact.NewMemoryStore()
	c := NewController(client, informer, fetcher, artifactStore)
	fixed := metav1.NewTime(time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC))
	c.now = func() metav1.Time { return fixed }
	return c, client, store, artifactStore
}

func statusUpdate(actions []clienttesting.Action) *sourcev1alpha1.GitRepository {
	for _, a := range actions {
		if a.Matches("update", "gitrepositories") && a.GetSubresource() == "status" {
			return a.(clienttesting.UpdateAction).GetObject().(*sourcev1alpha1.GitRepository)
		}
	}
	return nil
}

func TestSyncHandler_FetchedWritesStatus(t *testing.T) {
	c, client, _, artStore := newTestController(t, noneRepo(),
		fakeFetcher{result: FetchResult{
			Revision:    dummyGitRepositoryRevision,
			TreeHash:    "tree456",
			ResolvedRef: dummyGitRepositoryBranch,
			Files:       map[string][]byte{"app.yaml": []byte("kind: ConfigMap")},
		}})

	if err := c.syncHandler(context.Background(), fmt.Sprintf("%s/%s", dummyGitRepositoryNamespace, dummyGitRepositoryName)); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}

	got := statusUpdate(client.Actions())
	if got == nil {
		t.Fatalf("expected an UpdateStatus action, got none; actions=%v", client.Actions())
	}
	if got.Status.LastFetched.Revision != dummyGitRepositoryRevision {
		t.Errorf("persisted status = %+v, want revision %s", got.Status.LastFetched, dummyGitRepositoryRevision)
	}

	// The fetched content must be published to the artifact store.
	art, ok := artStore.Get(artifact.Key("GitRepository", dummyGitRepositoryNamespace, dummyGitRepositoryName))
	if !ok || art.Revision != dummyGitRepositoryRevision || string(art.Files["app.yaml"]) != "kind: ConfigMap" {
		t.Errorf("artifact store = %+v, ok=%v, want revision %s + app.yaml", art, ok, dummyGitRepositoryRevision)
	}
}

func TestSyncHandler_UnsupportedDoesNotRequeue(t *testing.T) {
	c, client, _, _ := newTestController(t, noneRepo(),
		fakeFetcher{err: errors.Join(ErrUnsupported, errors.New("semver not supported"))})

	if err := c.syncHandler(context.Background(), fmt.Sprintf("%s/%s", dummyGitRepositoryNamespace, dummyGitRepositoryName)); err != nil {
		t.Fatalf("syncHandler returned error for unsupported repo: %v", err)
	}

	got := statusUpdate(client.Actions())
	if got == nil {
		t.Fatalf("expected an UpdateStatus action, got none")
	}
	cond := apimeta.FindStatusCondition(got.Status.Conditions, conditionReady)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != conditionReasonUnsupported {
		t.Errorf("Ready condition = %+v, want False/Unsupported", cond)
	}
}

func TestSyncHandler_FetchErrorRequeues(t *testing.T) {
	c, _, _, _ := newTestController(t, noneRepo(),
		fakeFetcher{err: errors.New("connection refused")})

	if err := c.syncHandler(context.Background(), fmt.Sprintf("%s/%s", dummyGitRepositoryNamespace, dummyGitRepositoryName)); err == nil {
		t.Fatalf("syncHandler returned nil, want error so the queue retries")
	}
}

func TestSyncHandler_NoOpWhenStatusUnchanged(t *testing.T) {
	fetcher := fakeFetcher{result: FetchResult{Revision: dummyGitRepositoryRevision, TreeHash: "tree456", ResolvedRef: dummyGitRepositoryBranch}}
	c, client, store, _ := newTestController(t, noneRepo(), fetcher)

	if err := c.syncHandler(context.Background(), fmt.Sprintf("%s/%s", dummyGitRepositoryNamespace, dummyGitRepositoryName)); err != nil {
		t.Fatalf("first syncHandler: %v", err)
	}
	first := statusUpdate(client.Actions())
	if first == nil {
		t.Fatalf("expected status write on first sync")
	}

	settled := noneRepo()
	settled.Status = first.Status
	if err := store.Update(settled); err != nil {
		t.Fatalf("updating informer store: %v", err)
	}
	client.ClearActions()

	if err := c.syncHandler(context.Background(), fmt.Sprintf("%s/%s", dummyGitRepositoryNamespace, dummyGitRepositoryName)); err != nil {
		t.Fatalf("second syncHandler: %v", err)
	}
	if got := statusUpdate(client.Actions()); got != nil {
		t.Errorf("unexpected second status write: %+v", got.Status)
	}
}
