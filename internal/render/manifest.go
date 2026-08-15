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

package render

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// MarshalManifest serializes a rendered object set into the bytes stored as a
// render blob: a JSON array of the objects. It is the on-the-wire format of a
// persisted render — written by the release controller on persist-then-apply and
// read back by the observer (historical diff) and the controller (rollback). It
// is a pure function of objects, so it pairs 1:1 with the render's content hash.
func MarshalManifest(objects []*unstructured.Unstructured) ([]byte, error) {
	return json.Marshal(objects)
}

// ParseManifest is the inverse of MarshalManifest: it reconstructs the object set
// from a stored render blob.
func ParseManifest(data []byte) ([]*unstructured.Unstructured, error) {
	var objects []*unstructured.Unstructured
	if err := json.Unmarshal(data, &objects); err != nil {
		return nil, err
	}
	return objects, nil
}
