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

// Package diffresult defines the wire types for a Release /diff response: the
// structured, per-object drift between the desired render and the live cluster.
// It is a dependency-free leaf package shared by the observer (which produces
// the result) and the kubectl-convoy CLI (which renders it), so neither imports
// the other.
package diffresult

// Status is a single object's drift state.
type Status string

const (
	// StatusInSync means the live object matches the desired render.
	StatusInSync Status = "InSync"
	// StatusModified means the live object differs from the desired render.
	StatusModified Status = "Modified"
	// StatusMissing means the desired object is absent from the cluster.
	StatusMissing Status = "Missing"
)

// Result is the diff for a whole Release: one entry per desired object. Extra or
// orphaned live objects (not in the desired render) are not reported — that
// needs inventory tracking and is out of scope for this diff.
type Result struct {
	Objects []ObjectDiff `json:"objects"`
}

// HasDrift reports whether any object differs from the desired render.
func (r *Result) HasDrift() bool {
	for i := range r.Objects {
		if r.Objects[i].Status != StatusInSync {
			return true
		}
	}
	return false
}

// ObjectDiff is the drift state of a single desired object, identified by its
// GVK + namespace/name.
type ObjectDiff struct {
	Group     string `json:"group,omitempty"`
	Version   string `json:"version"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`

	Status Status `json:"status"`
	// Changes lists the field-level differences; set only when Status is Modified.
	Changes []FieldChange `json:"changes,omitempty"`
}

// FieldChange is one field-level difference between live and desired, located by
// a JSON Pointer path.
type FieldChange struct {
	// Path is the RFC6901 JSON Pointer to the changed field.
	Path string `json:"path"`
	// Op is the change kind: "add", "replace", or "remove".
	Op string `json:"op"`
	// Live is the current value (set for "replace" and "remove").
	Live any `json:"live,omitempty"`
	// Desired is the target value (set for "add" and "replace").
	Desired any `json:"desired,omitempty"`
}
