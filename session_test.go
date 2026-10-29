package main

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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

func TestFilter(t *testing.T) {
	sessions := []Session{
		{Title: "deploy pipeline", CWD: "/w/infra"},
		{Title: "login fix", CWD: "/w/holo", Body: "kyber crystal found"},
		{Title: "other", CWD: "/w/misc", Body: "nothing"},
	}
	if got := filter(sessions, ""); len(got) != 3 {
		t.Errorf("empty query: %v", got)
	}
	if got := filter(sessions, "lgnfx"); len(got) != 1 || got[0].idx != 1 {
		t.Errorf("fuzzy title: %v", got)
	}
	if got := filter(sessions, "infra"); len(got) != 1 || got[0].idx != 0 {
		t.Errorf("fuzzy project: %v", got)
	}
	if got := filter(sessions, "crystal kyber"); len(got) != 1 || got[0].idx != 1 {
		t.Errorf("body terms: %v", got)
	}
}

func TestRowHighlight(t *testing.T) {
	m := model{sessions: []Session{{Title: "login fix", CWD: "/w/holo", Branch: "main"}}, width: 80}
	mt := filter(m.sessions, "holo")[0]
	line := m.row(mt, lipgloss.NewStyle())
	if got := ansi.StringWidth(line); got != 80 {
		t.Errorf("width: %d", got)
	}
	hl := lipgloss.NewStyle().Foreground(matchColor).Render("holo")
	if !strings.Contains(line, hl) {
		t.Errorf("project not highlighted: %q", line)
	}
}
