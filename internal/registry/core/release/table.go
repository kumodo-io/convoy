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
			{Name: "Ready", Type: helper.StringValueString, Description: "Whether the repository is ready"},
			{Name: "Age", Type: helper.StringValueDate, Description: "Time since the Release was created"},
		},
	}

	rowFor := func(r *core.Release) metav1.TableRow {
		age := "<unknown>"
		if !r.CreationTimestamp.IsZero() {
			age = duration.HumanDuration(time.Since(r.CreationTimestamp.Time))
		}
		ready := "<unknown>"
		idx := slices.IndexFunc(r.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == "Ready"
		})
		if idx >= 0 {
			ready = string(r.Status.Conditions[idx].Status)
		}
		return metav1.TableRow{
			Cells:  []any{r.Name, string(r.Spec.Content.Type), sourceString(r.Spec.Source), r.Spec.ClusterRef.Name, ready, age},
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
