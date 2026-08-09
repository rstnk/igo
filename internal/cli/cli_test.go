package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rstnk/igo/internal/upstream"
)

// run executes igo with args, using an isolated cache directory.
func run(t *testing.T, opts *options, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	if opts == nil {
		opts = &options{}
	}

	root := newRoot(opts)
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	// A pipe stands in for stdin so overwrite prompts see a non-terminal.
	root.SetIn(&bytes.Buffer{})
	root.SetArgs(append([]string{"--cache-dir", t.TempDir()}, args...))

	err = root.Execute()
	return out.String(), errOut.String(), err
}

func TestGenerateWritesFile(t *testing.T) {
	dest := filepath.Join(t.TempDir(), ".gitignore")

	_, stderr, err := run(t, nil, "go", "macos", "vscode", "--offline", "-o", dest)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	got := string(content)
	for _, want := range []string{"# --- Go ---", "# --- macOS ---", "# --- VisualStudioCode ---", "go.work"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(stderr, "wrote "+dest) {
		t.Errorf("stderr = %q, want a confirmation naming the file", stderr)
	}
	if !strings.Contains(stderr, "3 embedded") {
		t.Errorf("stderr = %q, want the source summary", stderr)
	}
}

func TestGenerateKeepsUserOrder(t *testing.T) {
	stdout, _, err := run(t, nil, "vscode", "go", "--offline", "--stdout")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	vscode := strings.Index(stdout, "# --- VisualStudioCode ---")
	golang := strings.Index(stdout, "# --- Go ---")
	if vscode < 0 || golang < 0 || vscode > golang {
		t.Errorf("blocks are out of order:\n%s", stdout)
	}
}

func TestGenerateStdoutWritesNoFile(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, ".gitignore")

	stdout, _, err := run(t, nil, "go", "--offline", "--stdout", "-o", dest)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout, "# --- Go ---") {
		t.Errorf("stdout = %q, want the generated file", stdout)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("--stdout wrote a file anyway: %v", err)
	}
}

func TestGenerateRefusesToOverwrite(t *testing.T) {
	dest := filepath.Join(t.TempDir(), ".gitignore")
	if err := os.WriteFile(dest, []byte("hand written\n"), 0o644); err != nil {
		t.Fatalf("seeding file: %v", err)
	}

	_, _, err := run(t, nil, "go", "--offline", "-o", dest)
	if err == nil {
		t.Fatal("run succeeded, want a refusal")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error = %q, want it to mention --force", err)
	}

	content, _ := os.ReadFile(dest)
	if string(content) != "hand written\n" {
		t.Errorf("the existing file was modified: %q", content)
	}
}

func TestGenerateForceOverwrites(t *testing.T) {
	dest := filepath.Join(t.TempDir(), ".gitignore")
	if err := os.WriteFile(dest, []byte("hand written\n"), 0o644); err != nil {
		t.Fatalf("seeding file: %v", err)
	}

	if _, _, err := run(t, nil, "go", "--offline", "--force", "-o", dest); err != nil {
		t.Fatalf("run: %v", err)
	}
	content, _ := os.ReadFile(dest)
	if !strings.Contains(string(content), "# --- Go ---") {
		t.Errorf("file was not overwritten: %q", content)
	}
}

func TestGenerateIsIdempotent(t *testing.T) {
	dest := filepath.Join(t.TempDir(), ".gitignore")

	if _, _, err := run(t, nil, "go", "--offline", "-o", dest); err != nil {
		t.Fatalf("first run: %v", err)
	}
	first, _ := os.ReadFile(dest)

	// Re-running with the same templates must not need --force.
	_, stderr, err := run(t, nil, "go", "--offline", "-o", dest)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !strings.Contains(stderr, "already up to date") {
		t.Errorf("stderr = %q, want an up-to-date notice", stderr)
	}
	second, _ := os.ReadFile(dest)
	if string(first) != string(second) {
		t.Error("content changed between identical runs")
	}
}

