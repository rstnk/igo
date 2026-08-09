// Package cache stores templates fetched from GitHub so repeat runs work
// offline. Each template is written under its upstream path with a sidecar
// .json recording where it came from and when, which is what a future
// staleness warning would read.
package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrMiss means the entry is not cached.
var ErrMiss = errors.New("not cached")

// Entry is the metadata stored beside a cached template.
type Entry struct {
	// Path is the upstream template path, suffix stripped (e.g. "Global/Vim").
	Path string `json:"path"`
	// URL is where the content was fetched from.
	URL string `json:"url"`
	// FetchedAt is when the fetch completed.
	FetchedAt time.Time `json:"fetched_at"`
}

// Cache is a directory of fetched templates. The zero value is not usable;
// build one with New.
type Cache struct {
	dir string
}

// New returns a cache rooted at dir. An empty dir uses DefaultDir.
func New(dir string) (*Cache, error) {
	if dir == "" {
		var err error
		if dir, err = DefaultDir(); err != nil {
			return nil, err
		}
	}
	return &Cache{dir: dir}, nil
}

// DefaultDir is $XDG_CACHE_HOME/igo when that variable is set, and the
// platform's own cache location otherwise.
func DefaultDir() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" && filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "igo"), nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locating cache directory: %w", err)
	}
	return filepath.Join(base, "igo"), nil
}

// Dir returns the cache root.
func (c *Cache) Dir() string { return c.dir }

const (
	ext      = ".gitignore"
	metaExt  = ".json"
	indexTxt = "index.txt"
)

// Get returns the cached content and metadata for an upstream template
// path, or ErrMiss if it is absent.
func (c *Cache) Get(path string) ([]byte, Entry, error) {
	file, err := c.templatePath(path)
	if err != nil {
		return nil, Entry{}, err
	}
	content, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, Entry{}, ErrMiss
	}
	if err != nil {
		return nil, Entry{}, fmt.Errorf("reading cached %s: %w", path, err)
	}

	// A readable template with unreadable metadata is still usable; the
	// content is what generation needs.
	var entry Entry
	if raw, err := os.ReadFile(file + metaExt); err == nil {
		_ = json.Unmarshal(raw, &entry)
	}
	if entry.Path == "" {
		entry.Path = path
	}
	return content, entry, nil
}

// Put stores content for an upstream template path along with its metadata.
func (c *Cache) Put(path string, content []byte, entry Entry) error {
	file, err := c.templatePath(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}
	if err := writeAtomic(file, content); err != nil {
		return err
	}

	entry.Path = path
	meta, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding metadata for %s: %w", path, err)
	}
	return writeAtomic(file+metaExt, append(meta, '\n'))
}

// Index returns the cached copy of the upstream template list, or ErrMiss
// if the cache has never been refreshed.
func (c *Cache) Index() ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(c.dir, indexTxt))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrMiss
	}
	if err != nil {
		return nil, fmt.Errorf("reading cached index: %w", err)
	}
	var paths []string
	for line := range strings.Lines(string(raw)) {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			paths = append(paths, line)
		}
	}
	if len(paths) == 0 {
		return nil, ErrMiss
	}
	return paths, nil
}

// PutIndex replaces the cached upstream template list.
func (c *Cache) PutIndex(paths []string, source string) error {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", source)
	fmt.Fprintf(&b, "# refreshed %s\n", time.Now().UTC().Format(time.RFC3339))
	for _, p := range paths {
		b.WriteString(p)
		b.WriteByte('\n')
	}
	return writeAtomic(filepath.Join(c.dir, indexTxt), []byte(b.String()))
}

// Entries lists the metadata of every cached template, sorted by path.
func (c *Cache) Entries() ([]Entry, error) {
	var entries []Entry
	err := filepath.WalkDir(c.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ext+metaExt) {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var entry Entry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		entries = append(entries, entry)
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanning cache: %w", err)
	}
	return entries, nil
}

// Remove deletes a cached template and its metadata. Absent entries are
// not an error.
func (c *Cache) Remove(path string) error {
	file, err := c.templatePath(path)
	if err != nil {
		return err
	}
	for _, f := range []string{file, file + metaExt} {
		if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("removing cached %s: %w", path, err)
		}
	}
	return nil
}

// Clear removes every cached template and the cached index.
func (c *Cache) Clear() error {
	if err := os.RemoveAll(c.dir); err != nil {
		return fmt.Errorf("clearing cache: %w", err)
	}
	return nil
}

// templatePath maps an upstream path to a file inside the cache, refusing
// anything that would escape the cache root.
func (c *Cache) templatePath(path string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(path))
	if path == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid template path %q", path)
	}
	return filepath.Join(c.dir, clean+ext), nil
}

// writeAtomic replaces dest via a temp file and rename, so an interrupted
// run never leaves a half-written template behind.
func writeAtomic(dest string, content []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".tmp*")
	if err != nil {
		return fmt.Errorf("creating temp file for %s: %w", dest, err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", dest, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", dest, err)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", dest, err)
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return fmt.Errorf("writing %s: %w", dest, err)
	}
	return nil
}
