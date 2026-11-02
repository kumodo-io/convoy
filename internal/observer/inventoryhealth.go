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
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	corev1alpha1ac "github.com/kumodo-io/convoy/api/generated/applyconfiguration/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/healthresult"
)

// priorAndAnchors snapshots, from a ReleaseInventory, each entry's prior health
// State and its first-unhealthy anchor, both keyed by version-independent
// identity. prior detects the transition that completes a wave; anchors are the
// progress-deadline clocks (Progressing vs. Degraded) — read from the persisted
// object so an in-flight clock survives an observer restart instead of resetting.
//
// The anchor drives only the observer's Healthy-CONDITION label (Progressing vs.
// Degraded) and the /health view; it is deliberately NOT render-reset. The rollout gate
// no longer depends on it — the release controller owns the rollout deadline (measured
// from status.rollout.startedAt), so each render gets a fresh deadline by construction
// with no cross-writer race. See https://convoy.kumodo.io/documentation/architecture/rollout-state-machine/.
func priorAndAnchors(inv *corev1alpha1.ReleaseInventory) (map[string]corev1alpha1.ResourceState, map[string]time.Time) {
	prior := make(map[string]corev1alpha1.ResourceState, len(inv.Inventory))
	anchors := make(map[string]time.Time, len(inv.Inventory))
	for i := range inv.Inventory {
		it := inv.Inventory[i]
		id := itemIdentityID(it)
		if it.State != nil {
			prior[id] = *it.State
		}
		if it.FirstUnhealthy != nil && !it.FirstUnhealthy.IsZero() {
			anchors[id] = it.FirstUnhealthy.Time
		}
	}
	return prior, anchors
}

// inventoryHealthFieldManager is the Server-Side Apply field manager the health
// loop owns on a ReleaseInventory. It writes only each entry's health fields
// (state/firstUnhealthy/message); the release controller owns identity/wave/uid
// under its own manager, so the two co-own each entry without clobbering.
const inventoryHealthFieldManager = "convoy-health-inventory"

// writeInventoryHealth persists the assessed per-resource health onto the
// ReleaseInventory via Server-Side Apply under the health field manager. Each
// entry carries its identity key plus the health fields; wave/uid are left to the
// controller. A firstUnhealthy of zero (a Healthy object) is omitted, which clears
// any prior anchor this manager owned. An empty set writes nothing.
func (c *HealthController) writeInventoryHealth(ctx context.Context, rel *corev1alpha1.Release, entries []healthEntry) error {
	if len(entries) == 0 {
		return nil
	}
	ac := corev1alpha1ac.ReleaseInventory(rel.Name, rel.Namespace)
	for _, e := range entries {
		item := corev1alpha1ac.ResourceInventoryItem().
			WithGroup(e.ref.gvk.Group).
			WithKind(e.ref.gvk.Kind).
			WithNamespace(e.ref.namespace).
			WithName(e.ref.name).
			WithState(e.state)
		if e.message != "" {
			item.WithMessage(e.message)
		}
		if !e.firstUnhealthy.IsZero() {
			item.WithFirstUnhealthy(metav1.NewTime(e.firstUnhealthy))
		}
		// Record the render this resource was assessed at — the render it was actually
		// applied under (e.renderRef), so a gate can distinguish current-render health
		// from stale health left over from a prior render.
		if e.renderRef != "" {
			item.WithAssessedRenderRef(e.renderRef)
		}
		ac.WithInventory(item)
	}
	if _, err := c.client.CoreV1alpha1().ReleaseInventories(rel.Namespace).
		Apply(ctx, ac, metav1.ApplyOptions{FieldManager: inventoryHealthFieldManager, Force: true}); err != nil {
		return fmt.Errorf("applying inventory health for %s/%s: %w", rel.Namespace, rel.Name, err)
	}
	return nil
}

// resultFromInventory builds the /health payload from a ReleaseInventory's
// persisted entries, or nil if there are none.
func resultFromInventory(inv *corev1alpha1.ReleaseInventory) *healthresult.Result {
	if inv == nil || len(inv.Inventory) == 0 {
		return nil
	}
	res := &healthresult.Result{Objects: make([]healthresult.ObjectHealth, 0, len(inv.Inventory))}
	for i := range inv.Inventory {
		it := inv.Inventory[i]
		var state healthresult.State
		if it.State != nil {
			state = healthresult.State(*it.State)
		}
		res.Objects = append(res.Objects, healthresult.ObjectHealth{
			Group:     it.Group,
			Version:   it.Version,
			Kind:      it.Kind,
			Namespace: it.Namespace,
			Name:      it.Name,
			State:     state,
			Message:   it.Message,
		})
	}
	return res
}

// itemIdentityID is the version-independent identity key for a persisted
// inventory entry, matching objectRef.identityID and inventory.Ref.ID.
func itemIdentityID(it corev1alpha1.ResourceInventoryItem) string {
	return it.Group + "|" + it.Kind + "|" + it.Namespace + "|" + it.Name
}
