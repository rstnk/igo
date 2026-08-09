package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/rstnk/igo/internal/catalog"
)

func newListCmd(opts *options) *cobra.Command {
	var (
		namesOnly    bool
		embeddedOnly bool
	)

	cmd := &cobra.Command{
		Use:   "list [filter]",
		Short: "List available templates",
		Long: `List every template igo can generate from.

Templates marked "embedded" ship in the binary and work offline. "cached"
ones were fetched earlier and are also offline-ready. "fetch" ones need a
network call the first time you use them.

An optional filter keeps only templates whose name contains it.`,
		Example: `  igo list
  igo list jet
  igo list --names`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := ""
			if len(args) == 1 {
				filter = strings.ToLower(args[0])
			}
			return runList(cmd, opts, filter, namesOnly, embeddedOnly)
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&namesOnly, "names", false, "print bare names, one per line")
	flags.BoolVar(&embeddedOnly, "embedded", false, "list only templates embedded in the binary")
	return cmd
}

func runList(cmd *cobra.Command, opts *options, filter string, namesOnly, embeddedOnly bool) error {
	cat, err := opts.catalog()
	if err != nil {
		return err
	}
	listings, err := cat.List()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	matched := 0
	writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if !namesOnly {
		fmt.Fprintln(writer, "NAME\tSOURCE")
	}

	for _, item := range listings {
		if embeddedOnly && !item.Embedded {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(item.Path), filter) {
			continue
		}
		matched++
		if namesOnly {
			fmt.Fprintln(out, item.Name)
			continue
		}
		fmt.Fprintf(writer, "%s\t%s\n", item.Name, availability(item))
	}
	if err := writer.Flush(); err != nil {
		return err
	}

	if namesOnly {
		return nil
	}
	if matched == 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "no templates match %q\n", filter)
		return nil
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "\n%d %s", matched, plural(matched, "template"))
	if !cat.FreshIndex() {
		fmt.Fprint(cmd.ErrOrStderr(), " (from the build-time snapshot; run `igo update` to refresh)")
	}
	fmt.Fprintln(cmd.ErrOrStderr())
	return nil
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func availability(item catalog.Listing) string {
	switch {
	case item.Embedded:
		return "embedded"
	case item.Cached:
		return "cached"
	default:
		return "fetch"
	}
}
