package catalog

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/rstnk/igo/internal/cache"
	"github.com/rstnk/igo/internal/upstream"
	"github.com/rstnk/igo/templates"
)

// fakeUpstream serves a fixed set of templates over the same URL shapes as
// GitHub, so the fetch path is exercised without a network.
type fakeUpstream struct {
	server   *httptest.Server
	client   *upstream.Client
	contents map[string]string
	requests int
}

func newFakeUpstream(t *testing.T, contents map[string]string) *fakeUpstream {
	t.Helper()
	fake := &fakeUpstream{contents: contents}

	mux := http.NewServeMux()
	mux.HandleFunc("/git/trees/main", func(w http.ResponseWriter, r *http.Request) {
		var entries []string
		for path := range fake.contents {
			entries = append(entries, `{"path":"`+path+upstream.Ext+`","type":"blob"}`)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tree":[` + strings.Join(entries, ",") + `],"truncated":false}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fake.requests++
		path := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), upstream.Ext)
		content, ok := fake.contents[path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(content))
	})

	fake.server = httptest.NewServer(mux)
	t.Cleanup(fake.server.Close)
	fake.client = &upstream.Client{
		HTTP:    fake.server.Client(),
		APIBase: fake.server.URL,
		RawBase: fake.server.URL,
	}
	return fake
}

// newCatalog builds a Catalog whose template list is exactly index.
func newCatalog(t *testing.T, index []string, client *upstream.Client, offline bool) *Catalog {
	t.Helper()
	store, err := cache.New(t.TempDir())
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	if index != nil {
		if err := store.PutIndex(index, "test"); err != nil {
			t.Fatalf("PutIndex: %v", err)
		}
	}
	cat, err := New(Options{Cache: store, Client: client, Offline: offline})
	if err != nil {
		t.Fatalf("catalog.New: %v", err)
	}
	return cat
}

var testIndex = []string{
	"AL",
	"Global/AL",
	"Go",
	"Global/macOS",
	"Global/VisualStudioCode",
	"Global/JetBrains",
	"Python",
	"Rust",
	"community/BoxLang/ColdBox",
	"community/CFML/ColdBox",
	"community/Racket",
	"Racket",
}

func TestResolve(t *testing.T) {
	cat := newCatalog(t, testIndex, nil, true)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"exact name", "Go", "Go"},
		{"lowercase", "go", "Go"},
		{"uppercase", "RUST", "Rust"},
		{"alias", "vscode", "Global/VisualStudioCode"},
		{"alias for an IDE flavour", "goland", "Global/JetBrains"},
		{"surrounding space", "  python  ", "Python"},
		{"full path", "Global/macOS", "Global/macOS"},
		{"full path lowercased", "global/macos", "Global/macOS"},
		{"trailing extension", "Go.gitignore", "Go"},
		{"shallower path wins", "racket", "Racket"},
		{"root beats Global", "al", "AL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cat.Resolve(tt.input)
			if err != nil {
				t.Fatalf("Resolve(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("Resolve(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveAmbiguous(t *testing.T) {
	cat := newCatalog(t, testIndex, nil, true)

	_, err := cat.Resolve("ColdBox")
	var ambiguous *AmbiguousNameError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("err = %v, want AmbiguousNameError", err)
	}
	if len(ambiguous.Candidates) != 2 {
		t.Errorf("Candidates = %v, want both ColdBox templates", ambiguous.Candidates)
	}

	// The full path is the way out of the ambiguity.
	got, err := cat.Resolve("community/CFML/ColdBox")
	if err != nil || got != "community/CFML/ColdBox" {
		t.Errorf("Resolve(full path) = %q, %v", got, err)
	}
}

func TestResolveUnknownSuggests(t *testing.T) {
	cat := newCatalog(t, testIndex, nil, true)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"transposition", "pythno", "Python"},
		{"missing letter", "pyton", "Python"},
		{"substring", "jetbr", "JetBrains"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cat.Resolve(tt.input)
			var unknown *UnknownNameError
			if !errors.As(err, &unknown) {
				t.Fatalf("err = %v, want UnknownNameError", err)
			}
			if !containsString(unknown.Suggestions, tt.want) {
				t.Errorf("Suggestions = %v, want to include %q", unknown.Suggestions, tt.want)
			}
		})
	}
}

func TestResolveUnknownWithNoNearMatch(t *testing.T) {
	cat := newCatalog(t, testIndex, nil, true)

	_, err := cat.Resolve("qqqqzzzzwwww")
	var unknown *UnknownNameError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want UnknownNameError", err)
	}
	if len(unknown.Suggestions) != 0 {
		t.Errorf("Suggestions = %v, want none", unknown.Suggestions)
	}
	if !strings.Contains(err.Error(), "igo list") {
		t.Errorf("error should point at `igo list`, got %q", err.Error())
	}
}

