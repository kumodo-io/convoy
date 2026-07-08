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

package observer

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clienttesting "k8s.io/client-go/testing"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	fakeclient "github.com/kumodo-io/convoy/api/generated/clientset/versioned/fake"
	informers "github.com/kumodo-io/convoy/api/generated/informers/externalversions"
)

func testRelease(automatic bool) *corev1alpha1.Release {
	return &corev1alpha1.Release{
		ObjectMeta: metav1.ObjectMeta{Name: "r1", Namespace: "ns", Generation: 1},
		Spec: corev1alpha1.ReleaseSpec{
			SyncPolicy: corev1alpha1.ReleaseSyncPolicySpec{Automatic: automatic},
		},
	}
}

func newTestController(t *testing.T, rel *corev1alpha1.Release) (*Controller, *fakeclient.Clientset) {
	t.Helper()
	client := fakeclient.NewSimpleClientset(rel)
	factory := informers.NewSharedInformerFactory(client, 0)
	relInformer := factory.Core().V1alpha1().Releases()
	clInformer := factory.Core().V1alpha1().Clusters()
	if err := relInformer.Informer().GetStore().Add(rel); err != nil {
		t.Fatalf("seeding release store: %v", err)
	}
	c := NewController(client, relInformer, clInformer.Lister(), nil, nil, nil)
	fixed := metav1.NewTime(time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC))
	c.now = func() metav1.Time { return fixed }
	return c, client
}

// inSyncCondition decodes the InSync condition from the observer's status apply.
func inSyncCondition(actions []clienttesting.Action) *metav1.Condition {
	var found *metav1.Condition
	for _, a := range actions {
		p, ok := a.(clienttesting.PatchAction)
		if !ok || !a.Matches("patch", "releases") || a.GetSubresource() != "status" || p.GetPatchType() != types.ApplyPatchType {
			continue
		}
		rel := &corev1alpha1.Release{}
		if err := json.Unmarshal(p.GetPatch(), rel); err != nil {
			return nil
		}
		if c := apimeta.FindStatusCondition(rel.Status.Conditions, conditionInSync); c != nil {
			found = c
		}
	}
	return found
}

func TestSyncHandler_NoDrift(t *testing.T) {
	c, client := newTestController(t, testRelease(true))
	c.assessFn = func(context.Context, *corev1alpha1.Release) (bool, error) { return false, nil }
	synced := false
	c.syncFn = func(context.Context, *corev1alpha1.Release) error { synced = true; return nil }

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	cond := inSyncCondition(client.Actions())
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != reasonInSync {
		t.Errorf("InSync = %+v, want True/InSync", cond)
	}
	if synced {
		t.Error("requested sync despite no drift")
	}
}

func TestSyncHandler_DriftAutomaticFiresSync(t *testing.T) {
	c, client := newTestController(t, testRelease(true))
	c.assessFn = func(context.Context, *corev1alpha1.Release) (bool, error) { return true, nil }
	synced := false
	c.syncFn = func(context.Context, *corev1alpha1.Release) error { synced = true; return nil }

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	cond := inSyncCondition(client.Actions())
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != reasonDrifted {
		t.Errorf("InSync = %+v, want False/Drifted", cond)
	}
	if !synced {
		t.Error("did not request sync on drift in automatic mode")
	}
}

func TestSyncHandler_DriftManualDoesNotFireSync(t *testing.T) {
	c, client := newTestController(t, testRelease(false)) // manual
	c.assessFn = func(context.Context, *corev1alpha1.Release) (bool, error) { return true, nil }
	synced := false
	c.syncFn = func(context.Context, *corev1alpha1.Release) error { synced = true; return nil }

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	cond := inSyncCondition(client.Actions())
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != reasonDrifted {
		t.Errorf("InSync = %+v, want False/Drifted", cond)
	}
	if synced {
		t.Error("requested sync in manual mode — must not auto-correct drift")
	}
}

func TestSyncHandler_AssessErrorMarksUnknown(t *testing.T) {
	c, client := newTestController(t, testRelease(true))
	c.assessFn = func(context.Context, *corev1alpha1.Release) (bool, error) {
		return false, errors.New("cluster unreachable")
	}
	synced := false
	c.syncFn = func(context.Context, *corev1alpha1.Release) error { synced = true; return nil }

	// Transient assess error is returned for retry, after marking Unknown.
	if err := c.syncHandler(context.Background(), "ns/r1"); err == nil {
		t.Fatal("syncHandler returned nil, want the transient error for retry")
	}
	cond := inSyncCondition(client.Actions())
	if cond == nil || cond.Status != metav1.ConditionUnknown || cond.Reason != reasonUndetermined {
		t.Errorf("InSync = %+v, want Unknown/Undetermined", cond)
	}
	if synced {
		t.Error("requested sync despite undetermined drift")
	}
}
