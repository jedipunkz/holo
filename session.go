package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Session is one Claude Code session file (~/.claude/projects/<dir>/<id>.jsonl).
type Session struct {
	ID          string    `json:"id"`
	CWD         string    `json:"cwd"`
	Branch      string    `json:"branch"`
	Title       string    `json:"title"`
	FirstPrompt string    `json:"firstPrompt"`
	LastPrompt  string    `json:"lastPrompt"`
	Updated     time.Time `json:"updated"`
	// Body is the lowercased user/assistant text, used for full-text search.
	Body string `json:"-"`
}

// record covers only the fields holo needs. The JSONL format is undocumented,
// so unknown types and fields are ignored.
type record struct {
	Type       string    `json:"type"`
	SessionID  string    `json:"sessionId"`
	CWD        string    `json:"cwd"`
	GitBranch  string    `json:"gitBranch"`
	Timestamp  time.Time `json:"timestamp"`
	IsMeta     bool      `json:"isMeta"`
	AITitle    string    `json:"aiTitle"`
	LastPrompt string    `json:"lastPrompt"`
	Message    struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// LoadSessions reads every top-level session file under root, newest first.
// Subdirectories (subagents, memory) are skipped by the glob depth.
func LoadSessions(root string) ([]Session, error) {
	paths, err := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	results := make([]*Session, len(paths))
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Go(func() {
			// Unreadable files are skipped rather than failing the whole list.
			if s, err := parseFile(p); err == nil && s.CWD != "" {
				results[i] = &s
			}
		})
	}
	wg.Wait()

	var sessions []Session
	for _, s := range results {
		if s != nil {
			sessions = append(sessions, *s)
		}
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Updated.After(sessions[j].Updated) })
	return sessions, nil
}

func parseFile(path string) (Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return Session{}, err
	}
	defer f.Close()
	s := parse(f)
	if s.ID == "" {
		s.ID = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	}
	return s, nil
}

func parse(r io.Reader) Session {
	var s Session
	var body strings.Builder
	// bufio.Reader instead of Scanner: tool results can make a line exceed any fixed buffer.
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var rec record
			if json.Unmarshal(line, &rec) == nil {
				apply(&s, &body, rec)
			}
		}
		if err != nil {
			break
		}
	}
	if s.Title == "" {
		s.Title = s.FirstPrompt
	}
	s.Body = strings.ToLower(body.String())
	return s
}

func apply(s *Session, body *strings.Builder, rec record) {
	if s.ID == "" {
		s.ID = rec.SessionID
	}
	// Keep the first cwd: the session file lives under the launch directory,
	// and later records reflect `cd` during the session.
	if s.CWD == "" && rec.CWD != "" {
		s.CWD = rec.CWD
	}
	if rec.GitBranch != "" {
		s.Branch = rec.GitBranch
	}
	if rec.Timestamp.After(s.Updated) {
		s.Updated = rec.Timestamp
	}
	switch rec.Type {
	case "ai-title":
		s.Title = oneLine(rec.AITitle)
	case "last-prompt":
		s.LastPrompt = oneLine(rec.LastPrompt)
	case "user", "assistant":
		t := text(rec.Message.Content)
		if t == "" {
			return
		}
		body.WriteString(t)
		body.WriteByte('\n')
		// Slash commands and injected context start with a tag; skip them as a title.
		if rec.Type == "user" && !rec.IsMeta && s.FirstPrompt == "" && !strings.HasPrefix(t, "<") {
			s.FirstPrompt = oneLine(t)
		}
	}
}

// text extracts plain text from message.content, which is either a string or
// an array of blocks. tool_use / tool_result blocks are excluded as noise.
func text(raw json.RawMessage) string {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return strings.TrimSpace(str)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
