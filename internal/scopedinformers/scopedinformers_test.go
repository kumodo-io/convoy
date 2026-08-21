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

package scopedinformers

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	fakeclient "github.com/kumodo-io/convoy/api/generated/clientset/versioned/fake"
	"github.com/kumodo-io/convoy/internal/sharding"
)

const testNamespace = "default"

func release(name, shard string) *corev1alpha1.Release {
	r := &corev1alpha1.Release{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
	}
	if shard != "" {
		r.Labels = map[string]string{sharding.ShardLabel: shard}
	}
	return r
}

// TestSelector covers the three selector modes and asserts each string is
// well-formed and parseable, since a malformed selector would silently break the
// real watch. (Actual filtering is exercised in TestRebuildScopesAndReplaysHandler,
// which the fake clientset honours.)
func TestSelector(t *testing.T) {
	full := New(nil, 0, false)
	if got := full.selector([]int{0, 2}); got != "" {
		t.Errorf("full-cache selector = %q, want empty", got)
	}

	scoped := New(nil, 0, true)
	if got := scoped.selector(nil); got != sharding.ShardLabel+" in (none)" {
		t.Errorf("empty-owned selector = %q, want never-match sentinel", got)
	}
	got := scoped.selector([]int{0, 2, 5})
	if want := sharding.ShardLabel + " in (0,2,5)"; got != want {
		t.Errorf("scoped selector = %q, want %q", got, want)
	}
	for _, sel := range []string{scoped.selector(nil), got} {
		if _, err := labels.Parse(sel); err != nil {
			t.Errorf("selector %q does not parse: %v", sel, err)
		}
	}
}

// TestFullCacheListsAndReplaysHandler builds a Set in full-cache mode, primes it,
// and asserts the lister reads the seeded objects and a handler registered before
// the build receives events — including for objects created after the build.
func TestFullCacheListsAndReplaysHandler(t *testing.T) {
	client := fakeclient.NewSimpleClientset(release("app-a", "0"), release("app-b", "1"))
	ctx := t.Context()

	set := New(client, 0, false)

	var adds int64
	if _, err := set.Releases().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(any) { atomic.AddInt64(&adds, 1) },
	}); err != nil {
		t.Fatalf("AddEventHandler: %v", err)
	}

	if err := set.Rebuild(ctx, []int{0, 1}); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	rels, err := set.Releases().Lister().List(labels.Everything())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rels) != 2 {
		t.Fatalf("lister returned %d releases, want 2", len(rels))
	}
	waitFor(t, func() bool { return atomic.LoadInt64(&adds) == 2 }, "handler to see 2 initial adds")

	// An object created after the build must reach the same (replayed) handler and
	// the lister.
	if _, err := client.CoreV1alpha1().Releases(testNamespace).
		Create(ctx, release("app-c", "0"), metav1.CreateOptions{}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitFor(t, func() bool { return atomic.LoadInt64(&adds) == 3 }, "handler to see the post-build add")
}

// TestRebuildScopesAndReplaysHandler is the core guarantee: switching the owned
// shard set re-points the cache (drops objects for shards no longer owned, picks
// up objects for newly-owned shards) without dropping the handler registered
// before any build. It relies on the fake clientset honouring the LIST label
// selector, so it exercises real shard filtering.
func TestRebuildScopesAndReplaysHandler(t *testing.T) {
	client := fakeclient.NewSimpleClientset(release("app-a", "0"), release("app-b", "1"))
	ctx := t.Context()

	set := New(client, 0, true)

	seen := &nameSet{m: map[string]struct{}{}}
	if _, err := set.Releases().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) { seen.add(obj.(*corev1alpha1.Release).Name) },
	}); err != nil {
		t.Fatalf("AddEventHandler: %v", err)
	}

	// Own shard 0: the cache holds only app-a.
	if err := set.Rebuild(ctx, []int{0}); err != nil {
		t.Fatalf("first Rebuild: %v", err)
	}
	assertNames(t, set, "app-a")
	waitFor(t, func() bool { return seen.has("app-a") }, "handler to see app-a")

	// A no-op rebuild (unchanged selector) must not rebuild.
	if err := set.Rebuild(ctx, []int{0}); err != nil {
		t.Fatalf("no-op Rebuild: %v", err)
	}

	// Switch to shard 1: the swapped-in backing drops app-a and holds only app-b,
	// and the replayed handler sees app-b.
	if err := set.Rebuild(ctx, []int{1}); err != nil {
		t.Fatalf("second Rebuild: %v", err)
	}
	assertNames(t, set, "app-b")
	waitFor(t, func() bool { return seen.has("app-b") }, "replayed handler to see app-b on the new backing")
}

// TestListerBeforeRebuild asserts a lister read before the first Rebuild returns
// empty rather than panicking on a nil backing.
func TestListerBeforeRebuild(t *testing.T) {
	set := New(fakeclient.NewSimpleClientset(), 0, true)
	rels, err := set.Releases().Lister().List(labels.Everything())
	if err != nil {
		t.Fatalf("List before Rebuild: %v", err)
	}
	if len(rels) != 0 {
		t.Fatalf("List before Rebuild returned %d, want 0", len(rels))
	}
	if set.Clusters().Informer().HasSynced() {
		t.Errorf("HasSynced true before Rebuild")
	}
}

// assertNames asserts the Release lister returns exactly the given names.
func assertNames(t *testing.T, set *Set, want ...string) {
	t.Helper()
	rels, err := set.Releases().Lister().List(labels.Everything())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := map[string]struct{}{}
	for _, r := range rels {
		got[r.Name] = struct{}{}
	}
	if len(got) != len(want) {
		t.Fatalf("lister returned %v, want %v", keys(got), want)
	}
	for _, n := range want {
		if _, ok := got[n]; !ok {
			t.Fatalf("lister returned %v, want %v", keys(got), want)
		}
	}
}

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// nameSet is a concurrency-safe set of object names seen by an event handler.
type nameSet struct {
	mu sync.Mutex
	m  map[string]struct{}
}

func (s *nameSet) add(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[name] = struct{}{}
}

func (s *nameSet) has(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.m[name]
	return ok
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
