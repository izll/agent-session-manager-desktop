<div align="center">

<img src="build/logo-readme.png" alt="ASMGR Desktop" width="180">

# Agent Session Manager — Desktop

**Run a whole team of AI coding agents from one window.**
Claude, Codex, Gemini, Aider and more — each in its own live terminal, all
side by side, all persistent.

[![Release](https://img.shields.io/github/v/release/izll/agent-session-manager-desktop?style=flat)](https://github.com/izll/agent-session-manager-desktop/releases)
[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://go.dev)
[![Wails](https://img.shields.io/badge/Wails-v2-DF0000?style=flat)](https://wails.io)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

<img src="docs/screenshot-session.png" alt="A running Claude session in Agent Session Manager Desktop" width="900">

</div>

---

You've probably got three terminal windows open, each with a different agent
grinding away, and you keep alt-tabbing to check which one is stuck waiting on
you. **Agent Session Manager Desktop** puts them all in one place: every agent
gets its own tab, keeps running in the background, and tells you the moment it
needs your attention — even on your phone.

It's the graphical counterpart of the
[**ASMGR TUI**](https://github.com/izll/agent-session-manager), built with
[Wails](https://wails.io) (Go) + Svelte + [xterm.js](https://xtermjs.org).
Because every session lives in its own multiplexer session — `tmux` on Linux
and macOS, `psmux` on Windows — your agents survive closing the window,
restarting the app, even a machine hop: reattach and pick up exactly where you
left off.

> Prefer the terminal? The original TUI version lives at
> [izll/agent-session-manager](https://github.com/izll/agent-session-manager).

## Why you'll want it

- **Never miss a waiting agent again.** When an agent stops to ask you
  something, the header shows a ⏳ count you can click to jump straight to it —
  or answer *yes / no / Enter / Esc* right from the dropdown without switching
  tabs. Turn on desktop notifications and **ntfy mobile push** to get pinged
  wherever you are.
- **Everything keeps running.** Close the window, reboot the app, come back
  tomorrow — the sessions are still there and still working, because they live
  in the multiplexer rather than in the app. Reattach in one click.
- **See the whole picture.** The project dashboard lays out every session
  grouped like your sidebar, with live activity, per-repo Git status (branch,
  dirty state, ahead/behind, last commit) and your **Claude and Codex/GPT
  usage** — how much of your rate-limit window is left — at a glance.
- **Reach for the right agent instantly.** Search every session, group them
  into projects, star your favorites, and search *inside* a terminal's
  scrollback with `Ctrl+Shift+L`.
- **Read what it wrote, without leaving.** Browse and edit the project's files
  with syntax highlighting, search them by name or content, and review the
  session's diff file by file — reverting a single hunk if you disagree with
  it.

## Features

**Agents & sessions**

- **Many agents, your choice** — Claude, Gemini, Aider, Codex, Amazon Q,
  OpenCode, a custom command, or a plain shell.
- **Multi-tab sessions** — several agents or terminals per session, each its own
  multiplexer window, with a per-tab working directory if you want one
  somewhere else.
- **Resume & fork** — continue a previous conversation, or fork a Claude thread
  into a new tab or a brand-new session to explore a different path.
- **Background agents** — a dedicated panel lists Claude's `--bg` / Ctrl+B
  background agents; attach one into a tab or a new session, tail its logs, or
  stop it. Accidentally sent a session to the background? It reattaches cleanly
  on the next resume.
- **Session templates** — save an arrangement you keep rebuilding (an agent,
  its arguments, and the tabs beside it) and create it again in one step. Pin a
  template to a directory or leave it reusable and pick the directory when you
  use it.
- **Saved commands** — a searchable library on `Ctrl+P`, organised into groups.
  Commands can take parameters — `{{name}}`, or `{{name:default}}` — and you
  are prompted for them before the command runs.

**Staying in control**

- **Attention inbox** — the ⏳ dropdown of every tab waiting on input, with
  one-click replies, no tab switching.
- **Desktop + mobile notifications** — get told the instant an agent starts
  waiting, via `notify-send` and/or an ntfy topic on your phone. Fully opt-in.
- **Live status everywhere** — busy / waiting / idle dots and status lines in
  the sidebar and on the tab headers, read straight from the panes; hide a
  chatty tab's status line per-tab when you don't want the noise.
- **YOLO indicator** — shows when an agent is running unattended (Claude's
  *bypass permissions* / *auto mode*), read live so it tracks a Shift+Tab
  toggle.

**Seeing your work**

- **Project dashboard** — the bird's-eye view: grouped session cards, Git
  status per repo, and Claude / Codex usage windows.
- **Activity statistics** — locally-observed, per-project agent activity over
  time.
- **Diff & notes** — review a session's Git changes file by file, grouped into
  a directory tree, and revert a single file or a single hunk. Huge diffs are
  guarded so they never freeze the UI. Keep per-tab notes.
- **File browser & editor** — read and edit the files your agent is working on
  without leaving the app. Syntax highlighting for a dozen languages while both
  reading and editing (CodeMirror 6, with grammars loaded on demand), file
  search by name or content with `Ctrl+Shift+O`, and colour-coded file types.
  Saving preserves a file's line endings, BOM and trailing newline exactly, so
  opening and saving an untouched file leaves it byte-identical.
- **Current branch** — the working directory's Git branch beside the session
  name or in the status bar, following the pane as you `cd` around, with a
  read-only list of the repo's other branches.
- **Task Master** — optional MCP-backed task list per session, right in the
  panel.

**Comfort & polish**

- **Voice dictation** — talk to your agent instead of typing (free or API
  speech-to-text modes).
- **Make it yours** — eight interface accents or any colour you like; per-agent
  and per-tab terminal palettes, including ones you define; adjustable terminal
  font size globally, per tab, or with `Ctrl` + scroll; colour a session or a
  group, and hide the view or status bar on tabs that don't need them.
- **Rebindable shortcuts** — every keyboard shortcut is editable in Settings,
  and the help dialog is generated from the same list, so it always shows what
  the keys actually do. Shortcuts you don't use can be switched off; the few
  whose meaning comes from context (`Esc`, `Enter`, `/`) are shown but fixed.
  Keys named throughout this README are the defaults.
- **Selectable terminal renderer** — canvas (default), WebGL, or DOM,
  switchable live from Settings.
- **Undo a deletion** — deleted sessions and tabs go to a recycle bin you can
  restore from, kept for a period you choose.
- **Picks up where you left off** — the tab you were on, the view you had open,
  and your place in a file all come back when you return to a session.
- **20 languages** — the whole UI is translated.
- **Safe alongside itself** — open a second window and it won't stomp on the
  first one's terminals; it warns you and stays read-only for that project.
- **Self-updating** — checks for new releases in the background and updates in
  place on all three platforms, including `.deb` / `.rpm` installs and the
  macOS `.app` bundle. The Windows installer is per-user, so updating never
  needs administrator rights.
- **Take it with you** — export sessions to a file and import them on another
  machine.

<img src="docs/screenshot-dashboard.png" alt="The project dashboard: grouped sessions with Git status and agent usage" width="900">

## Install

Grab the latest build for your platform from the
[**Releases**](https://github.com/izll/agent-session-manager-desktop/releases)
page.

**Linux**

```bash
# Debian / Ubuntu
sudo dpkg -i asmgr-desktop_*_linux_x86_64.deb

# Fedora / RHEL
sudo rpm -i asmgr-desktop_*_linux_x86_64.rpm
```

Installs to `/usr/bin/asmgr-desktop` with an app-menu entry; runtime deps
(`libwebkit2gtk-4.1-0`/`webkit2gtk4.1`, `tmux`) are pulled in automatically.

**macOS** (Apple Silicon; Intel via Rosetta 2)

```bash
tar -xzf asmgr-desktop_*_darwin_arm64.tar.gz   # → asmgr-desktop.app
# move it to /Applications, then: brew install tmux
```

**Windows** (x64) — run `asmgr-desktop_*_windows_amd64_setup.exe`. It installs
per-user, so it never asks for administrator rights and in-place updates work.
There is also a `.tar.gz` if you would rather not install anything.

> ⚠️ **A terminal multiplexer is required at runtime.** Sessions run inside it,
> so the app cannot create one without it — it says so on startup and offers the
> install command.
>
> - **Linux** — `tmux`, pulled in automatically by the `.deb` / `.rpm`.
> - **macOS** — `brew install tmux`.
> - **Windows** — [psmux](https://github.com/psmux/psmux), a native Windows
>   multiplexer that speaks tmux's command language; no WSL or MSYS2 needed.
>   The app can install it for you with `winget`, or `winget install psmux`.

To build from source instead, see [Build](#build) below.

## Requirements

- Go 1.24+
- Node.js + npm
- `tmux` (Linux, macOS) or [psmux](https://github.com/psmux/psmux) (Windows)
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

## How it works

- Each session is a multiplexer session — `tmux` on Linux and macOS, `psmux` on
  Windows — so agents keep working when the window is closed and reattach
  instantly.
- The terminal talks to the agents over a local, token-authenticated WebSocket
  (xterm.js ⇄ Go ⇄ multiplexer), which keeps typing latency low.
- Output from a tab you are not looking at is held in the backend and replayed
  when you return to it, rather than being dropped and repainted. One WebKit
  main thread serves every tab, so a busy background agent must not be able to
  starve the one you are typing in.
- Session storage lives under `~/.config/agent-session-manager-desktop/`.

## License

MIT — see [LICENSE](LICENSE).
