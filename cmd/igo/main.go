// Command igo generates .gitignore files from github/gitignore templates.
package main

import (
	"os"

	"github.com/rstnk/igo/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
