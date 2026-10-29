package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
)

var (
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
)

// filter returns indexes into sessions matching query.
// Fuzzy matches on title/project/branch come first (by score), then sessions
// whose body contains every query term (newest first). Prompts stay out of the
// fuzzy haystack because long text matches almost any subsequence.
func filter(sessions []Session, query string) []int {
	query = strings.TrimSpace(query)
	idx := make([]int, 0, len(sessions))
	if query == "" {
		for i := range sessions {
			idx = append(idx, i)
		}
		return idx
	}

	headers := make([]string, len(sessions))
	for i, s := range sessions {
		headers[i] = strings.Join([]string{s.Title, project(s.CWD), s.Branch}, " ")
	}
	seen := make(map[int]bool)
	for _, m := range fuzzy.Find(query, headers) {
		idx = append(idx, m.Index)
		seen[m.Index] = true
	}

	terms := strings.Fields(strings.ToLower(query))
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
			idx = append(idx, i)
		}
	}
	return idx
}

type model struct {
	sessions []Session
	matches  []int
	cursor   int
	offset   int
	input    textinput.Model
	width    int
	height   int
	chosen   *Session
}

func newModel(sessions []Session, query string) model {
	in := textinput.New()
	in.Prompt = "> "
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
		m.input.SetWidth(msg.Width - 3)
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "enter":
			if len(m.matches) > 0 {
				s := m.sessions[m.matches[m.cursor]]
				m.chosen = &s
			}
			return m, tea.Quit
		case "up", "ctrl+p":
			m.move(-1)
			return m, nil
		case "down", "ctrl+n":
			m.move(1)
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

// listHeight is the screen height minus the input line and two footer lines.
func (m model) listHeight() int { return max(1, m.height-3) }

func (m model) View() tea.View {
	var b strings.Builder
	b.WriteString(m.input.View() + "\n")

	rows := m.listHeight()
	for i := m.offset; i < min(len(m.matches), m.offset+rows); i++ {
		line := m.row(m.sessions[m.matches[i]])
		if i == m.cursor {
			line = selectedStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}
	for i := len(m.matches) - m.offset; i < rows; i++ {
		b.WriteString("\n")
	}

	detail := ""
	if len(m.matches) > 0 {
		s := m.sessions[m.matches[m.cursor]]
		detail = s.CWD + "  " + s.ID
	}
	b.WriteString(dimStyle.Render(ansi.Truncate(detail, m.width, "…")) + "\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf("%d/%d  enter: resume  ↑↓/ctrl+p,n: move  esc: quit", len(m.matches), len(m.sessions))))

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m model) row(s Session) string {
	line := fmt.Sprintf("%s  %s  %s  %s",
		s.Updated.Local().Format("01-02 15:04"),
		pad(project(s.CWD), 20),
		pad(s.Branch, 16),
		s.Title)
	return pad(line, m.width)
}

// pad truncates or right-pads s to display width w (CJK-aware).
func pad(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

func project(cwd string) string { return filepath.Base(cwd) }
