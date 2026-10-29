package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func idxs(ms []match) []int {
	out := make([]int, len(ms))
	for i, m := range ms {
		out[i] = m.idx
	}
	return out
}

func TestFilter(t *testing.T) {
	sessions := []Session{
		{Title: "deploy pipeline", CWD: "/w/infra"},
		{Title: "login fix", CWD: "/w/holo", Body: "kyber crystal found"},
		{Title: "other", CWD: "/w/misc", Body: "nothing", ID: "2261faae-59c6"},
		{Title: "mentions id", CWD: "/w/x", Body: "see 2261faae-59c6"},
		{Title: "tests", CWD: "/w/y", Branch: "feat/zebra"},
	}
	tests := []struct {
		name  string
		query string
		want  []int
	}{
		{"empty query keeps order", "", []int{0, 1, 2, 3, 4}},
		{"whitespace-only query keeps order", "   ", []int{0, 1, 2, 3, 4}},
		{"fuzzy title", "lgnfx", []int{1}},
		{"fuzzy project", "infra", []int{0}},
		{"fuzzy branch", "zebra", []int{4}},
		{"fuzzy is case-insensitive", "INFRA", []int{0}},
		{"body needs every term, any order", "crystal kyber", []int{1}},
		{"body term missing", "kyber missing", nil},
		{"id prefix before body", "2261FAAE", []int{2, 3}},
		{"id must be a prefix", "59c6", []int{3}}, // mid-ID: only the body that mentions it
		{"no match", "qqqqqq", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := idxs(filter(sessions, tt.query))
			if !slices.Equal(got, tt.want) && !(len(got) == 0 && len(tt.want) == 0) {
				t.Errorf("filter(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestFilterIDMidMatchIsBodyOnly(t *testing.T) {
	sessions := []Session{{Title: "a", CWD: "/w/a", ID: "2261faae-59c6"}}
	if got := filter(sessions, "59c6"); len(got) != 0 {
		t.Errorf("mid-ID query should not match by ID: %v", idxs(got))
	}
}

func TestFilterOrder(t *testing.T) {
	sessions := []Session{
		{Title: "yyy", CWD: "/w/q", Body: "cafe"},            // body match
		{Title: "zzz", CWD: "/w/r", ID: "cafe1234"},          // id match
		{Title: "cafe menu", CWD: "/w/s", Body: "cafe here"}, // fuzzy match, also in body
	}
	got := filter(sessions, "cafe")
	if want := []int{2, 1, 0}; !slices.Equal(idxs(got), want) {
		t.Fatalf("order fuzzy > id > body: got %v, want %v", idxs(got), want)
	}
	if len(got[0].pos) == 0 {
		t.Error("fuzzy match should carry positions")
	}
	if got[1].pos != nil || got[2].pos != nil {
		t.Error("id/body matches should not carry positions")
	}
}

func TestFilterPositionsPointIntoHeader(t *testing.T) {
	sessions := []Session{{Title: "login fix", CWD: "/w/holo", Branch: "main"}}
	for _, q := range []string{"lgnfx", "holo", "main", "fix main"} {
		got := filter(sessions, q)
		if len(got) != 1 {
			t.Fatalf("%q: no match", q)
		}
		h := header(sessions[0])
		var picked strings.Builder
		for _, p := range got[0].pos {
			picked.WriteByte(h[p])
		}
		if want := strings.ReplaceAll(q, " ", ""); strings.ReplaceAll(picked.String(), " ", "") != want {
			t.Errorf("%q: positions pick %q from %q", q, picked.String(), h)
		}
	}
}

func TestHeader(t *testing.T) {
	s := Session{Title: "t", CWD: "/a/b/proj", Branch: "br"}
	if got := header(s); got != "t proj br" {
		t.Errorf("header: %q", got)
	}
}

func TestProject(t *testing.T) {
	for in, want := range map[string]string{
		"/w/holo":  "holo",
		"/w/holo/": "holo",
		"/":        "/",
		"":         ".",
	} {
		if got := project(in); got != want {
			t.Errorf("project(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAgo(t *testing.T) {
	day := 24 * time.Hour
	tests := []struct {
		d    time.Duration
		want string
	}{
		{-time.Hour, "now"}, // future timestamps (clock skew)
		{0, "now"},
		{59 * time.Second, "now"},
		{time.Minute, "1m ago"},
		{59 * time.Minute, "59m ago"},
		{time.Hour, "1h ago"},
		{23 * time.Hour, "23h ago"},
		{day, "1d ago"},
		{29 * day, "29d ago"},
		{30 * day, "1mo ago"},
		{364 * day, "12mo ago"},
		{365 * day, "1y ago"},
		{800 * day, "2y ago"},
	}
	for _, tt := range tests {
		got := ago(tt.d)
		if got != tt.want {
			t.Errorf("ago(%v) = %q, want %q", tt.d, got, tt.want)
		}
		if len(got) > 8 {
			t.Errorf("ago(%v) = %q exceeds 8 columns", tt.d, got)
		}
	}
}

func TestMark(t *testing.T) {
	plain := lipgloss.NewStyle()
	hl := lipgloss.NewStyle().Foreground(matchColor).Bold(true)

	tests := []struct {
		name string
		text string
		w    int
		pos  []int
		off  int
		want string // ANSI-stripped output
	}{
		{"pads to width", "ab", 5, nil, 0, "ab   "},
		{"exact width", "abcde", 5, nil, 0, "abcde"},
		{"truncates with ellipsis", "abcdef", 4, nil, 0, "abc…"},
		{"CJK truncation by display width", "日本語", 5, nil, 0, "日本…"},
		{"CJK padding by display width", "日本", 6, nil, 0, "日本  "},
		{"zero width", "abc", 0, nil, 0, ""},
		{"highlight keeps text", "holo", 6, []int{10, 11}, 10, "holo  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mark(tt.text, tt.w, tt.pos, tt.off, plain, hl)
			if s := ansi.Strip(got); s != tt.want {
				t.Errorf("stripped = %q, want %q", s, tt.want)
			}
			if w := ansi.StringWidth(got); w != tt.w {
				t.Errorf("width = %d, want %d", w, tt.w)
			}
		})
	}

	t.Run("highlights only positions at the offset", func(t *testing.T) {
		got := mark("holo", 6, []int{10, 11}, 10, plain, hl)
		if !strings.Contains(got, hl.Render("ho")) {
			t.Errorf("want %q highlighted: %q", "ho", got)
		}
		if strings.Contains(got, hl.Render("lo")) {
			t.Errorf("%q should not be highlighted: %q", "lo", got)
		}
	})
	t.Run("positions outside the field are ignored", func(t *testing.T) {
		if got := mark("holo", 4, []int{0, 1, 2}, 10, plain, hl); got != "holo" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("ellipsis is never highlighted", func(t *testing.T) {
		// byte 3 ('d') is replaced by the ellipsis when truncated to 4 columns.
		if got := mark("abcdef", 4, []int{3}, 0, plain, hl); got != "abc…" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("multi-byte runes highlight whole", func(t *testing.T) {
		got := mark("日本語", 6, []int{3}, 0, plain, hl) // byte 3 starts 本
		if !strings.Contains(got, hl.Render("本")) {
			t.Errorf("got %q", got)
		}
	})
}

func TestRow(t *testing.T) {
	now := time.Now()
	sessions := []Session{
		{Title: "login fix", CWD: "/w/holo", Branch: "main", Updated: now.Add(-3 * time.Hour)},
		{Title: "日本語のとても長いタイトルが画面幅を超える場合の切り詰め確認用テキスト", CWD: "/w/a-very-long-project-name", Branch: "feature/very-long-branch-name", Updated: now},
	}
	hl := lipgloss.NewStyle().Foreground(matchColor).Bold(true)

	t.Run("fills the width exactly", func(t *testing.T) {
		for _, w := range []int{80, 120} {
			m := model{sessions: sessions, width: w}
			for i := range sessions {
				for _, sel := range []bool{false, true} {
					if got := ansi.StringWidth(m.row(match{idx: i}, sel)); got != w {
						t.Errorf("width %d, session %d, selected %v: got %d", w, i, sel, got)
					}
				}
			}
		}
	})
	t.Run("never exceeds a narrow width", func(t *testing.T) {
		m := model{sessions: sessions, width: 30}
		if got := ansi.StringWidth(m.row(match{idx: 1}, true)); got > 30 {
			t.Errorf("width: %d", got)
		}
	})
	t.Run("columns", func(t *testing.T) {
		m := model{sessions: sessions, width: 120}
		line := ansi.Strip(m.row(match{idx: 0}, false))
		date := sessions[0].Updated.Local().Format("01-02 15:04")
		for _, want := range []string{date, "3h ago", "holo", "main", "login fix"} {
			if !strings.Contains(line, want) {
				t.Errorf("missing %q in %q", want, line)
			}
		}
		if !strings.HasPrefix(line, " ") {
			t.Errorf("unselected row should start with a blank marker: %q", line)
		}
	})
	t.Run("selected row has marker and background", func(t *testing.T) {
		m := model{sessions: sessions, width: 80}
		line := m.row(match{idx: 0}, true)
		if !strings.HasPrefix(ansi.Strip(line), "▌") {
			t.Errorf("no marker: %q", ansi.Strip(line))
		}
		if !strings.Contains(line, "48;2;60;56;54") { // #3c3836 as a truecolor background
			t.Errorf("no selection background in %q", line)
		}
	})
	t.Run("highlights each column", func(t *testing.T) {
		m := model{sessions: sessions[:1], width: 80}
		for q, want := range map[string]string{"holo": "holo", "main": "main", "login": "login"} {
			line := m.row(filter(m.sessions, q)[0], false)
			if !strings.Contains(line, hl.Render(want)) {
				t.Errorf("%q: %q not highlighted in %q", q, want, line)
			}
		}
	})
}

func key(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

func typed(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func update(t *testing.T, m model, msg tea.Msg) (model, tea.Cmd) {
	t.Helper()
	nm, cmd := m.Update(msg)
	return nm.(model), cmd
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func testSessions(n int) []Session {
	ss := make([]Session, n)
	for i := range ss {
		ss[i] = Session{ID: fmt.Sprintf("id-%d", i), Title: fmt.Sprintf("session %d", i), CWD: fmt.Sprintf("/w/p%d", i)}
	}
	return ss
}

func TestNewModel(t *testing.T) {
	ss := testSessions(3)
	m := newModel(ss, "p1")
	if m.input.Value() != "p1" {
		t.Errorf("input: %q", m.input.Value())
	}
	if got := idxs(m.matches); !slices.Equal(got, []int{1}) {
		t.Errorf("initial matches: %v", got)
	}
	if m.cursor != 0 || m.offset != 0 || m.chosen != nil {
		t.Errorf("initial state: %+v", m)
	}
	if m.Init() != nil {
		t.Error("Init should return nil")
	}
	if all := newModel(ss, ""); len(all.matches) != 3 {
		t.Errorf("empty query: %v", idxs(all.matches))
	}
}

func TestUpdateWindowSize(t *testing.T) {
	m, cmd := update(t, newModel(testSessions(1), ""), tea.WindowSizeMsg{Width: 70, Height: 20})
	if m.width != 70 || m.height != 20 || cmd != nil {
		t.Errorf("size %dx%d cmd %v", m.width, m.height, cmd)
	}
}

func TestUpdateMove(t *testing.T) {
	m, _ := update(t, newModel(testSessions(3), ""), tea.WindowSizeMsg{Width: 80, Height: 20})

	steps := []struct {
		msg  tea.KeyPressMsg
		want int
	}{
		{key(tea.KeyDown, 0), 0}, // clamped at the best match
		{key(tea.KeyUp, 0), 1},
		{key('p', tea.ModCtrl), 2},
		{key(tea.KeyUp, 0), 2}, // clamped at the last match
		{key('n', tea.ModCtrl), 1},
		{key(tea.KeyDown, 0), 0},
	}
	for i, s := range steps {
		var cmd tea.Cmd
		m, cmd = update(t, m, s.msg)
		if m.cursor != s.want || cmd != nil {
			t.Errorf("step %d (%s): cursor %d want %d, cmd %v", i, s.msg, m.cursor, s.want, cmd)
		}
	}
}

func TestUpdateMoveWithoutMatches(t *testing.T) {
	m := newModel(testSessions(2), "qqqqqq")
	for _, k := range []tea.KeyPressMsg{key(tea.KeyUp, 0), key(tea.KeyDown, 0)} {
		m, _ = update(t, m, k)
		if m.cursor != 0 {
			t.Errorf("%s: cursor %d", k, m.cursor)
		}
	}
}

func TestMoveScrolls(t *testing.T) {
	m := newModel(testSessions(10), "")
	m.height = 9 // listHeight 3
	for range 5 {
		m.move(1)
	}
	if m.cursor != 5 || m.offset != 3 {
		t.Errorf("after up x5: cursor %d offset %d", m.cursor, m.offset)
	}
	for range 4 {
		m.move(-1)
	}
	if m.cursor != 1 || m.offset != 1 {
		t.Errorf("after down x4: cursor %d offset %d", m.cursor, m.offset)
	}
}

func TestListHeight(t *testing.T) {
	for h, want := range map[int]int{0: 1, 6: 1, 7: 1, 8: 2, 30: 24} {
		if got := (model{height: h}).listHeight(); got != want {
			t.Errorf("height %d: %d, want %d", h, got, want)
		}
	}
}

func TestUpdateEnter(t *testing.T) {
	ss := testSessions(3)
	m := newModel(ss, "")
	m, _ = update(t, m, key(tea.KeyUp, 0))
	m, cmd := update(t, m, key(tea.KeyEnter, 0))
	if !isQuit(cmd) {
		t.Error("enter should quit")
	}
	if m.chosen == nil || m.chosen.ID != "id-1" {
		t.Errorf("chosen: %+v", m.chosen)
	}

	empty := newModel(ss, "qqqqqq")
	empty, cmd = update(t, empty, key(tea.KeyEnter, 0))
	if !isQuit(cmd) || empty.chosen != nil {
		t.Errorf("enter without matches: quit %v chosen %+v", isQuit(cmd), empty.chosen)
	}
}

func TestUpdateQuit(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{key(tea.KeyEscape, 0), key('c', tea.ModCtrl)} {
		m, cmd := update(t, newModel(testSessions(2), ""), k)
		if !isQuit(cmd) || m.chosen != nil {
			t.Errorf("%s: quit %v chosen %+v", k, isQuit(cmd), m.chosen)
		}
	}
}

func TestUpdateTypingRefilters(t *testing.T) {
	m := newModel(testSessions(12), "")
	m.height = 9
	for range 5 {
		m, _ = update(t, m, key(tea.KeyUp, 0))
	}
	if m.cursor == 0 || m.offset == 0 {
		t.Fatalf("precondition: cursor %d offset %d", m.cursor, m.offset)
	}

	m, _ = update(t, m, typed('p'))
	m, _ = update(t, m, typed('1'))
	m, _ = update(t, m, typed('1'))
	if m.input.Value() != "p11" {
		t.Errorf("input: %q", m.input.Value())
	}
	if got := idxs(m.matches); !slices.Equal(got, []int{11}) {
		t.Errorf("matches: %v", got)
	}
	if m.cursor != 0 || m.offset != 0 {
		t.Errorf("cursor/offset not reset: %d/%d", m.cursor, m.offset)
	}

	m, _ = update(t, m, key(tea.KeyBackspace, 0))
	if m.input.Value() != "p1" || len(m.matches) < 2 {
		t.Errorf("after backspace: %q %v", m.input.Value(), idxs(m.matches))
	}
}

func TestUpdateNonEditingKeyKeepsCursor(t *testing.T) {
	m := newModel(testSessions(3), "")
	m, _ = update(t, m, key(tea.KeyUp, 0))
	m, _ = update(t, m, key(tea.KeyLeft, 0))
	if m.cursor != 1 {
		t.Errorf("cursor reset by a key that does not edit the query: %d", m.cursor)
	}
}

func viewLines(m model) []string {
	return strings.Split(ansi.Strip(m.View().Content), "\n")
}

func TestView(t *testing.T) {
	ss := testSessions(2)
	m, _ := update(t, newModel(ss, ""), tea.WindowSizeMsg{Width: 100, Height: 10})
	v := m.View()
	if !v.AltScreen {
		t.Error("AltScreen should be on")
	}

	lines := viewLines(m)
	if len(lines) != 10 {
		t.Fatalf("want 10 lines, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	for i, l := range strings.Split(v.Content, "\n") {
		if w := ansi.StringWidth(l); w > 100 {
			t.Errorf("line %d is %d columns wide", i, w)
		}
	}

	// Detail box (lines 0-2): selected session and count.
	if !strings.HasPrefix(lines[0], "╭") || !strings.HasPrefix(lines[2], "╰") {
		t.Errorf("detail box border: %q / %q", lines[0], lines[2])
	}
	for _, want := range []string{"/w/p0", "id-0", "2/2"} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("detail line missing %q: %q", want, lines[1])
		}
	}
	if !strings.HasSuffix(strings.TrimSuffix(lines[1], "│"), "2/2") {
		t.Errorf("count should be right-aligned: %q", lines[1])
	}

	// List (lines 3-6): blank rows on top, best match on the bottom row.
	if lines[3] != "" || lines[4] != "" {
		t.Errorf("unused rows should be blank and on top: %q %q", lines[3], lines[4])
	}
	if !strings.Contains(lines[5], "session 1") || strings.HasPrefix(lines[5], "▌") {
		t.Errorf("second match row: %q", lines[5])
	}
	if !strings.Contains(lines[6], "session 0") || !strings.HasPrefix(lines[6], "▌") {
		t.Errorf("best match should be on the bottom row and selected: %q", lines[6])
	}

	// Input box (lines 7-9).
	if !strings.HasPrefix(lines[7], "╭") || !strings.Contains(lines[8], "❯") || !strings.HasPrefix(lines[9], "╰") {
		t.Errorf("input box: %q / %q / %q", lines[7], lines[8], lines[9])
	}
}

func TestViewFollowsCursor(t *testing.T) {
	m, _ := update(t, newModel(testSessions(2), ""), tea.WindowSizeMsg{Width: 100, Height: 10})
	m, _ = update(t, m, key(tea.KeyUp, 0))
	lines := viewLines(m)
	if !strings.Contains(lines[1], "id-1") {
		t.Errorf("detail should follow the cursor: %q", lines[1])
	}
	if !strings.HasPrefix(lines[5], "▌") || strings.HasPrefix(lines[6], "▌") {
		t.Errorf("selection marker should move up: %q / %q", lines[5], lines[6])
	}
}

func TestViewScrolled(t *testing.T) {
	m, _ := update(t, newModel(testSessions(10), ""), tea.WindowSizeMsg{Width: 100, Height: 9}) // 3 rows
	for range 4 {
		m, _ = update(t, m, key(tea.KeyUp, 0))
	}
	lines := viewLines(m)
	// offset 2: rows show matches 4, 3, 2 from top to bottom, cursor on 4 (top).
	for i, want := range []string{"session 4", "session 3", "session 2"} {
		if !strings.Contains(lines[3+i], want) {
			t.Errorf("row %d: want %q, got %q", i, want, lines[3+i])
		}
	}
	if !strings.HasPrefix(lines[3], "▌") {
		t.Errorf("cursor row should be the top row: %q", lines[3])
	}
}

func TestViewNoMatches(t *testing.T) {
	m, _ := update(t, newModel(testSessions(2), "qqqqqq"), tea.WindowSizeMsg{Width: 100, Height: 10})
	content := ansi.Strip(m.View().Content)
	if !strings.Contains(content, "0/2") {
		t.Errorf("count missing: %s", content)
	}
	if strings.Contains(content, "▌") || strings.Contains(content, "session 0") || strings.Contains(content, "session 1") {
		t.Errorf("no rows expected: %s", content)
	}
}

func TestViewBeforeWindowSize(t *testing.T) {
	// The first View can run before any WindowSizeMsg arrives.
	m := newModel(testSessions(2), "")
	if m.View().Content == "" {
		t.Error("empty view")
	}
}
