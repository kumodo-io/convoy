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
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	"github.com/kumodo-io/convoy/internal/auth"
	"github.com/kumodo-io/convoy/internal/fetch"
)

// resolveValues turns the ordered Helm values sources into ordered raw-YAML
// layers (later layers override earlier ones at render time). A nil error with
// fewer layers than entries means optional sources were absent. Errors are
// transient (missing ConfigMap/Secret/source — requeue) unless joined with
// ErrUnsupported (terminal config problem). gitRepository sources are fetched
// through the coordinator.
func resolveValues(ctx context.Context, entries []corev1alpha1.ValuesSourceSpec, namespace string, resolver auth.SecretResolver, coordinator fetch.Coordinator) ([][]byte, error) {
	var layers [][]byte
	for i, e := range entries {
		switch e.Type {
		case corev1alpha1.ValuesSourceTypeInline:
			if e.Inline == nil || len(e.Inline.Raw) == 0 {
				return nil, errors.Join(ErrUnsupported, fmt.Errorf("values[%d]: inline block is empty", i))
			}
			layers = append(layers, e.Inline.Raw)

		case corev1alpha1.ValuesSourceTypeConfigMap:
			if e.ConfigMap == nil {
				return nil, errors.Join(ErrUnsupported, fmt.Errorf("values[%d]: configMap block is missing", i))
			}
			data, ok, err := configMapValue(ctx, resolver, namespace, e.ConfigMap)
			if err != nil {
				return nil, fmt.Errorf("values[%d]: %w", i, err)
			}
			if ok {
				layers = append(layers, data)
			}

		case corev1alpha1.ValuesSourceTypeSecret:
			if e.Secret == nil {
				return nil, errors.Join(ErrUnsupported, fmt.Errorf("values[%d]: secret block is missing", i))
			}
			data, ok, err := secretValue(ctx, resolver, namespace, e.Secret)
			if err != nil {
				return nil, fmt.Errorf("values[%d]: %w", i, err)
			}
			if ok {
				layers = append(layers, data)
			}

		case corev1alpha1.ValuesSourceTypeGitRepository:
			if e.GitRepository == nil || e.GitRepository.ReleaseGitRepositorySpec == nil {
				return nil, errors.Join(ErrUnsupported, fmt.Errorf("values[%d]: gitRepository block is incomplete", i))
			}
			data, err := gitRepositoryValues(ctx, coordinator, e.GitRepository, namespace)
			if err != nil {
				return nil, fmt.Errorf("values[%d]: %w", i, err)
			}
			layers = append(layers, data...)

		default:
			return nil, errors.Join(ErrUnsupported, fmt.Errorf("values[%d]: unknown values source type %q", i, e.Type))
		}
	}
	return layers, nil
}

// gitRepositoryValues fetches the values source through the coordinator and reads
// the listed paths in order. A non-ready source (terminal) is reported as
// ErrUnsupported; fetch errors are transient.
func gitRepositoryValues(ctx context.Context, coordinator fetch.Coordinator, spec *corev1alpha1.ValuesSourceGitRepositorySpec, namespace string) ([][]byte, error) {
	res, err := coordinator.Ensure(ctx, corev1alpha1.ReleaseSourceSpec{
		Type:          corev1alpha1.ReleaseSourceTypeGitRepository,
		GitRepository: spec.ReleaseGitRepositorySpec,
	}, namespace)
	if err != nil {
		return nil, err // transient (requeue)
	}
	if !res.Ready {
		return nil, errors.Join(ErrUnsupported, fmt.Errorf("values source %s not ready: %s", spec.Name, res.Message))
	}

	var out [][]byte
	for _, p := range spec.Paths {
		data, ok := res.Artifact.Files[strings.TrimPrefix(p, "/")]
		if !ok {
			return nil, fmt.Errorf("values file %q not found in source %s@%s", p, spec.Name, res.Revision)
		}
		out = append(out, data)
	}
	return out, nil
}

// configMapValue reads sel.Key from the named ConfigMap in namespace. ok is false
// when an optional source (or its key) is absent.
func configMapValue(ctx context.Context, resolver auth.SecretResolver, namespace string, sel *corev1.ConfigMapKeySelector) ([]byte, bool, error) {
	cm, err := resolver.ConfigMap(ctx, namespace, sel.Name)
	if apierrors.IsNotFound(err) {
		if isOptional(sel.Optional) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("configMap %s/%s not found", namespace, sel.Name) // transient
	}
	if err != nil {
		return nil, false, err
	}
	v, ok := cm.Data[sel.Key]
	if !ok {
		if isOptional(sel.Optional) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("configMap %s/%s has no key %q", namespace, sel.Name, sel.Key)
	}
	return []byte(v), true, nil
}

// secretValue reads sel.Key from the named Secret in namespace. ok is false when
// an optional source (or its key) is absent.
func secretValue(ctx context.Context, resolver auth.SecretResolver, namespace string, sel *corev1.SecretKeySelector) ([]byte, bool, error) {
	s, err := resolver.Secret(ctx, namespace, sel.Name)
	if apierrors.IsNotFound(err) {
		if isOptional(sel.Optional) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("secret %s/%s not found", namespace, sel.Name) // transient
	}
	if err != nil {
		return nil, false, err
	}
	v, ok := s.Data[sel.Key]
	if !ok {
		if isOptional(sel.Optional) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("secret %s/%s has no key %q", namespace, sel.Name, sel.Key)
	}
	return v, true, nil
}

func isOptional(b *bool) bool { return b != nil && *b }
