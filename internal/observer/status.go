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

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	corev1alpha1ac "github.com/kumodo-io/convoy/api/generated/applyconfiguration/core/v1alpha1"
)

const (
	// observeFieldManager is the SSA field manager the observer owns. It writes
	// only the InSync condition, disjoint from the release controller's fields.
	observeFieldManager = "convoy-observe"

	// conditionInSync reports whether the live cluster matches the desired render.
	conditionInSync = "InSync"

	reasonInSync       = "InSync"
	reasonDrifted      = "Drifted"
	reasonUndetermined = "Undetermined"
)

// writeInSync applies the InSync condition via Server-Side Apply under the
// observer's field manager. It owns only that condition (conditions are a
// listType=map keyed by type, so this co-exists with the release controller's
// Applied condition). LastTransitionTime is preserved while the status is
// unchanged, and an unchanged condition is skipped to avoid write churn.
func (c *Controller) writeInSync(ctx context.Context, rel *corev1alpha1.Release, status metav1.ConditionStatus, reason, message string) error {
	ltt := c.now()
	existing := apimeta.FindStatusCondition(rel.Status.Conditions, conditionInSync)
	if existing != nil {
		if existing.Status == status && existing.Reason == reason && existing.Message == message &&
			existing.ObservedGeneration == rel.Generation {
			return nil // no change; skip the write
		}
		if existing.Status == status {
			ltt = existing.LastTransitionTime // status unchanged: keep the transition time
		}
	}

	cond := metav1ac.Condition().
		WithType(conditionInSync).
		WithStatus(status).
		WithReason(reason).
		WithMessage(message).
		WithObservedGeneration(rel.Generation).
		WithLastTransitionTime(ltt)

	ac := corev1alpha1ac.Release(rel.Name, rel.Namespace).
		WithStatus(corev1alpha1ac.ReleaseStatus().WithConditions(cond))

	if _, err := c.client.CoreV1alpha1().Releases(rel.Namespace).
		ApplyStatus(ctx, ac, metav1.ApplyOptions{FieldManager: observeFieldManager, Force: true}); err != nil {
		return fmt.Errorf("applying InSync condition: %w", err)
	}
	return nil
}

// requestSync triggers a reconcile of the Release by POSTing its /sync
// subresource — the same channel the CLI and users use. Only called in automatic
// mode: in manual mode drift is reported (InSync=False) but not auto-corrected.
func (c *Controller) requestSync(ctx context.Context, rel *corev1alpha1.Release) error {
	return c.client.CoreV1alpha1().RESTClient().
		Post().
		Namespace(rel.Namespace).
		Resource("releases").
		Name(rel.Name).
		SubResource("sync").
		Do(ctx).
		Error()
}
