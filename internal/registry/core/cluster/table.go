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

package cluster

import (
	"context"
	"fmt"
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

// ConvertToTable renders Cluster objects as a kubectl-style table.
func (tableConvertor) ConvertToTable(_ context.Context, object runtime.Object, _ runtime.Object) (*metav1.Table, error) {
	table := &metav1.Table{
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "Name", Type: helper.StringValueString, Format: "name", Description: "Name of the Cluster"},
			{Name: "Mode", Type: helper.StringValueString, Description: "Connection mode (agentless or agent)"},
			{Name: "Reachable", Type: helper.StringValueString, Description: "Whether the cluster is reachable"},
			{Name: "Version", Type: helper.StringValueString, Description: "Discovered Kubernetes version"},
			{Name: "Age", Type: helper.StringValueDate, Description: "Time since the Cluster was created"},
		},
	}

	rowFor := func(c *core.Cluster) metav1.TableRow {
		age := "<unknown>"
		if !c.CreationTimestamp.IsZero() {
			age = duration.HumanDuration(time.Since(c.CreationTimestamp.Time))
		}
		reachable := "Unknown"
		if c.Status.ObservedGeneration != 0 {
			reachable = fmt.Sprintf("%t", c.Status.Reachable)
		}
		return metav1.TableRow{
			Cells:  []any{c.Name, string(c.Spec.ConnectionMode), reachable, c.Status.DiscoveredVersion, age},
			Object: runtime.RawExtension{Object: c},
		}
	}

	switch t := object.(type) {
	case *core.Cluster:
		table.Rows = append(table.Rows, rowFor(t))
	case *core.ClusterList:
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
