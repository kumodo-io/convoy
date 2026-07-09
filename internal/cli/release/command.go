package release

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	cliclient "github.com/kumodo-io/convoy/internal/cli/client"
	"github.com/kumodo-io/convoy/internal/diffresult"
)

// Command groups the release-scoped verbs (pin/unpin/sync). Grouping by
// resource keeps a resource's operations together and leaves room for sibling
// resource groups (e.g. `cluster sync`) as they land.
func Command(f *cliclient.ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "release",
		Short: "Manage a release: pin/unpin a render, request a sync",
	}
	cmd.AddCommand(
		newReleasePinCmd(f),
		newReleaseUnpinCmd(f),
		newReleaseSyncCmd(f),
		newReleasePreviewCmd(f),
		newReleaseDiffCmd(f),
	)
	return cmd
}

func newReleasePinCmd(f *cliclient.ConfigFlags) *cobra.Command {
	var renderRef string
	cmd := &cobra.Command{
		Use:   "pin <name>",
		Short: "Pin a release to a render, withholding newer renders until unpinned",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, namespace, err := f.Client()
			if err != nil {
				return err
			}
			return runPin(cmd.Context(), cmd.OutOrStdout(), client, namespace, args[0], renderRef)
		},
	}
	cmd.Flags().StringVar(&renderRef, "render-ref", "", "render to pin to; defaults to the current successful render (freeze)")
	return cmd
}

func newReleaseUnpinCmd(f *cliclient.ConfigFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "unpin <name>",
		Short: "Clear a release's pin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, namespace, err := f.Client()
			if err != nil {
				return err
			}
			return runUnpin(cmd.Context(), cmd.OutOrStdout(), client, namespace, args[0])
		},
	}
}

func newReleaseSyncCmd(f *cliclient.ConfigFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "sync <name>",
		Short: "Request an immediate reconcile of a release (and apply, in manual sync mode)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, namespace, err := f.Client()
			if err != nil {
				return err
			}
			return runSync(cmd.Context(), cmd.OutOrStdout(), client, namespace, args[0])
		},
	}
}

func newReleasePreviewCmd(f *cliclient.ConfigFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "preview <name>",
		Short: "Show the rendered manifests that would be applied for a release",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, namespace, err := f.Client()
			if err != nil {
				return err
			}
			return runPreview(cmd.Context(), cmd.OutOrStdout(), client, namespace, args[0])
		},
	}
}

func newReleaseDiffCmd(f *cliclient.ConfigFlags) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "diff <name>",
		Short: "Show how a release's live state differs from its desired render",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, namespace, err := f.Client()
			if err != nil {
				return err
			}
			return runDiff(cmd.Context(), cmd.OutOrStdout(), client, namespace, args[0], output)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output format: empty for human-readable, or 'json' for the raw result")
	return cmd
}

// runPin resolves the pin target (defaulting to the current successful render)
// and pins the release.
func runPin(ctx context.Context, out io.Writer, client cliclient.Client, namespace, name, renderRef string) error {
	if renderRef == "" {
		rel, err := client.GetRelease(ctx, namespace, name)
		if err != nil {
			return err
		}
		if rel.Status.LastSuccessful == nil || rel.Status.LastSuccessful.RenderRef == "" {
			return fmt.Errorf("release %s/%s has no successful render to freeze; specify --render-ref", namespace, name)
		}
		renderRef = rel.Status.LastSuccessful.RenderRef
	}

	if _, err := client.Pin(ctx, namespace, name, renderRef); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "pinned %s/%s to render %s\n", namespace, name, renderRef); err != nil {
		return err
	}
	return nil
}

func runUnpin(ctx context.Context, out io.Writer, client cliclient.Client, namespace, name string) error {
	if _, err := client.Unpin(ctx, namespace, name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "unpinned %s/%s\n", namespace, name); err != nil {
		return err
	}
	return nil
}

func runSync(ctx context.Context, out io.Writer, client cliclient.Client, namespace, name string) error {
	if _, err := client.Sync(ctx, namespace, name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "requested sync for %s/%s\n", namespace, name); err != nil {
		return err
	}
	return nil
}

func runPreview(ctx context.Context, out io.Writer, client cliclient.Client, namespace, name string) error {
	manifests, err := client.Preview(ctx, namespace, name)
	if err != nil {
		return err
	}
	if _, err := out.Write(manifests); err != nil {
		return err
	}
	return nil
}

func runDiff(ctx context.Context, out io.Writer, client cliclient.Client, namespace, name, output string) error {
	raw, err := client.Diff(ctx, namespace, name)
	if err != nil {
		return err
	}
	if output == "json" {
		_, err := out.Write(raw)
		return err
	}

	var result diffresult.Result
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("decoding diff result: %w", err)
	}
	return renderDiff(out, &result)
}

// renderDiff writes a human-readable summary of the diff: one line per object
// with its status, then the field changes for modified objects.
func renderDiff(out io.Writer, result *diffresult.Result) error {
	if !result.HasDrift() {
		_, err := fmt.Fprintln(out, "in sync")
		return err
	}
	for _, o := range result.Objects {
		if o.Status == diffresult.StatusInSync {
			continue
		}
		if _, err := fmt.Fprintf(out, "%s %s/%s: %s\n", o.Kind, o.Namespace, o.Name, o.Status); err != nil {
			return err
		}
		for _, ch := range o.Changes {
			var line string
			switch ch.Op {
			case "add":
				line = fmt.Sprintf("  + %s: %v", ch.Path, ch.Desired)
			case "remove":
				line = fmt.Sprintf("  - %s: %v", ch.Path, ch.Live)
			default: // replace
				line = fmt.Sprintf("  ~ %s: %v -> %v", ch.Path, ch.Live, ch.Desired)
			}
			if _, err := fmt.Fprintln(out, line); err != nil {
				return err
			}
		}
	}
	return nil
}
