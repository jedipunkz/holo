package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Benchmarks run on synthetic data so results are reproducible. Sizes mirror
// the observation in DESIGN.md (45 sessions / 59 MB, about 1.3 MB each).

var words = strings.Fields("the a fix add test build deploy login session error config " +
	"kyber crystal holo fuzzy search resume branch commit review refactor " +
	"日本語 テスト 検索 修正 設定")

// sentence returns n pseudo-random words from a fixed seed.
func sentence(r *rand.Rand, n int) string {
	var b strings.Builder
	for i := range n {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(words[r.IntN(len(words))])
	}
	return b.String()
}

// sessionJSONL builds a session file of roughly size bytes. A quarter of the
// bytes are tool results, which holo parses but drops, as in real sessions.
func sessionJSONL(r *rand.Rand, id string, size int) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	ts := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cwd := "/w/" + id
	_ = enc.Encode(map[string]any{"type": "ai-title", "aiTitle": sentence(r, 5)})
	for i := 0; buf.Len() < size; i++ {
		ts = ts.Add(time.Minute)
		base := map[string]any{"sessionId": id, "cwd": cwd, "gitBranch": "main", "timestamp": ts}
		switch i % 4 {
		case 0:
			base["type"], base["message"] = "user", map[string]any{"content": sentence(r, 30)}
		case 1, 2:
			base["type"], base["message"] = "assistant", map[string]any{"content": []map[string]any{
				{"type": "text", "text": sentence(r, 120)},
				{"type": "tool_use", "name": "Bash"},
			}}
		case 3:
			base["type"], base["message"] = "user", map[string]any{"content": []map[string]any{
				{"type": "tool_result", "content": sentence(r, 200)},
			}}
		}
		_ = enc.Encode(base)
	}
	return buf.Bytes()
}

func writeSessions(b *testing.B, n, size int) (root string, total int64) {
	b.Helper()
	root = b.TempDir()
	r := rand.New(rand.NewPCG(1, 2))
	for i := range n {
		id := fmt.Sprintf("s%04d", i)
		data := sessionJSONL(r, id, size)
		p := filepath.Join(root, "proj"+id, id+".jsonl")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			b.Fatal(err)
		}
		total += int64(len(data))
	}
	return root, total
}

func BenchmarkLoadSessions(b *testing.B) {
	for _, c := range []struct{ n, size int }{
		{45, 1300 << 10}, // like DESIGN.md: about 59 MB
		{500, 100 << 10}, // many small sessions
		{200, 1300 << 10},
	} {
		b.Run(fmt.Sprintf("%dx%dKB", c.n, c.size>>10), func(b *testing.B) {
			root, total := writeSessions(b, c.n, c.size)
			b.SetBytes(total)
			for b.Loop() {
				if _, err := LoadSessions(root); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkParse(b *testing.B) {
	data := sessionJSONL(rand.New(rand.NewPCG(1, 2)), "s", 1300<<10)
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		parse(bytes.NewReader(data))
	}
}

// memSessions builds parsed sessions without touching the disk.
func memSessions(n, bodyWords int) []Session {
	r := rand.New(rand.NewPCG(1, 2))
	ss := make([]Session, n)
	for i := range ss {
		ss[i] = Session{
			ID:      fmt.Sprintf("%08x-0000-0000-0000-000000000000", i),
			CWD:     fmt.Sprintf("/w/project-%d", i),
			Branch:  "feat/" + sentence(r, 2),
			Title:   sentence(r, 6),
			Updated: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute),
			Body:    strings.ToLower(sentence(r, bodyWords)),
		}
	}
	return ss
}

func BenchmarkFilter(b *testing.B) {
	queries := map[string]string{
		"empty":     "",
		"fuzzy":     "login fix",
		"id":        "0000002a",
		"body":      "kyber crystal",
		"no-match":  "zzzzqqqq", // worst case: every body is scanned
		"multibyte": "検索",
	}
	for _, c := range []struct{ n, bodyWords int }{
		{45, 200_000},  // about 1.3 MB of body each
		{1000, 10_000}, // many short sessions
	} {
		sessions := memSessions(c.n, c.bodyWords)
		for name, q := range queries {
			b.Run(fmt.Sprintf("%d/%s", c.n, name), func(b *testing.B) {
				for b.Loop() {
					filter(sessions, q)
				}
			})
		}
	}
}

func BenchmarkView(b *testing.B) {
	for _, q := range []string{"", "login"} {
		name := q
		if name == "" {
			name = "empty"
		}
		b.Run(name, func(b *testing.B) {
			nm, _ := newModel(memSessions(1000, 10), q).Update(tea.WindowSizeMsg{Width: 200, Height: 60})
			m := nm.(model)
			for b.Loop() {
				m.View()
			}
		})
	}
}

func BenchmarkRow(b *testing.B) {
	m := model{sessions: memSessions(1, 10), width: 200}
	mt := filter(m.sessions, m.sessions[0].Title[:4])[0]
	for b.Loop() {
		m.row(mt, true)
	}
}
