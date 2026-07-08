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

func TestEqualIgnoringNoise_ServerNoiseIgnored(t *testing.T) {
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

	if !equalIgnoringNoise(live, desired) {
		t.Errorf("expected equal after stripping server noise:\nlive=%v\ndesired=%v", normalize(live), normalize(desired))
	}
}

func TestEqualIgnoringNoise_RealDataChangeDetected(t *testing.T) {
	live := cm(map[string]any{"k": "old"}, func(m map[string]any) {
		m[metadataFieldName].(map[string]any)["resourceVersion"] = "12345"
	})
	desired := cm(map[string]any{"k": "new"}, nil)

	if equalIgnoringNoise(live, desired) {
		t.Error("expected drift detected for a real data change, got equal")
	}
}

func TestEqualIgnoringNoise_AddedFieldDetected(t *testing.T) {
	live := cm(map[string]any{"k": "v"}, nil)
	desired := cm(map[string]any{"k": "v", "added": "x"}, nil)

	if equalIgnoringNoise(live, desired) {
		t.Error("expected drift detected when desired adds a managed field, got equal")
	}
}
