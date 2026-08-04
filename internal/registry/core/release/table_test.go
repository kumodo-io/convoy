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
	"testing"

	"github.com/kumodo-io/convoy/api/core"
)

func hs(name string, state core.LifecycleHookState) core.LifecycleHookStatus {
	return core.LifecycleHookStatus{Name: name, State: state}
}

func TestLifecycleColumn(t *testing.T) {
	rel := func(ls core.LifecycleStatus) *core.Release {
		return &core.Release{Status: core.ReleaseStatus{Lifecycle: ls}}
	}

	if got := lifecycleColumn(rel(core.LifecycleStatus{})); got != "N/A" {
		t.Errorf("no hooks: %q, want N/A", got)
	}

	singleOK := core.LifecycleStatus{PreSync: []core.LifecycleHookStatus{hs("a", core.LifecycleHookStateSucceeded), hs("b", core.LifecycleHookStateSkipped)}}
	if got := lifecycleColumn(rel(singleOK)); got != "Completed:PreSync" {
		t.Errorf("all ok: %q, want Completed:PreSync", got)
	}

	lastOKReported := core.LifecycleStatus{
		PreSync:  []core.LifecycleHookStatus{hs("a", core.LifecycleHookStateSucceeded)},
		PostSync: []core.LifecycleHookStatus{hs("b", core.LifecycleHookStateSucceeded)},
	}
	if got := lifecycleColumn(rel(lastOKReported)); got != "Completed:PostSync" {
		t.Errorf("last ok: %q, want Completed:PostSync", got)
	}

	running := core.LifecycleStatus{PreSync: []core.LifecycleHookStatus{hs("a", core.LifecycleHookStateRunning)}}
	if got := lifecycleColumn(rel(running)); got != "Running:PreSync" {
		t.Errorf("running: %q, want Running:PreSync", got)
	}

	// Failed wins, and reports the earliest phase in pipeline order (PreSync before PostSync).
	failed := core.LifecycleStatus{
		PreSync:  []core.LifecycleHookStatus{hs("a", core.LifecycleHookStateSucceeded)},
		PostSync: []core.LifecycleHookStatus{hs("b", core.LifecycleHookStateFailed)},
	}
	if got := lifecycleColumn(rel(failed)); got != "Failed:PostSync" {
		t.Errorf("failed: %q, want Failed:PostSync", got)
	}
}

func hsAt(name string, state core.LifecycleHookState, renderRef string) core.LifecycleHookStatus {
	return core.LifecycleHookStatus{Name: name, State: state, RenderRef: renderRef}
}

func TestLifecycleColumn_FiltersStaleRender(t *testing.T) {
	// Current render is "B" (LastAttempted). preSync re-ran for B (Running); postSync
	// still carries the PRIOR render "A" (Succeeded) because it hasn't re-armed yet.
	// The column must reflect the current render only → Running:PreSync, not the stale
	// Completed:PostSync.
	r := &core.Release{
		Status: core.ReleaseStatus{
			LastAttempted: &core.ReleaseAttemptedRenderStatus{RenderRef: "B"},
			Lifecycle: core.LifecycleStatus{
				PreSync:  []core.LifecycleHookStatus{hsAt("migrate", core.LifecycleHookStateRunning, "B")},
				PostSync: []core.LifecycleHookStatus{hsAt("smoke", core.LifecycleHookStateSucceeded, "A")},
			},
		},
	}
	if got := lifecycleColumn(r); got != "Running:PreSync" {
		t.Errorf("stale postSync not filtered: %q, want Running:PreSync", got)
	}

	// Once every current-render hook is done, the column reflects that (not the stale one).
	r.Status.Lifecycle.PreSync = []core.LifecycleHookStatus{hsAt("migrate", core.LifecycleHookStateSucceeded, "B")}
	if got := lifecycleColumn(r); got != "Completed:PreSync" {
		t.Errorf("current-render completion: %q, want Completed:PreSync", got)
	}
}
