package templates

import (
	"slices"
	"strings"
	"testing"
)

func TestIndexIsSortedAndUnique(t *testing.T) {
	index := Index()
	if len(index) == 0 {
		t.Fatal("Index() is empty")
	}
	if !slices.IsSorted(index) {
		t.Error("Index() is not sorted")
	}
	if len(slices.Compact(slices.Clone(index))) != len(index) {
		t.Error("Index() contains duplicates")
	}
	for _, path := range index {
		if strings.HasSuffix(path, ext) {
			t.Errorf("index entry %q still carries the %s suffix", path, ext)
		}
	}
}

// The snapshot must cover the templates the design doc promises offline.
func TestEmbeddedCoverage(t *testing.T) {
	want := []string{
		"Go", "Python", "R",
		"Global/macOS", "Global/Linux", "Global/Windows",
		"Global/VisualStudioCode", "Global/JetBrains", "Global/Zed",
	}
	got := Embedded()
	for _, path := range want {
		if !slices.Contains(got, path) {
			t.Errorf("%q is not embedded; Embedded() = %v", path, got)
		}
	}
}

func TestEmbeddedPathsExistUpstream(t *testing.T) {
	index := Index()
	for _, path := range Embedded() {
		if !slices.Contains(index, path) {
			t.Errorf("embedded template %q is not in the upstream index", path)
		}
	}
}

func TestReadEmbedded(t *testing.T) {
	for _, path := range Embedded() {
		content, ok := Read(path)
		if !ok {
			t.Errorf("Read(%q) reported it is not embedded", path)
			continue
		}
		if strings.TrimSpace(content) == "" {
			t.Errorf("Read(%q) returned no content", path)
		}
	}
}

func TestReadRejectsPathsOutsideTheSnapshot(t *testing.T) {
	// Files are stored flat by base name, so a same-named template from
	// another directory must not resolve to the embedded one.
	for _, path := range []string{"Rust", "community/Something/macOS", "macOS", "../Go"} {
		if _, ok := Read(path); ok {
			t.Errorf("Read(%q) returned content, want a miss", path)
		}
	}
}

// Base names must stay unique, since the snapshot stores files flat.
func TestEmbeddedBaseNamesAreUnique(t *testing.T) {
	seen := make(map[string]string)
	for _, path := range Embedded() {
		base := path[strings.LastIndex(path, "/")+1:]
		if other, ok := seen[base]; ok {
			t.Errorf("%q and %q share the base name %q", other, path, base)
		}
		seen[base] = path
	}
}
