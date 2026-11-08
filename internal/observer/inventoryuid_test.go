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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	clienttesting "k8s.io/client-go/testing"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

const invUID types.UID = "inv-uid"

var invResource = schema.GroupResource{Group: corev1alpha1.SchemeGroupVersion.Group, Resource: "releaseinventories"}

func seededInventory() *corev1alpha1.ReleaseInventory {
	return &corev1alpha1.ReleaseInventory{Name: "r1", Namespace: "ns", UID: invUID}
}

func appliedUID(t *testing.T, actions []clienttesting.Action) types.UID {
	t.Helper()
	for _, a := range actions {
		p, ok := a.(clienttesting.PatchAction)
		if !ok || p.GetPatchType() != types.ApplyPatchType {
			continue
		}
		var body struct {
			Metadata struct {
				UID types.UID `json:"uid"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(p.GetPatch(), &body); err != nil {
			t.Fatalf("decoding apply: %v", err)
		}
		return body.Metadata.UID
	}
	t.Fatal("no apply sent")
	return ""
}

func TestInventoryWrites_SendUIDPrecondition(t *testing.T) {
	rel := &corev1alpha1.Release{Name: "r1", Namespace: "ns"}
	for _, tc := range []struct {
		name  string
		write func(*HealthController) error
	}{
		{name: "health", write: func(c *HealthController) error {
			return c.writeInventoryHealth(context.Background(), rel, invUID,
				[]healthEntry{deploymentEntry(depName, corev1alpha1.ResourceStateHealthy, "", time.Time{})})
		}},
		{name: "hook status", write: func(c *HealthController) error {
			return c.writeHookStatus(context.Background(), rel, invUID,
				[]hookStatusEntry{{name: "migrate", phase: corev1alpha1.LifecycleHookPhasePreSync, state: corev1alpha1.LifecycleHookStateRunning}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, client := inventoryHealthController(t, seededInventory())
			if err := tc.write(c); err != nil {
				t.Fatalf("write: %v", err)
			}
			if got := appliedUID(t, client.Actions()); got != invUID {
				t.Errorf("apply uid = %q, want %q", got, invUID)
			}
		})
	}
}

func TestUIDRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "deleted", err: apierrors.NewConflict(invResource, "r1", errors.New("uid mismatch")), want: true},
		{name: "replaced", err: apierrors.NewInvalid(corev1alpha1.SchemeGroupVersion.WithKind("ReleaseInventory").GroupKind(), "r1",
			field.ErrorList{field.Invalid(field.NewPath("metadata", "uid"), "x", "field is immutable")}), want: true},
		{name: "other invalid field", err: apierrors.NewInvalid(corev1alpha1.SchemeGroupVersion.WithKind("ReleaseInventory").GroupKind(), "r1",
			field.ErrorList{field.Invalid(field.NewPath("metadata", "name"), "x", "bad")})},
		{name: "not found", err: apierrors.NewNotFound(invResource, "r1")},
		{name: "other", err: errors.New("boom")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := uidRejected(tc.err); got != tc.want {
				t.Errorf("uidRejected = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWriteInventoryHealth_UIDRejectedIsGone(t *testing.T) {
	c, client := inventoryHealthController(t, seededInventory())
	client.PrependReactor("patch", "releaseinventories", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewConflict(invResource, "r1", errors.New("uid mismatch"))
	})
	err := c.writeInventoryHealth(context.Background(), &corev1alpha1.Release{Name: "r1", Namespace: "ns"}, invUID, nil)
	if !errors.Is(err, errInventoryGone) {
		t.Errorf("err = %v, want errInventoryGone", err)
	}
}

func TestHealth_InventoryGoneSkipsHealthWrite(t *testing.T) {
	c, client := newTestHealthController(t, withAssessment(testRelease(true)))
	c.assessFn = func(context.Context, *corev1alpha1.Release) (*assessment, error) {
		return nil, errInventoryGone
	}

	if err := c.syncHandler(context.Background(), "ns/r1"); err != nil {
		t.Fatalf("syncHandler: %v", err)
	}
	if cond := healthCondition(client.Actions()); cond != nil {
		t.Errorf("Healthy = %+v, want no write while the inventory is gone", cond)
	}
}