func TestResolveEmpty(t *testing.T) {
	cat := newCatalog(t, testIndex, nil, true)
	if _, err := cat.Resolve("   "); err == nil {
		t.Error("Resolve(blank) succeeded, want an error")
	}
}

// Every alias must name a template that really exists in the snapshot,
// or the shortcut is a trap.
func TestAliasesResolveAgainstTheRealIndex(t *testing.T) {
	cat := newCatalog(t, nil, nil, true)
	for alias := range aliases {
		if _, err := cat.Resolve(alias); err != nil {
			t.Errorf("alias %q does not resolve: %v", alias, err)
		}
	}
}

// The templates named in the design doc must all be offline-ready.
func TestCoreTemplatesAreEmbedded(t *testing.T) {
	cat := newCatalog(t, nil, nil, true)
	for _, name := range []string{"go", "python", "r", "macos", "linux", "windows", "vscode", "jetbrains", "zed"} {
		tmpl, err := cat.Load(t.Context(), name)
		if err != nil {
			t.Errorf("Load(%q): %v", name, err)
			continue
		}
		if tmpl.Source != FromEmbedded {
			t.Errorf("Load(%q) source = %v, want embedded", name, tmpl.Source)
		}
		if tmpl.Content == "" {
			t.Errorf("Load(%q) returned empty content", name)
		}
	}
}

func TestLoadFetchesThenCaches(t *testing.T) {
	fake := newFakeUpstream(t, map[string]string{"Rust": "target/\n"})
	cat := newCatalog(t, []string{"Rust"}, fake.client, false)
	ctx := t.Context()

	tmpl, err := cat.Load(ctx, "rust")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tmpl.Source != FromNetwork {
		t.Errorf("first Load source = %v, want fetched", tmpl.Source)
	}
	if tmpl.Content != "target/\n" {
		t.Errorf("content = %q", tmpl.Content)
	}

	tmpl, err = cat.Load(ctx, "rust")
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if tmpl.Source != FromCache {
		t.Errorf("second Load source = %v, want cached", tmpl.Source)
	}
	if fake.requests != 1 {
		t.Errorf("made %d requests, want 1", fake.requests)
	}
}

func TestLoadOfflineRefusesToFetch(t *testing.T) {
	fake := newFakeUpstream(t, map[string]string{"Rust": "target/\n"})
	cat := newCatalog(t, []string{"Rust"}, fake.client, true)

	_, err := cat.Load(t.Context(), "rust")
	if err == nil {
		t.Fatal("Load succeeded offline, want an error")
	}
	if !strings.Contains(err.Error(), "--offline") {
		t.Errorf("error should mention --offline, got %q", err)
	}
	if fake.requests != 0 {
		t.Errorf("made %d requests while offline, want 0", fake.requests)
	}
}

func TestLoadAllRejectsTyposBeforeFetching(t *testing.T) {
	fake := newFakeUpstream(t, map[string]string{"Rust": "target/\n"})
	cat := newCatalog(t, []string{"Rust"}, fake.client, false)

	if _, err := cat.LoadAll(t.Context(), []string{"rust", "rustt"}); err == nil {
		t.Fatal("LoadAll succeeded with a typo, want an error")
	}
	if fake.requests != 0 {
		t.Errorf("fetched %d templates despite a typo in the list, want 0", fake.requests)
	}
}

func TestLoadAllDropsRepeats(t *testing.T) {
	cat := newCatalog(t, nil, nil, true)

	loaded, err := cat.LoadAll(t.Context(), []string{"go", "Go", "golang", "python"})
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded %d templates, want 2", len(loaded))
	}
	if loaded[0].Name != "Go" || loaded[1].Name != "Python" {
		t.Errorf("names = %q, %q; want Go, Python", loaded[0].Name, loaded[1].Name)
	}
}

func TestLoadAllKeepsUserOrder(t *testing.T) {
	cat := newCatalog(t, nil, nil, true)

	loaded, err := cat.LoadAll(t.Context(), []string{"vscode", "go", "macos"})
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	want := []string{"VisualStudioCode", "Go", "macOS"}
	for i, tmpl := range loaded {
		if tmpl.Name != want[i] {
			t.Errorf("position %d = %q, want %q", i, tmpl.Name, want[i])
		}
	}
}

