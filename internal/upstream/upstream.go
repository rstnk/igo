// Package upstream talks to github/gitignore: it lists what templates exist
// and downloads their contents.
package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// ErrNotFound means the requested template does not exist upstream.
var ErrNotFound = errors.New("template not found upstream")

// Repo is the upstream source, useful in messages and cache metadata.
const Repo = "github/gitignore"

// Ext is the suffix every upstream template file carries.
const Ext = ".gitignore"

// Default endpoints, overridable on a Client for tests.
const (
	DefaultAPIBase = "https://api.github.com/repos/github/gitignore"
	DefaultRawBase = "https://raw.githubusercontent.com/github/gitignore/main"
)

// Client fetches templates over HTTP.
type Client struct {
	HTTP    *http.Client
	APIBase string
	RawBase string
}

// New returns a Client pointed at the real GitHub endpoints.
func New() *Client {
	return &Client{
		HTTP:    &http.Client{Timeout: 30 * time.Second},
		APIBase: DefaultAPIBase,
		RawBase: DefaultRawBase,
	}
}

type treeResponse struct {
	Tree []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	} `json:"tree"`
	Truncated bool `json:"truncated"`
}

// Index lists every template path upstream, sorted, with the .gitignore
// suffix stripped (e.g. "Go", "Global/Vim").
func (c *Client) Index(ctx context.Context) ([]string, error) {
	body, err := c.get(ctx, c.APIBase+"/git/trees/main?recursive=1")
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", Repo, err)
	}
	var tree treeResponse
	if err := json.Unmarshal(body, &tree); err != nil {
		return nil, fmt.Errorf("parsing %s listing: %w", Repo, err)
	}
	if tree.Truncated {
		return nil, fmt.Errorf("listing %s: response was truncated", Repo)
	}

	var paths []string
	for _, entry := range tree.Tree {
		if entry.Type == "blob" && strings.HasSuffix(entry.Path, Ext) {
			paths = append(paths, strings.TrimSuffix(entry.Path, Ext))
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("listing %s: no templates found", Repo)
	}
	sort.Strings(paths)
	return paths, nil
}

// URL is where a template path is fetched from.
func (c *Client) URL(path string) string {
	return c.RawBase + "/" + path + Ext
}

// Template downloads one template by its upstream path, returning the
// content and the URL it came from.
func (c *Client) Template(ctx context.Context, path string) ([]byte, string, error) {
	url := c.URL(path)
	body, err := c.get(ctx, url)
	if err != nil {
		return nil, url, fmt.Errorf("fetching %s: %w", path, err)
	}
	return body, url, nil
}

func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return io.ReadAll(resp.Body)
	case http.StatusNotFound:
		return nil, ErrNotFound
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, fmt.Errorf("%s (GitHub rate limit reached, try again later)", resp.Status)
	default:
		return nil, fmt.Errorf("%s", resp.Status)
	}
}
