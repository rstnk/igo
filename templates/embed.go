// Package templates holds the offline core: a snapshot of the most-used
// github/gitignore templates, index.txt listing everything upstream offers,
// and embedded.txt naming which upstream paths the snapshot covers.
// Refresh all three with `go generate ./templates`.
package templates

import (
	"embed"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"
)

//go:generate go run ../tools/vendor-templates data

//go:embed data
var data embed.FS

const ext = ".gitignore"

// Files exposes the embedded data directory rooted at its contents.
var Files = mustSub()

func mustSub() fs.FS {
	sub, err := fs.Sub(data, "data")
	if err != nil {
		panic(err)
	}
	return sub
}

var (
	index    = sync.OnceValue(func() []string { return readList("index.txt") })
	embedded = sync.OnceValue(func() []string { return readList("embedded.txt") })
)

// Index returns every template path known upstream at snapshot time, with
// the .gitignore suffix stripped (e.g. "Go", "Global/macOS").
func Index() []string {
	return slices.Clone(index())
}

// Embedded returns the upstream paths baked into the binary, which are
// usable with no network access.
func Embedded() []string {
	return slices.Clone(embedded())
}

// Read returns the embedded template for an upstream path. It reports
// false for any path outside the snapshot.
func Read(upstreamPath string) (string, bool) {
	if !slices.Contains(embedded(), upstreamPath) {
		return "", false
	}
	// The snapshot stores files flat under their base name; embedded.txt
	// guarantees those base names are unique.
	content, err := fs.ReadFile(Files, path.Base(upstreamPath)+ext)
	if err != nil {
		return "", false
	}
	return string(content), true
}

// readList parses a newline-separated list file, skipping # comments.
func readList(name string) []string {
	raw, err := fs.ReadFile(Files, name)
	if err != nil {
		panic("templates: " + err.Error())
	}
	var out []string
	for line := range strings.Lines(string(raw)) {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}
