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

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

const (
	kindDeployment  = "Deployment"
	kindConfigMap   = "ConfigMap"
	groupApps       = "apps"
	nameAPI         = "api"
	nameCfg         = "cfg"
	caseIdentical   = "identical"
	hookNameMigrate = "migrate"
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

func TestMemoryStore_PutGetHooks_PreservesObserverFields(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	// Controller records identity.
	if err := s.PutHooks(ctx, "ns", "r", []HookRef{{Name: hookNameMigrate, Phase: corev1alpha1.LifecycleHookPhasePreSync, WorkloadName: "w1", RenderRef: "sha-a"}}); err != nil {
		t.Fatalf("PutHooks: %v", err)
	}
	got, err := s.GetHooks(ctx, "ns", "r")
	if err != nil || len(got) != 1 || got[0].WorkloadName != "w1" {
		t.Fatalf("GetHooks after Put = %+v (err %v), want one entry w1", got, err)
	}

	// Observer writes runtime state (simulated by mutating the stored item in place,
	// as the durable store's separate field manager would).
	ms := s.(*memoryStore)
	state := corev1alpha1.LifecycleHookStateSucceeded
	ms.hooks["ns/r"][0].State = &state
	ms.hooks["ns/r"][0].Message = "done"

	// Controller re-asserts identity (new render) — observer fields must survive.
	if err := s.PutHooks(ctx, "ns", "r", []HookRef{{Name: hookNameMigrate, Phase: corev1alpha1.LifecycleHookPhasePreSync, WorkloadName: "w2", RenderRef: "sha-b"}}); err != nil {
		t.Fatalf("PutHooks re-arm: %v", err)
	}
	got, _ = s.GetHooks(ctx, "ns", "r")
	if len(got) != 1 || got[0].WorkloadName != "w2" || got[0].RenderRef != "sha-b" {
		t.Errorf("re-arm identity = %+v, want w2/sha-b", got)
	}
	if got[0].State == nil || *got[0].State != state || got[0].Message != "done" {
		t.Errorf("observer fields not preserved across re-arm: state=%v message=%q", got[0].State, got[0].Message)
	}

	// Delete clears hooks too.
	if err := s.Delete(ctx, "ns", "r"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got, _ := s.GetHooks(ctx, "ns", "r"); len(got) != 0 {
		t.Errorf("GetHooks after Delete = %+v, want empty", got)
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
