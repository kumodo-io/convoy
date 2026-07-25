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

package inventory

import (
	"context"
	"testing"
)

const (
	kindDeployment = "Deployment"
	kindConfigMap  = "ConfigMap"
	groupApps      = "apps"
	nameAPI        = "api"
)

func TestMemoryStore_PutGetDelete(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	if _, ok, _ := s.Get(ctx, "ns", "r"); ok {
		t.Errorf("empty store returned ok for missing key")
	}

	refs := []Ref{{Version: "v1", Kind: kindConfigMap, Namespace: "ns", Name: "a"}}
	if err := s.Put(ctx, "ns", "r", refs); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, ok, err := s.Get(ctx, "ns", "r")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || len(got) != 1 || got[0].Name != "a" {
		t.Errorf("Get after Put = %+v, ok=%v; want [a]", got, ok)
	}

	if err := s.Delete(ctx, "ns", "r"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, _ := s.Get(ctx, "ns", "r"); ok {
		t.Errorf("Get after Delete returned ok")
	}
}

func TestDiff_IgnoresVersion(t *testing.T) {
	old := []Ref{
		{Group: groupApps, Version: "v1", Kind: kindDeployment, Namespace: "x", Name: "a"},
		{Version: "v1", Kind: kindConfigMap, Namespace: "x", Name: "gone"},
	}
	current := []Ref{
		// Same Deployment, apiVersion "bumped" — must NOT count as an orphan.
		{Group: groupApps, Version: "v1beta1", Kind: kindDeployment, Namespace: "x", Name: "a"},
	}

	orphans := Diff(old, current)
	if len(orphans) != 1 || orphans[0].Name != "gone" {
		t.Errorf("orphans = %+v, want only [gone] (Deployment retained despite version change)", orphans)
	}
}
