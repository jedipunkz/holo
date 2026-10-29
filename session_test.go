package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sample = `{"type":"user","sessionId":"abc","cwd":"/w/holo","gitBranch":"main","timestamp":"2026-10-01T00:00:00Z","isMeta":true,"message":{"content":"meta stuff"}}
{"type":"user","sessionId":"abc","cwd":"/w/holo","timestamp":"2026-10-01T00:00:01Z","message":{"content":"<command-name>/clear</command-name>"}}
not json
{"type":"user","sessionId":"abc","cwd":"/w/holo","timestamp":"2026-10-01T00:00:02Z","message":{"content":"Fix the\nlogin bug"}}
{"type":"assistant","sessionId":"abc","cwd":"/w/holo","timestamp":"2026-10-01T00:00:03Z","message":{"content":[{"type":"text","text":"Found Kyber crystal"},{"type":"tool_use","name":"Bash"}]}}
{"type":"user","sessionId":"abc","cwd":"/w/holo","timestamp":"2026-10-01T00:00:04Z","message":{"content":[{"type":"tool_result","content":"secret tool output"}]}}
{"type":"user","sessionId":"abc","cwd":"/w/holo/sub","timestamp":"2026-10-01T00:00:04Z","message":{"content":"after cd"}}
{"type":"last-prompt","sessionId":"abc","lastPrompt":"thanks"}`

func TestParse(t *testing.T) {
	s := parse(strings.NewReader(sample))
	if s.ID != "abc" || s.CWD != "/w/holo" || s.Branch != "main" {
		t.Fatalf("meta: %+v", s)
	}
	if s.FirstPrompt != "Fix the login bug" || s.Title != "Fix the login bug" {
		t.Errorf("first prompt / title fallback: %q / %q", s.FirstPrompt, s.Title)
	}
	if s.LastPrompt != "thanks" || s.Updated.Second() != 4 {
		t.Errorf("last prompt / updated: %q / %v", s.LastPrompt, s.Updated)
	}
	if !strings.Contains(s.Body, "kyber crystal") || strings.Contains(s.Body, "secret tool output") {
		t.Errorf("body: %q", s.Body)
	}

	withTitle := parse(strings.NewReader(sample + "\n" + `{"type":"ai-title","aiTitle":"Login fix"}`))
	if withTitle.Title != "Login fix" {
		t.Errorf("ai-title: %q", withTitle.Title)
	}
}

