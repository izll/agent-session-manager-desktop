import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * The files view belongs to the tab, and a dialog owns the keyboard.
 *
 * Two faults with one shape: something reads the session when it should read
 * the tab, or reads the pane when a dialog is up.
 */
const browser = readFileSync(
  new URL('../src/lib/components/MainPanel/FileBrowser.svelte', import.meta.url),
  'utf8',
);
const terminal = readFileSync(
  new URL('../src/lib/utils/terminal.ts', import.meta.url),
  'utf8',
);

// A tab can be opened in its own directory, so the tree has to reload when the
// tab or project changes — every tab of one session carries the same session id,
// and separate projects may legitimately reuse both ids.
assert.match(
  browser,
  /\$: browseKey = `\$\{\$activeProjectId\}:\$\{\$selectedSessionId \?\? ''\}:\$\{\$selectedWindowIdx \?\? 0\}`/,
  'the tree must be keyed on project, session, and tab',
);
assert.match(
  browser,
  /if \(active && browseKey !== loadedBrowseKey\)/,
  'reloading must compare the tab key, not the session id',
);

// Every call that reads or writes a file has to say which tab it is for, or the
// backend resolves the session's directory instead.
const calls = browser.match(/App\.(ListSessionDirectory|ReadSessionDirectoryFile|OpenSessionFileForEdit|SaveSessionFileEdit)/g) ?? [];
assert.equal(calls.length, 4, 'expected the four browse entry points');
const liveWindowIdx = browser.match(/get\(selectedWindowIdx\) \?\? 0/g) ?? [];
assert.ok(liveWindowIdx.length >= 3, 'browse reads must capture the selected window index');
assert.match(
  browser,
  /App\.SaveSessionFileEdit\([\s\S]*?windowIdx,[\s\S]*?\);/,
  'save must use the editor target snapshot, not whichever tab is selected when it returns',
);

// xterm listens on its own textarea, below the window listeners a dialog uses,
// so it sees a key first and stopPropagation cannot help. Escape closed the
// commit history and reached the pane behind it at the same time.
assert.match(
  terminal,
  /if \(document\.querySelector\('\.dialog-overlay'\)\) \{\s*return false;/,
  'the terminal must decline keys while a dialog is open',
);

console.log('tabScopedBrowsing: ok');
