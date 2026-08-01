package release

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	corev1alpha1 "github.com/kumodo-io/convoy/api/core/v1alpha1"
	cliclient "github.com/kumodo-io/convoy/internal/cli/client"
	"github.com/kumodo-io/convoy/internal/diffresult"
	"github.com/kumodo-io/convoy/internal/healthresult"
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
		newReleaseHealthCmd(f),
		newReleaseMutationsCmd(f),
		newReleaseLifecycleCmd(f),
	)
	return cmd
}

func newReleaseLifecycleCmd(f *cliclient.ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lifecycle",
		Short: "View or edit a release's lifecycle hooks",
	}
	get := &cobra.Command{
		Use:   "get <name>",
		Short: "Print a release's lifecycle hooks as YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, namespace, err := f.Client()
			if err != nil {
				return err
			}
			return runLifecycleGet(cmd.Context(), cmd.OutOrStdout(), client, namespace, args[0])
		},
	}

	var file, fromString string
	set := &cobra.Command{
		Use:   "set <name>",
		Short: "Replace a release's lifecycle hooks (from -f <file>, -f - for stdin, or --from-string)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, namespace, err := f.Client()
			if err != nil {
				return err
			}
			lifecycle, err := readLifecycle(file, fromString, cmd.InOrStdin())
			if err != nil {
				return err
			}
			return runLifecycleSet(cmd.Context(), cmd.OutOrStdout(), client, namespace, args[0], lifecycle)
		},
	}
	set.Flags().StringVarP(&file, "file", "f", "", "YAML/JSON file with the lifecycle hooks ('-' reads stdin)")
	set.Flags().StringVar(&fromString, "from-string", "", "inline YAML/JSON lifecycle hooks")

	cmd.AddCommand(get, set)
	return cmd
}

func newReleaseMutationsCmd(f *cliclient.ConfigFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mutations",
		Short: "View or edit a release's post-render mutation pipeline",
	}
	get := &cobra.Command{
		Use:   "get <name>",
		Short: "Print a release's mutation pipeline as YAML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, namespace, err := f.Client()
			if err != nil {
				return err
			}
			return runMutationsGet(cmd.Context(), cmd.OutOrStdout(), client, namespace, args[0])
		},
	}
	cmd.AddCommand(get, newReleaseMutationsWriteCmd(f, false), newReleaseMutationsWriteCmd(f, true))
	return cmd
}

// newReleaseMutationsWriteCmd builds `mutations set` (replace) or `mutations add`
// (append). Both take the pipeline from exactly one of `-f <file>`, `-f -`
// (stdin), or `--from-string`.
func newReleaseMutationsWriteCmd(f *cliclient.ConfigFlags, appendMode bool) *cobra.Command {
	use, short := "set <name>", "Replace a release's mutation pipeline"
	if appendMode {
		use, short = "add <name>", "Append to a release's mutation pipeline"
	}
	var file, fromString string
	cmd := &cobra.Command{
		Use:   use,
		Short: short + " (from -f <file>, -f - for stdin, or --from-string)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, namespace, err := f.Client()
			if err != nil {
				return err
			}
			mutations, err := readMutations(file, fromString, cmd.InOrStdin())
			if err != nil {
				return err
			}
			return runMutationsWrite(cmd.Context(), cmd.OutOrStdout(), client, namespace, args[0], mutations, appendMode)
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "YAML/JSON file with the list of mutations ('-' reads stdin)")
	cmd.Flags().StringVar(&fromString, "from-string", "", "inline YAML/JSON list of mutations")
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

func newReleaseHealthCmd(f *cliclient.ConfigFlags) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "health <name>",
		Short: "Show the health of a release's managed resources",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, namespace, err := f.Client()
			if err != nil {
				return err
			}
			return runHealth(cmd.Context(), cmd.OutOrStdout(), client, namespace, args[0], output)
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

func runHealth(ctx context.Context, out io.Writer, client cliclient.Client, namespace, name, output string) error {
	raw, err := client.Health(ctx, namespace, name)
	if err != nil {
		return err
	}
	if output == "json" {
		_, err := out.Write(raw)
		return err
	}

	var result healthresult.Result
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("decoding health result: %w", err)
	}
	return renderHealth(out, &result)
}

func runMutationsGet(ctx context.Context, out io.Writer, client cliclient.Client, namespace, name string) error {
	rel, err := client.GetRelease(ctx, namespace, name)
	if err != nil {
		return err
	}
	if len(rel.Mutations) == 0 {
		_, err := fmt.Fprintln(out, "no mutations")
		return err
	}
	b, err := yaml.Marshal(rel.Mutations)
	if err != nil {
		return err
	}
	_, err = out.Write(b)
	return err
}

