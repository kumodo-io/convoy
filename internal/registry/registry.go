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

// Package registry is an umbrella package whose sub-packages provide etcd-backed
// REST storage for the convoy API types, grouped as {api-group}/{api-type}.
package registry

import (
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
)

// REST is the shared RESTStorage type for convoy resources, backed by etcd via
// the generic registry Store. Per-type sub-packages build a Store and wrap it
// in a REST.
type REST struct {
	*genericregistry.Store
}
