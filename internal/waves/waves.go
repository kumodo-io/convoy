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

// Package waves groups a Release's rendered objects into ordered sync waves. A
// wave number and per-resource readiness strategy are read from convoy sync-wave
// annotations on each object (written directly in source, or by a syncWave
// mutation). It is a pure helper: it holds no clients and does no I/O, so the
// deploy path can order applies and the observer can group health by wave from
// the same rules.
package waves

import (
	"sort"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

// DefaultTimeout is used for a waitForHealthyWithTimeout resource that does not
// set the sync-timeout annotation.
const DefaultTimeout = 30 * time.Second

// Wave is a group of objects sharing a wave number, in ascending-apply order.
type Wave struct {
	// Number is the wave weight; lower applies earlier. Default 0.
	Number int
	// Objects are the rendered objects assigned to this wave.
	Objects []*unstructured.Unstructured
	// NeedsHealth is true if any object requires a health bar (waitForHealthy or
	// waitForHealthyWithTimeout) before later waves may proceed.
	NeedsHealth bool
	// Bounded is true if any object requires waitForHealthyWithTimeout — the wave's
	// health wait is bounded; if it is not healthy within Timeout the wave fails.
	// A wave mixing bounded and unbounded (waitForHealthy) resources is treated as
	// bounded (fail-safe: never block a rollout indefinitely when a timeout was
	// requested).
	Bounded bool
	// Timeout is the health-wait deadline for a bounded wave: the maximum
	// sync-timeout among its waitForHealthyWithTimeout resources.
	Timeout time.Duration
}

// WaveOf returns an object's wave number from the sync-wave annotation. A missing
// or malformed value is wave 0 (the default), never an error — ordering must not
// fail on a bad annotation.
func WaveOf(obj *unstructured.Unstructured) int {
	v, ok := obj.GetAnnotations()[corev1alpha1.ConvoySyncWaveAnnotation]
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}

// StrategyOf returns an object's readiness strategy from the sync-strategy
// annotation, defaulting to waitForApplied.
func StrategyOf(obj *unstructured.Unstructured) corev1alpha1.ReleaseWaveStrategy {
	switch corev1alpha1.ReleaseWaveStrategy(obj.GetAnnotations()[corev1alpha1.ConvoySyncStrategyAnnotation]) {
	case corev1alpha1.ReleaseWaveStrategyWaitForHealthy:
		return corev1alpha1.ReleaseWaveStrategyWaitForHealthy
	case corev1alpha1.ReleaseWaveStrategyWaitForHealthyWithTimeout:
		return corev1alpha1.ReleaseWaveStrategyWaitForHealthyWithTimeout
	default:
		return corev1alpha1.ReleaseWaveStrategyWaitForApplied
	}
}

// TimeoutOf returns an object's health-wait timeout from the sync-timeout
// annotation, defaulting to DefaultTimeout when unset or malformed.
func TimeoutOf(obj *unstructured.Unstructured) time.Duration {
	v, ok := obj.GetAnnotations()[corev1alpha1.ConvoySyncTimeoutAnnotation]
	if !ok {
		return DefaultTimeout
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return DefaultTimeout
	}
	return d
}

// Group buckets objects by wave number and returns the waves in ascending order.
// Object order within a wave is preserved from the input.
func Group(objects []*unstructured.Unstructured) []Wave {
	byNumber := map[int]*Wave{}
	var numbers []int
	for _, obj := range objects {
		n := WaveOf(obj)
		w, ok := byNumber[n]
		if !ok {
			w = &Wave{Number: n}
			byNumber[n] = w
			numbers = append(numbers, n)
		}
		w.Objects = append(w.Objects, obj)
		switch StrategyOf(obj) {
		case corev1alpha1.ReleaseWaveStrategyWaitForHealthy:
			w.NeedsHealth = true
		case corev1alpha1.ReleaseWaveStrategyWaitForHealthyWithTimeout:
			w.NeedsHealth = true
			w.Bounded = true
			if t := TimeoutOf(obj); t > w.Timeout {
				w.Timeout = t
			}
		}
	}
	sort.Ints(numbers)

	out := make([]Wave, 0, len(numbers))
	for _, n := range numbers {
		out = append(out, *byNumber[n])
	}
	return out
}
