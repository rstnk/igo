package upstream

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Client{HTTP: server.Client(), APIBase: server.URL, RawBase: server.URL}
}

func TestIndex(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tree":[
			{"path":"Rust.gitignore","type":"blob"},
			{"path":"Global","type":"tree"},
			{"path":"Global/Vim.gitignore","type":"blob"},
			{"path":"README.md","type":"blob"},
			{"path":"Go.gitignore","type":"blob"}
		],"truncated":false}`))
	})

	got, err := client.Index(t.Context())
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	want := []string{"Global/Vim", "Go", "Rust"}
	if !slices.Equal(got, want) {
		t.Errorf("Index() = %v, want %v (sorted, blobs only, .gitignore only)", got, want)
	}
}

func TestIndexRejectsTruncatedResponse(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tree":[{"path":"Go.gitignore","type":"blob"}],"truncated":true}`))
	})

	if _, err := client.Index(t.Context()); err == nil {
		t.Error("Index accepted a truncated listing, want an error")
	}
}

func TestIndexRejectsEmptyListing(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tree":[],"truncated":false}`))
	})

	if _, err := client.Index(t.Context()); err == nil {
		t.Error("Index accepted an empty listing, want an error")
	}
}

func TestIndexRejectsGarbage(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>not json</html>`))
	})

	if _, err := client.Index(t.Context()); err == nil {
		t.Error("Index accepted a non-JSON response, want an error")
	}
}

func TestTemplate(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Global/Vim.gitignore" {
			t.Errorf("requested %q, want /Global/Vim.gitignore", r.URL.Path)
		}
		_, _ = w.Write([]byte("*.swp\n"))
	})

	content, url, err := client.Template(t.Context(), "Global/Vim")
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	if string(content) != "*.swp\n" {
		t.Errorf("content = %q", content)
	}
	if !strings.HasSuffix(url, "/Global/Vim.gitignore") {
		t.Errorf("url = %q", url)
	}
}

func TestTemplateNotFound(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	if _, _, err := client.Template(t.Context(), "Nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestTemplateRateLimited(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	_, _, err := client.Template(t.Context(), "Rust")
	if err == nil {
		t.Fatal("Template succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error = %q, want it to explain the rate limit", err)
	}
}

func TestTemplateServerError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	_, _, err := client.Template(t.Context(), "Rust")
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want a plain error", err)
	}
}

func TestDefaultEndpoints(t *testing.T) {
	client := New()
	if got := client.URL("Global/Vim"); got != DefaultRawBase+"/Global/Vim.gitignore" {
		t.Errorf("URL() = %q", got)
	}
	if client.HTTP == nil || client.HTTP.Timeout == 0 {
		t.Error("New() should set a request timeout")
	}
}
