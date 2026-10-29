package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// captureStdout runs f with os.Stdout redirected and returns what it wrote.
func captureStdout(t *testing.T, f func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	ferr := f()
	w.Close()
	return <-done, ferr
}

func TestProjectsDir(t *testing.T) {
	t.Run("CLAUDE_CONFIG_DIR", func(t *testing.T) {
		t.Setenv("CLAUDE_CONFIG_DIR", "/cfg")
		got, err := projectsDir()
		if err != nil || got != filepath.Join("/cfg", "projects") {
			t.Errorf("got %q, %v", got, err)
		}
	})
	t.Run("home fallback", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("CLAUDE_CONFIG_DIR", "")
		t.Setenv("HOME", home)
		got, err := projectsDir()
		if err != nil || got != filepath.Join(home, ".claude", "projects") {
			t.Errorf("got %q, %v", got, err)
		}
	})
	t.Run("no home", func(t *testing.T) {
		t.Setenv("CLAUDE_CONFIG_DIR", "")
		t.Setenv("HOME", "")
		if _, err := projectsDir(); err == nil {
			t.Error("want error when HOME is unset")
		}
	})
}

var listSessions = []Session{
	{ID: "id-1", CWD: "/w/a", Branch: "main", Title: "first", FirstPrompt: "fp", LastPrompt: "lp",
		Updated: time.Date(2026, 10, 2, 3, 4, 0, 0, time.UTC), Body: "secret body"},
	{ID: "id-2", CWD: "/w/b", Title: "second", Updated: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)},
}

func TestListTSV(t *testing.T) {
	out, err := captureStdout(t, func() error { return list(listSessions, false) })
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines: %q", out)
	}
	want := listSessions[0].Updated.Local().Format("2006-01-02 15:04") + "\tid-1\t/w/a\tfirst"
	if lines[0] != want {
		t.Errorf("got %q, want %q", lines[0], want)
	}
	if strings.Contains(out, "secret body") {
		t.Error("body must not be printed")
	}
}

func TestListJSON(t *testing.T) {
	out, err := captureStdout(t, func() error { return list(listSessions, true) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "secret body") || strings.Contains(out, `"Body"`) {
		t.Errorf("body must be excluded: %s", out)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out)
	}
	if len(got) != 2 {
		t.Fatalf("len: %d", len(got))
	}
	for _, k := range []string{"id", "cwd", "branch", "title", "firstPrompt", "lastPrompt", "updated"} {
		if _, ok := got[0][k]; !ok {
			t.Errorf("missing key %q in %v", k, got[0])
		}
	}
	if got[0]["id"] != "id-1" || got[0]["updated"] != "2026-10-02T03:04:00Z" {
		t.Errorf("values: %v", got[0])
	}
}

func TestListEmpty(t *testing.T) {
	out, err := captureStdout(t, func() error { return list(nil, false) })
	if err != nil || out != "" {
		t.Errorf("tsv: %q, %v", out, err)
	}
	out, err = captureStdout(t, func() error { return list(nil, true) })
	if err != nil || strings.TrimSpace(out) != "null" {
		t.Errorf("json: %q, %v", out, err)
	}
}

func TestRunHelp(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		out, err := captureStdout(t, func() error { return run([]string{arg}) })
		if err != nil || strings.TrimSpace(out) != usage {
			t.Errorf("%s: %q, %v", arg, out, err)
		}
	}
}

func TestRunLs(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	writeFile(t, filepath.Join(cfg, "projects", "p", "s.jsonl"),
		`{"type":"user","sessionId":"sid","cwd":"/w/p","timestamp":"2026-10-01T00:00:00Z","message":{"content":"hello"}}`)

	out, err := captureStdout(t, func() error { return run([]string{"ls"}) })
	if err != nil || !strings.Contains(out, "\tsid\t/w/p\thello") {
		t.Errorf("ls: %q, %v", out, err)
	}

	out, err = captureStdout(t, func() error { return run([]string{"ls", "--json"}) })
	if err != nil || !strings.Contains(out, `"id": "sid"`) {
		t.Errorf("ls --json: %q, %v", out, err)
	}
}

func TestRunLoadError(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "[") // makes the glob pattern malformed
	if err := run([]string{"ls"}); err == nil {
		t.Error("want error from LoadSessions")
	}
}

func TestRunNoHome(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", "")
	if err := run([]string{"ls"}); err == nil {
		t.Error("want error from projectsDir")
	}
}

func TestResumeMissingDir(t *testing.T) {
	err := resume(Session{ID: "x", CWD: filepath.Join(t.TempDir(), "missing")})
	if err == nil || !strings.Contains(err.Error(), "cannot enter session directory") {
		t.Errorf("got %v", err)
	}
}

func TestResumeClaudeNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir) // restored after the test; resume changes the working directory
	t.Setenv("PATH", "")
	// Guard: if claude were found, resume would exec and replace the test binary.
	if _, err := exec.LookPath("claude"); err == nil {
		t.Skip("claude is still resolvable with an empty PATH")
	}

	if err := resume(Session{ID: "x", CWD: dir}); err == nil {
		t.Error("want error when claude is not on PATH")
	}
	wd, _ := os.Getwd()
	if real, _ := filepath.EvalSymlinks(dir); wd != dir && wd != real {
		t.Errorf("resume should chdir to the session cwd: %q", wd)
	}
}
