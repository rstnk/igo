// Command vendor-templates snapshots github/gitignore into templates/data.
//
// It writes two things: the .gitignore files for igo's embedded core set,
// and index.txt, the full list of template paths upstream offers. The index
// is what lets `igo list` and typo suggestions work without a network call.
//
// Run it via `go generate ./templates` or `make vendor`.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	treeURL = "https://api.github.com/repos/github/gitignore/git/trees/main?recursive=1"
	rawBase = "https://raw.githubusercontent.com/github/gitignore"
	ext     = ".gitignore"
)

// core lists the upstream paths baked into the binary. Files land in
// templates/data under their base name, so these must not collide.
var core = []string{
	"Go",
	"Python",
	"R",
	"Global/macOS",
	"Global/Linux",
	"Global/Windows",
	"Global/VisualStudioCode",
	"Global/JetBrains",
	"Global/Zed",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "vendor-templates:", err)
		os.Exit(1)
	}
}

func run() error {
	outDir := "data"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", outDir, err)
	}

	sha, paths, err := fetchIndex()
	if err != nil {
		return err
	}
	if err := writeIndex(filepath.Join(outDir, "index.txt"), sha, paths); err != nil {
		return err
	}
	fmt.Printf("index.txt: %d templates at %s\n", len(paths), sha[:12])

	available := make(map[string]bool, len(paths))
	for _, p := range paths {
		available[p] = true
	}
	for _, p := range core {
		if !available[p] {
			return fmt.Errorf("core template %q no longer exists upstream", p)
		}
		if err := fetchTemplate(sha, p, outDir); err != nil {
			return err
		}
		fmt.Printf("embedded %s\n", p)
	}

	// embedded.txt maps the flat files above back to their upstream paths,
	// so resolution never has to guess which path a base name came from.
	manifest := strings.Join(core, "\n") + "\n"
	dest := filepath.Join(outDir, "embedded.txt")
	if err := os.WriteFile(dest, []byte(manifest), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", dest, err)
	}
	return nil
}

type treeResponse struct {
	SHA  string `json:"sha"`
	Tree []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	} `json:"tree"`
	Truncated bool `json:"truncated"`
}

// fetchIndex returns the commit SHA of upstream main and every template
// path in it, with the .gitignore suffix stripped.
func fetchIndex() (string, []string, error) {
	body, err := get(treeURL)
	if err != nil {
		return "", nil, fmt.Errorf("listing upstream tree: %w", err)
	}
	var tree treeResponse
	if err := json.Unmarshal(body, &tree); err != nil {
		return "", nil, fmt.Errorf("parsing upstream tree: %w", err)
	}
	if tree.Truncated {
		return "", nil, fmt.Errorf("upstream tree response was truncated")
	}

	var paths []string
	for _, entry := range tree.Tree {
		if entry.Type == "blob" && strings.HasSuffix(entry.Path, ext) {
			paths = append(paths, strings.TrimSuffix(entry.Path, ext))
		}
	}
	if len(paths) == 0 {
		return "", nil, fmt.Errorf("upstream tree contained no %s files", ext)
	}
	sort.Strings(paths)
	return tree.SHA, paths, nil
}

func writeIndex(dest, sha string, paths []string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# github/gitignore @ %s\n", sha)
	fmt.Fprintf(&b, "# vendored %s\n", time.Now().UTC().Format(time.RFC3339))
	for _, p := range paths {
		b.WriteString(p)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(dest, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", dest, err)
	}
	return nil
}

// fetchTemplate downloads one template pinned to sha, so a snapshot always
// matches the index it shipped with.
func fetchTemplate(sha, tmplPath, outDir string) error {
	body, err := get(fmt.Sprintf("%s/%s/%s%s", rawBase, sha, tmplPath, ext))
	if err != nil {
		return fmt.Errorf("fetching %s: %w", tmplPath, err)
	}
	dest := filepath.Join(outDir, path.Base(tmplPath)+ext)
	if err := os.WriteFile(dest, body, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", dest, err)
	}
	return nil
}

func get(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}
