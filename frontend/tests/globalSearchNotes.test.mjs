import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const dialog = read('../src/lib/components/Dialogs/GlobalSearchDialog.svelte');
const panel = read('../src/lib/components/MainPanel/MainPanel.svelte');
const notes = read('../src/lib/components/MainPanel/Notes.svelte');

const { isNoteResult, resolveNoteTarget } = await import('../src/lib/utils/noteSearchResult.ts');

const sessions = [{
  id: 's1',
  mainWindowIndex: 1,
  followedWindows: [{ id: 't-a', index: 2 }, { id: 't-b', index: 5 }],
}];

test('a note result is told apart from a conversation', () => {
  assert.equal(isNoteResult({ kind: 'note' }), true);
  assert.equal(isNoteResult({}), false);
  assert.equal(resolveNoteTarget({ sessionId: 's1' }, sessions), null,
    'a conversation result was given a note to open');
});

// The session's note belongs to no tab: opening it leaves the session on the
// tab it remembers rather than moving it.
test('a session note opens the session on the session scope', () => {
  assert.deepEqual(
    resolveNoteTarget({ kind: 'note', sessionId: 's1', noteScope: 'session', windowIdx: -1 }, sessions),
    { sessionId: 's1', windowIdx: null, scope: 'session' });
});

// Tabs are found by ID: the index the search saw may have moved since, and
// the main tab's index is only in the session list (base-index may be 1).
test('a tab note opens its tab by ID, the main tab by the session\'s index', () => {
  assert.deepEqual(
    resolveNoteTarget({ kind: 'note', sessionId: 's1', noteScope: 'tab', tabId: 't-b', windowIdx: 3 }, sessions),
    { sessionId: 's1', windowIdx: 5, scope: 'tab' }, 'a stale window index won over the tab ID');
  assert.deepEqual(
    resolveNoteTarget({ kind: 'note', sessionId: 's1', noteScope: 'tab', tabId: 'main', windowIdx: -1 }, sessions),
    { sessionId: 's1', windowIdx: 1, scope: 'tab' }, 'the main tab was not taken from mainWindowIndex');
});

test('a note whose session or tab has gone leads nowhere', () => {
  assert.equal(resolveNoteTarget({ kind: 'note', sessionId: 'gone', noteScope: 'session' }, sessions), null);
  assert.equal(resolveNoteTarget({ kind: 'note', sessionId: 's1', noteScope: 'tab', tabId: 't-x', windowIdx: 2 }, sessions), null,
    'a deleted tab fell back to whichever tab now has its old index');
});

test('the search dialog marks note results and opens them', () => {
  assert.match(dialog, /\{#if isNoteResult\(entry\)\}/, 'note results are drawn as conversations');
  assert.match(dialog, /class="note-badge">\{\$t\(noteLabelKey\(entry\)\)\}/, 'note results carry no type badge');
  assert.match(dialog, /on:dblclick=\{\(\) => openNote\(entry\)\}/);
  const open = dialog.slice(dialog.indexOf('function openNote'));
  assert.match(open.slice(0, 1200), /selectSession\(target\.sessionId\);\s*if \(target\.windowIdx !== null\) selectWindow\(target\.windowIdx\);\s*requestNoteJump\(/,
    'the note is requested before its session and tab are selected');
  assert.match(open.slice(0, 1200), /query: query\.trim\(\)/, 'the find bar is not given the query');
});

// The tab-change reset puts the terminal back on every session/tab change; the
// request must be handled after it in the same update, or it is undone.
test('the panel switches to notes after the tab-change reset', () => {
  const reset = panel.indexOf("activeView = 'terminal';\n      fullDiffActive = tabDiffMemory.has(key);");
  const request = panel.indexOf('$: if ($notesViewRequested) {');
  assert.ok(reset > 0 && request > reset, 'the notes request is handled before the reset');
  assert.match(panel.slice(request, request + 200), /clearNotesViewRequest\(\);\s*selectView\('notes'\);/);
});

// After the activation block (so it beats a default scope from the settings),
// before the target watch (so the scope it sets is loaded in the same update).
test('the notes view takes the scope and query from the search', () => {
  const activation = notes.indexOf('applyDefaultScope();\n    void activateNotes();');
  const jump = notes.indexOf('$: if (active && $pendingNoteJump) takeNoteJump($pendingNoteJump);');
  const watch = notes.indexOf('$: wantedWindowIdx =');
  assert.ok(activation > 0 && jump > activation && watch > jump, 'the note jump is handled in the wrong place');

  const take = notes.slice(notes.indexOf('function takeNoteJump'));
  const body = take.slice(0, take.indexOf('\n  }\n'));
  assert.match(body, /scope = jump\.scope;/);
  assert.doesNotMatch(body, /setScope|localStorage/, 'a search result overwrote the remembered scope');
  assert.match(body, /findQuery = jump\.query;/);
  assert.match(notes, /function revealJumpMatch\(\) \{\s*if \(matches\.length\) \{\s*goToMatch\(0\);/);
});

test('the note result strings are translated everywhere', () => {
  const dir = new URL('../src/lib/i18n/locales/', import.meta.url);
  const keys = ['search.sessionNote', 'search.tabNote', 'search.openNote',
    'search.openNoteHint', 'search.noteGone', 'search.notesHint'];
  const english = JSON.parse(readFileSync(new URL('en.json', dir), 'utf8'));
  for (const name of readdirSync(dir).filter((n) => n.endsWith('.json'))) {
    const strings = JSON.parse(readFileSync(new URL(name, dir), 'utf8'));
    for (const key of keys) {
      assert.ok(strings[key]?.trim(), `${name} has no ${key}`);
      if (name !== 'en.json') {
        assert.notEqual(strings[key], english[key], `${name} leaves ${key} in English`);
      }
    }
  }
});
