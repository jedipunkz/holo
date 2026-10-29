# holo design

holo is a standalone CLI that searches past Claude Code sessions and resumes the
selected one. It covers what `claude --resume` offers, plus fuzzy search across
all projects and full-text search over conversation bodies.

## Goals

- Start in a TUI, type into a search box, and narrow sessions incrementally.
- Search across every project by default, not just the current directory.
- Resume the selected session with `claude --resume <id>`.
- Read only. holo never writes to the Claude Code config directory.

## Non-goals

- Index or cache (SQLite etc.). Add one when startup exceeds about 1 second.
- Deleting or editing sessions.

## Data source

Claude Code stores one session per file:

```
~/.claude/projects/<encoded-cwd>/<session-id>.jsonl
```

- `CLAUDE_CONFIG_DIR` replaces `~/.claude` when set.
- Only top-level `*.jsonl` files are read. Subdirectories (`<session-id>/`,
  `memory/`) hold subagent transcripts and memory, and are skipped.
- `<encoded-cwd>` cannot be decoded reliably (`/` and `.` both become `-`), so
  the working directory is taken from the `cwd` field of the records.
- The format is undocumented (observed on Claude Code 2.1.312) and may change.
  Unknown record types and fields are ignored, and malformed lines are skipped.

### Records used

| `type`        | Fields                                                              | Purpose                    |
|---------------|---------------------------------------------------------------------|----------------------------|
| `user`        | `message.content`, `cwd`, `gitBranch`, `timestamp`, `isMeta`        | first prompt, search body  |
| `assistant`   | `message.content[].text`                                            | search body                |
| `ai-title`    | `aiTitle`                                                           | list title                 |
| `last-prompt` | `lastPrompt`                                                        | search body                |

`message.content` is either a string or an array of blocks. Only `text` blocks
are used; `tool_use` and `tool_result` are excluded as noise.

### Session fields

| Field         | Rule                                                                                   |
|---------------|----------------------------------------------------------------------------------------|
| `ID`          | `sessionId` of the first record, falling back to the file name                         |
| `CWD`         | first non-empty `cwd`. The file lives under the launch directory; later records may reflect a `cd` during the session |
| `Branch`      | last non-empty `gitBranch`                                                             |
| `Title`       | last `ai-title`, falling back to the first prompt                                      |
| `FirstPrompt` | first `user` text that is not `isMeta` and does not start with `<` (slash commands, injected context) |
| `Updated`     | latest `timestamp`                                                                     |
| `Body`        | lowercased user/assistant text, for full-text search                                   |

Sessions without any `cwd` are dropped. The list is sorted by `Updated`,
newest first.

## Loading

- All files are parsed in parallel on every start (one goroutine per file).
  45 sessions / 59 MB load in about 0.07 s.
- Lines are read with `bufio.Reader`, not `bufio.Scanner`, because tool results
  can make a single line exceed any fixed buffer.

## Search

Given the query in the search box:

1. Empty query: all sessions, newest first.
2. Fuzzy match ([sahilm/fuzzy](https://github.com/sahilm/fuzzy)) against
   `title + project + branch`, ordered by score. Prompts are kept out of this
   haystack because long text matches almost any subsequence.
3. Then sessions not matched above whose `Body` contains every
   whitespace-separated query term (case-insensitive), newest first.

## TUI

Built with [Bubble Tea v2](https://charm.land/bubbletea), Bubbles v2
`textinput`, and Lip Gloss v2. Runs in the alternate screen.

```
> search box
MM-DD HH:MM  project               branch            title
...
<cwd>  <session-id>                      (selected session)
<matches>/<total>  enter: resume  ↑↓/ctrl+p,n: move  esc: quit
```

| Key                 | Action                         |
|---------------------|--------------------------------|
| typing              | refilter, cursor back to top   |
| `↑` / `ctrl+p`      | move up                        |
| `↓` / `ctrl+n`      | move down                      |
| `enter`             | resume the selected session    |
| `esc` / `ctrl+c`    | quit                           |

Columns are truncated and padded by display width, so CJK titles align.

## Resume

1. `chdir` to the session's `CWD`, because Claude Code looks sessions up by the
   current project directory.
2. `syscall.Exec("claude", "--resume", id)` replaces the holo process, so the
   terminal belongs to Claude Code directly.

## CLI

```
holo [query]       open the TUI with an initial query
holo ls            print sessions as TSV: updated, id, cwd, title
holo ls --json     print sessions as JSON (Body excluded)
```

## Code layout

| File              | Contents                                         |
|-------------------|--------------------------------------------------|
| `main.go`         | argument handling, `ls`, resume via exec         |
| `session.go`      | JSONL parsing and `LoadSessions`                 |
| `tui.go`          | search filter and Bubble Tea model               |
| `session_test.go` | parse and filter tests                           |

## Risks

- The JSONL format is internal to Claude Code and can change between versions.
- Resume relies on running `claude --resume` from the session's first `cwd`.
