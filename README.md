# holo

Fuzzy search your Claude Code sessions across every project and resume one.

holo reads the session files Claude Code keeps under `~/.claude/projects/`,
lists them in a TUI, and runs `claude --resume <id>` in the session's working
directory when you pick one. It never writes to the Claude Code config
directory.

## Requirements

- Go 1.27.3 or later (to build)
- [Claude Code](https://github.com/anthropics/claude-code) with `claude` on
  your `PATH`

## Install

```sh
go install github.com/jedipunkz/holo@latest
```

Or from a clone:

```sh
make install       # go install .
make dev-install   # build and copy to ~/.bin/holo
```

## Usage

```sh
holo               # open the TUI
holo login bug     # open the TUI with an initial query
holo ls            # print sessions as TSV: updated, id, cwd, title
holo ls --json     # print sessions as JSON
```

| Key              | Action                      |
|------------------|-----------------------------|
| typing           | filter sessions             |
| `↑` / `ctrl+p`   | move up (next match)        |
| `↓` / `ctrl+n`   | move down (previous match)  |
| `enter`          | resume the selected session |
| `esc` / `ctrl+c` | quit                        |

The best match (or the newest session for an empty query) sits on the bottom
row, just above the input, like fzf.

## Search

Results are ordered as follows:

1. Fuzzy matches on title, project, and branch, by score. Matched characters
   are highlighted.
2. Sessions whose ID starts with the query, e.g. `2261faae`.
3. Sessions whose conversation text contains every query word
   (case-insensitive).

## Configuration

holo honors `CLAUDE_CONFIG_DIR` the same way Claude Code does: when it is set,
sessions are read from `$CLAUDE_CONFIG_DIR/projects` instead of
`~/.claude/projects`.

The colors follow the gruvbox dark palette and assume a dark terminal
background.

## Development

```sh
make          # go build -o holo .
make test     # go test -race ./...
make perf     # benchmarks on synthetic data
make update   # go get -u ./... && go mod tidy
```

See [DESIGN.md](DESIGN.md) for how sessions are parsed and searched.

## Caveats

Claude Code's session file format is undocumented and may change between
versions. Unknown records and malformed lines are skipped.
