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
	"github.com/kumodo-io/convoy/internal/healthresult"
)

const (
	// healthFieldManager is the SSA field manager the health loop owns. It writes
	// only the Healthy condition + status.healthSummary, disjoint from the drift
	// loop's InSync condition and the release controller's fields.
	healthFieldManager = "convoy-health"

	// waveHealthFieldManager owns only status.waves[].healthy. It is separate from
	// healthFieldManager because each SSA Apply is the complete desired state for
	// its manager; a distinct manager lets the wave-health write and the condition
	// write coexist, and lets a pass that has no fresh wave data simply skip the
	// wave write (leaving prior healthy values untouched) rather than dropping them.
	waveHealthFieldManager = "convoy-health-waves"

	// conditionHealthy reports whether the Release's managed resources are healthy.
	conditionHealthy = "Healthy"

	reasonHealthy       = "Healthy"
	reasonProgressing   = "Progressing"
	reasonDegraded      = "Degraded"
	reasonUnknown       = "Unknown"
	reasonNotConfigured = "NotConfigured"
)

// writeHealth applies the Healthy condition and health summary via Server-Side
// Apply under the health field manager. It owns only that condition (conditions
// are listType=map keyed by type, so it co-exists with InSync/Applied) and the
// healthSummary field. LastTransitionTime is preserved while the status is
// unchanged, and an unchanged write is skipped to avoid churn.
func (c *HealthController) writeHealth(ctx context.Context, rel *corev1alpha1.Release, status metav1.ConditionStatus, reason, message string, summary healthresult.Summary) error {
	ltt := c.now()
	existing := apimeta.FindStatusCondition(rel.Status.Conditions, conditionHealthy)
	if existing != nil {
		summaryUnchanged := rel.Status.HealthSummary == toSummary(summary)
		if existing.Status == status && existing.Reason == reason && existing.Message == message &&
			existing.ObservedGeneration == rel.Generation && summaryUnchanged {
			return nil // no change; skip the write
		}
		if existing.Status == status {
			ltt = existing.LastTransitionTime
		}
	}

	cond := metav1ac.Condition().
		WithType(conditionHealthy).
		WithStatus(status).
		WithReason(reason).
		WithMessage(message).
		WithObservedGeneration(rel.Generation).
		WithLastTransitionTime(ltt)

	ac := corev1alpha1ac.Release(rel.Name, rel.Namespace).
		WithStatus(corev1alpha1ac.ReleaseStatus().
			WithConditions(cond).
			WithHealthSummary(corev1alpha1ac.ReleaseHealthSummary().
				WithTotal(summary.Total).
				WithHealthy(summary.Healthy).
				WithProgressing(summary.Progressing).
				WithDegraded(summary.Degraded).
				WithUnknown(summary.Unknown)))

	if _, err := c.client.CoreV1alpha1().Releases(rel.Namespace).
		ApplyStatus(ctx, ac, metav1.ApplyOptions{FieldManager: healthFieldManager, Force: true}); err != nil {
		return fmt.Errorf("applying Healthy condition: %w", err)
	}
	return nil
}

// writeWaveHealth applies status.waves[].healthy for the given waves under the
// wave-health field manager. Each entry is built field by field with only number
// (the list-map key) and healthy, so the observer co-owns wave entries with the
// controller (which owns applied) without clobbering it. A no-op (values already
// match) is skipped; an empty map writes nothing.
func (c *HealthController) writeWaveHealth(ctx context.Context, rel *corev1alpha1.Release, waveHealthy map[int64]bool) error {
	if len(waveHealthy) == 0 {
		return nil
	}
	// Skip when every value already matches what's on the object.
	existing := map[int64]*bool{}
	for i := range rel.Status.Waves {
		existing[rel.Status.Waves[i].Number] = rel.Status.Waves[i].Healthy
	}
	changed := false
	for n, h := range waveHealthy {
		if e := existing[n]; e == nil || *e != h {
			changed = true
			break
		}
	}
	if !changed {
		return nil
	}

	statusAC := corev1alpha1ac.ReleaseStatus()
	for n, h := range waveHealthy {
		statusAC.WithWaves(corev1alpha1ac.ResourceWaveStatus().WithNumber(n).WithHealthy(h))
	}
	ac := corev1alpha1ac.Release(rel.Name, rel.Namespace).WithStatus(statusAC)
	if _, err := c.client.CoreV1alpha1().Releases(rel.Namespace).
		ApplyStatus(ctx, ac, metav1.ApplyOptions{FieldManager: waveHealthFieldManager, Force: true}); err != nil {
		return fmt.Errorf("applying wave health: %w", err)
	}
	return nil
}

// toSummary maps the wire summary to the core status type for the no-op check.
func toSummary(s healthresult.Summary) corev1alpha1.ReleaseHealthSummary {
	return corev1alpha1.ReleaseHealthSummary{
		Total:       s.Total,
		Healthy:     s.Healthy,
		Progressing: s.Progressing,
		Degraded:    s.Degraded,
		Unknown:     s.Unknown,
	}
}

// conditionFor maps an aggregate health result to the Healthy condition's
// status/reason/message. The reason reflects the worst state present so a user
// sees the most actionable signal.
func conditionFor(result *healthresult.Result) (metav1.ConditionStatus, string, string) {
	s := result.Summarize()
	if result.Healthy() {
		return metav1.ConditionTrue, reasonHealthy, fmt.Sprintf("all %d resource(s) healthy", s.Total)
	}
	msg := fmt.Sprintf("%d healthy, %d progressing, %d degraded, %d unknown of %d",
		s.Healthy, s.Progressing, s.Degraded, s.Unknown, s.Total)
	switch {
	case s.Degraded > 0:
		return metav1.ConditionFalse, reasonDegraded, msg
	case s.Unknown > 0:
		return metav1.ConditionUnknown, reasonUnknown, msg
	default:
		return metav1.ConditionFalse, reasonProgressing, msg
	}
}
