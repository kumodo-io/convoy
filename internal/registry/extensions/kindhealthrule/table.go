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

package kindhealthrule

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/apiserver/pkg/registry/rest"

	"github.com/kumodo-io/convoy/api/extensions"
	"github.com/kumodo-io/convoy/internal/helper"
)

func newTableConvertor() rest.TableConvertor { return tableConvertor{} }

type tableConvertor struct{}

// ConvertToTable renders KindHealthRule objects as a kubectl-style table.
func (tableConvertor) ConvertToTable(_ context.Context, object runtime.Object, _ runtime.Object) (*metav1.Table, error) {
	table := &metav1.Table{
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "Name", Type: helper.StringValueString, Format: "name", Description: "Name of the KindHealthRule"},
			{Name: "API Group", Type: helper.StringValueString, Description: "Targeted API group"},
			{Name: "Kind", Type: helper.StringValueString, Description: "Targeted kind"},
			{Name: "Version", Type: helper.StringValueString, Description: "Targeted API version"},
			{Name: "Age", Type: helper.StringValueDate, Description: "Time since the KindHealthRule was created"},
		},
	}

	rowFor := func(r *extensions.KindHealthRule) metav1.TableRow {
		age := "<unknown>"
		if !r.CreationTimestamp.IsZero() {
			age = duration.HumanDuration(time.Since(r.CreationTimestamp.Time))
		}
		group := r.Spec.Target.Group
		if group == "" {
			group = "<core>"
		}
		return metav1.TableRow{
			Cells:  []any{r.Name, group, r.Spec.Target.Kind, r.Spec.Target.Version, age},
			Object: runtime.RawExtension{Object: r},
		}
	}

	switch t := object.(type) {
	case *extensions.KindHealthRule:
		table.Rows = append(table.Rows, rowFor(t))
	case *extensions.KindHealthRuleList:
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