func TestParseEdgeCases(t *testing.T) {
	long := strings.Repeat("x", 200_000) // larger than bufio.Scanner's 64 KiB default

	tests := []struct {
		name  string
		input string
		check func(t *testing.T, s Session)
	}{
		{
			name:  "empty input",
			input: "",
			check: func(t *testing.T, s Session) {
				if s.ID != "" || s.CWD != "" || s.Title != "" || s.Body != "" || !s.Updated.IsZero() {
					t.Errorf("want zero session, got %+v", s)
				}
			},
		},
		{
			name:  "blank and whitespace-only lines are ignored",
			input: "\n   \n\t\n" + `{"type":"user","sessionId":"a","cwd":"/w","message":{"content":"hi"}}` + "\n\n",
			check: func(t *testing.T, s Session) {
				if s.ID != "a" || s.FirstPrompt != "hi" {
					t.Errorf("got %+v", s)
				}
			},
		},
		{
			name:  "line longer than 64 KiB",
			input: `{"type":"user","sessionId":"a","cwd":"/w","message":{"content":"` + long + `"}}`,
			check: func(t *testing.T, s Session) {
				if len(s.FirstPrompt) != len(long) || !strings.Contains(s.Body, long) {
					t.Errorf("long line not parsed: first prompt len %d", len(s.FirstPrompt))
				}
			},
		},
		{
			name: "first sessionId and first cwd win",
			input: `{"type":"user","sessionId":"first","cwd":"/a"}
{"type":"user","sessionId":"second","cwd":"/b"}`,
			check: func(t *testing.T, s Session) {
				if s.ID != "first" || s.CWD != "/a" {
					t.Errorf("got id %q cwd %q", s.ID, s.CWD)
				}
			},
		},
		{
			name: "cwd taken from first non-empty record",
			input: `{"type":"ai-title","aiTitle":"t"}
{"type":"user","cwd":"/late"}`,
			check: func(t *testing.T, s Session) {
				if s.CWD != "/late" {
					t.Errorf("cwd: %q", s.CWD)
				}
			},
		},
		{
			name: "branch is the last non-empty gitBranch",
			input: `{"type":"user","gitBranch":"a"}
{"type":"user","gitBranch":"b"}
{"type":"user","gitBranch":""}`,
			check: func(t *testing.T, s Session) {
				if s.Branch != "b" {
					t.Errorf("branch: %q", s.Branch)
				}
			},
		},
		{
			name: "updated is the latest timestamp regardless of order",
			input: `{"type":"user","timestamp":"2026-10-03T00:00:00Z"}
{"type":"user","timestamp":"2026-10-01T00:00:00Z"}
{"type":"user"}`,
			check: func(t *testing.T, s Session) {
				if want := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC); !s.Updated.Equal(want) {
					t.Errorf("updated: %v", s.Updated)
				}
			},
		},
		{
			name: "last ai-title wins and is flattened to one line",
			input: `{"type":"user","message":{"content":"prompt"}}
{"type":"ai-title","aiTitle":"old"}
{"type":"ai-title","aiTitle":"new\n  title"}`,
			check: func(t *testing.T, s Session) {
				if s.Title != "new title" {
					t.Errorf("title: %q", s.Title)
				}
			},
		},
		{
			name:  "last-prompt is flattened to one line",
			input: `{"type":"last-prompt","lastPrompt":"a\n\tb"}`,
			check: func(t *testing.T, s Session) {
				if s.LastPrompt != "a b" {
					t.Errorf("last prompt: %q", s.LastPrompt)
				}
			},
		},
		{
			name: "assistant text never becomes the first prompt",
			input: `{"type":"assistant","message":{"content":"hello from assistant"}}
{"type":"user","message":{"content":"real prompt"}}`,
			check: func(t *testing.T, s Session) {
				if s.FirstPrompt != "real prompt" || s.Title != "real prompt" {
					t.Errorf("first prompt: %q title: %q", s.FirstPrompt, s.Title)
				}
				if !strings.Contains(s.Body, "hello from assistant") {
					t.Errorf("assistant text missing from body: %q", s.Body)
				}
			},
		},
		{
			name: "meta and tagged user text are searchable but not titles",
			input: `{"type":"user","isMeta":true,"message":{"content":"Meta Note"}}
{"type":"user","message":{"content":"<system>Injected</system>"}}`,
			check: func(t *testing.T, s Session) {
				if s.FirstPrompt != "" || s.Title != "" {
					t.Errorf("first prompt: %q title: %q", s.FirstPrompt, s.Title)
				}
				if !strings.Contains(s.Body, "meta note") || !strings.Contains(s.Body, "<system>injected</system>") {
					t.Errorf("body: %q", s.Body)
				}
			},
		},
		{
			name:  "body is lowercased",
			input: `{"type":"user","message":{"content":"MiXeD Case"}}`,
			check: func(t *testing.T, s Session) {
				if s.Body != "mixed case\n" || s.FirstPrompt != "MiXeD Case" {
					t.Errorf("body: %q first prompt: %q", s.Body, s.FirstPrompt)
				}
			},
		},
		{
			name: "other record types do not add to body",
			input: `{"type":"summary","message":{"content":"summary text"}}
{"type":"system","message":{"content":"system text"}}`,
			check: func(t *testing.T, s Session) {
				if s.Body != "" {
					t.Errorf("body: %q", s.Body)
				}
			},
		},
		{
			name:  "empty user content adds nothing",
			input: `{"type":"user","message":{"content":"   "}}`,
			check: func(t *testing.T, s Session) {
				if s.Body != "" || s.FirstPrompt != "" {
					t.Errorf("body: %q first prompt: %q", s.Body, s.FirstPrompt)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, parse(strings.NewReader(tt.input)))
		})
	}
}

