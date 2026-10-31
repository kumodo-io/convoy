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

package controllermanager

import (
	"testing"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

func TestReleaseClusterIndexKeys(t *testing.T) {
	rel := &corev1alpha1.Release{
		Namespace: "team",
		Spec: corev1alpha1.ReleaseSpec{
			ClusterRef: corev1alpha1.ReleaseClusterRefSpec{Name: "prod", Namespace: "infra"},
		},
	}

	keys, err := releaseClusterIndexKeys(rel)
	if err != nil || len(keys) != 1 || keys[0] != "infra/prod" {
		t.Errorf("explicit cluster namespace: keys=%v err=%v, want [infra/prod]", keys, err)
	}

	// Cluster namespace defaults to the Release's own namespace.
	rel.Spec.ClusterRef.Namespace = ""
	keys, _ = releaseClusterIndexKeys(rel)
	if len(keys) != 1 || keys[0] != "team/prod" {
		t.Errorf("defaulted namespace: keys=%v, want [team/prod]", keys)
	}

	// Non-Release objects yield no keys.
	if keys, err := releaseClusterIndexKeys("not-a-release"); err != nil || keys != nil {
		t.Errorf("non-Release: keys=%v err=%v, want nil,nil", keys, err)
	}
}
