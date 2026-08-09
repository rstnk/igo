// Package catalog turns the names a user types into template contents.
//
// Names resolve against the list of templates github/gitignore publishes,
// which ships in the binary and is refreshed by `igo update`. Content comes
// from the embedded snapshot first, then the local cache, and only then the
// network, so common templates never need a connection.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/rstnk/igo/internal/cache"
	"github.com/rstnk/igo/internal/upstream"
	"github.com/rstnk/igo/templates"
)

const ext = upstream.Ext

// Source records where a template's content came from.
type Source int

const (
	// FromEmbedded means the content shipped inside the binary.
	FromEmbedded Source = iota
	// FromCache means the content was read from the local cache.
	FromCache
	// FromNetwork means the content was downloaded and then cached.
	FromNetwork
)

func (s Source) String() string {
	switch s {
	case FromEmbedded:
		return "embedded"
	case FromCache:
		return "cached"
	case FromNetwork:
		return "fetched"
	}
	return "unknown"
}

// Template is a resolved template and its content.
type Template struct {
	// Name is how the template is labelled in output, e.g. "Go" or,
	// when a base name is ambiguous, "community/Racket".
	Name string
	// Path is the upstream path, e.g. "Global/macOS".
	Path string
	// Source is where the content was read from.
	Source Source
	// Content is the raw template text.
	Content string
}

// Options configures a Catalog.
type Options struct {
	// Cache stores fetched templates. Required.
	Cache *cache.Cache
	// Client fetches from upstream. Defaults to upstream.New().
	Client *upstream.Client
	// Offline makes any operation that would need the network fail
	// instead of reaching for it.
	Offline bool
}

// Catalog resolves and loads templates.
type Catalog struct {
	cache   *cache.Cache
	client  *upstream.Client
	offline bool

	index    []string
	fresh    bool // index came from the cache rather than the binary
	byPath   map[string]string
	byBase   map[string][]string
	display  map[string]string
	embedded map[string]bool
}

// New builds a Catalog. It uses the cached upstream template list when one
// exists, falling back to the list embedded at build time.
func New(opts Options) (*Catalog, error) {
	if opts.Cache == nil {
		return nil, errors.New("catalog: cache is required")
	}
	client := opts.Client
	if client == nil {
		client = upstream.New()
	}

	c := &Catalog{cache: opts.Cache, client: client, offline: opts.Offline}

	index, err := opts.Cache.Index()
	switch {
	case err == nil:
		c.fresh = true
	case errors.Is(err, cache.ErrMiss):
		index = templates.Index()
	default:
		return nil, err
	}
	c.setIndex(index)
	return c, nil
}

// setIndex rebuilds the lookup tables from a list of upstream paths.
func (c *Catalog) setIndex(index []string) {
	slices.Sort(index)
	c.index = index
	c.byPath = make(map[string]string, len(index))
	c.byBase = make(map[string][]string)
	c.display = make(map[string]string, len(index))

	c.embedded = make(map[string]bool)
	for _, p := range templates.Embedded() {
		c.embedded[p] = true
	}

	for _, upstreamPath := range index {
		c.byPath[strings.ToLower(upstreamPath)] = upstreamPath
		base := strings.ToLower(path.Base(upstreamPath))
		c.byBase[base] = append(c.byBase[base], upstreamPath)
	}

	// A base name displays bare only when it unambiguously belongs to one
	// template; the rest are shown by full path so they stay selectable.
	for base, candidates := range c.byBase {
		winner, err := c.Resolve(base)
		for _, candidate := range candidates {
			if err == nil && candidate == winner {
				c.display[candidate] = path.Base(candidate)
			} else {
				c.display[candidate] = candidate
			}
		}
	}
}

func (c *Catalog) displayName(upstreamPath string) string {
	if name, ok := c.display[upstreamPath]; ok {
		return name
	}
	return upstreamPath
}

// FreshIndex reports whether the template list came from the cache (true)
// or from the snapshot compiled into the binary (false).
func (c *Catalog) FreshIndex() bool { return c.fresh }

// Load resolves a name and returns the template with its content.
func (c *Catalog) Load(ctx context.Context, name string) (Template, error) {
	upstreamPath, err := c.Resolve(name)
	if err != nil {
		return Template{}, err
	}
	return c.loadPath(ctx, upstreamPath)
}

