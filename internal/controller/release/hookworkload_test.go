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

package release

import (
	"strings"
	"testing"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

func TestHookWorkloadName(t *testing.T) {
	n := hookWorkloadName(appName, corev1alpha1.LifecycleHookPhasePreSync, hookNameMigrate, "sha256:a")

	// Deterministic + stable within a render.
	if n2 := hookWorkloadName(appName, corev1alpha1.LifecycleHookPhasePreSync, hookNameMigrate, "sha256:a"); n != n2 {
		t.Errorf("name not stable: %q vs %q", n, n2)
	}
	// Phase-prefixed, lowercased.
	if !strings.HasPrefix(n, "presync-migrate-") {
		t.Errorf("name %q missing phase/hook prefix", n)
	}
	// Fits the Job budget.
	if len(n) > 52 {
		t.Errorf("name %q length %d exceeds 52", n, len(n))
	}
	// Varies by render (so deleteBeforeCreation=false can accumulate).
	if nb := hookWorkloadName(appName, corev1alpha1.LifecycleHookPhasePreSync, hookNameMigrate, "sha256:b"); nb == n {
		t.Error("name did not vary with renderRef")
	}
	// Varies by phase.
	if np := hookWorkloadName(appName, corev1alpha1.LifecycleHookPhasePostSync, hookNameMigrate, "sha256:a"); np == n {
		t.Error("name did not vary with phase")
	}
	// Varies by release (distinct hashes even in a shared namespace).
	if nr := hookWorkloadName("other", corev1alpha1.LifecycleHookPhasePreSync, hookNameMigrate, "sha256:a"); nr == n {
		t.Error("name did not vary with release")
	}
}

func TestHookWorkloadName_LongHookNameCapped(t *testing.T) {
	long := strings.Repeat("x", 100)
	n := hookWorkloadName(appName, corev1alpha1.LifecycleHookPhasePostDelete, long, "sha256:a")
	if len(n) > 52 {
		t.Errorf("name %q length %d exceeds 52 for a long hook name", n, len(n))
	}
	// Two different long names sharing a 32-char prefix still differ via the hash.
	other := strings.Repeat("x", 90) + "different"
	if n2 := hookWorkloadName(appName, corev1alpha1.LifecycleHookPhasePostDelete, other, "sha256:a"); n2 == n {
		t.Error("distinct long hook names collided")
	}
}

func TestTargetNamespaceOf(t *testing.T) {
	tests := []struct {
		name    string
		content corev1alpha1.ReleaseContentSpec
		want    string
	}{
		{"helm", corev1alpha1.ReleaseContentSpec{Helm: &corev1alpha1.ReleaseContentHelmSpec{ReleaseNamespace: "hns"}}, "hns"},
		{"manifests", corev1alpha1.ReleaseContentSpec{Manifests: &corev1alpha1.ReleaseContentManifestsSpec{TargetNamespace: "mns"}}, "mns"},
		{"kustomize", corev1alpha1.ReleaseContentSpec{Kustomize: &corev1alpha1.ReleaseContentKustomizeSpec{TargetNamespace: "kns"}}, "kns"},
		{"jsonnet", corev1alpha1.ReleaseContentSpec{Jsonnet: &corev1alpha1.ReleaseContentJsonnetSpec{TargetNamespace: "jns"}}, "jns"},
		{"unset", corev1alpha1.ReleaseContentSpec{Manifests: &corev1alpha1.ReleaseContentManifestsSpec{}}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rel := &corev1alpha1.Release{Spec: corev1alpha1.ReleaseSpec{Content: tc.content}}
			if got := targetNamespaceOf(rel); got != tc.want {
				t.Errorf("targetNamespaceOf = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildHookWorkload_StampsIdentityAndLabels(t *testing.T) {
	rel := &corev1alpha1.Release{
		Name: appName, Namespace: "ns", UID: "uid-9",
		Spec: corev1alpha1.ReleaseSpec{Content: corev1alpha1.ReleaseContentSpec{Manifests: &corev1alpha1.ReleaseContentManifestsSpec{TargetNamespace: "app-ns"}}},
	}
	hook := podHookSpec(hookNameMigrate, 0, "", corev1alpha1.LifecycleHookOnFailureActionFail, corev1alpha1.LifecycleHookCleanupNever)
	hook.PodTemplate.Metadata.Labels = map[string]string{"user": "label", labelManagedBy: "should-be-overridden"}

	w, err := buildHookWorkload(rel, hookCluster, corev1alpha1.LifecycleHookPhasePreSync, hook, "sha256:a")
	if err != nil {
		t.Fatalf("buildHookWorkload: %v", err)
	}
	if w.GetNamespace() != "app-ns" {
		t.Errorf("namespace = %q, want app-ns (target)", w.GetNamespace())
	}
	if w.GetName() != hookWorkloadName(appName, corev1alpha1.LifecycleHookPhasePreSync, hookNameMigrate, "sha256:a") {
		t.Errorf("name = %q, want derived", w.GetName())
	}
	labels := w.GetLabels()
	if labels[labelManagedBy] != managedByConvoy {
		t.Errorf("managed-by = %q, want ownership label to win over user label", labels[labelManagedBy])
	}
	if labels[labelReleaseUID] != "uid-9" || labels[labelHookName] != hookNameMigrate || labels[labelHookPhase] != "PreSync" {
		t.Errorf("ownership labels missing/incorrect: %v", labels)
	}
	if labels["user"] != "label" {
		t.Errorf("user label dropped: %v", labels)
	}
}