// readMutations resolves the mutation list from exactly one of: `--from-string`,
// a file (`-f <path>`), or stdin (`-f -`).
func runLifecycleGet(ctx context.Context, out io.Writer, client cliclient.Client, namespace, name string) error {
	rel, err := client.GetRelease(ctx, namespace, name)
	if err != nil {
		return err
	}
	if rel.Lifecycle == nil {
		_, err := fmt.Fprintln(out, "no lifecycle hooks")
		return err
	}
	b, err := yaml.Marshal(rel.Lifecycle)
	if err != nil {
		return err
	}
	_, err = out.Write(b)
	return err
}

func runLifecycleSet(ctx context.Context, out io.Writer, client cliclient.Client, namespace, name string, lifecycle *corev1alpha1.ReleaseLifecycle) error {
	if _, err := client.SetLifecycle(ctx, namespace, name, lifecycle); err != nil {
		return err
	}
	_, err := fmt.Fprintf(out, "set lifecycle hooks on %s/%s\n", namespace, name)
	return err
}

// readLifecycle resolves the ReleaseLifecycle from exactly one of -f <file>,
// -f - (stdin), or --from-string.
func readLifecycle(file, fromString string, stdin io.Reader) (*corev1alpha1.ReleaseLifecycle, error) {
	if fromString != "" && file != "" {
		return nil, fmt.Errorf("--file and --from-string are mutually exclusive")
	}
	var raw []byte
	var err error
	switch {
	case fromString != "":
		raw = []byte(fromString)
	case file == "-":
		raw, err = io.ReadAll(stdin)
	case file != "":
		raw, err = os.ReadFile(file)
	default:
		return nil, fmt.Errorf("provide the lifecycle hooks via -f <file>, -f - (stdin), or --from-string")
	}
	if err != nil {
		return nil, err
	}
	lifecycle := &corev1alpha1.ReleaseLifecycle{}
	if err := yaml.Unmarshal(raw, lifecycle); err != nil {
		return nil, fmt.Errorf("parsing lifecycle hooks: %w", err)
	}
	return lifecycle, nil
}

func readMutations(file, fromString string, stdin io.Reader) ([]corev1alpha1.ReleaseMutation, error) {
	if fromString != "" && file != "" {
		return nil, fmt.Errorf("--file and --from-string are mutually exclusive")
	}
	var raw []byte
	var err error
	switch {
	case fromString != "":
		raw = []byte(fromString)
	case file == "-":
		raw, err = io.ReadAll(stdin)
	case file != "":
		raw, err = os.ReadFile(file)
	default:
		return nil, fmt.Errorf("provide the mutations via -f <file>, -f - (stdin), or --from-string")
	}
	if err != nil {
		return nil, err
	}
	var mutations []corev1alpha1.ReleaseMutation
	if err := yaml.Unmarshal(raw, &mutations); err != nil {
		return nil, fmt.Errorf("parsing mutations: %w", err)
	}
	return mutations, nil
}

// runMutationsWrite replaces (set) or appends to (add) the release's pipeline.
func runMutationsWrite(ctx context.Context, out io.Writer, client cliclient.Client, namespace, name string, mutations []corev1alpha1.ReleaseMutation, appendMode bool) error {
	final := mutations
	if appendMode {
		rel, err := client.GetRelease(ctx, namespace, name)
		if err != nil {
			return err
		}
		// Copy the existing slice before appending so we don't alias its backing array.
		final = append(append([]corev1alpha1.ReleaseMutation{}, rel.Mutations...), mutations...)
	}
	if _, err := client.SetMutations(ctx, namespace, name, final); err != nil {
		return err
	}
	if appendMode {
		_, err := fmt.Fprintf(out, "added %d mutation(s) to %s/%s (%d total)\n", len(mutations), namespace, name, len(final))
		return err
	}
	_, err := fmt.Fprintf(out, "set %d mutation(s) on %s/%s\n", len(final), namespace, name)
	return err
}

// renderHealth writes a human-readable per-object health summary.
func renderHealth(out io.Writer, result *healthresult.Result) error {
	if len(result.Objects) == 0 {
		_, err := fmt.Fprintln(out, "not assessed")
		return err
	}
	for _, o := range result.Objects {
		line := fmt.Sprintf("%s %s/%s: %s", o.Kind, o.Namespace, o.Name, o.State)
		if o.Message != "" {
			line += "  (" + o.Message + ")"
		}
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	return nil
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
