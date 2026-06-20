# ASMGR Desktop — Agent Session Manager

A desktop GUI for managing multiple AI coding-agent sessions side by side, built
with [Wails](https://wails.io) (Go) + Svelte + TypeScript + [xterm.js](https://xtermjs.org).
Each session runs in its own `tmux` session, so agents keep working in the
background even when the window is closed, and you can reattach any time.

It is the desktop port of the ASMGR TUI.

## Features

- **Multi-agent** — Claude, Gemini, Aider, Codex, Amazon Q, OpenCode, a custom
  command, or a plain terminal.
- **Multi-tab sessions** — several agents/terminals per session, each a `tmux`
  window. Per-tab activity dots (working / waiting) right in the tab headers.
- **Projects & groups** — organize sessions; favorites; search/filter.
- **Live status** — busy / waiting / idle indicators and status lines in the
  sidebar, polled from the panes.
- **Session resume & fork** — continue previous conversations; fork a Claude
  conversation into a new tab or session.
- **YOLO indicator** — shows when an agent is running without prompting
  (Claude's *bypass permissions* / *auto mode*), read live from the pane so it
  follows a Shift+Tab toggle.
- **Diff & notes** — view git changes per session (large diffs are guarded so
  they never freeze the UI); per-tab notes.
- **Selectable terminal renderer** — canvas (default), WebGL, or DOM, switchable
  at runtime from Settings.

## Requirements

- Go 1.24+
- Node.js + npm
- `tmux`
- Linux: WebKitGTK. On Ubuntu 24.04+ / Fedora 40+ only `webkit2gtk-4.1` is
  available — build with the `webkit2_41` tag (see below).
- [Wails CLI](https://wails.io/docs/gettingstarted/installation)

## Build

```bash
# Linux with webkit2gtk-4.1 (Ubuntu 24.04+, Fedora 40+):
wails build -tags webkit2_41

# Other / older WebKitGTK:
wails build
```

The binary is written to `build/bin/`.

### Development

```bash
wails dev -tags webkit2_41
```

`wails dev` also serves the frontend at <http://localhost:34115>, so you can open
it in a regular browser (with Go methods bridged) for fast iteration.

## Notes

- The terminal talks to the agents over a local WebSocket (xterm.js ⇄ Go ⇄
  `tmux`), which keeps typing latency low.
- Session storage lives under `~/.config/agent-session-manager-desktop/`.

## License

MIT — see [LICENSE](LICENSE).
