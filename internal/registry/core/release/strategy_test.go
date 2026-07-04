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

package release_test

import (
	"testing"

	"github.com/kumodo-io/convoy/api/core"
	releasestorage "github.com/kumodo-io/convoy/internal/registry/core/release"
)

func TestGetAttrs_ExposesClusterRefFields(t *testing.T) {
	rel := &core.Release{
		Spec: core.ReleaseSpec{
			ClusterRef: core.ReleaseClusterRefSpec{Name: dummyClusterRef, Namespace: "infra"},
		},
	}

	_, flds, err := releasestorage.GetAttrs(rel)
	if err != nil {
		t.Fatalf("GetAttrs returned error: %v", err)
	}
	if got := flds["spec.clusterRef.name"]; got != dummyClusterRef {
		t.Errorf("spec.clusterRef.name = %q, want prod-eu", got)
	}
	if got := flds["spec.clusterRef.namespace"]; got != "infra" {
		t.Errorf("spec.clusterRef.namespace = %q, want infra", got)
	}
}

func TestGetAttrs_RejectsNonRelease(t *testing.T) {
	if _, _, err := releasestorage.GetAttrs(&core.Cluster{}); err == nil {
		t.Errorf("GetAttrs returned nil error for a non-Release object")
	}
}
