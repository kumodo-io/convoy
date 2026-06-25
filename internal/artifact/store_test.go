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

package artifact

import "testing"

func TestMemoryStore_PutGetOverwrite(t *testing.T) {
	s := NewMemoryStore()
	key := Key("GitRepository", "ns", "repo")

	if _, ok := s.Get(key); ok {
		t.Fatalf("Get on empty store returned ok=true")
	}

	s.Put(key, Artifact{Revision: "r1", Files: map[string][]byte{"a.yaml": []byte("1")}})
	got, ok := s.Get(key)
	if !ok || got.Revision != "r1" || string(got.Files["a.yaml"]) != "1" {
		t.Fatalf("Get after Put = %+v, ok=%v", got, ok)
	}

	s.Put(key, Artifact{Revision: "r2"})
	if got, _ := s.Get(key); got.Revision != "r2" {
		t.Errorf("Revision after overwrite = %q, want r2", got.Revision)
	}
}

func TestKey(t *testing.T) {
	if k := Key("GitRepository", "ns", "repo"); k != "GitRepository/ns/repo" {
		t.Errorf("Key = %q, want GitRepository/ns/repo", k)
	}
}
