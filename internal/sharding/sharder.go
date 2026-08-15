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

// Package sharding partitions controller work across replicas. A pure Sharder
// maps a work identity (a namespace/name) into a fixed shard space, and a
// lease-based Coordinator (see coordinator.go) tracks which of those shards the
// local replica currently owns. Controllers gate their reconcile on ownership so
// each shard's work runs on exactly one replica, with lease-driven failover.
package sharding

import (
	"hash/fnv"
)

// Sharder maps any work identity into a fixed shard space [0, S). The mapping is
// pure and stable: the same identity always yields the same shard for a given S,
// so every replica agrees on where a Release/Cluster/Source belongs.
type Sharder struct {
	shards int
}

// NewSharder returns a Sharder over shards buckets. A shards value below 1 is
// clamped to 1 (a single shard — the unsharded degenerate case).
func NewSharder(shards int) Sharder {
	if shards < 1 {
		shards = 1
	}
	return Sharder{shards: shards}
}

// Shards is the fixed shard count this Sharder partitions into.
func (s Sharder) Shards() int {
	return s.shards
}

// Shard returns the shard index in [0, S) for the given namespace/name identity.
// Keys are joined as "namespace/name" and hashed with FNV-1a.
func (s Sharder) Shard(namespace, name string) int {
	h := fnv.New32a()
	// Writing to an fnv hash never errors.
	_, _ = h.Write([]byte(namespace))
	_, _ = h.Write([]byte{'/'})
	_, _ = h.Write([]byte(name))
	return int(h.Sum32() % uint32(s.shards)) //nolint:gosec // modulo by positive shards is in-range
}

// desiredShards returns the set of shards the member at sorted index myIndex (of
// n live members) should own, using striped fair-share assignment: member k owns
// every shard i where i mod n == k. This spreads shards evenly and is
// deterministic, so two members never target the same shard. With n <= 0 (no
// known members) nothing is desired.
func desiredShards(myIndex, n, shards int) map[int]bool {
	desired := make(map[int]bool)
	if n <= 0 || myIndex < 0 || myIndex >= n {
		return desired
	}
	for i := myIndex; i < shards; i += n {
		desired[i] = true
	}
	return desired
}
