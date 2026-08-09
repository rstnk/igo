package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rstnk/igo/internal/catalog"
	"github.com/rstnk/igo/internal/merge"
)

const defaultOutput = ".gitignore"

type generateFlags struct {
	output string
	stdout bool
	force  bool
}

func newGenerateCmd(opts *options) *cobra.Command {
	var gen generateFlags

	cmd := &cobra.Command{
		Use:   "igo <template>...",
		Short: "Generate a .gitignore from github/gitignore templates",
		Long: `igo builds a .gitignore by combining templates from github/gitignore.

Templates are listed explicitly, in the order you want them to appear.
Nothing is added for you: no OS or editor defaults, no environment
detection. Common templates ship inside the binary, so they work with no
network; anything else is fetched once and cached.`,
		Example: `  igo go macos vscode
  igo python --stdout
  igo rust jetbrains -o build/.gitignore`,
		Args:          cobra.MinimumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGenerate(cmd, opts, &gen, args)
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&gen.output, "output", "o", defaultOutput, "path to write")
	flags.BoolVar(&gen.stdout, "stdout", false, "print to stdout instead of writing a file")
	flags.BoolVar(&gen.force, "force", false, "overwrite an existing file without asking")
	return cmd
}

func runGenerate(cmd *cobra.Command, opts *options, gen *generateFlags, names []string) error {
	cat, err := opts.catalog()
	if err != nil {
		return err
	}

	loaded, err := cat.LoadAll(cmd.Context(), names)
	if err != nil {
		return err
	}

	blocks := make([]merge.Block, len(loaded))
	for i, tmpl := range loaded {
		blocks[i] = merge.Block{Name: tmpl.Name, Content: tmpl.Content}
	}
	content := merge.Merge(blocks)

	if gen.stdout {
		_, err := fmt.Fprint(cmd.OutOrStdout(), content)
		return err
	}

	written, err := writeOutput(cmd, gen, content)
	if err != nil || !written {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s (%s)\n", gen.output, summarise(loaded))
	return nil
}

// writeOutput writes content to the target path, reporting whether it did.
// An identical existing file is left alone; a differing one needs --force
// or, on a terminal, a yes.
func writeOutput(cmd *cobra.Command, gen *generateFlags, content string) (bool, error) {
	existing, err := os.ReadFile(gen.output)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return false, fmt.Errorf("reading %s: %w", gen.output, err)
	case string(existing) == content:
		fmt.Fprintf(cmd.ErrOrStderr(), "%s is already up to date\n", gen.output)
		return false, nil
	case gen.force:
	default:
		ok, err := confirmOverwrite(cmd, gen.output)
		if err != nil {
			return false, err
		}
		if !ok {
			fmt.Fprintln(cmd.ErrOrStderr(), "aborted")
			return false, nil
		}
	}

	if err := os.WriteFile(gen.output, []byte(content), 0o644); err != nil {
		return false, fmt.Errorf("writing %s: %w", gen.output, err)
	}
	return true, nil
}

// confirmOverwrite asks before replacing a file that differs. Without a
// terminal there is nobody to ask, so it fails and points at --force.
func confirmOverwrite(cmd *cobra.Command, path string) (bool, error) {
	in, ok := cmd.InOrStdin().(*os.File)
	if !ok || !isTerminal(in) {
		return false, fmt.Errorf("%s already exists with different content; pass --force to overwrite it", path)
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "%s already exists with different content. Overwrite? [y/N] ", path)
	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && answer == "" {
		return false, nil
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// summarise describes where the templates came from, e.g. "3 templates,
// 1 fetched".
func summarise(loaded []catalog.Template) string {
	counts := make(map[catalog.Source]int, 3)
	for _, tmpl := range loaded {
		counts[tmpl.Source]++
	}

	parts := make([]string, 0, 3)
	for _, source := range []catalog.Source{catalog.FromEmbedded, catalog.FromCache, catalog.FromNetwork} {
		if n := counts[source]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, source))
		}
	}
	return strings.Join(parts, ", ")
}
