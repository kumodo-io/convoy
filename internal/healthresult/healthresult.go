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

// Package healthresult defines the wire types for a Release /health response:
// the per-object health of a Release's managed resources. It is a dependency-free
// leaf package shared by the observer (which produces the result) and the
// kubectl-convoy CLI (which renders it), so neither imports the other.
package healthresult

// State is a single object's health verdict. It mirrors the internal
// health.State values so the two stay in lockstep, but is redeclared here to keep
// this package dependency-free.
type State string

const (
	StateHealthy     State = "Healthy"
	StateProgressing State = "Progressing"
	StateDegraded    State = "Degraded"
	StateUnknown     State = "Unknown"
)

// Result is the health of a whole Release: one entry per managed object.
type Result struct {
	Objects []ObjectHealth `json:"objects"`
}

// ObjectHealth is the health verdict of a single managed object, identified by
// its GVK + namespace/name.
type ObjectHealth struct {
	Group     string `json:"group,omitempty"`
	Version   string `json:"version"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`

	State State `json:"state"`
	// Message is human-readable context (a failing rule's message, "no applicable
	// health rules", or an assessment error).
	Message string `json:"message,omitempty"`
}

// Summary counts objects by state.
type Summary struct {
	Total       int32 `json:"total"`
	Healthy     int32 `json:"healthy"`
	Progressing int32 `json:"progressing"`
	Degraded    int32 `json:"degraded"`
	Unknown     int32 `json:"unknown"`
}

// Summarize tallies the objects by state.
func (r *Result) Summarize() Summary {
	s := Summary{Total: int32(len(r.Objects))}
	for i := range r.Objects {
		switch r.Objects[i].State {
		case StateHealthy:
			s.Healthy++
		case StateProgressing:
			s.Progressing++
		case StateDegraded:
			s.Degraded++
		case StateUnknown:
			s.Unknown++
		}
	}
	return s
}

// Healthy reports whether every managed object is Healthy.
func (r *Result) Healthy() bool {
	for i := range r.Objects {
		if r.Objects[i].State != StateHealthy {
			return false
		}
	}
	return true
}
