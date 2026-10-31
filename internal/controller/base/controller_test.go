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

package base

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestProcessNextItem_ForgetsOnSuccess(t *testing.T) {
	var got string
	c := NewController("test", func(_ context.Context, key string) error {
		got = key
		return nil
	})
	c.queue.Add("ns/a")

	if !c.processNextItem(context.Background()) {
		t.Fatal("processNextItem returned false, want true")
	}
	if got != "ns/a" {
		t.Errorf("reconcile got key %q, want ns/a", got)
	}
	if n := c.queue.NumRequeues("ns/a"); n != 0 {
		t.Errorf("NumRequeues = %d after success, want 0 (forgotten)", n)
	}
}

func TestProcessNextItem_RetriesOnError(t *testing.T) {
	c := NewController("test", func(_ context.Context, _ string) error {
		return errors.New("boom")
	})
	c.queue.Add("ns/a")

	if !c.processNextItem(context.Background()) {
		t.Fatal("processNextItem returned false, want true")
	}
	if n := c.queue.NumRequeues("ns/a"); n != 1 {
		t.Errorf("NumRequeues = %d after error, want 1 (rate-limited retry)", n)
	}
}

func TestShouldEnqueueOnUpdate(t *testing.T) {
	const reconcileAnnotation = "convoy.kumodo.io/sync-requested-at"
	obj := func(gen int64, rv string, annotations map[string]string) *metav1.PartialObjectMetadata {
		return &metav1.PartialObjectMetadata{Generation: gen, ResourceVersion: rv, Annotations: annotations}
	}
	stamp := map[string]string{reconcileAnnotation: "2026-07-06T00:00:00Z"}
	cases := []struct {
		name       string
		annotation string // reconcile-request annotation the controller watches ("" = none)
		old, new   *metav1.PartialObjectMetadata
		want       bool
	}{
		{"resync (identical RV)", "", obj(1, "100", nil), obj(1, "100", nil), true},
		{"spec change (generation bumped)", "", obj(1, "100", nil), obj(2, "101", nil), true},
		{"status-only (gen same, RV changed)", "", obj(1, "100", nil), obj(1, "101", nil), false},
		{"reconcile-request stamped (watched)", reconcileAnnotation, obj(1, "100", nil), obj(1, "101", stamp), true},
		{"unrelated annotation change (watched)", reconcileAnnotation, obj(1, "100", nil), obj(1, "101", map[string]string{"other": "x"}), false},
		{"reconcile-request stamped (not watched)", "", obj(1, "100", nil), obj(1, "101", stamp), false},
	}
	for _, c := range cases {
		var opts []Option
		if c.annotation != "" {
			opts = append(opts, WithReconcileRequestAnnotation(c.annotation))
		}
		ctrl := NewController("test", func(context.Context, string) error { return nil }, opts...)
		if got := ctrl.shouldEnqueueOnUpdate(c.old, c.new); got != c.want {
			t.Errorf("%s: shouldEnqueueOnUpdate = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestEnqueueAfter_NonPositiveEnqueuesImmediately(t *testing.T) {
	c := NewController("test", func(context.Context, string) error { return nil })

	// A non-positive delay delegates to Add, so the key is immediately queued.
	c.EnqueueAfter("ns/a", 0)
	if n := c.queue.Len(); n != 1 {
		t.Errorf("queue.Len() = %d after EnqueueAfter(_, 0), want 1", n)
	}
}

func partialMeta(name string) *metav1.PartialObjectMetadata {
	return &metav1.PartialObjectMetadata{Namespace: "ns", Name: name}
}

func TestFilteredEnqueue_DropsUnownedKeepsOwned(t *testing.T) {
	c := NewController("test",
		func(_ context.Context, _ string) error { return nil },
		WithEnqueueFilter(func(o any) bool {
			return o.(*metav1.PartialObjectMetadata).Name == "keep"
		}),
	)

	c.filteredEnqueue(partialMeta("drop"))
	if got := c.queue.Len(); got != 0 {
		t.Fatalf("unowned object enqueued: queue len = %d, want 0", got)
	}
	c.filteredEnqueue(partialMeta("keep"))
	if got := c.queue.Len(); got != 1 {
		t.Fatalf("owned object not enqueued: queue len = %d, want 1", got)
	}
}

func TestFilteredEnqueue_NoFilterEnqueuesAll(t *testing.T) {
	c := NewController("test", func(_ context.Context, _ string) error { return nil })
	c.filteredEnqueue(partialMeta("anything"))
	if got := c.queue.Len(); got != 1 {
		t.Fatalf("no filter should enqueue: queue len = %d, want 1", got)
	}
}

func TestEnqueueKeys(t *testing.T) {
	c := NewController("test", func(_ context.Context, _ string) error { return nil })
	c.EnqueueKeys([]string{"ns/a", "ns/b"})
	if got := c.queue.Len(); got != 2 {
		t.Fatalf("EnqueueKeys: queue len = %d, want 2", got)
	}
}

func TestWithShardGate_GatesReconcileAndEnqueue(t *testing.T) {
	owns := func(_, name string) bool { return name == "keep" }
	var reconciled []string
	c := NewController("test",
		func(_ context.Context, key string) error { reconciled = append(reconciled, key); return nil },
		WithShardGate(owns),
	)

	// Reconcile gate: unowned key is skipped, owned key runs.
	if err := c.reconcile(context.Background(), "ns/drop"); err != nil {
		t.Fatalf("unowned reconcile err = %v", err)
	}
	if err := c.reconcile(context.Background(), "ns/keep"); err != nil {
		t.Fatalf("owned reconcile err = %v", err)
	}
	if len(reconciled) != 1 || reconciled[0] != "ns/keep" {
		t.Fatalf("reconciled = %v, want [ns/keep]", reconciled)
	}

	// Enqueue filter installed by the gate.
	c.filteredEnqueue(partialMeta("drop"))
	c.filteredEnqueue(partialMeta("keep"))
	if got := c.queue.Len(); got != 1 {
		t.Fatalf("gate enqueue filter: queue len = %d, want 1", got)
	}
}

func TestWithShardGate_NilOwnsIsNoop(t *testing.T) {
	var ran bool
	c := NewController("test",
		func(_ context.Context, _ string) error { ran = true; return nil },
		WithShardGate(nil),
	)
	if err := c.reconcile(context.Background(), "ns/x"); err != nil || !ran {
		t.Fatalf("nil gate should pass through: err=%v ran=%v", err, ran)
	}
}
