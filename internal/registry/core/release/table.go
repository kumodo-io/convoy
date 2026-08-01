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
	"fmt"
	"slices"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/apiserver/pkg/registry/rest"

	"github.com/kumodo-io/convoy/api/core"
	"github.com/kumodo-io/convoy/internal/helper"
)

const unknownState = "<unknown>"

func newTableConvertor() rest.TableConvertor { return tableConvertor{} }

type tableConvertor struct{}

// ConvertToTable renders Release objects as a kubectl-style table.
func (tableConvertor) ConvertToTable(_ context.Context, object runtime.Object, _ runtime.Object) (*metav1.Table, error) {
	table := &metav1.Table{
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "Name", Type: helper.StringValueString, Format: "name", Description: "Name of the Release"},
			{Name: "Content", Type: helper.StringValueString, Description: "Content interpretation type"},
			{Name: "Source", Type: helper.StringValueString, Description: "Referenced source (type/name)"},
			{Name: "Cluster", Type: helper.StringValueString, Description: "Target cluster"},
			{Name: "Applied", Type: helper.StringValueString, Description: "Whether the release render is applied"},
			{Name: "InSync", Type: helper.StringValueString, Description: "Whether the release is in sync"},
			{Name: "Lifecycle", Type: helper.StringValueString, Description: "Lifecycle hook status"},
			{Name: "Age", Type: helper.StringValueDate, Description: "Time since the Release was created"},
		},
	}

	rowFor := func(r *core.Release) metav1.TableRow {
		age := unknownState
		if !r.CreationTimestamp.IsZero() {
			age = duration.HumanDuration(time.Since(r.CreationTimestamp.Time))
		}
		applied := unknownState
		appliedIdx := slices.IndexFunc(r.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == "Applied"
		})
		if appliedIdx >= 0 {
			applied = string(r.Status.Conditions[appliedIdx].Status)
		}
		inSync := unknownState
		inSyncIdx := slices.IndexFunc(r.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == "InSync"
		})
		if inSyncIdx >= 0 {
			inSync = string(r.Status.Conditions[inSyncIdx].Status)
		}
		return metav1.TableRow{
			Cells:  []any{r.Name, string(r.Spec.Content.Type), sourceString(r.Spec.Source), r.Spec.ClusterRef.Name, applied, inSync, lifecycleColumn(r), age},
			Object: runtime.RawExtension{Object: r},
		}
	}

	switch t := object.(type) {
	case *core.Release:
		table.Rows = append(table.Rows, rowFor(t))
	case *core.ReleaseList:
		table.ResourceVersion = t.ResourceVersion
		table.Continue = t.Continue
		for i := range t.Items {
			table.Rows = append(table.Rows, rowFor(&t.Items[i]))
		}
	default:
		return nil, fmt.Errorf("unexpected object type %T", object)
	}

	return table, nil
}

// lifecycleColumn summarizes a Release's lifecycle-hook status for the table:
// "N/A" when it has no hooks, "Failed:<phase>" / "Running:<phase>" for the first
// phase (in pipeline order) with a failing/running hook, else "Succeeded".
func lifecycleColumn(r *core.Release) string {
	phases := []struct {
		name  string
		hooks []core.LifecycleHookStatus
	}{
		{"PreRender", r.Status.Lifecycle.PreRender},
		{"PostRender", r.Status.Lifecycle.PostRender},
		{"PreSync", r.Status.Lifecycle.PreSync},
		{"SyncFailed", r.Status.Lifecycle.SyncFailed},
		{"PostSync", r.Status.Lifecycle.PostSync},
		{"PreDelete", r.Status.Lifecycle.PreDelete},
		{"PostDelete", r.Status.Lifecycle.PostDelete},
	}
	total, firstFailed, firstRunning := 0, "", ""
	for _, p := range phases {
		for i := range p.hooks {
			total++
			switch p.hooks[i].State {
			case core.LifecycleHookStateFailed, core.LifecycleHookStateTimedOut:
				if firstFailed == "" {
					firstFailed = p.name
				}
			case core.LifecycleHookStateSucceeded, core.LifecycleHookStateSkipped:
				// terminal-ok
			default:
				if firstRunning == "" {
					firstRunning = p.name
				}
			}
		}
	}
	switch {
	case total == 0:
		return "N/A"
	case firstFailed != "":
		return "Failed:" + firstFailed
	case firstRunning != "":
		return "Running:" + firstRunning
	default:
		return "Succeeded"
	}
}

// sourceString renders a Release source as "type/name" using the per-type block
// that matches the discriminator.
func sourceString(s core.ReleaseSourceSpec) string {
	var name string
	switch s.Type {
	case core.ReleaseSourceTypeGitRepository:
		if s.GitRepository != nil {
			name = s.GitRepository.Name
		}
	case core.ReleaseSourceTypeHelmRegistry:
		if s.HelmRegistry != nil {
			name = s.HelmRegistry.Name
		}
	case core.ReleaseSourceTypeOCIRegistry:
		if s.OCIRegistry != nil {
			name = s.OCIRegistry.Name
		}
	}
	if name == "" {
		return string(s.Type)
	}
	return fmt.Sprintf("%s/%s", s.Type, name)
}
