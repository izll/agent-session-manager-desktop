# Changelog

Notable changes to Agent Session Manager Desktop, newest first.

Entries describe what changed for someone using the app. Internal refactoring,
test and CI work is left out unless it changed behaviour. Dates are release
dates; the format follows [Keep a Changelog](https://keepachangelog.com).

## 0.9.12 — 2026-08-04

### Added

- **Activity detection patterns can be corrected without a release.** The
  phrases that tell the app an agent is working or waiting for an answer were
  compiled in, so when Claude or Codex reworded a prompt the app stopped
  noticing that agent waiting — and the fix had to wait for a new version. They
  live in a file now, which ships inside the binary and is refreshed from the
  repository in the background. **Settings → Maintenance** has a manual refresh,
  showing the version in force and whether anything changed.
- A changelog, and release notes taken from it. Every release page previously
  carried the same description of the app rather than what had changed.

### Fixed

- **Windows: the installer named "asmgr-desktop" as its publisher.** No company
  name was set, so Wails fell back to the project name. It says "izll" now.
  SmartScreen will still warn — that needs a code signing certificate — but the
  publisher is no longer the project's internal name.

## 0.9.11 — 2026-08-03

### Fixed

- **Windows: the Start menu shortcut was not created.** The per-user switch in
  0.9.10 set the installer's execution level, but the Wails installer macros read
  a separate constant to decide between the all-users and per-user shell folders.
  That constant still said "admin", so an installer running without elevation
  aimed its shortcut at a Start menu it could not write to — and NSIS does not
  report that as an error.
- **Windows: an older system-wide install was not always found.** Its registry
  entry may sit in the 32-bit view rather than the 64-bit one, and an install can
  survive on disk with no registry entry at all. Both are checked now, so the
  installer can offer to remove the old copy instead of leaving it behind.

## 0.9.10 — 2026-08-03

### Fixed

- **Windows: updating in place failed with "Access is denied".** The app writes
  the new build beside the running executable and swaps it, which Program Files
  does not allow without elevation. The installer is per-user now, so neither
  installing nor updating asks for administrator rights, and the quiet background
  update the other platforms have works here too. An existing system-wide install
  is detected and can be removed during setup.
- **macOS: the app was listed as "asmgr-desktop".** Finder and Launchpad take
  that from the bundle's directory name rather than from its display name, so the
  bundle is named after the product now. Updating from an older release still
  works: the updater finds a bundle by its `.app` extension rather than by name,
  and carries the new name across.

## 0.9.9 — 2026-08-03

### Fixed

- **Leftover characters after switching back to a tab.** A hidden tab's output is
  dropped at the source, so xterm holds the frame from when the tab was hidden,
  while tmux believes the client is up to date and sends only what it considers
  changed. Neither a refresh nor a redraw keystroke can clear those leftovers,
  because tmux does not know they are there. The client clears its own screen on
  return now, and the redraw is sent immediately rather than after a delay.
- **Every file in the diff was labelled "modified", new ones included.** The file
  list is built from `git diff --numstat`, which reports line counts and a path
  and nothing else; the file's actual status is read alongside it now.

### Added

- A returning tab shows a spinner until its first output arrives, so a slow pane
  is distinguishable from a hang.
- The diff list opens on modified files. One repository in daily use here has ten
  modified files against 2291 untracked ones from an IDE directory, which buried
  the work being reviewed.

## 0.9.8 — 2026-08-03

Everything below was developed under the 0.9.7 tag, whose release build failed
type-checking before producing any artifacts — there is no 0.9.7 release.

### Fixed

- **Accented characters, box drawing and emoji arrived mangled.** A GUI launch
  inherits no locale, so tmux was not running in UTF-8 mode. The pane's own
  contents were correct throughout, which is why this looked like a font or
  renderer problem and was chased through renderers, Unicode tables, glyph caches
  and font stacks first.
- **Panes kept the size they were created with.** The attach target is replaced
  by a session id so it resolves exactly, but the checks gating the resize, the
  redraw and the mirror cleanup still compared it against a name, so none of them
  ran. A client refresh never ran either: it takes a client, not a session name.
- **A stray "/clear" appeared in Claude Code's composer.** The keystroke that
  makes a terminal program redraw itself is input, and several arriving close
  together — from a resize, a tab switch, and the resize that follows one — were
  read as a command. Redraws are coalesced per tab now.
- **The diff view never finished loading while a build was writing files.** It
  fetched the entire diff up front, then fetched it again to build the file list.
  Files are listed first and loaded one at a time when opened.
- **macOS: agents were reported as not installed.** A GUI launch inherits no PATH
  either, so Homebrew and user bin directories were not searched.
- **macOS: the terminal stayed at 80×24 inside a large window.**
- **Windows: the terminal was unusable with more than one tab.**

### Added

- Open the current directory in the file manager, from the status bar.
- A terminal font selector, and a per-platform renderer default: the canvas
  renderer drops characters on macOS and Windows, so those use DOM.
- Optionally reopen the session that was selected at shutdown.
- The diff file list is grouped by status, with filter buttons.
- macOS no longer asks for Accessibility permission when dictation is off.

## 0.9.6 — 2026-07-29

### Added

- **Windows terminal support without tmux**, driving psmux's control mode over
  pipes. This is what makes the Windows build usable at all: sessions attach,
  keystrokes are delivered, and panes are sized and repainted.

### Fixed

- Attaching directly where the multiplexer cannot mirror a window, and refusing
  to attach to a session that is not running.
- Paths are split on backslashes too, so Windows directories are handled.
- External commands no longer flash a console window on Windows.

## 0.9.5 — 2026-07-28

### Added

- Read and edit files with CodeMirror 6.
- Trashed sessions expire after a configurable period.
- Jump to a favourite with `Ctrl+Shift+1..7`.
- The sidebar width is remembered, and resets on double-click.

### Fixed

- The file browser keeps its place across tab and session switches.
- A tab's note is marked on the Notes tab rather than in the status bar.

## 0.9.4 — 2026-07-27

### Added

- Save a session's arrangement as a reusable template.
- Search files by name and content in the browser.

### Fixed

- Context menus stay on screen, and only one opens at a time.

## 0.9.3 — 2026-07-27

### Added

- A read/write file browser view, with files coloured by type and their contents
  highlighted.
- The diff file list can be shown as a directory tree.
- Each tab remembers which view it was left on.
- A session's colour is reachable from its own context menu.

## 0.9.2 — 2026-07-27

### Added

- The session's git branch is shown, following the pane's real directory.
- Groups can be reordered and coloured.

## 0.9.1 — 2026-07-27

### Added

- The interface accent colour can be changed.

### Fixed

- The black block beside the sidebar scrollbar.
- The settings dialog no longer resizes between tabs.

## 0.9.0 — 2026-07-27

### Added

- A library of saved commands.
- The terminal font size is adjustable, per tab, and can be reset.
- The view and status bars can be hidden per tab.

### Fixed

- Codex waiting for an answer is noticed.

## 0.8.0 — 2026-07-26

### Added

- Export sessions to a file and import them back.
- Updates install in place, with the notice staying visible.

## 0.7.9 — 2026-07-26

### Fixed

- The update check reports what it found.

## 0.7.8 — 2026-07-26

### Added

- Browse the diff by file and revert individual changes.
- Updates are checked for once a day in the background.

### Fixed

- The diff is shown only where there is something to diff.

## 0.7.7 — 2026-07-25

### Fixed

- An unusable GPU is detected by probing it rather than by inspecting devices.

## 0.7.6 — 2026-07-25

### Added

- All 19 translations completed.
- The YOLO and resume badges can be switched off.

### Fixed

- Starting on machines without a usable GPU.

## 0.7.5 — 2026-07-25

### Added

- Sessions reopen on the tab they were left on.
- Terminal colour palettes, with scheme import.
- Plain terminal sessions, with no agent.

## 0.7.4 — 2026-07-25

### Fixed

- Codex sessions survive a tab restart.

## 0.7.3 — 2026-07-20

### Added

- Session recovery, split terminals, and a command palette.

## 0.7.2 — 2026-07-19

### Fixed

- A second instance can no longer kill another's terminals.
- Terminal cleanup is hardened against a crash in xterm's link handling.

## 0.7.1 — 2026-07-17

### Added

- Session search also filters the Favorites section.

## 0.7.0 — 2026-07-17

### Added

- Project activity statistics.
- An attention inbox with quick replies, and a background-agent manager.
- Per-tab working directories.
- Bulk start and stop for a group.
- Status lines can be toggled per tab in the session list.

### Fixed

- Claude sessions stuck in a background agent are released on resume.

## 0.6.0 — 2026-07-16

### Added

- A project dashboard with grouped session cards.
- Claude and GPT/Codex subscription usage on the dashboard.
- Notifications when an agent starts waiting for you.
- Terminal scrollback search (`Ctrl+Shift+L`).
- Per-tab colour customization.

### Fixed

- `Shift+Enter` inserts a newline instead of submitting.

## 0.5.2 — 2026-07-06

### Fixed

- Frameless window resizing under fractional scaling, and a stray black corner.
- A black terminal after stopping and starting a tab.

## 0.5.1 — 2026-06-25

### Added

- macOS and Windows builds.
- `.rpm` packages alongside `.deb`.

### Fixed

- tmux attaches when the app is launched from a desktop menu, where TERM is
  not set to anything usable.

## 0.5.0 — 2026-06-21

First public release.

### Added

- Multiple AI coding agents — Claude, Codex, Gemini, Aider and others — each in
  its own persistent terminal tab, backed by tmux.
- A YOLO button that follows the agent's live mode.
- A choice of terminal renderer.
