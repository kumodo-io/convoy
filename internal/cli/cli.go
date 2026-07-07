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

// Package cli implements the `kubectl-convoy` plugin: pin, unpin, and sync
// commands over the convoy Release subresources.
package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	cliclient "github.com/kumodo-io/convoy/internal/cli/client"
	"github.com/kumodo-io/convoy/internal/cli/release"
)

// NewCommand builds the root `kubectl convoy` command tree.
func NewCommand() *cobra.Command {
	f := &cliclient.ConfigFlags{}
	root := &cobra.Command{
		Use:   "kubectl-convoy",
		Short: "kubectl plugin for convoy releases",
		Long:  "Manage convoy Releases: pin/unpin a render and request a sync.",
		// Runtime errors shouldn't dump usage; usage errors still do.
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&f.Kubeconfig, "kubeconfig", "", "path to the kubeconfig file (defaults to standard discovery)")
	root.PersistentFlags().StringVar(&f.Context, "context", "", "the kubeconfig context to use")
	root.PersistentFlags().StringVarP(&f.Namespace, "namespace", "n", "", "namespace of the release (defaults to the context's namespace)")

	root.AddCommand(release.Command(f))
	return root
}

// Run executes the plugin with signal-cancelable context and returns an exit code.
func Run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := NewCommand().ExecuteContext(ctx); err != nil {
		// cobra already printed the error; just surface a non-zero code.
		return 1
	}
	return 0
}
