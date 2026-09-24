import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const notes = read('../src/lib/components/MainPanel/Notes.svelte');
const panel = read('../src/lib/components/MainPanel/MainPanel.svelte');
const app = read('../../app.go');

// A note can be this tab's or the session's. The session's is its own target,
// window -1, so drafts, save queues and the unsaved guard treat it as just
// another note — and the backend has to read -1 the same way.
test('the session note is the same target on both sides', () => {
  assert.match(notes, /const SESSION_NOTES = -1;/);
  assert.match(app, /const SessionNotesWindow = -1/);
  const load = notes.slice(notes.indexOf('async function loadNotes'));
  assert.match(load.slice(0, 300), /const windowIdx = targetWindowIdx\(\);/,
    'loading ignores the chosen scope');
});

// Switching scope is a change of target: the edit in progress is kept as a
// draft and saved, and the other note is loaded.
test('switching between the tab and the session reloads the note', () => {
  assert.match(notes, /\$: wantedWindowIdx = scope === 'session' \? SESSION_NOTES : \$selectedWindowIdx;/);
  assert.match(notes, /\$: if \(\$activeProjectId !== lastProjectId \|\| \$selectedSessionId !== lastSessionId \|\| wantedWindowIdx !== lastWindowIdx\)/,
    'a scope switch does not save the draft and load the other note');
  assert.match(notes, /localStorage\.setItem\(SCOPE_KEY, next\)/, 'the choice is not remembered');
});

// The main tab has its own note now, and it is found by not being a followed
// tab — index 0 is only the main tab while tmux's base-index is 0.
test('the notes dot reads the main tab\'s own note and the session\'s', () => {
  const at = panel.indexOf('$: currentTabNotes =');
  const body = panel.slice(at, panel.indexOf('})();', at));
  assert.doesNotMatch(body, /\$selectedWindowIdx === 0/, 'the main tab is still taken to be index 0');
  assert.match(body, /currentSession\.mainTabNotes/, 'the main tab shows the session note as its own');
  assert.match(panel, /\{#if currentTabNotes \|\| currentSessionNotes\}/,
    'a session note leaves no dot');
});

test('the scope labels are translated everywhere', () => {
  const dir = new URL('../src/lib/i18n/locales/', import.meta.url);
  for (const name of readdirSync(dir).filter((n) => n.endsWith('.json'))) {
    const strings = JSON.parse(readFileSync(new URL(name, dir), 'utf8'));
    for (const key of ['notes.scopeTab', 'notes.scopeSession', 'notes.scopeTabHint', 'notes.scopeSessionHint']) {
      assert.ok(strings[key]?.trim(), `${name} has no ${key}`);
    }
  }
});
