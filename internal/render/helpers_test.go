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
	"context"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/fetch"
)

const (
	dummyFile     = "app.yaml"
	configMapKind = "ConfigMap"
	nsProd        = "prod" // shared target-namespace fixture (helm + kustomize tests)
)

// fakeCoordinator stands in for the fetch coordinator: it returns a canned
// Result/error without touching a real source or store.
type fakeCoordinator struct {
	result fetch.Result
	err    error
}

func (f *fakeCoordinator) Ensure(context.Context, corev1alpha1.ReleaseSourceSpec, string) (fetch.Result, error) {
	return f.result, f.err
}
