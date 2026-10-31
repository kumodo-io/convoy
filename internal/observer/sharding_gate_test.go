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
	"testing"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

const clusterProd = "prod"

func relWithCluster(clusterNs string) *corev1alpha1.Release {
	return &corev1alpha1.Release{
		Namespace: "team", Name: "rel",
		Spec: corev1alpha1.ReleaseSpec{
			ClusterRef: corev1alpha1.ReleaseClusterRefSpec{Namespace: clusterNs, Name: clusterProd},
		},
	}
}

func TestClusterKeyOf(t *testing.T) {
	ns, name := clusterKeyOf(relWithCluster("infra"))
	if ns != "infra" || name != clusterProd {
		t.Fatalf("explicit cluster ns: got %s/%s, want infra/prod", ns, name)
	}
	ns, name = clusterKeyOf(relWithCluster(""))
	if ns != "team" || name != clusterProd {
		t.Fatalf("defaulted cluster ns: got %s/%s, want team/prod", ns, name)
	}
}

func TestReleaseOwned(t *testing.T) {
	if !releaseOwned(nil, relWithCluster("infra")) {
		t.Error("nil owns must pass (sharding disabled)")
	}
	if !releaseOwned(func(string, string) bool { return false }, "not-a-release") {
		t.Error("non-Release object must pass")
	}

	var gotNs, gotName string
	got := releaseOwned(func(ns, name string) bool { gotNs, gotName = ns, name; return true },
		relWithCluster("infra"))
	if !got || gotNs != "infra" || gotName != clusterProd {
		t.Fatalf("owns called with %s/%s (ret %v), want infra/prod true", gotNs, gotName, got)
	}
	if releaseOwned(func(string, string) bool { return false }, relWithCluster("infra")) {
		t.Error("owns=false must reject the Release")
	}
}
