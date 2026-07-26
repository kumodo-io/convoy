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

package health

import (
	"time"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

// DeriveState maps an assessment to a health state, using the first-unhealthy
// anchor and the deadline to distinguish Progressing from Degraded.
//
//   - assessment error   → Unknown (and the anchor is left untouched)
//   - passed             → Healthy, anchor cleared (zero)
//   - failed, no deadline→ Progressing (never escalates)
//   - failed, within     → Progressing (anchor set to firstUnhealthy or now)
//   - failed, past        → Degraded
//
// firstUnhealthy is the stored anchor from the previous pass (zero if the
// resource was healthy or unseen). The returned anchor is what the caller stores
// for next time.
func DeriveState(passed bool, evalErr error, firstUnhealthy time.Time, deadline *time.Duration, now time.Time) (state corev1alpha1.ResourceState, anchor time.Time) {
	if evalErr != nil {
		// Couldn't assess: report Unknown, preserve any existing anchor so the
		// progress clock isn't reset by a transient error.
		return corev1alpha1.ResourceStateUnknown, firstUnhealthy
	}
	if passed {
		return corev1alpha1.ResourceStateHealthy, time.Time{} // clear the anchor
	}

	// Failing: establish or keep the anchor.
	if firstUnhealthy.IsZero() {
		firstUnhealthy = now
	}
	if deadline == nil {
		return corev1alpha1.ResourceStateProgressing, firstUnhealthy // never escalates without a deadline
	}
	if now.Sub(firstUnhealthy) >= *deadline {
		return corev1alpha1.ResourceStateDegraded, firstUnhealthy
	}
	return corev1alpha1.ResourceStateProgressing, firstUnhealthy
}
