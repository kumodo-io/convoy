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

package cluster

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kumodo-io/convoy/api/core"
	"github.com/kumodo-io/convoy/internal/sharding"
)

func TestStrategy_StampsShardFromSelf(t *testing.T) {
	s := NewStrategy(nil, 8)
	c := &core.Cluster{ObjectMeta: metav1.ObjectMeta{Namespace: "infra", Name: "prod"}}
	s.PrepareForCreate(context.Background(), c)

	want := sharding.NewSharder(8).ShardLabelValue("infra", "prod")
	if got := c.Labels[sharding.ShardLabel]; got != want {
		t.Fatalf("shard label = %q, want %q", got, want)
	}
}
