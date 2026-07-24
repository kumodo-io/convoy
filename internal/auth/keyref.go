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

package auth

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// ResolveSecretKey reads a single key from the Secret named by sel, in namespace.
// The Secret reference is namespace-local to the referring object.
func ResolveSecretKey(ctx context.Context, resolver SecretResolver, namespace string, sel corev1.SecretKeySelector) ([]byte, error) {
	if sel.Name == "" {
		return nil, fmt.Errorf("secret reference has no name")
	}
	s, err := resolver.Secret(ctx, namespace, sel.Name)
	if err != nil {
		return nil, fmt.Errorf("reading secret %s/%s: %w", namespace, sel.Name, err)
	}
	v, ok := s.Data[sel.Key]
	if !ok {
		return nil, fmt.Errorf("secret %s/%s has no key %q", namespace, sel.Name, sel.Key)
	}
	return v, nil
}

// ResolveConfigMapKey reads a single key from the ConfigMap named by sel, in
// namespace. The ConfigMap reference is namespace-local to the referring object.
func ResolveConfigMapKey(ctx context.Context, resolver SecretResolver, namespace string, sel corev1.ConfigMapKeySelector) ([]byte, error) {
	if sel.Name == "" {
		return nil, fmt.Errorf("configMap reference has no name")
	}
	cm, err := resolver.ConfigMap(ctx, namespace, sel.Name)
	if err != nil {
		return nil, fmt.Errorf("reading configMap %s/%s: %w", namespace, sel.Name, err)
	}
	if v, ok := cm.Data[sel.Key]; ok {
		return []byte(v), nil
	}
	if v, ok := cm.BinaryData[sel.Key]; ok {
		return v, nil
	}
	return nil, fmt.Errorf("configMap %s/%s has no key %q", namespace, sel.Name, sel.Key)
}
