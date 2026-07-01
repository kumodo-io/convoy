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

import "testing"

func TestMemoryStore_PutGetDelete(t *testing.T) {
	s := NewMemoryStore()

	if _, ok := s.Get("ns/r"); ok {
		t.Errorf("empty store returned ok for missing key")
	}

	refs := []Ref{{Version: "v1", Kind: "ConfigMap", Namespace: "ns", Name: "a"}}
	s.Put("ns/r", refs)

	got, ok := s.Get("ns/r")
	if !ok || len(got) != 1 || got[0].Name != "a" {
		t.Errorf("Get after Put = %+v, ok=%v; want [a]", got, ok)
	}

	s.Delete("ns/r")
	if _, ok := s.Get("ns/r"); ok {
		t.Errorf("Get after Delete returned ok")
	}
}

func TestDiff_IgnoresVersion(t *testing.T) {
	old := []Ref{
		{Group: "apps", Version: "v1", Kind: "Deployment", Namespace: "x", Name: "a"},
		{Version: "v1", Kind: "ConfigMap", Namespace: "x", Name: "gone"},
	}
	current := []Ref{
		// Same Deployment, apiVersion "bumped" — must NOT count as an orphan.
		{Group: "apps", Version: "v1beta1", Kind: "Deployment", Namespace: "x", Name: "a"},
	}

	orphans := Diff(old, current)
	if len(orphans) != 1 || orphans[0].Name != "gone" {
		t.Errorf("orphans = %+v, want only [gone] (Deployment retained despite version change)", orphans)
	}
}
