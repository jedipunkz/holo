package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
)

var (
	// TokyoNight dark palette
	matchColor   = lipgloss.Color("#7aa2f7") // blue; also the accent color
	dateColor    = lipgloss.Color("#565f89") // comment
	projectColor = lipgloss.Color("#7dcfff") // cyan
	branchColor  = lipgloss.Color("#bb9af7") // purple
	agoColor     = lipgloss.Color("#e0af68") // yellow
	titleColor   = lipgloss.Color("#c0caf5") // fg
	selectedBg   = lipgloss.Color("#292e42") // selection
	dimStyle     = lipgloss.NewStyle().Foreground(dateColor)
	box          = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(matchColor)
)

// match is a filtered session. pos holds fuzzy-matched byte offsets into
// header(s); it is empty for body matches.
type match struct {
	idx int
	pos []int
}

func header(s Session) string {
	return strings.Join([]string{s.Title, project(s.CWD), s.Branch}, " ")
}

// filter returns the sessions matching query.
// Fuzzy matches on title/project/branch come first (by score), then sessions
// whose ID starts with the query, then sessions whose body contains every
// query term (newest first). Prompts stay out of the
// fuzzy haystack because long text matches almost any subsequence.
func filter(sessions []Session, query string) []match {
	query = strings.TrimSpace(query)
	res := make([]match, 0, len(sessions))
	if query == "" {
		for i := range sessions {
			res = append(res, match{idx: i})
		}
		return res
	}

	headers := make([]string, len(sessions))
	for i, s := range sessions {
		headers[i] = header(s)
	}
	seen := make(map[int]bool)
	for _, m := range fuzzy.Find(query, headers) {
		res = append(res, match{idx: m.Index, pos: m.MatchedIndexes})
		seen[m.Index] = true
	}

	q := strings.ToLower(query)
	for i, s := range sessions {
		if !seen[i] && strings.HasPrefix(s.ID, q) {
			res = append(res, match{idx: i})
			seen[i] = true
		}
	}

	terms := strings.Fields(q)
	for i, s := range sessions {
		if seen[i] {
			continue
		}
		all := true
		for _, t := range terms {
			if !strings.Contains(s.Body, t) {
				all = false
				break
			}
		}
		if all {
			res = append(res, match{idx: i})
		}
	}
	return res
}

type model struct {
	sessions []Session
	matches  []match
	cursor   int
	offset   int
	input    textinput.Model
	width    int
	height   int
	chosen   *Session
}

const (
	minPreviewBoxHeight = 6
	maxPreviewBoxHeight = 10
)

func newModel(sessions []Session, query string) model {
	in := textinput.New()
	in.Prompt = "❯ "
	st := textinput.DefaultDarkStyles()
	st.Focused.Prompt = lipgloss.NewStyle().Foreground(matchColor)
	in.SetStyles(st)
	in.Placeholder = "search sessions"
	in.SetValue(query)
	in.Focus()
	return model{sessions: sessions, matches: filter(sessions, query), input: in}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(msg.Width - 5) // border 2, prompt 2, cursor 1
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "enter":
			if len(m.matches) > 0 {
				s := m.sessions[m.matches[m.cursor].idx]
				m.chosen = &s
			}
			return m, tea.Quit
		// The list is drawn bottom-up, so moving up goes to the next match.
		case "up", "ctrl+p":
			m.move(1)
			return m, nil
		case "down", "ctrl+n":
			m.move(-1)
			return m, nil
		}
	}

	prev := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != prev {
		m.matches = filter(m.sessions, m.input.Value())
		m.cursor, m.offset = 0, 0
	}
	return m, cmd
}

func (m *model) move(d int) {
	m.cursor = max(0, min(len(m.matches)-1, m.cursor+d))
	rows := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	} else if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
}

// previewHeight reserves room for the preview when the terminal can fit it.
func (m model) previewHeight() int {
	if m.height < 7+minPreviewBoxHeight {
		return 0
	}
	return min(maxPreviewBoxHeight, max(minPreviewBoxHeight, m.height/3))
}

// listHeight leaves room for the detail, preview, and input boxes.
func (m model) listHeight() int { return max(1, m.height-6-m.previewHeight()) }

