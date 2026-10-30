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
  holo [query]         search sessions in a TUI and resume the selected one
  holo ls [--json]     print sessions (TSV: updated, id, cwd, title)
  holo shell fish|zsh  print key bindings (Ctrl-Q opens holo)`

// shellInit holds the key bindings per shell. holo runs claude in place of
// itself, so the binding returns to the prompt once claude exits.
var shellInit = map[string]string{
	"fish": `# holo: Ctrl-Q opens holo. Add to ~/.config/fish/config.fish:
#   holo shell fish | source
function __holo_open
    holo
    commandline -f repaint
end
bind \cq __holo_open
if bind -M insert >/dev/null 2>&1
    bind -M insert \cq __holo_open
end
`,
	// zsh runs holo as an ordinary command line, which hands it the terminal;
	// push-input restores what was typed afterwards. flow_control is unset
	// because the tty otherwise consumes Ctrl-Q as XON before zle sees it.
	"zsh": `# holo: Ctrl-Q opens holo. Add to ~/.zshrc:
#   eval "$(holo shell zsh)"
unsetopt flow_control
__holo_open() {
  zle push-input
  BUFFER=holo
  zle accept-line
}
zle -N __holo_open
bindkey -M emacs '^Q' __holo_open
bindkey -M viins '^Q' __holo_open
`,
}

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
	if len(args) == 2 && args[0] == "shell" && shellInit[args[1]] != "" {
		fmt.Print(shellInit[args[1]])
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
