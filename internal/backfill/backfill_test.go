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

package backfill

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	fakeclient "github.com/kumodo-io/convoy/api/generated/clientset/versioned/fake"
	"github.com/kumodo-io/convoy/internal/sharding"
)

const clusterName = "prod"

func TestRun_StampsMissingLabels(t *testing.T) {
	ctx := context.Background()
	const ns = "team"
	cluster := &corev1alpha1.Cluster{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: clusterName}}
	rel := &corev1alpha1.Release{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "app"},
		Spec:       corev1alpha1.ReleaseSpec{ClusterRef: corev1alpha1.ReleaseClusterRefSpec{Name: clusterName}},
	}
	inv := &corev1alpha1.ReleaseInventory{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "app"}}
	client := fakeclient.NewSimpleClientset(cluster, rel, inv)
	sharder := sharding.NewSharder(8)

	if err := Run(ctx, client, sharder); err != nil {
		t.Fatalf("Run: %v", err)
	}

	gotCluster, _ := client.CoreV1alpha1().Clusters(ns).Get(ctx, clusterName, metav1.GetOptions{})
	if want := sharder.ShardLabelValue(ns, clusterName); gotCluster.Labels[sharding.ShardLabel] != want {
		t.Errorf("cluster shard = %q, want %q", gotCluster.Labels[sharding.ShardLabel], want)
	}
	// Release + its inventory share the cluster shard.
	wantRel := sharder.ShardLabelValue(ns, clusterName)
	gotRel, _ := client.CoreV1alpha1().Releases(ns).Get(ctx, "app", metav1.GetOptions{})
	if gotRel.Labels[sharding.ShardLabel] != wantRel {
		t.Errorf("release shard = %q, want %q", gotRel.Labels[sharding.ShardLabel], wantRel)
	}
	gotInv, _ := client.CoreV1alpha1().ReleaseInventories(ns).Get(ctx, "app", metav1.GetOptions{})
	if gotInv.Labels[sharding.ShardLabel] != wantRel {
		t.Errorf("inventory shard = %q, want %q (its Release's)", gotInv.Labels[sharding.ShardLabel], wantRel)
	}
}

func TestRun_Idempotent(t *testing.T) {
	ctx := context.Background()
	sharder := sharding.NewSharder(8)
	cluster := &corev1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: clusterName, Labels: map[string]string{sharding.ShardLabel: sharder.ShardLabelValue("team", "prod")}},
	}
	client := fakeclient.NewSimpleClientset(cluster)
	if err := Run(ctx, client, sharder); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// No patch action should have been issued for the already-labeled cluster.
	for _, a := range client.Actions() {
		if a.GetVerb() == "patch" {
			t.Errorf("unexpected patch on already-labeled object: %v", a)
		}
	}
}
