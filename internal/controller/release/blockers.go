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
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/inventory"
)

const (
	// conditionDeleted reports a Release deletion that waits on its target cluster.
	conditionDeleted               = "Deleted"
	conditionReasonDeletionBlocked = "DeletionBlocked"

	verbList = "list"

	// maxBlockersShown bounds a blocked message; the count of the rest follows.
	maxBlockersShown = 5
)

var crdGVR = schema.GroupVersionResource{Group: CustomResourceDefinitionGroup, Version: "v1", Resource: "customresourcedefinitions"}

// pruneGuarded prunes refs, but deletes a Namespace or a CRD only when the delete
// takes nothing outside managed with it. It returns the refs it kept and why.
func (c *Controller) pruneGuarded(ctx context.Context, cluster *corev1alpha1.Cluster, refs, managed []inventory.Ref) ([]inventory.Ref, string, error) {
	var guarded, rest []inventory.Ref
	for _, r := range refs {
		if isCRD(r) || isNamespace(r) {
			guarded = append(guarded, r)
		} else {
			rest = append(rest, r)
		}
	}
	if err := c.deployer.Prune(ctx, cluster, rest); err != nil {
		return nil, "", err
	}
	var safe, kept []inventory.Ref
	var notes []string
	for _, r := range guarded {
		blockers, err := c.deployer.Blockers(ctx, cluster, r, managed)
		if err != nil {
			return nil, "", fmt.Errorf("checking %s %q: %w", r.Kind, r.Name, err)
		}
		if len(blockers) > 0 {
			kept = append(kept, r)
			notes = append(notes, blockedMessage(r, blockers))
			continue
		}
		safe = append(safe, r)
	}
	if err := c.deployer.Prune(ctx, cluster, safe); err != nil {
		return nil, "", err
	}
	return kept, strings.Join(notes, "; "), nil
}

// withKept adds the orphans pruneGuarded kept, so the next reconcile checks them
// again. They take the current render: the health loop reports a Release healthy
// only when every inventory entry is at the last attempted render.
func withKept(applied, kept []inventory.Ref, renderRef string) []inventory.Ref {
	if len(kept) == 0 {
		return applied
	}
	out := slices.Clone(applied)
	for _, r := range kept {
		r.RenderRef = renderRef
		out = append(out, r)
	}
	return out
}

func joinNote(message, note string) string {
	if note == "" {
		return message
	}
	return message + "; " + note
}

func (d *connectorDeployer) Blockers(ctx context.Context, cluster *corev1alpha1.Cluster, target inventory.Ref, managed []inventory.Ref) ([]string, error) {
	cfg, err := d.connector.Connect(ctx, cluster)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("building dynamic client: %w", err)
	}
	var objs []unstructured.Unstructured
	var failures []string
	switch {
	case isNamespace(target):
		disco, err := discovery.NewDiscoveryClientForConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("building discovery client: %w", err)
		}
		// Partial results come with ErrGroupDiscoveryFailed; each failed group blocks.
		lists, err := disco.ServerPreferredNamespacedResources()
		var listFailures []string
		objs, listFailures = listNamespaced(ctx, dyn, lists, target.Name)
		failures = append(discoveryFailures(err), listFailures...)
	case isCRD(target):
		objs, failures = listCustomResources(ctx, dyn, target.Name)
	default:
		return nil, nil
	}
	return append(failures, unmanaged(objs, managed)...), nil
}

func discoveryFailures(err error) []string {
	if err == nil {
		return nil
	}
	var groupErr *discovery.ErrGroupDiscoveryFailed
	if !errors.As(err, &groupErr) {
		return []string{fmt.Sprintf("cannot discover resources: %v", err)}
	}
	out := make([]string, 0, len(groupErr.Groups))
	for gv, e := range groupErr.Groups {
		out = append(out, fmt.Sprintf("cannot discover %s: %v", gv, e))
	}
	slices.Sort(out)
	return out
}

// unmanaged describes the objects in objs that are not in managed and not owned,
// through an ownerReference chain, by an object in managed.
func unmanaged(objs []unstructured.Unstructured, managed []inventory.Ref) []string {
	ids := make(map[string]bool, len(managed))
	uids := make(map[types.UID]bool, len(managed))
	services := map[string]bool{}
	for _, r := range managed {
		ids[r.ID()] = true
		if r.UID != "" {
			uids[r.UID] = true
		}
		if r.Group == "" && r.Kind == "Service" {
			services[r.Namespace+"/"+r.Name] = true
		}
	}
	var pending []unstructured.Unstructured
	for _, o := range objs {
		gk := o.GroupVersionKind().GroupKind()
		switch {
		case ids[inventory.Ref{Group: gk.Group, Kind: gk.Kind, Namespace: o.GetNamespace(), Name: o.GetName()}.ID()]:
			// An inventory written before refs carried a UID still roots the owner chain.
			uids[o.GetUID()] = true
		case kubernetesDefault(gk, o.GetName()):
		case gk == (schema.GroupKind{Kind: "Endpoints"}) && services[o.GetNamespace()+"/"+o.GetName()]:
		default:
			pending = append(pending, o)
		}
	}
	// Repeat until stable: a Pod is owned by a ReplicaSet that a managed Deployment owns.
	for changed := true; changed; {
		changed = false
		rest := pending[:0]
		for _, o := range pending {
			if ownedBy(o, uids) {
				uids[o.GetUID()] = true
				changed = true
				continue
			}
			rest = append(rest, o)
		}
		pending = rest
	}
	out := make([]string, 0, len(pending))
	for _, o := range pending {
		out = append(out, describe(o))
	}
	slices.Sort(out)
	return out
}

