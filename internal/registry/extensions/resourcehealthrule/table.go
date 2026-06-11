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

package resourcehealthrule

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

// ConvertToTable renders ResourceHealthRule objects as a kubectl-style table.
func (tableConvertor) ConvertToTable(_ context.Context, object runtime.Object, _ runtime.Object) (*metav1.Table, error) {
	table := &metav1.Table{
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "Name", Type: helper.StringValueString, Format: "name", Description: "Name of the ResourceHealthRule"},
			{Name: "Kind", Type: helper.StringValueString, Description: "Targeted kind"},
			{Name: "Target", Type: helper.StringValueString, Description: "Targeted resource instance (namespace/name)"},
			{Name: "Age", Type: helper.StringValueDate, Description: "Time since the ResourceHealthRule was created"},
		},
	}

	rowFor := func(r *extensions.ResourceHealthRule) metav1.TableRow {
		age := "<unknown>"
		if !r.CreationTimestamp.IsZero() {
			age = duration.HumanDuration(time.Since(r.CreationTimestamp.Time))
		}
		target := r.Spec.Target.Name
		if r.Spec.Target.Namespace != "" {
			target = fmt.Sprintf("%s/%s", r.Spec.Target.Namespace, r.Spec.Target.Name)
		}
		return metav1.TableRow{
			Cells:  []any{r.Name, r.Spec.Target.Kind, target, age},
			Object: runtime.RawExtension{Object: r},
		}
	}

	switch t := object.(type) {
	case *extensions.ResourceHealthRule:
		table.Rows = append(table.Rows, rowFor(t))
	case *extensions.ResourceHealthRuleList:
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
