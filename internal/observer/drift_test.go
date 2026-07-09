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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kumodo-io/convoy/internal/diffresult"
)

func cm(data map[string]any, mutate func(m map[string]any)) *unstructured.Unstructured {
	m := map[string]any{
		"apiVersion":      "v1",
		"kind":            "ConfigMap",
		metadataFieldName: map[string]any{"name": "cm", "namespace": "ns"},
		"data":            data,
	}
	if mutate != nil {
		mutate(m)
	}
	return &unstructured.Unstructured{Object: m}
}

// diff is a small helper: normalize both sides and compute the field changes.
func diff(t *testing.T, live, desired *unstructured.Unstructured) []diffresult.FieldChange {
	t.Helper()
	changes, err := fieldChanges(normalize(live), normalize(desired))
	if err != nil {
		t.Fatalf("fieldChanges error: %v", err)
	}
	return changes
}

func TestFieldChanges_ServerNoiseIgnored(t *testing.T) {
	live := cm(map[string]any{"k": "v"}, func(m map[string]any) {
		meta := m[metadataFieldName].(map[string]any)
		meta["resourceVersion"] = "12345"
		meta["uid"] = "abc-def"
		meta["generation"] = int64(7)
		meta["creationTimestamp"] = "2026-07-08T00:00:00Z"
		meta["managedFields"] = []any{map[string]any{"manager": "convoy"}}
		m["status"] = map[string]any{"observedGeneration": int64(7)}
	})
	// The dry-run result carries none of that server noise but the same data.
	desired := cm(map[string]any{"k": "v"}, nil)

	if changes := diff(t, live, desired); len(changes) != 0 {
		t.Errorf("expected no changes after stripping server noise, got %+v", changes)
	}
}

func TestFieldChanges_ReplaceCarriesLiveAndDesired(t *testing.T) {
	live := cm(map[string]any{"k": "old"}, func(m map[string]any) {
		m[metadataFieldName].(map[string]any)["resourceVersion"] = "12345"
	})
	desired := cm(map[string]any{"k": "new"}, nil)

	changes := diff(t, live, desired)
	if len(changes) != 1 {
		t.Fatalf("got %d changes, want 1: %+v", len(changes), changes)
	}
	c := changes[0]
	if c.Op != "replace" || c.Path != "/data/k" || c.Live != "old" || c.Desired != "new" {
		t.Errorf("change = %+v, want replace /data/k old->new", c)
	}
}

func TestFieldChanges_AddCarriesDesiredOnly(t *testing.T) {
	live := cm(map[string]any{"k": "v"}, nil)
	desired := cm(map[string]any{"k": "v", "added": "x"}, nil)

	changes := diff(t, live, desired)
	if len(changes) != 1 {
		t.Fatalf("got %d changes, want 1: %+v", len(changes), changes)
	}
	c := changes[0]
	if c.Op != "add" || c.Path != "/data/added" || c.Desired != "x" || c.Live != nil {
		t.Errorf("change = %+v, want add /data/added desired=x live=nil", c)
	}
}

func TestResolvePointer(t *testing.T) {
	root := map[string]any{
		"spec": map[string]any{
			"replicas": float64(3),
			"ports":    []any{map[string]any{"port": float64(80)}},
		},
	}
	if v, ok := resolvePointer(root, "/spec/replicas"); !ok || v != float64(3) {
		t.Errorf("resolvePointer(/spec/replicas) = %v,%v want 3,true", v, ok)
	}
	if v, ok := resolvePointer(root, "/spec/ports/0/port"); !ok || v != float64(80) {
		t.Errorf("resolvePointer(/spec/ports/0/port) = %v,%v want 80,true", v, ok)
	}
	if _, ok := resolvePointer(root, "/spec/missing"); ok {
		t.Error("resolvePointer(/spec/missing) = true, want false")
	}
	if _, ok := resolvePointer(root, "/spec/ports/9/port"); ok {
		t.Error("resolvePointer(out-of-range index) = true, want false")
	}
}
