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

// A default fixed in the settings applies each time the view is opened; the
// switch inside the view still changes it while it stays open. "Last used"
// leaves what was remembered.
test('the settings can fix what the notes and task views open on', () => {
  assert.match(notes, /\$: if \(active && !wasActive\) \{\s*wasActive = true;\s*applyDefaultScope\(\);/,
    'opening the notes view ignores the configured default');
  const apply = notes.slice(notes.indexOf('function applyDefaultScope'));
  assert.match(apply.slice(0, 200), /fixed === 'tab' \|\| fixed === 'session'/);

  const tasks = read('../src/lib/components/MainPanel/TaskPanel.svelte');
  assert.match(tasks, /\$: if \(active && !wasActive\) \{\s*wasActive = true;\s*applyDefaultTabFilter\(\);/,
    'opening the task view ignores the configured default');

  const dialog = read('../src/lib/components/Dialogs/SettingsDialog.svelte');
  assert.match(dialog, /saveSettings\(\{ notesDefaultScope: e\.detail as NotesDefaultScope \}\)/);
  assert.match(dialog, /saveSettings\(\{ tasksDefaultFilter: e\.detail as TasksDefaultFilter \}\)/);

  const settingsStore = read('../src/lib/stores/settings.ts');
  assert.match(settingsStore, /notesDefaultScope: 'last',/, 'a fresh install does not start on "last used"');
  assert.match(settingsStore, /tasksDefaultFilter: 'last',/);
});

// Each half of the switch carries a dot when its note has something in it,
// so an empty note is not opened just to find out.
test('the switch marks which note has something in it', async () => {
  const { notePresence } = await import('../src/lib/utils/noteScope.ts');
  const base = { storedTab: '', storedSession: '' };

  // The open note follows what is typed, saved or not.
  assert.deepEqual(notePresence({ ...base, open: 'tab', openText: 'draft' }), { tab: true, session: false });
  assert.deepEqual(notePresence({ ...base, open: 'tab', openText: '   ' }), { tab: false, session: false },
    'whitespace alone counts as a note');

  // The other note: this view's own copy wins over the stored one, which the
  // session list may not have caught up with yet.
  assert.deepEqual(notePresence({ open: 'tab', openText: '', otherDraft: '', storedTab: '', storedSession: 'old' }),
    { tab: false, session: false }, 'a note just emptied still shows its stored text');
  assert.deepEqual(notePresence({ open: 'session', openText: 'x', storedTab: 'kept', storedSession: '' }),
    { tab: true, session: true });

  assert.match(notes, /\{#if presence\.tab\}<span class="scope-dot"/);
  assert.match(notes, /\{#if presence\.session\}<span class="scope-dot"/);
});