func TestList(t *testing.T) {
	cat := newCatalog(t, testIndex, nil, true)

	listings, err := cat.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listings) != len(testIndex) {
		t.Fatalf("listed %d templates, want %d", len(listings), len(testIndex))
	}

	byPath := make(map[string]Listing, len(listings))
	for _, item := range listings {
		byPath[item.Path] = item
	}
	if !byPath["Go"].Embedded {
		t.Error("Go should be marked embedded")
	}
	if byPath["Rust"].Embedded {
		t.Error("Rust should not be marked embedded")
	}
	// Ambiguous base names must list under a name that selects them.
	if got := byPath["community/CFML/ColdBox"].Name; got != "community/CFML/ColdBox" {
		t.Errorf("ambiguous listing name = %q, want the full path", got)
	}
	if got := byPath["Rust"].Name; got != "Rust" {
		t.Errorf("unambiguous listing name = %q, want %q", got, "Rust")
	}
}

func TestNewFallsBackToTheEmbeddedIndex(t *testing.T) {
	cat := newCatalog(t, nil, nil, true)
	if cat.FreshIndex() {
		t.Error("FreshIndex() = true with an empty cache, want false")
	}
	if len(cat.index) != len(templates.Index()) {
		t.Errorf("index has %d entries, want %d", len(cat.index), len(templates.Index()))
	}
}

func TestUpdate(t *testing.T) {
	fake := newFakeUpstream(t, map[string]string{"Rust": "target/\n", "Zig": "zig-out/\n"})
	cat := newCatalog(t, []string{"Rust", "Retired"}, fake.client, false)
	ctx := t.Context()

	if _, err := cat.Load(ctx, "rust"); err != nil {
		t.Fatalf("Load: %v", err)
	}

	result, err := cat.Update(ctx)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if result.Templates != 2 {
		t.Errorf("Templates = %d, want 2", result.Templates)
	}
	if !containsString(result.Added, "Zig") {
		t.Errorf("Added = %v, want to include Zig", result.Added)
	}
	if !containsString(result.Removed, "Retired") {
		t.Errorf("Removed = %v, want to include Retired", result.Removed)
	}
	if result.Refetched != 1 {
		t.Errorf("Refetched = %d, want 1", result.Refetched)
	}
	if !cat.FreshIndex() {
		t.Error("FreshIndex() = false after an update")
	}

	// The refreshed list is live immediately, without a new process.
	if _, err := cat.Resolve("zig"); err != nil {
		t.Errorf("Resolve(zig) after update: %v", err)
	}
}

func TestUpdateDropsTemplatesGoneFromUpstream(t *testing.T) {
	fake := newFakeUpstream(t, map[string]string{"Rust": "target/\n"})
	cat := newCatalog(t, []string{"Rust", "Retired"}, fake.client, false)
	ctx := t.Context()

	// Cache a template that upstream is about to stop publishing.
	if err := cat.cache.Put("Retired", []byte("old\n"), cache.Entry{}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	result, err := cat.Update(ctx)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !containsString(result.Dropped, "Retired") {
		t.Errorf("Dropped = %v, want to include Retired", result.Dropped)
	}
	if _, _, err := cat.cache.Get("Retired"); !errors.Is(err, cache.ErrMiss) {
		t.Errorf("Retired is still cached: %v", err)
	}
}

func TestUpdateOfflineFails(t *testing.T) {
	cat := newCatalog(t, testIndex, nil, true)
	if _, err := cat.Update(t.Context()); err == nil {
		t.Error("Update succeeded offline, want an error")
	}
}

func TestUpstreamNotFound(t *testing.T) {
	// A template in the list but missing from the repo, which is what a
	// stale index looks like.
	fake := newFakeUpstream(t, map[string]string{})
	cat := newCatalog(t, []string{"Rust"}, fake.client, false)

	_, err := cat.Load(t.Context(), "rust")
	if err == nil {
		t.Fatal("Load succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "igo update") {
		t.Errorf("error should suggest `igo update`, got %q", err)
	}
}

func TestNewRequiresCache(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Error("New without a cache succeeded, want an error")
	}
}

func TestSourceString(t *testing.T) {
	tests := map[Source]string{
		FromEmbedded: "embedded",
		FromCache:    "cached",
		FromNetwork:  "fetched",
		Source(99):   "unknown",
	}
	for source, want := range tests {
		if got := source.String(); got != want {
			t.Errorf("Source(%d).String() = %q, want %q", source, got, want)
		}
	}
}

func TestEditDistance(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"abc", "abc", 0},
		{"python", "pythno", 2},
		{"kitten", "sitting", 3},
		{"café", "cafe", 1},
	}
	for _, tt := range tests {
		if got := editDistance(tt.a, tt.b); got != tt.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func containsString(items []string, want string) bool {
	return slices.Contains(items, want)
}
