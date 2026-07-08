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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

// manifestsContent builds a manifests content spec over the given paths.
func manifestsContent(paths ...string) corev1alpha1.ReleaseContentSpec {
	return corev1alpha1.ReleaseContentSpec{
		Type:      corev1alpha1.ReleaseContentTypeManifests,
		Manifests: &corev1alpha1.ReleaseContentManifestsSpec{Paths: paths},
	}
}

// fakeResolver serves ConfigMaps/Secrets from in-memory maps keyed by name.
type fakeResolver struct {
	configMaps map[string]*corev1.ConfigMap
	secrets    map[string]*corev1.Secret
}

func (f fakeResolver) ConfigMap(_ context.Context, _, name string) (*corev1.ConfigMap, error) {
	if cm, ok := f.configMaps[name]; ok {
		return cm, nil
	}
	return nil, apierrors.NewNotFound(corev1.Resource("configmaps"), name)
}

func (f fakeResolver) Secret(_ context.Context, _, name string) (*corev1.Secret, error) {
	if s, ok := f.secrets[name]; ok {
		return s, nil
	}
	return nil, apierrors.NewNotFound(corev1.Resource("secrets"), name)
}
