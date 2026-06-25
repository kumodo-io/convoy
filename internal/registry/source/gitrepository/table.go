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

package gitrepository

import (
	"context"
	"fmt"
	"slices"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/apiserver/pkg/registry/rest"

	"github.com/kumodo-io/convoy/api/source"
	"github.com/kumodo-io/convoy/internal/helper"
)

func newTableConvertor() rest.TableConvertor { return tableConvertor{} }

type tableConvertor struct{}

// ConvertToTable renders GitRepository objects as a kubectl-style table.
func (tableConvertor) ConvertToTable(_ context.Context, object runtime.Object, _ runtime.Object) (*metav1.Table, error) {
	table := &metav1.Table{
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "Name", Type: helper.StringValueString, Format: "name", Description: "Name of the GitRepository"},
			{Name: "URL", Type: helper.StringValueString, Description: "Repository URL"},
			{Name: "Ref", Type: helper.StringValueString, Description: "Tracked ref"},
			{Name: "Ready", Type: helper.StringValueString, Description: "Whether the repository is ready"},
			{Name: "Age", Type: helper.StringValueDate, Description: "Time since the GitRepository was created"},
		},
	}

	rowFor := func(r *source.GitRepository) metav1.TableRow {
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
			Cells:  []any{r.Name, r.Spec.URL, refString(r.Spec.Ref), ready, age},
			Object: runtime.RawExtension{Object: r},
		}
	}

	switch t := object.(type) {
	case *source.GitRepository:
		table.Rows = append(table.Rows, rowFor(t))
	case *source.GitRepositoryList:
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

func refString(ref source.GitRepositoryRefSpec) string {
	switch {
	case ref.Branch != nil:
		return "branch:" + *ref.Branch
	case ref.Tag != nil:
		return "tag:" + *ref.Tag
	case ref.Commit != nil:
		return "commit:" + *ref.Commit
	case ref.Semver != nil:
		return "semver:" + *ref.Semver
	default:
		return ""
	}
}
