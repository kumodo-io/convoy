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
	"bytes"
	"fmt"
	"text/template"

	"github.com/google/cel-go/cel"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
)

// HookContext builds the templating/CEL context for a lifecycle hook: the same
// lowercase, credential-free projection mutations use, exposed as `.cluster`
// (Cluster.Spec.Metadata keys + `.cluster.name`) and `.release` (the Release's
// identity). Unlike mutations there is no `.object` — a hook is not scoped to a
// single rendered object. Both the Go-template workload rendering and the `when`
// CEL expression consume this map.
func HookContext(rel *corev1alpha1.Release, cluster *corev1alpha1.Cluster) map[string]any {
	clusterMeta, self := contextValues(rel, cluster)
	return map[string]any{ctxKeyCluster: clusterMeta, ctxKeyRelease: self}
}

// EvalHookIf evaluates a hook's `when` CEL guard against the hook context. An
// empty expression means "always run" (true). The expression must evaluate to a
// bool; a compile or type error is returned so the caller can skip the hook (the
// runner treats false/error as Skipped).
func EvalHookIf(when string, ctx map[string]any) (bool, error) {
	if when == "" {
		return true, nil
	}
	env, err := cel.NewEnv(
		cel.Variable(ctxKeyCluster, cel.MapType(cel.StringType, cel.StringType)),
		cel.Variable(ctxKeyRelease, cel.MapType(cel.StringType, cel.DynType)),
	)
	if err != nil {
		return false, fmt.Errorf("building hook CEL env: %w", err)
	}
	ast, issues := env.Compile(when)
	if issues != nil && issues.Err() != nil {
		return false, fmt.Errorf("compiling hook `when`: %w", issues.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		return false, fmt.Errorf("building hook `when` program: %w", err)
	}
	out, _, err := prg.Eval(ctx)
	if err != nil {
		return false, fmt.Errorf("evaluating hook `when`: %w", err)
	}
	b, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("hook `when` must evaluate to a bool, got %T", out.Value())
	}
	return b, nil
}

// RenderHookTemplate renders a hook workload document through Go templates with the
// hook context, so `{{ .cluster.region }}`-style interpolation in the user-authored
// template resolves. missingkey=zero matches the mutation/doc-template policy: an
// absent key renders empty rather than erroring.
func RenderHookTemplate(doc []byte, ctx map[string]any) ([]byte, error) {
	t, err := template.New("hook").Option("missingkey=zero").Parse(string(doc))
	if err != nil {
		return nil, fmt.Errorf("parsing hook template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, ctx); err != nil {
		return nil, fmt.Errorf("executing hook template: %w", err)
	}
	return buf.Bytes(), nil
}
