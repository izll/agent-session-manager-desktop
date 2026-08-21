import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * The commit history follows the session you are looking at.
 *
 * Its path prop tracks the selected session, so switching to a tab in another
 * repository has to reload. It did not: the reactive block fired only on `show`,
 * and even when it did rerun, the branch from the previous repository was still
 * in hand — GetGitHistory was then asked for a branch the new repository has
 * never heard of, on top of a commit list and diff left over from the old one.
 */
const dialog = readFileSync(
  new URL('../src/lib/components/Dialogs/GitHistoryDialog.svelte', import.meta.url),
  'utf8',
);

// The trigger has to see the path, not only the open flag.
assert.match(
  dialog,
  /\$: if \(show && sessionId && path && repositoryKey !== openedRepositoryKey\)/,
  'reopening must be driven by the full session/tab/root identity as well as by show',
);

// Guarded against the path last opened rather than a plain boolean: this block
// reruns for unrelated state too, and without the comparison it would reload
// the history on every keystroke in the dialog.
assert.match(dialog, /openedRepositoryKey = repositoryKey;/, 'the guard must record which repository identity was opened');
assert.match(
  dialog,
  /\$: if \(!show && openedRepositoryKey\) \{[\s\S]*?openedRepositoryKey = '';/,
  'closing must clear the guard, or reopening the same session shows stale history',
);

// Everything below belongs to the repository being left. The branch is the one
// that actually breaks the query; the rest merely lingers on screen.
const openBody = dialog.match(/async function open\(target: RepositoryTarget\) \{[\s\S]*?await Promise\.all/);
assert.ok(openBody, 'open() should still exist');
for (const field of ['branch', 'branches', 'currentBranch', 'commits', 'selectedHash', 'files', 'selectedPath', 'diff']) {
  assert.match(
    openBody[0],
    new RegExp(`\\b${field} = `),
    `open() must reset ${field} — it belongs to the previous repository`,
  );
}


// And the path handed to it has to be the TAB's directory, not the session's.
//
// This is what the first fix missed: reloading on a path change is useless if
// the path never changes. selectedSession derives from selectedSessionId alone,
// so switching between tabs of one session left it identical — and a tab can be
// opened in its own directory, or cd-ed somewhere else entirely.
const app = readFileSync(new URL('../src/App.svelte', import.meta.url), 'utf8');

assert.match(
  app,
  /GetTabWorkingDirectory\(session\.id, windowIdx\)/,
  "the history must open on the tab's real directory, resolved from tmux",
);
assert.doesNotMatch(
  app,
  /<GitHistoryDialog[^>]*path=\{\$selectedSession\?\.path/,
  'the session path ignores which tab is on screen',
);
// Both ways in — the header button and the shortcut — must resolve it, or one
// of them opens on whatever the last resolution left behind.
const shortcutOpens = app.match(/case 'history\.show':[\s\S]{0,200}?return;/);
assert.ok(shortcutOpens, "the history.show shortcut should still exist");
assert.match(
  shortcutOpens[0],
  /handleShowGitHistory\(\)/,
  'the shortcut must go through the same resolution as the header button',
);

console.log('historyPathSwitch: ok');
