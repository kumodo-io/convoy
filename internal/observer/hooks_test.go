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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

// job builds a Job unstructured with the given status subobject (the only part
// jobHookState reads).
func job(status map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{statusFieldName: status}}
}

func jobCond(condType, condStatus, message string) map[string]any {
	return map[string]any{"type": condType, statusFieldName: condStatus, "message": message}
}

func TestJobHookState(t *testing.T) {
	complete := job(map[string]any{fieldConditions: []any{jobCond(jobConditionComplete, "True", "")}})
	if s, _ := jobHookState(complete); s != corev1alpha1.LifecycleHookStateSucceeded {
		t.Errorf("Complete job = %s, want Succeeded", s)
	}

	failed := job(map[string]any{fieldConditions: []any{jobCond(terminalFailed, "True", "backoff limit")}})
	if s, m := jobHookState(failed); s != corev1alpha1.LifecycleHookStateFailed || m != "backoff limit" {
		t.Errorf("Failed job = %s/%q, want Failed/'backoff limit'", s, m)
	}

	// A condition present but not True does not terminate.
	running := job(map[string]any{fieldConditions: []any{jobCond(jobConditionComplete, "False", "")}})
	if s, _ := jobHookState(running); s != corev1alpha1.LifecycleHookStateRunning {
		t.Errorf("non-True condition = %s, want Running", s)
	}

	if s, _ := jobHookState(job(map[string]any{})); s != corev1alpha1.LifecycleHookStateRunning {
		t.Errorf("no conditions = %s, want Running", s)
	}
}

func TestPodHookState(t *testing.T) {
	pod := func(phase, reason string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			statusFieldName: map[string]any{fieldPhase: phase, "reason": reason},
		}}
	}
	if s, _ := podHookState(pod(podPhaseSucceeded, "")); s != corev1alpha1.LifecycleHookStateSucceeded {
		t.Errorf("Succeeded pod = %s, want Succeeded", s)
	}
	if s, m := podHookState(pod(terminalFailed, "Evicted")); s != corev1alpha1.LifecycleHookStateFailed || m != "Evicted" {
		t.Errorf("Failed pod = %s/%q, want Failed/Evicted", s, m)
	}
	if s, _ := podHookState(pod("Running", "")); s != corev1alpha1.LifecycleHookStateRunning {
		t.Errorf("Running pod = %s, want Running", s)
	}
	if s, _ := podHookState(pod("Pending", "")); s != corev1alpha1.LifecycleHookStateRunning {
		t.Errorf("Pending pod = %s, want Running", s)
	}
}

func TestHookWorkloadState_Timeout(t *testing.T) {
	created := metav1.NewTime(time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC))
	timeout := &metav1.Duration{Duration: 5 * time.Minute}
	item := corev1alpha1.ReleaseLifecycleHookItem{Kind: "Job", CreatedAt: &created, Timeout: timeout}
	runningJob := job(map[string]any{}) // no terminal condition → Running

	// 3 min in: still Running (under the 5m timeout).
	if s, _ := hookWorkloadState(item, runningJob, created.Add(3*time.Minute)); s != corev1alpha1.LifecycleHookStateRunning {
		t.Errorf("at 3m = %s, want Running", s)
	}
	// 6 min in: TimedOut.
	if s, _ := hookWorkloadState(item, runningJob, created.Add(6*time.Minute)); s != corev1alpha1.LifecycleHookStateTimedOut {
		t.Errorf("at 6m = %s, want TimedOut", s)
	}
	// A completed job is not overridden by timeout.
	done := job(map[string]any{"conditions": []any{map[string]any{"type": "Complete", "status": "True"}}})
	if s, _ := hookWorkloadState(item, done, created.Add(6*time.Minute)); s != corev1alpha1.LifecycleHookStateSucceeded {
		t.Errorf("completed job past timeout = %s, want Succeeded", s)
	}
	// No timeout set → never times out.
	noTimeout := corev1alpha1.ReleaseLifecycleHookItem{Kind: "Job", CreatedAt: &created}
	if s, _ := hookWorkloadState(noTimeout, runningJob, created.Add(24*time.Hour)); s != corev1alpha1.LifecycleHookStateRunning {
		t.Errorf("no timeout = %s, want Running", s)
	}
}

func TestNeedsHookPoll(t *testing.T) {
	created := metav1.NewTime(time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC))
	earlier := metav1.NewTime(created.Add(-time.Minute))
	later := metav1.NewTime(created.Add(time.Minute))

	// No workload → never poll.
	if needsHookPoll(corev1alpha1.ReleaseLifecycleHookItem{}) {
		t.Error("no workload polled, want skip")
	}
	// Armed, never finished → poll.
	if !needsHookPoll(corev1alpha1.ReleaseLifecycleHookItem{WorkloadName: "w", CreatedAt: &created}) {
		t.Error("armed unfinished not polled, want poll")
	}
	// Finished, not re-armed (finishedAt after createdAt) → don't poll.
	if needsHookPoll(corev1alpha1.ReleaseLifecycleHookItem{WorkloadName: "w", CreatedAt: &created, FinishedAt: &later}) {
		t.Error("finished hook polled, want skip")
	}
	// Re-armed since it finished (createdAt after finishedAt) → poll.
	if !needsHookPoll(corev1alpha1.ReleaseLifecycleHookItem{WorkloadName: "w", CreatedAt: &created, FinishedAt: &earlier}) {
		t.Error("re-armed hook not polled, want poll")
	}
}
