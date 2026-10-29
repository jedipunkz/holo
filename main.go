// holo searches Claude Code sessions and resumes the selected one.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
)

const usage = `usage:
  holo [query]     search sessions in a TUI and resume the selected one
  holo ls [--json] print sessions (TSV: updated, id, cwd, title)`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "holo:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println(usage)
		return nil
	}

	root, err := projectsDir()
	if err != nil {
		return err
	}
	sessions, err := LoadSessions(root)
	if err != nil {
		return err
	}

	if len(args) > 0 && args[0] == "ls" {
		return list(sessions, len(args) > 1 && args[1] == "--json")
	}

	final, err := tea.NewProgram(newModel(sessions, strings.Join(args, " "))).Run()
	if err != nil {
		return err
	}
	if s := final.(model).chosen; s != nil {
		return resume(*s)
	}
	return nil
}

// projectsDir honors CLAUDE_CONFIG_DIR, which Claude Code uses in place of ~/.claude.
func projectsDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "projects"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

func list(sessions []Session, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(sessions)
	}
	for _, s := range sessions {
		fmt.Printf("%s\t%s\t%s\t%s\n", s.Updated.Local().Format("2006-01-02 15:04"), s.ID, s.CWD, s.Title)
	}
	return nil
}

// resume replaces this process with `claude --resume <id>` in the session's cwd,
// because Claude Code looks sessions up by the current project directory.
func resume(s Session) error {
	if err := os.Chdir(s.CWD); err != nil {
		return fmt.Errorf("cannot enter session directory: %w", err)
	}
	bin, err := exec.LookPath("claude")
	if err != nil {
		return err
	}
	return syscall.Exec(bin, []string{"claude", "--resume", s.ID}, os.Environ())
}
