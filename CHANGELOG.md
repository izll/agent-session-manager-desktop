# Changelog

Notable changes to Agent Session Manager Desktop, newest first.

Entries describe what changed for someone using the app. Internal refactoring,
test and CI work is left out unless it changed behaviour. Dates are release
dates; the format follows [Keep a Changelog](https://keepachangelog.com).

## 0.9.28 — 2026-08-14

### Added

- **Backups now go back months, not hours.** They were the newest 25, whatever
  they happened to be — on a busy day that was under a day of history, and
  anything noticed the next morning was already gone. They are kept by age now:
  everything from the last hour, one an hour for today, one a day for a
  fortnight, one a week for a quarter.
- **Tasks are backed up too**, and restore on their own tab in the recovery
  centre — recovering a deleted task should not roll every session and setting
  back with it. They live in each working directory rather than in the store
  everything else comes from, so nothing had been watching them.
- **The commit message pane in the history view can be resized.** It was a
  fixed height that held a subject line and little else, so a commit with a
  real body had to be scrolled a few lines at a time.

### Fixed

- **The mouse wheel would not scroll far back** in a session, though
  Ctrl+PageUp went much further. The pane kept a thousand lines while tmux kept
  fifty thousand; it keeps five thousand now and tmux takes over past that.
- **On some machines the wheel did nothing at all** in a terminal. Where tmux's
  own default is `mouse off`, the sessions the terminal actually attaches to
  never had it turned on.
- **A line with a background colour stopped a few pixels short of the pane's
  edge**, showing black behind it. The scrollbar is gone from terminal panes as
  well, since tmux does the scrolling.
- **The terminal ignored its theme's background** in the margins around the
  text, which showed as black on any theme that is not.
- **The task badge in the header only updated every five minutes**, so ticking
  a task off left the old number in place.
- **"Back up now" did nothing** when nothing had changed since the last
  automatic backup — reasonable for an automatic one, but not for a button
  pressed to make a restore point.
- Dialogs were capped at a width that every one of them overrode; the recovery
  centre is wider, and switching between its tabs no longer resizes the window.

## 0.9.27 — 2026-08-14

### Added

- **Deadlines on tasks**, with a date and a time. The list marks one that has
  passed in red and one due within a day in amber, and always spells the date
  out rather than relying on colour alone.
- **A view of every task from every project**, reached from the header and
  grouped by session, ordered by deadline. Tasks with no deadline sort last
  rather than first, where they would sit ahead of the one due this afternoon.
  The button carries a badge with the number still open.
- **Deleting a session says what work it still has.** Only tasks assigned to
  that session count — warning about a project's whole backlog on every close
  is how a prompt gets ignored.
- **Find in notes** (Ctrl+F), with a match count, Enter/F3/Ctrl+G to step
  through and Shift to go back.
- **Find in the side-by-side diff**, matching what is displayed rather than the
  markup, so a search for "span" does not hit every coloured token. A row
  matches if either column does.
- **Find in the file browser** — CodeMirror's own, so it brings case matching,
  whole word, regular expressions and replace with it.
- **Undo and redo in notes** (Ctrl+Z, Ctrl+Y).
- **Ten seconds to undo a task change** from a bar at the bottom of the screen:
  ticking a subtask, moving a task, removing a dependency, and deleting either.
- **Open a file from the diff**, at the line showing at the top of the pane
  rather than at the file's first change — in a long file that is nowhere near
  what you were reading.
- **Filtering tasks by text**, now also searching implementation notes and
  subtask titles, where a half-remembered line usually lives.

### Fixed

- **Subtask checkboxes never ticked** outside Task Master mode, and could not be
  unticked once they did. The two backends describe completion differently, and
  the panel read one while writing the other — so a tick saved successfully and
  came back empty.
- **An unticked checkbox was a solid white square**, the brightest thing on a
  dark row.
- **Dependencies asked for a task id** — a number shown nowhere else in the
  app. They are picked from a filterable list of task titles now.
- **Scrollbars were 6px**, which is hard to hit: the pointer has to land within
  a few pixels, and missing it scrolls the page instead. They are 16px now,
  with the visible bar still slim.
- **Leaving the diff tab and coming back restarted the review** — the file list
  blinked away, the selection went, and the pane returned to the top.
- **The side-by-side diff had no background of its own**, so in the
  commit-history dialog the backdrop showed through and tinted the code.
- Task badges line up in columns, so priority sits above priority however much
  else a row carries.

## 0.9.26 — 2026-08-12

### Fixed

- **The app could die a second after starting.** On some machines it never got
  as far as a window, exiting with a signal-handler error instead. The cause
  was a race inside WebKit rather than anything about the build — the same
  binary would start from one directory and crash from another, which is why it
  looked machine-specific. Fixed by a Wails update that closes the race.
- **"Copy on select" now does what it says.** Dragging out a selection put
  nothing on the clipboard, and the setting made no difference either way. Three
  things were in the way at once, so fixing any one of them changed nothing
  visible. The setting now decides for both a drag and a double or triple click,
  and it no longer needs `xclip` installed — which is what made it appear to
  work on a machine that happened to have it set up by hand, and nowhere else.
  Agents that handle the mouse themselves still do: Claude Code in fullscreen
  mode keeps its own selection behaviour, and the setting's description says so.

## 0.9.25 — 2026-08-11

### Added

- **Forking is instant, and Codex sessions can fork too.** A fork used to spin
  a spinner for minutes and spend a turn to do it: it replayed the whole
  conversation just to find out what the branch would be called, and left a
  stray "." and its reply at the head of it. The branch is now made as the
  forked tab starts, which costs nothing. A fork to a new session also switches
  to it rather than leaving it to be found in the sidebar, and asks which group
  it belongs in.

### Fixed

- **A large diff was slow to open.** Only the visible lines were rendered, but
  every line in the file was syntax-coloured first — a 5,000-line file cost
  287ms before anything appeared, and now costs under a millisecond. Colouring
  itself is unchanged.
- **The two columns of a side-by-side diff scroll sideways together**, so a
  long line can be compared at the same place on both sides. The colour of a
  changed line now also reaches the end of the pane instead of stopping partway
  along, and the line numbers stay in view while the code scrolls under them.
- **A forked tab went back to the original conversation when restarted** — it
  had stored the id it was branched from rather than the branch's own.
- **A tab never showed its conversation id**, which was most obvious on a forked
  tab. The same mistake had been quietly disabling the warning that a
  conversation is already open somewhere else.
- **A fork of a Codex session came back as a Claude one.**

## 0.9.24 — 2026-08-08

### Added

- **The tasks tab shows how many tasks are still waiting.** The size of a
  session's backlog was only visible by opening the view.

## 0.9.23 — 2026-08-08

### Added

- **A side-by-side diff, with the two sides scrolled independently.** The
  columns are kept in step by what pairs with what, not by a shared position:
  where one side has nothing to show — a run of new lines, or lines only
  removed — it waits while the other runs past, and picks up again at the far
  end. A band between the panes joins each change to what it replaced, in the
  colour of the change and coming to a point on the side it does not appear
  on, so "three lines became one" is read across rather than counted.
- **Revert a single block, not just a whole hunk.** The `»` arrows now work in
  the whole-file view as well, where a file arrives from git as one hunk and
  reverting used to mean discarding every change in it. After a revert the
  view returns to where that block was instead of jumping to the top of the
  file.
- **Switching tabs and back resumes the diff where you left it** — the file,
  the scroll position, and the change you were stepping through.

### Fixed

- **Reverting a change at the very end of a file failed** with "patch does not
  apply", leaving the change in place.

## 0.9.22 — 2026-08-07

### Added

- **A quick-jump window on `Ctrl+J`** — a hand-ordered list of the tabs and
  sessions you keep returning to. The first nine answer to the number keys, so
  the order is the point: arrange it and "3" is always the build tab. The
  arrows walk the rest, including everything past nine. `Alt+J` adds what is
  on screen, and both the tab and session context menus offer the same.
  Entries can be reordered by dragging or with `Alt+↑↓`, given a note of your
  own with `F2`, and each row shows whether it is busy, waiting or idle. An
  entry whose session has stopped keeps its place rather than renumbering
  everything below it.
- **Terminal tabs remember where you left them** (from 0.9.21), now also
  restored when a single tab is restarted rather than the whole session.

### Fixed

- **A tab you were not looking at could come back missing its scrollback.**
  Output held for a hidden tab was discarded entirely once it passed a limit,
  and the limit was sized for plain text rather than for an agent redrawing
  its screen many times a second. The newest output is now kept and only the
  oldest falls away, and closing a tab releases what it was holding.

## 0.9.21 — 2026-08-06

### Added

- **Terminal tabs remember where you left them.** A terminal tab you had
  navigated into a subdirectory used to come back at the project root, so you
  had to find your way there again. Agent tabs keep their configured
  directory, since that is part of what identifies the conversation they
  resume.
- **The shell a terminal tab runs is selectable** in Settings → Terminal. On
  Windows that means Command Prompt or PowerShell; elsewhere the list is the
  shells the system actually has.

### Fixed

- **A restarted terminal tab could start the wrong shell — or none at all.**
  Creating a tab lets the multiplexer pick the shell, but restarting one had
  to name it, and the name it used was the `$SHELL` variable. Windows has no
  `$SHELL`, so every restart asked for bash and got an empty pane that died on
  arrival. On Linux and macOS the tab could quietly come back running a
  different shell from the one it was created with, since tmux starts the
  shell from your account entry and ignores `$SHELL`.
- **Two settings sections were both called "Megjelenés" in Hungarian.** The
  second one is the terminal's rendering options, and now says so.

## 0.9.20 — 2026-08-06

### Fixed

- **The dictation log never wrote anything, in any release.** The file was
  opened by a function nobody called, so every line was discarded — turning
  logging on in Settings changed nothing and the log stayed empty. Anyone
  looking there for the reason a dictation had failed found nothing at all.
  It now records what happened, and failures are written whether or not
  verbose logging is switched on: a failure is discovered after the fact,
  when turning logging on and reproducing it is no longer possible.
- **A failed dictation blamed the API key, whatever the actual cause.** Google
  reports an expired key and a malformed request under the same error code, so
  everything from an audio problem upwards was reported as an invalid or
  expired key — sending you to check the one thing that was fine. Failures now
  say what Google actually said, and only name the key when the key is the
  problem.

## 0.9.19 — 2026-08-05

### Added

- **The log has its own dialog**, opened from Settings → Maintenance, wide
  enough to read a stack trace without wrapping. It shows either the
  application log or the dictation log, filters by text, and hides the noisiest
  polling lines by default so what is left is worth reading. Each log can be
  cleared when it has served its purpose.
- **Dictation writes to a log of its own.** Its diagnostics used to go to the
  terminal, which is nowhere at all when the app is started from the desktop —
  the one place a dictation problem could be explained was the one place nobody
  was looking.

### Fixed

- **Dictation could fail silently in streaming mode.** A rejected API key, an
  exhausted quota, or a dropped connection ended the stream without a word: the
  microphone level kept moving, so it looked like it was working while nothing
  was being transcribed. Every one of those now says what happened and what to
  do about it. API mode already reported a bad key; the two modes now agree.
- **Some error messages came out wrong or blank.** The backend was sending
  finished sentences where the interface expected translation keys, and two of
  the keys it did send were not defined in any language. Both sides now use the
  same keys, and all seven exist in all 20 languages.
- **Error messages could be hidden behind the dialog that caused them.** A
  dictation failure reported while the settings dialog was open — which is
  exactly where dictation is set up and tested — was drawn underneath it.
- **The settings dialog could freeze on the Dictation tab.** An empty list of
  problems arrived as nothing at all rather than as an empty list, which stopped
  the interface redrawing: the window stayed up, hover still worked, and
  nothing else responded.
- **Dictation could hang the app for good when the sound server was wedged.**
  Eight calls out to `pactl` had no time limit, so any one of them that never
  returned took the app with it. They now give up after three seconds.
- **The log could grow without bound.** One measured run had reached 68,266
  lines; it is now capped, and truncating it no longer leaves the file padded
  with empty space.
- **The API key could not be checked for typos.** It is now revealable with an
  eye button, like a password field.

## 0.9.18 — 2026-08-05

### Added

- **The application log is viewable from Settings → Maintenance**, with buttons
  to refresh it, copy it, and open the folder it lives in. It shows the end of
  the current run, which is where the explanation for something going wrong
  usually is.

### Fixed

- **Forking could hang with no way out.** The call that branches a conversation
  had no time limit, and it replays the whole conversation before answering — a
  stalled one left the dialog spinning indefinitely. It now gives up after
  three minutes and says so, and a refusal from Claude is reported in its own
  words rather than as "exit status 1".

## 0.9.17 — 2026-08-05

### Fixed

- **Forking a conversation branched the wrong one from any tab but the first.**
  Fork read the session's main window every time, so working in a second Claude
  tab and pressing Fork produced a branch of a different conversation — under
  the name you had chosen for the work in front of you, and without any sign
  that anything was wrong. The tab you are on is now the one that gets branched.
- **The Fork button appeared on tabs that have no conversation** — a terminal
  tab inside a Claude session — and was missing from Claude tabs inside a
  session of another agent.
- **A fork that could not start left a session behind that could never run.**
  The dialog closed as though it had worked. It now checks what it needs before
  creating anything, and reports the failure instead of logging it.
- **A forked tab started without the session's arguments**, so the branch ran
  differently configured from the tab it came from. It also failed to launch
  when the conversation was held by a background agent.
- **The new tab was created but not opened** — you had to go and find it.
- **The API key in Settings can be revealed** with an eye button, for checking
  a paste.

## 0.9.16 — 2026-08-05

### Added

- **Review a change inside the file it lives in.** The diff showed a few lines
  either side of each change, which is enough to see what changed and rarely
  enough to judge it. It now opens on the whole file with the changes marked,
  as an IDE does, with a button back to the previous view. Long files stay
  affordable because only the visible lines are rendered — which also removes
  the 2000-line cap that used to truncate large diffs.
- **Step through a review.** `↑`/`↓` in the file header, or `Ctrl+F7` and
  `Ctrl+Shift+F7`, move between changes — running past the end of a file into
  the next one, and past the last back to the first. The file a step will move
  to is named at the edge it moves towards.
- **The diff can sit above what you are working on** instead of replacing it,
  on a divider you can drag. It is available above the terminal, the notes, the
  file browser and the task list.
- **Syntax colouring in the diff**, reusing the grammars the file browser
  already loads. Kotlin, Java, C#, C, C++, Scala, Dart, PHP, Ruby, Lua and
  Swift are new — and Go, which was barely coloured at all.
- **Every keyboard shortcut is editable**, in Settings → Shortcuts. The help
  dialog is generated from the same list, so it always shows what the keys
  actually do. Shortcuts can be switched off; the few whose meaning comes from
  context (`Esc`, `Enter`, `/`) are shown but fixed.
- **The task list works without Task Master.** Tasks are stored by the app
  itself, so adding, editing, completing and ordering them never needed the
  integration — but the whole view was hidden with it. Only the AI-backed
  actions are gated now. Finished tasks can be ordered by when they were ticked
  off.
- **The app says when it is closing down.** Shutdown saves each session's
  place, detaches cleanly and reaps what it started, which takes long enough to
  look like a hang.

### Fixed

- **Dropdowns ran off the bottom of the window**, and what was past the edge
  could neither be seen nor scrolled to. They open upwards when there is no room
  below.
- **Sending a task to an agent pressed Escape first**, to dismiss an
  autocomplete popup. Claude Code reads that as "clear the composer", so the
  text just pasted was discarded and the Enter that followed submitted nothing.
- **The weekly usage limits showed no reset time**, and the ones that did showed
  a time with no date — which says nothing for a limit that resets days away.
- **Gradient session colours previewed as a solid bar** with the name invisible
  inside it.
- **Live values on the dashboard did not update.** The busy/waiting indicators
  and tab rows on each card kept whatever they were first drawn with while the
  sidebar beside them changed.
- **Editing a task reported that Task Master was turned off** when saving.
- **Clicking the tab you are already on** did nothing unless the diff was open;
  it returns to the terminal from any view.
- **Task dialogs opened with focus on a header button** rather than the field
  being filled in.

## 0.9.15 — 2026-08-04

### Added

- **The app says when the terminal multiplexer is missing, instead of failing
  around it.** Every session runs inside tmux (Linux, macOS) or psmux
  (Windows), and neither was ever checked for — so creating a session appeared
  to work, saved the session, and only then failed, leaving a permanent entry
  in the sidebar for a session that had never run and never could. A banner now
  says what is missing and gives the command that installs it. On Windows it
  also offers to install psmux with winget, which needs no administrator
  rights. The `.deb` and `.rpm` packages depend on tmux, so a package install
  never sees this.

### Fixed

- **The open tab could not be clicked while the diff was showing.** Clicking
  the tab you were already on did nothing, so with the diff open there was no
  way back to it by mouse. Introduced in 0.9.13, along with the fix that keeps
  the diff open when switching between tabs.

## 0.9.14 — 2026-08-04

### Fixed

- **Windows: the terminal was slow to catch up after resizing the window.**
  Every size change ran three separate multiplexer processes, each costing
  20-31ms here, and one of them re-derived a size that had just been set — from
  the containing window rather than the pane, so on a resize it fought the value
  it followed. Maximising walks through sizes, and they all arrived on the same
  lock keystrokes use, so the pane stopped accepting typing while it caught up.
  One process runs on that path now, and the pane-size check that used to follow
  it happens shortly afterwards, off the interactive path.

## 0.9.13 — 2026-08-04

### Fixed

- **Switching to another conversation inside a tab was not noticed.** Both
  Claude and Codex let you move to a different conversation from inside a
  running session, without restarting anything — so the id recorded when the tab
  opened still pointed at the conversation you had left, and restarting the tab
  reopened that one, or an empty one. Each tab now follows what its agent is
  actually on. For Claude this took three fixes: detection was inspecting the
  MCP servers a tab had spawned rather than the agent itself, it was reading the
  arguments the process was started with rather than what the agent reports, and
  both the poll and the save step were discarding a detected change because a
  value was already stored.
- **Stray keystrokes reached agents that were not expecting them.** The app sent
  a redraw keystroke automatically — after a resize, from a pane-size check on a
  timer, and once when a tab attached. It only means "redraw" to a program that
  chooses to read it that way: Codex's `/resume` picker took it as a keystroke
  and cleared its own screen, and Claude Code turned a run of them into a
  `/clear` typed into the composer. Nothing sends one automatically now; a
  resize signals the program on its own, which is what makes it lay itself out
  again. The Refresh button still sends one.
- **Leftover characters after returning to a background tab.** A hidden tab's
  output was dropped rather than held, so the multiplexer believed the tab was
  up to date and sent only what it thought had changed. Output is now kept while
  a tab is in the background and replayed when it comes back.
- **The diff closed when switching tabs.** Each tab already remembered whether
  it was left on the diff, but the switch itself cleared that memory — and
  cleared it for the tab being opened rather than the one being left, so the
  diff never survived either way. `Ctrl+PageUp`/`PageDown` behaves the same as
  clicking a tab.

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
