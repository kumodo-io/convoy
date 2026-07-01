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
	obj := func(gen int64, rv string) *metav1.PartialObjectMetadata {
		return &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Generation: gen, ResourceVersion: rv}}
	}
	cases := []struct {
		name     string
		old, new *metav1.PartialObjectMetadata
		want     bool
	}{
		{"resync (identical RV)", obj(1, "100"), obj(1, "100"), true},
		{"spec change (generation bumped)", obj(1, "100"), obj(2, "101"), true},
		{"status-only (gen same, RV changed)", obj(1, "100"), obj(1, "101"), false},
	}
	for _, c := range cases {
		if got := shouldEnqueueOnUpdate(c.old, c.new); got != c.want {
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