func (m model) View() tea.View {
	var b strings.Builder
	// Detail box: selected session on the left, match count on the right.
	inner := max(0, m.width-2)
	count := dimStyle.Render(fmt.Sprintf("%d/%d", len(m.matches), len(m.sessions)))
	detail := ""
	if len(m.matches) > 0 {
		s := m.sessions[m.matches[m.cursor].idx]
		detail = lipgloss.NewStyle().Foreground(projectColor).Render(s.CWD) + "  " + dimStyle.Render(s.ID)
	}
	detail = ansi.Truncate(detail, max(0, inner-ansi.StringWidth(count)-2), "…")
	gap := strings.Repeat(" ", max(0, inner-ansi.StringWidth(detail)-ansi.StringWidth(count)))
	b.WriteString(box.Width(m.width).Render(detail+gap+count) + "\n")

	// Best match (index 0) sits on the bottom row, just above the input like fzf.
	rows := m.listHeight()
	for i := m.offset + rows - 1; i >= m.offset; i-- {
		if i >= len(m.matches) {
			b.WriteString("\n")
			continue
		}
		b.WriteString(m.row(m.matches[i], i == m.cursor) + "\n")
	}
	if m.previewHeight() > 0 {
		inner := max(1, m.width-2)
		innerHeight := m.previewHeight() - 2
		var previewLinesContent []string
		if len(m.matches) > 0 {
			s := m.sessions[m.matches[m.cursor].idx]
			assistant := s.PreviewAssistant
			if assistant == "" {
				assistant = "(no response yet)"
			}
			userHeight := (innerHeight + 1) / 2
			assistantHeight := innerHeight / 2
			previewLinesContent = append(
				previewLinesContent,
				previewLines("You", s.PreviewUser, inner, userHeight, lipgloss.NewStyle().Foreground(projectColor))...,
			)
			previewLinesContent = append(
				previewLinesContent,
				previewLines("Claude", assistant, inner, assistantHeight, lipgloss.NewStyle().Foreground(titleColor))...,
			)
		} else {
			previewLinesContent = previewLines("Preview", "No session selected", inner, innerHeight, dimStyle)
		}
		for len(previewLinesContent) < innerHeight {
			previewLinesContent = append(previewLinesContent, "")
		}
		b.WriteString(box.Width(m.width).Render(strings.Join(previewLinesContent, "\n")) + "\n")
	}
	b.WriteString(box.Width(m.width).Render(m.input.View()))

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// row renders one list line with per-column colors and fuzzy-matched
// characters in matchColor. Each segment is styled separately so the
// selection background is not cut off by a highlight's reset sequence.
func (m model) row(mt match, selected bool) string {
	s := m.sessions[mt.idx]
	base, marker := lipgloss.NewStyle(), " "
	if selected {
		base, marker = base.Background(selectedBg), "▌"
	}
	hl := base.Foreground(matchColor).Bold(true)
	date := s.Updated.Local().Format("01-02 15:04")
	rel := fmt.Sprintf("%8s", ago(time.Since(s.Updated)))
	proj := project(s.CWD)
	sep := base.Render("  ")
	titleW := max(0, m.width-1-len(date)-1-len(rel)-20-16-3*len("  "))
	line := base.Foreground(matchColor).Render(marker) + base.Foreground(dateColor).Render(date) +
		base.Render(" ") + base.Foreground(agoColor).Render(rel) + sep +
		mark(proj, 20, mt.pos, len(s.Title)+1, base.Foreground(projectColor), hl) + sep +
		mark(s.Branch, 16, mt.pos, len(s.Title)+1+len(proj)+1, base.Foreground(branchColor), hl) + sep +
		mark(s.Title, titleW, mt.pos, 0, base.Foreground(titleColor), hl)
	return ansi.Truncate(line, m.width, "")
}

func previewLines(label, text string, width, maxLines int, style lipgloss.Style) []string {
	if maxLines <= 0 {
		return nil
	}
	text = ansi.Strip(text)
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' {
			return ' '
		}
		return r
	}, text)
	text = strings.TrimSpace(text)
	if text == "" {
		text = "(empty)"
	}
	width = max(1, width)
	lines := strings.Split(ansi.Wrap(label+": "+text, width, " "), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		last := strings.TrimRight(lines[maxLines-1], " ")
		lines[maxLines-1] = ansi.Truncate(last, max(0, width-1), "") + "…"
	}
	for i := range lines {
		lines[i] = style.Render(lines[i])
	}
	return lines
}

// ago formats d coarsely, e.g. "5m ago" (at most 8 columns).
func ago(d time.Duration) string {
	switch day := 24 * time.Hour; {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < day:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	case d < 30*day:
		return fmt.Sprintf("%dd ago", int(d/day))
	case d < 365*day:
		return fmt.Sprintf("%dmo ago", int(d/(30*day)))
	default:
		return fmt.Sprintf("%dy ago", int(d/(365*day)))
	}
}

// mark truncates or right-pads text to display width w (CJK-aware), rendering bytes whose
// header offset (off+i) is in pos with hl and the rest with base.
func mark(text string, w int, pos []int, off int, base, hl lipgloss.Style) string {
	t := ansi.Truncate(text, w, "…")
	visible := len(t)
	if t != text {
		visible -= len("…")
	}
	on := func(i int) bool { return i < visible && slices.Contains(pos, off+i) }

	var b strings.Builder
	for i := 0; i < len(t); {
		j := i
		for j < len(t) && on(j) == on(i) {
			_, n := utf8.DecodeRuneInString(t[j:])
			j += n
		}
		st := base
		if on(i) {
			st = hl
		}
		b.WriteString(st.Render(t[i:j]))
		i = j
	}
	b.WriteString(base.Render(strings.Repeat(" ", max(0, w-ansi.StringWidth(t)))))
	return b.String()
}

func project(cwd string) string { return filepath.Base(cwd) }
