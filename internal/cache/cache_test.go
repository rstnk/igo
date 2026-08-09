package cache

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	c, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestPutGetRoundTrip(t *testing.T) {
	c := newTestCache(t)
	fetched := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	want := []byte("target/\n")

	if err := c.Put("Rust", want, Entry{URL: "https://example.test/Rust.gitignore", FetchedAt: fetched}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, entry, err := c.Get("Rust")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("content = %q, want %q", got, want)
	}
	if entry.Path != "Rust" {
		t.Errorf("Path = %q, want %q", entry.Path, "Rust")
	}
	if entry.URL != "https://example.test/Rust.gitignore" {
		t.Errorf("URL = %q", entry.URL)
	}
	if !entry.FetchedAt.Equal(fetched) {
		t.Errorf("FetchedAt = %v, want %v", entry.FetchedAt, fetched)
	}
}

func TestGetMiss(t *testing.T) {
	c := newTestCache(t)
	if _, _, err := c.Get("Rust"); !errors.Is(err, ErrMiss) {
		t.Errorf("err = %v, want ErrMiss", err)
	}
}

func TestPutNestedPath(t *testing.T) {
	c := newTestCache(t)
	if err := c.Put("community/AWS/SAM", []byte("x\n"), Entry{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir(), "community", "AWS", "SAM.gitignore")); err != nil {
		t.Errorf("expected nested file: %v", err)
	}
	if _, _, err := c.Get("community/AWS/SAM"); err != nil {
		t.Errorf("Get: %v", err)
	}
}

func TestRejectsPathEscape(t *testing.T) {
	c := newTestCache(t)
	for _, path := range []string{"", "../evil", "a/../../evil", "/etc/passwd"} {
		t.Run(path, func(t *testing.T) {
			if err := c.Put(path, []byte("x"), Entry{}); err == nil {
				t.Errorf("Put(%q) succeeded, want an error", path)
			}
			if _, _, err := c.Get(path); err == nil {
				t.Errorf("Get(%q) succeeded, want an error", path)
			}
		})
	}
}

func TestGetSurvivesMissingMetadata(t *testing.T) {
	c := newTestCache(t)
	if err := c.Put("Rust", []byte("target/\n"), Entry{URL: "u"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := os.Remove(filepath.Join(c.Dir(), "Rust.gitignore.json")); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	got, entry, err := c.Get("Rust")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "target/\n" {
		t.Errorf("content = %q", got)
	}
	if entry.Path != "Rust" {
		t.Errorf("Path = %q, want the requested path filled in", entry.Path)
	}
}

func TestIndexRoundTrip(t *testing.T) {
	c := newTestCache(t)
	if _, err := c.Index(); !errors.Is(err, ErrMiss) {
		t.Errorf("empty cache Index() err = %v, want ErrMiss", err)
	}

	want := []string{"Go", "Global/Vim", "community/Racket"}
	if err := c.PutIndex(want, "github/gitignore"); err != nil {
		t.Fatalf("PutIndex: %v", err)
	}
	got, err := c.Index()
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("Index() = %v, want %v", got, want)
	}
}

func TestEntries(t *testing.T) {
	c := newTestCache(t)
	if entries, err := c.Entries(); err != nil || len(entries) != 0 {
		t.Fatalf("empty cache: entries = %v, err = %v", entries, err)
	}

	for _, path := range []string{"Rust", "community/AWS/SAM"} {
		if err := c.Put(path, []byte("x\n"), Entry{URL: "u"}); err != nil {
			t.Fatalf("Put(%s): %v", path, err)
		}
	}
	// The index must not be mistaken for a cached template.
	if err := c.PutIndex([]string{"Rust"}, "src"); err != nil {
		t.Fatalf("PutIndex: %v", err)
	}

	entries, err := c.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = e.Path
	}
	slices.Sort(paths)
	if want := []string{"Rust", "community/AWS/SAM"}; !slices.Equal(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
}

func TestRemove(t *testing.T) {
	c := newTestCache(t)
	if err := c.Put("Rust", []byte("x\n"), Entry{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := c.Remove("Rust"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, _, err := c.Get("Rust"); !errors.Is(err, ErrMiss) {
		t.Errorf("err = %v, want ErrMiss", err)
	}
	if err := c.Remove("Rust"); err != nil {
		t.Errorf("removing an absent entry should succeed, got %v", err)
	}
}

func TestClear(t *testing.T) {
	c := newTestCache(t)
	if err := c.Put("Rust", []byte("x\n"), Entry{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := c.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, _, err := c.Get("Rust"); !errors.Is(err, ErrMiss) {
		t.Errorf("err = %v, want ErrMiss", err)
	}
}

func TestDefaultDirPrefersXDG(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/custom/cache")
	got, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if want := filepath.Join("/custom/cache", "igo"); got != want {
		t.Errorf("DefaultDir() = %q, want %q", got, want)
	}
}

func TestDefaultDirIgnoresRelativeXDG(t *testing.T) {
	// The spec calls XDG_CACHE_HOME absolute; a relative value would put
	// the cache wherever the user happened to be standing.
	t.Setenv("XDG_CACHE_HOME", "relative/path")
	got, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if got == filepath.Join("relative/path", "igo") {
		t.Errorf("DefaultDir() honoured a relative XDG_CACHE_HOME: %q", got)
	}
}