func TestText(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"string is trimmed", `"  hello  "`, "hello"},
		{"text blocks are joined", `[{"type":"text","text":"a"},{"type":"text","text":"b"}]`, "a\nb"},
		{"non-text blocks are dropped", `[{"type":"tool_use","name":"Bash"},{"type":"text","text":"keep"},{"type":"tool_result","content":"drop"}]`, "keep"},
		{"empty text blocks are dropped", `[{"type":"text","text":""},{"type":"text","text":"x"}]`, "x"},
		{"only tool blocks", `[{"type":"tool_use"}]`, ""},
		{"empty array", `[]`, ""},
		{"null", `null`, ""},
		{"number", `42`, ""},
		{"object", `{"type":"text","text":"x"}`, ""},
		{"missing content", ``, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := text(json.RawMessage(tt.raw)); got != tt.want {
				t.Errorf("text(%s) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestOneLine(t *testing.T) {
	for in, want := range map[string]string{
		"":             "",
		"a":            "a",
		"  a  b  ":     "a b",
		"a\nb\r\nc\td": "a b c d",
		"日本語\n  テキスト":  "日本語 テキスト",
	} {
		if got := oneLine(in); got != want {
			t.Errorf("oneLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()

	t.Run("id falls back to file name", func(t *testing.T) {
		p := filepath.Join(dir, "file-id.jsonl")
		writeFile(t, p, `{"type":"user","cwd":"/w"}`)
		s, err := parseFile(p)
		if err != nil || s.ID != "file-id" {
			t.Errorf("got %q, %v", s.ID, err)
		}
	})
	t.Run("sessionId takes precedence over file name", func(t *testing.T) {
		p := filepath.Join(dir, "other.jsonl")
		writeFile(t, p, `{"type":"user","sessionId":"from-record","cwd":"/w"}`)
		s, err := parseFile(p)
		if err != nil || s.ID != "from-record" {
			t.Errorf("got %q, %v", s.ID, err)
		}
	})
	t.Run("missing file", func(t *testing.T) {
		if _, err := parseFile(filepath.Join(dir, "missing.jsonl")); err == nil {
			t.Error("want error")
		}
	})
}

func TestLoadSessions(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "p1", "old.jsonl"),
		`{"type":"user","sessionId":"old","cwd":"/w/a","timestamp":"2026-10-01T00:00:00Z","message":{"content":"old one"}}`)
	writeFile(t, filepath.Join(root, "p2", "new.jsonl"),
		`{"type":"user","cwd":"/w/b","timestamp":"2026-10-02T00:00:00Z","message":{"content":"new one"}}`)
	writeFile(t, filepath.Join(root, "p2", "nocwd.jsonl"),
		`{"type":"user","sessionId":"nocwd","message":{"content":"dropped"}}`)
	writeFile(t, filepath.Join(root, "p1", "old", "subagents", "agent.jsonl"),
		`{"type":"user","sessionId":"sub","cwd":"/w/a"}`)
	writeFile(t, filepath.Join(root, "toplevel.jsonl"),
		`{"type":"user","sessionId":"top","cwd":"/w/a"}`)
	writeFile(t, filepath.Join(root, "p1", "notes.txt"),
		`{"type":"user","sessionId":"txt","cwd":"/w/a"}`)

	sessions, err := LoadSessions(root)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range sessions {
		ids = append(ids, s.ID)
	}
	if got := strings.Join(ids, ","); got != "new,old" {
		t.Errorf("ids (newest first, subdirs/no-cwd/non-jsonl skipped): %s", got)
	}
}

func TestLoadSessionsSkipsUnreadable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read files regardless of mode")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "p", "ok.jsonl"), `{"type":"user","sessionId":"ok","cwd":"/w"}`)
	bad := filepath.Join(root, "p", "bad.jsonl")
	writeFile(t, bad, `{"type":"user","sessionId":"bad","cwd":"/w"}`)
	if err := os.Chmod(bad, 0); err != nil {
		t.Fatal(err)
	}

	sessions, err := LoadSessions(root)
	if err != nil || len(sessions) != 1 || sessions[0].ID != "ok" {
		t.Errorf("got %+v, %v", sessions, err)
	}
}

func TestLoadSessionsEmptyAndInvalidRoot(t *testing.T) {
	sessions, err := LoadSessions(filepath.Join(t.TempDir(), "missing"))
	if err != nil || len(sessions) != 0 {
		t.Errorf("missing root: %v, %v", sessions, err)
	}
	if _, err := LoadSessions("["); err == nil {
		t.Error("bad glob pattern: want error")
	}
}
