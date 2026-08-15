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

package sharding

import (
	"context"
	"fmt"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
)

func TestSharder_StableAndInRange(t *testing.T) {
	s := NewSharder(16)
	for i := 0; i < 1000; i++ {
		ns, name := fmt.Sprintf("ns-%d", i%7), fmt.Sprintf("rel-%d", i)
		a := s.Shard(ns, name)
		b := s.Shard(ns, name)
		if a != b {
			t.Fatalf("Shard not stable for %s/%s: %d != %d", ns, name, a, b)
		}
		if a < 0 || a >= 16 {
			t.Fatalf("shard %d out of range [0,16)", a)
		}
	}
}

func TestSharder_Distribution(t *testing.T) {
	s := NewSharder(8)
	counts := make([]int, 8)
	for i := 0; i < 8000; i++ {
		counts[s.Shard("prod", fmt.Sprintf("cluster-%d", i))]++
	}
	for shard, n := range counts {
		if n == 0 {
			t.Errorf("shard %d got no keys — poor distribution", shard)
		}
	}
}

func TestSharder_ClampsBelowOne(t *testing.T) {
	if got := NewSharder(0).Shards(); got != 1 {
		t.Fatalf("NewSharder(0).Shards() = %d, want 1", got)
	}
	if got := NewSharder(0).Shard("a", "b"); got != 0 {
		t.Fatalf("single-shard mapping = %d, want 0", got)
	}
}

func TestDesiredShards_StripedFairShare(t *testing.T) {
	// S=10, N=3: member 0 -> {0,3,6,9}, member 1 -> {1,4,7}, member 2 -> {2,5,8}.
	// Union covers all, no overlap, sizes differ by at most 1.
	const shards, n = 10, 3
	union := make(map[int]bool)
	sizes := make([]int, n)
	for idx := 0; idx < n; idx++ {
		d := desiredShards(idx, n, shards)
		sizes[idx] = len(d)
		for s := range d {
			if union[s] {
				t.Fatalf("shard %d desired by more than one member", s)
			}
			union[s] = true
			if s%n != idx {
				t.Fatalf("member %d desired shard %d (s%%n=%d)", idx, s, s%n)
			}
		}
	}
	if len(union) != shards {
		t.Fatalf("union covers %d shards, want %d", len(union), shards)
	}
	if sizes[0] != 4 || sizes[1] != 3 || sizes[2] != 3 {
		t.Fatalf("sizes = %v, want [4 3 3]", sizes)
	}
}

func TestDesiredShards_NoMembers(t *testing.T) {
	if d := desiredShards(0, 0, 8); len(d) != 0 {
		t.Fatalf("no members should desire nothing, got %v", d)
	}
	if d := desiredShards(5, 3, 8); len(d) != 0 {
		t.Fatalf("out-of-range index should desire nothing, got %v", d)
	}
}

// testClock is a manually-advanced clock shared by test coordinators.
type testClock struct{ t time.Time }

func (c *testClock) now() time.Time          { return c.t }
func (c *testClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestCoordinator(client *fake.Clientset, clk *testClock, id string, shards int) *Coordinator {
	c := NewCoordinator(client, Config{
		Component:     "test",
		ReplicaID:     id,
		Namespace:     "convoy-system",
		Shards:        shards,
		LeaseDuration: 30 * time.Second,
		RenewInterval: 10 * time.Second,
	})
	c.now = clk.now
	return c
}

func TestCoordinator_SingleReplicaOwnsAll(t *testing.T) {
	client := fake.NewSimpleClientset()
	clk := &testClock{t: time.Unix(1_700_000_000, 0)}
	c := newTestCoordinator(client, clk, "r1", 8)

	c.tick(context.Background())

	for i := 0; i < 8; i++ {
		if !c.Owns(i) {
			t.Fatalf("single replica must own shard %d", i)
		}
	}
	if got := len(c.OwnedShards()); got != 8 {
		t.Fatalf("OwnedShards len = %d, want 8", got)
	}
}

// converge runs alternating ticks until ownership stabilizes (release-then-acquire
// handoff needs a few rounds).
func converge(ctx context.Context, coords ...*Coordinator) {
	for round := 0; round < 6; round++ {
		for _, c := range coords {
			c.tick(ctx)
		}
	}
}

func TestCoordinator_TwoReplicasPartitionExactlyOnce(t *testing.T) {
	const shards = 8
	client := fake.NewSimpleClientset()
	clk := &testClock{t: time.Unix(1_700_000_000, 0)}
	c1 := newTestCoordinator(client, clk, "r1", shards)
	c2 := newTestCoordinator(client, clk, "r2", shards)

	converge(context.Background(), c1, c2)

	for i := 0; i < shards; i++ {
		o1, o2 := c1.Owns(i), c2.Owns(i)
		if o1 == o2 {
			t.Fatalf("shard %d owned by both=%v (must be exactly one)", i, o1 && o2)
		}
	}
	// Striped over sorted [r1,r2]: r1 owns even, r2 owns odd.
	if got := c1.OwnedShards(); fmt.Sprint(got) != "[0 2 4 6]" {
		t.Fatalf("r1 owns %v, want [0 2 4 6]", got)
	}
	if got := c2.OwnedShards(); fmt.Sprint(got) != "[1 3 5 7]" {
		t.Fatalf("r2 owns %v, want [1 3 5 7]", got)
	}
}

func TestCoordinator_FailoverReassignsDeadReplicasShards(t *testing.T) {
	const shards = 8
	client := fake.NewSimpleClientset()
	clk := &testClock{t: time.Unix(1_700_000_000, 0)}
	ctx := context.Background()
	c1 := newTestCoordinator(client, clk, "r1", shards)
	c2 := newTestCoordinator(client, clk, "r2", shards)

	converge(ctx, c1, c2)

	// r2 dies: it stops ticking and its leases go stale.
	clk.advance(60 * time.Second) // past the 30s lease duration
	for round := 0; round < 6; round++ {
		c1.tick(ctx) // only r1 keeps renewing
	}

	for i := 0; i < shards; i++ {
		if !c1.Owns(i) {
			t.Fatalf("after r2 death, r1 must own shard %d", i)
		}
	}
}

func TestCoordinator_OnChangeFires(t *testing.T) {
	client := fake.NewSimpleClientset()
	clk := &testClock{t: time.Unix(1_700_000_000, 0)}
	c := newTestCoordinator(client, clk, "r1", 4)

	var fired int
	c.OnChange(func() { fired++ })

	c.tick(context.Background()) // gains all 4 shards -> change
	if fired == 0 {
		t.Fatal("OnChange should fire when ownership first populates")
	}
	before := fired
	c.tick(context.Background()) // steady state -> no change
	if fired != before {
		t.Fatalf("OnChange fired on steady state (%d -> %d)", before, fired)
	}
}