func TestGenerateUnknownTemplate(t *testing.T) {
	dest := filepath.Join(t.TempDir(), ".gitignore")

	_, _, err := run(t, nil, "go", "pythno", "--offline", "-o", dest)
	if err == nil {
		t.Fatal("run succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "Python") {
		t.Errorf("error = %q, want a suggestion", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Error("a file was written despite the bad template name")
	}
}

func TestGenerateNeedsAtLeastOneTemplate(t *testing.T) {
	if _, _, err := run(t, nil); err == nil {
		t.Error("run with no templates succeeded, want an error")
	}
}

func TestGenerateOfflineRefusesToFetch(t *testing.T) {
	_, _, err := run(t, nil, "rust", "--offline", "--stdout")
	if err == nil {
		t.Fatal("run succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "--offline") {
		t.Errorf("error = %q, want it to mention --offline", err)
	}
}

func TestListMarksAvailability(t *testing.T) {
	stdout, stderr, err := run(t, nil, "list", "jetbrains")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout, "JetBrains") || !strings.Contains(stdout, "embedded") {
		t.Errorf("stdout = %q, want JetBrains marked embedded", stdout)
	}
	if !strings.Contains(stderr, "1 template") {
		t.Errorf("stderr = %q, want a count", stderr)
	}
}

func TestListNamesOnly(t *testing.T) {
	stdout, _, err := run(t, nil, "list", "--names", "--embedded")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	lines := strings.Fields(strings.TrimSpace(stdout))
	if len(lines) != 9 {
		t.Errorf("got %d embedded templates, want 9:\n%s", len(lines), stdout)
	}
	if strings.Contains(stdout, "NAME") {
		t.Errorf("--names should print no header:\n%s", stdout)
	}
}

func TestListNoMatch(t *testing.T) {
	stdout, stderr, err := run(t, nil, "list", "definitelynotatemplate")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(stdout, "definitelynotatemplate") {
		t.Errorf("stdout = %q, want no rows", stdout)
	}
	if !strings.Contains(stderr, "no templates match") {
		t.Errorf("stderr = %q, want a no-match notice", stderr)
	}
}

func TestUpdateRefreshesTheIndex(t *testing.T) {
	server := httptest.NewServer(fakeUpstreamHandler(map[string]string{
		"Go":  "go\n",
		"Zig": "zig-out/\n",
	}))
	t.Cleanup(server.Close)

	opts := &options{client: &upstream.Client{
		HTTP:    server.Client(),
		APIBase: server.URL,
		RawBase: server.URL,
	}}

	stdout, _, err := run(t, opts, "update")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout, "2 templates available") {
		t.Errorf("stdout = %q, want the new template count", stdout)
	}
	if !strings.Contains(stdout, "removed upstream") {
		t.Errorf("stdout = %q, want the templates dropped from the snapshot reported", stdout)
	}
}

func TestUpdateOfflineFails(t *testing.T) {
	if _, _, err := run(t, nil, "update", "--offline"); err == nil {
		t.Error("update succeeded offline, want an error")
	}
}

func TestVersion(t *testing.T) {
	stdout, _, err := run(t, nil, "--version")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.HasPrefix(stdout, "igo ") {
		t.Errorf("stdout = %q, want it to start with the program name", stdout)
	}
}

// fakeUpstreamHandler answers the two request shapes igo makes of GitHub.
func fakeUpstreamHandler(contents map[string]string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/git/trees/main", func(w http.ResponseWriter, r *http.Request) {
		var entries []string
		for path := range contents {
			entries = append(entries, `{"path":"`+path+upstream.Ext+`","type":"blob"}`)
		}
		_, _ = w.Write([]byte(`{"tree":[` + strings.Join(entries, ",") + `],"truncated":false}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), upstream.Ext)
		content, ok := contents[path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(content))
	})
	return mux
}