// kubernetesDefault reports the objects Kubernetes creates in every Namespace.
func kubernetesDefault(gk schema.GroupKind, name string) bool {
	if gk.Group != "" {
		return false
	}
	return (gk.Kind == "ServiceAccount" && name == "default") || (gk.Kind == "ConfigMap" && name == "kube-root-ca.crt")
}

func ownedBy(o unstructured.Unstructured, uids map[types.UID]bool) bool {
	for _, ref := range o.GetOwnerReferences() {
		if uids[ref.UID] {
			return true
		}
	}
	return false
}

func describe(o unstructured.Unstructured) string {
	if o.GetNamespace() == "" {
		return o.GetKind() + " " + o.GetName()
	}
	return o.GetKind() + " " + o.GetNamespace() + "/" + o.GetName()
}

// listNamespaced lists the objects of every listable resource in lists within
// namespace. Each resource it cannot list becomes a blocker.
func listNamespaced(ctx context.Context, dyn dynamic.Interface, lists []*metav1.APIResourceList, namespace string) ([]unstructured.Unstructured, []string) {
	var objs []unstructured.Unstructured
	var failures []string
	for _, l := range lists {
		gv, err := schema.ParseGroupVersion(l.GroupVersion)
		if err != nil {
			failures = append(failures, fmt.Sprintf("cannot parse %s: %v", l.GroupVersion, err))
			continue
		}
		for _, r := range l.APIResources {
			// Events record history and go with the Namespace on purpose.
			if !r.Namespaced || strings.Contains(r.Name, "/") || !slices.Contains(r.Verbs, verbList) || r.Name == "events" {
				continue
			}
			list, err := dyn.Resource(gv.WithResource(r.Name)).Namespace(namespace).List(ctx, metav1.ListOptions{})
			if err != nil {
				failures = append(failures, fmt.Sprintf("cannot list %s: %v", schema.GroupResource{Group: gv.Group, Resource: r.Name}, err))
				continue
			}
			for _, item := range list.Items {
				item.SetGroupVersionKind(gv.WithKind(r.Kind))
				objs = append(objs, item)
			}
		}
	}
	return objs, failures
}

// listCustomResources lists, in all namespaces, the custom resources of the CRD
// named crdName. A failed read or list becomes a blocker.
func listCustomResources(ctx context.Context, dyn dynamic.Interface, crdName string) ([]unstructured.Unstructured, []string) {
	crd, err := dyn.Resource(crdGVR).Get(ctx, crdName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{fmt.Sprintf("cannot read %s %s: %v", CustomResourceDefinitionKind, crdName, err)}
	}
	group, _, _ := unstructured.NestedString(crd.Object, "spec", "group")
	plural, _, _ := unstructured.NestedString(crd.Object, "spec", "names", "plural")
	kind, _, _ := unstructured.NestedString(crd.Object, "spec", "names", "kind")
	version := servedVersion(crd)
	if version == "" {
		return nil, []string{fmt.Sprintf("cannot list %s: no served version", crdName)}
	}
	gv := schema.GroupVersion{Group: group, Version: version}
	list, err := dyn.Resource(gv.WithResource(plural)).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, []string{fmt.Sprintf("cannot list %s: %v", crdName, err)}
	}
	objs := make([]unstructured.Unstructured, 0, len(list.Items))
	for _, item := range list.Items {
		item.SetGroupVersionKind(gv.WithKind(kind))
		objs = append(objs, item)
	}
	return objs, nil
}

// servedVersion picks the storage version when it is served, else the first served one.
func servedVersion(crd *unstructured.Unstructured) string {
	versions, _, _ := unstructured.NestedSlice(crd.Object, "spec", "versions")
	first := ""
	for _, v := range versions {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(m, "name")
		served, _, _ := unstructured.NestedBool(m, "served")
		storage, _, _ := unstructured.NestedBool(m, "storage")
		if !served {
			continue
		}
		if storage {
			return name
		}
		if first == "" {
			first = name
		}
	}
	return first
}

// blockedMessage explains why target was not deleted.
func blockedMessage(target inventory.Ref, blockers []string) string {
	shown, more := blockers, ""
	if len(blockers) > maxBlockersShown {
		shown, more = blockers[:maxBlockersShown], fmt.Sprintf(" and %d more", len(blockers)-maxBlockersShown)
	}
	return fmt.Sprintf("%s %s not deleted, blocked by: %s%s", target.Kind, target.Name, strings.Join(shown, ", "), more)
}
