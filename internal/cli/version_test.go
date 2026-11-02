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

package cli

import (
	"bytes"
	"runtime/debug"
	"testing"
)

func buildInfo(v string) func() (*debug.BuildInfo, bool) {
	return func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: v}}, true
	}
}

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name    string
		stamped string
		info    func() (*debug.BuildInfo, bool)
		want    string
	}{
		{name: "stamped wins", stamped: "v0.10.0", info: buildInfo("v0.9.0"), want: "v0.10.0"},
		{name: "go install version", info: buildInfo("v0.9.0"), want: "v0.9.0"},
		{name: "local build", info: buildInfo(develVersion), want: develVersion},
		{name: "empty module version", info: buildInfo(""), want: develVersion},
		{name: "no build info", info: func() (*debug.BuildInfo, bool) { return nil, false }, want: develVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveVersion(tt.stamped, tt.info); got != tt.want {
				t.Errorf("resolveVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersionCommand(t *testing.T) {
	old := version
	version = "v1.2.3"
	t.Cleanup(func() { version = old })

	root := NewCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("version: %v", err)
	}
	if got, want := out.String(), "kubectl-convoy v1.2.3\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}
