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

package release

import (
	"context"
	"testing"

	"github.com/kumodo-io/convoy/api/core"
	"github.com/kumodo-io/convoy/internal/sharding"
)

func TestStrategy_StampsShardFromCluster(t *testing.T) {
	s := NewStrategy(nil, 8)
	rel := &core.Release{
		Namespace: "team", Name: "app",
		Spec: core.ReleaseSpec{ClusterRef: core.ReleaseClusterRefSpec{Name: "prod"}},
	}
	s.PrepareForCreate(context.Background(), rel)

	// Cluster namespace defaults to the Release's own namespace.
	want := sharding.NewSharder(8).ShardLabelValue("team", "prod")
	if got := rel.Labels[sharding.ShardLabel]; got != want {
		t.Fatalf("create: shard label = %q, want %q", got, want)
	}

	// A user tampering with the label is overwritten on update.
	rel.Labels[sharding.ShardLabel] = "999"
	old := rel.DeepCopy()
	s.PrepareForUpdate(context.Background(), rel, old)
	if got := rel.Labels[sharding.ShardLabel]; got != want {
		t.Fatalf("update: shard label = %q, want %q (should overwrite tampering)", got, want)
	}
}