// loadPath reads an already-resolved template, preferring the embedded
// snapshot, then the cache, then the network.
func (c *Catalog) loadPath(ctx context.Context, upstreamPath string) (Template, error) {
	tmpl := Template{Name: c.displayName(upstreamPath), Path: upstreamPath}

	if content, ok := templates.Read(upstreamPath); ok {
		tmpl.Source, tmpl.Content = FromEmbedded, content
		return tmpl, nil
	}

	content, _, err := c.cache.Get(upstreamPath)
	if err == nil {
		tmpl.Source, tmpl.Content = FromCache, string(content)
		return tmpl, nil
	}
	if !errors.Is(err, cache.ErrMiss) {
		return Template{}, err
	}

	if c.offline {
		return Template{}, fmt.Errorf("%s is not embedded or cached, and --offline forbids fetching it", tmpl.Name)
	}

	content, url, err := c.client.Template(ctx, upstreamPath)
	if err != nil {
		if errors.Is(err, upstream.ErrNotFound) {
			return Template{}, fmt.Errorf("%s is listed but missing from %s; try `igo update`", tmpl.Name, upstream.Repo)
		}
		return Template{}, err
	}
	if err := c.cache.Put(upstreamPath, content, cache.Entry{URL: url, FetchedAt: time.Now().UTC()}); err != nil {
		return Template{}, err
	}
	tmpl.Source, tmpl.Content = FromNetwork, string(content)
	return tmpl, nil
}

// LoadAll resolves every name before loading any of them, so a typo fails
// the run before it writes a partial file or hits the network.
func (c *Catalog) LoadAll(ctx context.Context, names []string) ([]Template, error) {
	paths := make([]string, len(names))
	for i, name := range names {
		upstreamPath, err := c.Resolve(name)
		if err != nil {
			return nil, err
		}
		paths[i] = upstreamPath
	}

	loaded := make([]Template, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, upstreamPath := range paths {
		if seen[upstreamPath] {
			continue
		}
		seen[upstreamPath] = true
		tmpl, err := c.loadPath(ctx, upstreamPath)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, tmpl)
	}
	return loaded, nil
}

// Listing is one row of `igo list`.
type Listing struct {
	// Name is the shortest name that selects this template.
	Name string
	// Path is the upstream path.
	Path string
	// Embedded reports whether the content ships in the binary.
	Embedded bool
	// Cached reports whether the content is in the local cache.
	Cached bool
}

// List returns every known template, sorted by upstream path.
func (c *Catalog) List() ([]Listing, error) {
	entries, err := c.cache.Entries()
	if err != nil {
		return nil, err
	}
	cached := make(map[string]bool, len(entries))
	for _, entry := range entries {
		cached[entry.Path] = true
	}

	listings := make([]Listing, 0, len(c.index))
	for _, upstreamPath := range c.index {
		listings = append(listings, Listing{
			Name:     c.displayName(upstreamPath),
			Path:     upstreamPath,
			Embedded: c.embedded[upstreamPath],
			Cached:   cached[upstreamPath],
		})
	}
	return listings, nil
}

// UpdateResult summarises what `igo update` did.
type UpdateResult struct {
	// Templates is how many templates upstream now publishes.
	Templates int
	// Added and Removed are the changes to that list since the last run.
	Added, Removed []string
	// Refetched is how many cached templates were re-downloaded.
	Refetched int
	// Dropped are cached templates that no longer exist upstream.
	Dropped []string
}

// Update refreshes the template list from upstream and re-downloads every
// template already in the cache, so a later run works offline with current
// content.
func (c *Catalog) Update(ctx context.Context) (UpdateResult, error) {
	if c.offline {
		return UpdateResult{}, errors.New("update needs network access, which --offline forbids")
	}

	index, err := c.client.Index(ctx)
	if err != nil {
		return UpdateResult{}, err
	}
	result := UpdateResult{
		Templates: len(index),
		Added:     missing(index, c.index),
		Removed:   missing(c.index, index),
	}
	if err := c.cache.PutIndex(index, upstream.Repo); err != nil {
		return UpdateResult{}, err
	}
	c.setIndex(index)
	c.fresh = true

	entries, err := c.cache.Entries()
	if err != nil {
		return UpdateResult{}, err
	}
	current := make(map[string]bool, len(index))
	for _, p := range index {
		current[p] = true
	}

	for _, entry := range entries {
		if !current[entry.Path] {
			// Gone upstream: resolution would refuse the name anyway, so
			// leaving the file behind only wastes space.
			if err := c.cache.Remove(entry.Path); err != nil {
				return result, err
			}
			result.Dropped = append(result.Dropped, entry.Path)
			continue
		}
		content, url, err := c.client.Template(ctx, entry.Path)
		if err != nil {
			return result, err
		}
		if err := c.cache.Put(entry.Path, content, cache.Entry{URL: url, FetchedAt: time.Now().UTC()}); err != nil {
			return result, err
		}
		result.Refetched++
	}
	return result, nil
}

// missing returns the entries of a that are absent from b.
func missing(a, b []string) []string {
	have := make(map[string]bool, len(b))
	for _, s := range b {
		have[s] = true
	}
	var out []string
	for _, s := range a {
		if !have[s] {
			out = append(out, s)
		}
	}
	return out
}
