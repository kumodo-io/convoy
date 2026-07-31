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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/render"
)

// Ownership labels stamped on every hook workload. They identify the owning
// Release + hook and drive deleteBeforeCreation / cleanup by label selector (cross-
// cluster ownerRefs don't resolve, so labels are the ownership mechanism). The
// render is encoded in the workload *name*, not a label (renderRef values aren't
// valid label values), so the selector spans renders.
const (
	labelManagedBy        = "convoy.kumodo.io/managed-by"
	labelReleaseName      = "convoy.kumodo.io/release-name"
	labelReleaseNamespace = "convoy.kumodo.io/release-namespace"
	labelReleaseUID       = "convoy.kumodo.io/release-uid"
	labelHookPhase        = "convoy.kumodo.io/hook-phase"
	labelHookName         = "convoy.kumodo.io/hook-name"

	managedByConvoy = "convoy"

	// hookNameSegmentMax caps the readable hook-name segment of a workload name so
	// the whole name stays within the Job budget (≤52: 10 phase + 32 name + 8 hash
	// + 2 dashes), leaving room for the Pod-name suffix the Job controller appends.
	hookNameSegmentMax = 32
)

// targetNamespaceOf resolves the Release's configured target namespace from the
// active content type. Empty when unset — the caller lets the cluster default it.
func targetNamespaceOf(rel *corev1alpha1.Release) string {
	c := rel.Spec.Content
	switch {
	case c.Helm != nil:
		return c.Helm.ReleaseNamespace
	case c.Manifests != nil:
		return c.Manifests.TargetNamespace
	case c.Kustomize != nil:
		return c.Kustomize.TargetNamespace
	case c.Jsonnet != nil:
		return c.Jsonnet.TargetNamespace
	default:
		return ""
	}
}

// hookWorkloadName derives a hook workload's name: {phase}-{hookname≤32}-{8-char
// hash(release | hook | renderRef)}. The phase prefix + hash guarantee uniqueness
// across phases and releases (even in a shared namespace); including renderRef in
// the hash makes the name vary per render (so deleteBeforeCreation=false can
// accumulate) while staying stable within a render (idempotent create). The hash
// is over the *original* identity, so truncated hook-name segments still resolve
// to distinct names.
func hookWorkloadName(releaseName string, phase corev1alpha1.LifecycleHookPhase, hookName, renderRef string) string {
	sum := sha256.Sum256([]byte(releaseName + "|" + hookName + "|" + renderRef))
	hash := hex.EncodeToString(sum[:4]) // 4 bytes → 8 hex chars
	return fmt.Sprintf("%s-%s-%s", strings.ToLower(string(phase)), sanitizeDNSLabel(hookName, hookNameSegmentMax), hash)
}

// hookOwnershipLabels are the labels every workload for a hook carries. The
// (release-uid, hook-phase, hook-name) subset is the deleteBeforeCreation/cleanup
// selector.
func hookOwnershipLabels(rel *corev1alpha1.Release, phase corev1alpha1.LifecycleHookPhase, hookName string) map[string]string {
	return map[string]string{
		labelManagedBy:        managedByConvoy,
		labelReleaseName:      rel.Name,
		labelReleaseNamespace: rel.Namespace,
		labelReleaseUID:       string(rel.UID),
		labelHookPhase:        string(phase),
		labelHookName:         hookName,
	}
}

// hookSelector is the label selector matching all of a hook's workloads across
// renders, for deleteBeforeCreation and cleanup.
func hookSelector(rel *corev1alpha1.Release, phase corev1alpha1.LifecycleHookPhase, hookName string) string {
	return fmt.Sprintf("%s=%s,%s=%s,%s=%s",
		labelReleaseUID, rel.UID,
		labelHookPhase, phase,
		labelHookName, hookName)
}

// buildHookWorkload assembles the target-cluster workload (Job or Pod) for a hook:
// it wraps the user-authored spec + partial metadata, renders it through Go
// templates with the hook context (so `{{ .cluster.region }}` resolves), then
// stamps the controller-owned name, namespace, and ownership labels (which win over
// any user label of the same key). Templating runs before the controller fields are
// stamped, so those are never subject to interpolation.
func buildHookWorkload(rel *corev1alpha1.Release, cluster *corev1alpha1.Cluster, phase corev1alpha1.LifecycleHookPhase, hook corev1alpha1.LifecycleHookSpec, renderRef string) (*unstructured.Unstructured, error) {
	var (
		typed any
		meta  corev1alpha1.PartialMetadata
	)
	switch {
	case hook.JobTemplate != nil:
		meta = hook.JobTemplate.Metadata
		typed = &batchv1.Job{
			TypeMeta:   metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"},
			ObjectMeta: partialObjectMeta(meta),
			Spec:       hook.JobTemplate.Spec,
		}
	case hook.PodTemplate != nil:
		meta = hook.PodTemplate.Metadata
		typed = &corev1.Pod{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
			ObjectMeta: partialObjectMeta(meta),
			Spec:       hook.PodTemplate.Spec,
		}
	default:
		return nil, fmt.Errorf("hook %q has neither podTemplate nor jobTemplate", hook.Name)
	}

	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(typed)
	if err != nil {
		return nil, fmt.Errorf("converting hook %q workload: %w", hook.Name, err)
	}
	docJSON, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshaling hook %q workload: %w", hook.Name, err)
	}
	rendered, err := render.RenderHookTemplate(docJSON, render.HookContext(rel, cluster))
	if err != nil {
		return nil, fmt.Errorf("rendering hook %q workload: %w", hook.Name, err)
	}
	obj := map[string]any{}
	if err := json.Unmarshal(rendered, &obj); err != nil {
		return nil, fmt.Errorf("unmarshaling rendered hook %q workload: %w", hook.Name, err)
	}
	u := &unstructured.Unstructured{Object: obj}

	// Stamp controller-owned identity after templating.
	u.SetName(hookWorkloadName(rel.Name, phase, hook.Name, renderRef))
	if ns := u.GetNamespace(); ns == "" {
		u.SetNamespace(targetNamespaceOf(rel)) // may be "" → cluster defaults at create
	}
	labels := u.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	for k, v := range hookOwnershipLabels(rel, phase, hook.Name) {
		labels[k] = v // ownership wins over user labels
	}
	u.SetLabels(labels)
	return u, nil
}

// partialObjectMeta projects a hook's PartialMetadata onto an ObjectMeta. The name
// is deliberately left unset — the controller owns it.
func partialObjectMeta(m corev1alpha1.PartialMetadata) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace:   m.Namespace,
		Labels:      m.Labels,
		Annotations: m.Annotations,
	}
}

// sanitizeDNSLabel lowercases s, replaces runs of invalid characters with '-',
// caps it at max, and trims dashes so the result is a valid RFC1123 label segment.
func sanitizeDNSLabel(s string, max int) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	out := b.String()
	if len(out) > max {
		out = out[:max]
	}
	out = strings.Trim(out, "-")
	if out == "" {
		out = "hook"
	}
	return out
}
