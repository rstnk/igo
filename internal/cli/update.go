package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

func newUpdateCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Refresh the template list and cached templates from GitHub",
		Long: `Refresh igo's view of github/gitignore.

This re-reads the list of available templates and re-downloads every
template already in the cache, so later runs stay offline-capable with
current content. Templates embedded in the binary are unaffected; they
change only when igo itself is rebuilt.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cat, err := opts.catalog()
			if err != nil {
				return err
			}

			result, err := cat.Update(cmd.Context())
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%d %s available\n", result.Templates, plural(result.Templates, "template"))
			report(out, "added", result.Added)
			report(out, "removed upstream", result.Removed)
			if result.Refetched > 0 {
				fmt.Fprintf(out, "re-fetched %d cached %s\n", result.Refetched, plural(result.Refetched, "template"))
			}
			report(out, "dropped from cache", result.Dropped)
			return nil
		},
	}
}

// report prints a labelled list, abbreviating anything long.
func report(out io.Writer, label string, items []string) {
	if len(items) == 0 {
		return
	}
	const shown = 10
	summary := strings.Join(items[:min(len(items), shown)], ", ")
	if len(items) > shown {
		summary += fmt.Sprintf(", and %d more", len(items)-shown)
	}
	fmt.Fprintf(out, "%s (%d): %s\n", label, len(items), summary)
}
