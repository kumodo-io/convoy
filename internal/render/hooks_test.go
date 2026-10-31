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

package render

import (
	"testing"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

func hookTestRelease() *corev1alpha1.Release {
	return &corev1alpha1.Release{Name: appName, Namespace: "team-a"}
}

func hookTestCluster() *corev1alpha1.Cluster {
	return &corev1alpha1.Cluster{
		Name: "prod-eu",
		Spec: corev1alpha1.ClusterSpec{Metadata: map[string]string{"region": "eu-west-1", "tier": "production"}},
	}
}

func TestHookContext_FlattensClusterMetadata(t *testing.T) {
	ctx := HookContext(hookTestRelease(), hookTestCluster())
	cluster, _ := ctx[ctxKeyCluster].(map[string]string)
	// Spec.Metadata keys sit DIRECTLY under .cluster (no .metadata segment).
	if cluster["region"] != "eu-west-1" {
		t.Errorf(".cluster.region = %q, want eu-west-1", cluster["region"])
	}
	if cluster["name"] != "prod-eu" {
		t.Errorf(".cluster.name = %q, want prod-eu", cluster["name"])
	}
	release, _ := ctx[ctxKeyRelease].(map[string]any)
	if release["name"] != appName || release["namespace"] != "team-a" {
		t.Errorf(".release = %v, want name=app namespace=team-a", release)
	}
}

func TestEvalHookIf(t *testing.T) {
	ctx := HookContext(hookTestRelease(), hookTestCluster())

	// Empty → always run.
	if ok, err := EvalHookIf("", ctx); err != nil || !ok {
		t.Errorf("empty when = (%v,%v), want (true,nil)", ok, err)
	}
	// True condition on flattened cluster metadata.
	if ok, err := EvalHookIf(`cluster.tier == "production"`, ctx); err != nil || !ok {
		t.Errorf("prod match = (%v,%v), want (true,nil)", ok, err)
	}
	// False condition.
	if ok, err := EvalHookIf(`cluster.region == "us-east-1"`, ctx); err != nil || ok {
		t.Errorf("region mismatch = (%v,%v), want (false,nil)", ok, err)
	}
	// Release binding.
	if ok, err := EvalHookIf(`release.name == "app"`, ctx); err != nil || !ok {
		t.Errorf("release match = (%v,%v), want (true,nil)", ok, err)
	}
	// Non-bool result is an error.
	if _, err := EvalHookIf(`cluster.region`, ctx); err == nil {
		t.Error("non-bool when did not error")
	}
	// Compile error surfaces.
	if _, err := EvalHookIf(`this is not cel`, ctx); err == nil {
		t.Error("invalid CEL did not error")
	}
}

func TestRenderHookTemplate(t *testing.T) {
	ctx := HookContext(hookTestRelease(), hookTestCluster())
	out, err := RenderHookTemplate([]byte(`{"region":"{{ .cluster.region }}","rel":"{{ .release.name }}"}`), ctx)
	if err != nil {
		t.Fatalf("RenderHookTemplate: %v", err)
	}
	want := `{"region":"eu-west-1","rel":"app"}`
	if string(out) != want {
		t.Errorf("rendered = %s, want %s", out, want)
	}
}
