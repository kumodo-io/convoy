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

package waves

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

func obj(name string, ann map[string]string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": name, "namespace": "ns"},
	}}
	if ann != nil {
		u.SetAnnotations(ann)
	}
	return u
}

func TestWaveOf(t *testing.T) {
	if got := WaveOf(obj("a", nil)); got != 0 {
		t.Errorf("no annotation → %d, want 0", got)
	}
	if got := WaveOf(obj("a", map[string]string{corev1alpha1.ConvoySyncWaveAnnotation: "-10"})); got != -10 {
		t.Errorf("wave -10 → %d", got)
	}
	if got := WaveOf(obj("a", map[string]string{corev1alpha1.ConvoySyncWaveAnnotation: "notanint"})); got != 0 {
		t.Errorf("malformed wave → %d, want 0 (default, never error)", got)
	}
}

func TestStrategyOf(t *testing.T) {
	if got := StrategyOf(obj("a", nil)); got != corev1alpha1.ReleaseWaveStrategyWaitForApplied {
		t.Errorf("default strategy = %q, want waitForApplied", got)
	}
	if got := StrategyOf(obj("a", map[string]string{corev1alpha1.ConvoySyncStrategyAnnotation: "waitForHealthy"})); got != corev1alpha1.ReleaseWaveStrategyWaitForHealthy {
		t.Errorf("strategy = %q, want waitForHealthy", got)
	}
	if got := StrategyOf(obj("a", map[string]string{corev1alpha1.ConvoySyncStrategyAnnotation: "bogus"})); got != corev1alpha1.ReleaseWaveStrategyWaitForApplied {
		t.Errorf("unknown strategy = %q, want waitForApplied fallback", got)
	}
}

func TestTimeoutOf(t *testing.T) {
	if got := TimeoutOf(obj("a", nil)); got != DefaultTimeout {
		t.Errorf("default timeout = %v, want %v", got, DefaultTimeout)
	}
	if got := TimeoutOf(obj("a", map[string]string{corev1alpha1.ConvoySyncTimeoutAnnotation: "2m"})); got != 2*time.Minute {
		t.Errorf("timeout = %v, want 2m", got)
	}
	if got := TimeoutOf(obj("a", map[string]string{corev1alpha1.ConvoySyncTimeoutAnnotation: "garbage"})); got != DefaultTimeout {
		t.Errorf("malformed timeout = %v, want default", got)
	}
}

func TestGroup_OrdersAscendingAndFlagsHealth(t *testing.T) {
	objs := []*unstructured.Unstructured{
		obj("late", map[string]string{corev1alpha1.ConvoySyncWaveAnnotation: "10"}),
		obj("default1", nil),
		obj("early", map[string]string{
			corev1alpha1.ConvoySyncWaveAnnotation:     "-5",
			corev1alpha1.ConvoySyncStrategyAnnotation: "waitForHealthy",
		}),
		obj("default2", nil),
	}
	got := Group(objs)

	if len(got) != 3 {
		t.Fatalf("got %d waves, want 3 (-5, 0, 10)", len(got))
	}
	if got[0].Number != -5 || got[1].Number != 0 || got[2].Number != 10 {
		t.Errorf("wave order = %d,%d,%d, want -5,0,10", got[0].Number, got[1].Number, got[2].Number)
	}
	// Wave 0 keeps both default objects in input order.
	if len(got[1].Objects) != 2 || got[1].Objects[0].GetName() != "default1" || got[1].Objects[1].GetName() != "default2" {
		t.Errorf("wave 0 objects = %v, want [default1 default2] in order", names(got[1].Objects))
	}
	// The -5 wave has a waitForHealthy resource → NeedsHealth; others don't.
	if !got[0].NeedsHealth {
		t.Error("wave -5 NeedsHealth = false, want true")
	}
	if got[1].NeedsHealth || got[2].NeedsHealth {
		t.Error("waves 0/10 NeedsHealth = true, want false")
	}
}

func names(objs []*unstructured.Unstructured) []string {
	out := make([]string, len(objs))
	for i, o := range objs {
		out[i] = o.GetName()
	}
	return out
}
