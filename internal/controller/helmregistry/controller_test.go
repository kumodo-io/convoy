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

package helmregistry

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
)

const (
	dummyName      = "dummy-helmregistry"
	dummyNamespace = "dummy-namespace"
	dummyURL       = "https://charts.example.com"
)

// fakeFetcher implements Fetcher. The verify-only controller exercises Verify.
type fakeFetcher struct {
	verifyErr error
}

func (f fakeFetcher) Fetch(context.Context, *sourcev1alpha1.HelmRegistry, Ref) (FetchResult, error) {
	return FetchResult{}, nil
}

func (f fakeFetcher) Verify(context.Context, *sourcev1alpha1.HelmRegistry) error { return f.verifyErr }

func noneReg() *sourcev1alpha1.HelmRegistry {
	return &sourcev1alpha1.HelmRegistry{
		Name:       dummyName,
		Namespace:  dummyNamespace,
		Generation: 1,
		Spec: sourcev1alpha1.HelmRegistrySpec{
			URL:  dummyURL,
			Auth: sourcev1alpha1.HelmRegistryAuthSpec{Type: sourcev1alpha1.HelmRegistryAuthTypeNone},
		},
	}
}

func newTestController(t *testing.T, reg *sourcev1alpha1.HelmRegistry, fetcher Fetcher) (*Controller, *fakeclient.Clientset, cache.Store) {
	t.Helper()
	client := fakeclient.NewSimpleClientset(reg)
	factory := informers.NewSharedInformerFactory(client, 0)
	informer := factory.Source().V1alpha1().HelmRegistries()
	store := informer.Informer().GetStore()
	if err := store.Add(reg); err != nil {
		t.Fatalf("seeding informer store: %v", err)
	}
	c := NewController(client, informer, fetcher)
	fixed := metav1.NewTime(time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC))
	c.now = func() metav1.Time { return fixed }
	return c, client, store
}

func statusUpdate(actions []clienttesting.Action) *sourcev1alpha1.HelmRegistry {
	for _, a := range actions {
		if a.Matches("update", "helmregistries") && a.GetSubresource() == "status" {
			return a.(clienttesting.UpdateAction).GetObject().(*sourcev1alpha1.HelmRegistry)
		}
	}
	return nil
}

func TestSyncHandler_VerifiedWritesStatus(t *testing.T) {
	c, client, _ := newTestController(t, noneReg(), fakeFetcher{})

	if err := c.syncHandler(context.Background(), fmt.Sprintf("%s/%s", dummyNamespace, dummyName)); err != nil {
		t.Fatalf("syncHandler returned error: %v", err)
	}

	got := statusUpdate(client.Actions())
	if got == nil {
		t.Fatalf("expected an UpdateStatus action, got none; actions=%v", client.Actions())
	}
	cond := apimeta.FindStatusCondition(got.Status.Conditions, conditionReady)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != conditionReasonVerified {
		t.Errorf("Ready condition = %+v, want True/Verified", cond)
	}
}

func TestSyncHandler_UnsupportedDoesNotRequeue(t *testing.T) {
	c, client, _ := newTestController(t, noneReg(),
		fakeFetcher{verifyErr: errors.Join(ErrUnsupported, errors.New("basic auth not supported"))})

	if err := c.syncHandler(context.Background(), fmt.Sprintf("%s/%s", dummyNamespace, dummyName)); err != nil {
		t.Fatalf("syncHandler returned error for unsupported registry: %v", err)
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

func TestSyncHandler_VerifyErrorRequeues(t *testing.T) {
	c, _, _ := newTestController(t, noneReg(), fakeFetcher{verifyErr: errors.New("connection refused")})

	if err := c.syncHandler(context.Background(), fmt.Sprintf("%s/%s", dummyNamespace, dummyName)); err == nil {
		t.Fatalf("syncHandler returned nil, want error so the queue retries")
	}
}

func TestSyncHandler_NoOpWhenStatusUnchanged(t *testing.T) {
	c, client, store := newTestController(t, noneReg(), fakeFetcher{})

	if err := c.syncHandler(context.Background(), fmt.Sprintf("%s/%s", dummyNamespace, dummyName)); err != nil {
		t.Fatalf("first syncHandler: %v", err)
	}
	first := statusUpdate(client.Actions())
	if first == nil {
		t.Fatalf("expected status write on first sync")
	}

	settled := noneReg()
	settled.Status = first.Status
	if err := store.Update(settled); err != nil {
		t.Fatalf("updating informer store: %v", err)
	}
	client.ClearActions()

	if err := c.syncHandler(context.Background(), fmt.Sprintf("%s/%s", dummyNamespace, dummyName)); err != nil {
		t.Fatalf("second syncHandler: %v", err)
	}
	if got := statusUpdate(client.Actions()); got != nil {
		t.Errorf("unexpected second status write: %+v", got.Status)
	}
}
