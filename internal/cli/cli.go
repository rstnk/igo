// Package cli wires igo's commands together.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rstnk/igo/internal/cache"
	"github.com/rstnk/igo/internal/catalog"
	"github.com/rstnk/igo/internal/upstream"
)

// Version is the build version, overridable with
// -ldflags "-X github.com/rstnk/igo/internal/cli.Version=v1.2.3".
var Version = "dev"

// options holds the settings shared by every command.
type options struct {
	cacheDir string
	offline  bool
	// client overrides the upstream endpoints; tests point it at a
	// local server, and a nil value means the real GitHub.
	client *upstream.Client
}

// catalog builds a Catalog from the current options.
func (o *options) catalog() (*catalog.Catalog, error) {
	store, err := cache.New(o.cacheDir)
	if err != nil {
		return nil, err
	}
	return catalog.New(catalog.Options{Cache: store, Client: o.client, Offline: o.offline})
}

// New returns igo's root command with its subcommands attached.
func New() *cobra.Command {
	return newRoot(&options{})
}

func newRoot(opts *options) *cobra.Command {
	root := newGenerateCmd(opts)
	root.Version = Version
	root.SetVersionTemplate("igo {{.Version}}\n")
	root.SilenceUsage = true
	root.SilenceErrors = true

	flags := root.PersistentFlags()
	flags.BoolVar(&opts.offline, "offline", false, "fail instead of fetching templates over the network")
	flags.StringVar(&opts.cacheDir, "cache-dir", "", "override the template cache directory")

	root.AddCommand(newListCmd(opts), newUpdateCmd(opts))
	return root
}

// Execute runs igo and returns the process exit code.
func Execute() int {
	root := New()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(root.ErrOrStderr(), "igo:", err)
		return 1
	}
	return 0
}
