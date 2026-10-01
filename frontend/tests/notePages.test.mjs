import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';

// Read with line endings normalised, so a CRLF checkout matches too.
const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const notes = read('../src/lib/components/MainPanel/Notes.svelte');
const goPages = read('../../session/note_pages.go');

const {
  FIRST_PAGE_ID, editablePages, pagesKey, notePagesText, hasNoteText, resolveActivePage,
  setPageText, renamePage, addPage, deletePage, movePage, dropIndex, stepPage, pageLabel, cleanTitle,
} = await import('../src/lib/utils/notePages.ts');
const { resolveNoteTarget, notePageName } = await import('../src/lib/utils/noteSearchResult.ts');

const plan = { id: 'a', title: 'Plan', text: 'ship on friday' };
const risks = { id: 'b', title: 'Risks', text: 'the migration' };
const t = (key, params) => (key === 'notes.untitledPage' ? 'Note' : `Note ${params?.n}`);

// An empty note comes back from the backend with no pages; there must still
// be one to type into, and it must be the one an old text-only note is read
// as, so the open page is the same before and after the first save.
test('an empty note is shown as one empty page', () => {
  assert.deepEqual(editablePages([]), [{ id: FIRST_PAGE_ID, title: '', text: '' }]);
  assert.deepEqual(editablePages(null), [{ id: FIRST_PAGE_ID, title: '', text: '' }]);
  assert.match(goPages, new RegExp(`const LegacyNotePageID = "${FIRST_PAGE_ID}"`),
    'the frontend and the backend name the first page differently');
  const fixed = editablePages([{ id: 'x', title: 'A' }, { id: 'x', text: 'dup' }, {}]);
  assert.equal(new Set(fixed.map((p) => p.id)).size, 3, 'duplicate or missing page IDs survive');
  assert.equal(fixed[0].text, '');
});

test('an empty note and one empty untitled page are the same saved note', () => {
  assert.equal(pagesKey([]), pagesKey(editablePages([])));
  assert.notEqual(pagesKey([plan]), pagesKey([{ ...plan, text: 'changed' }]));
  assert.notEqual(pagesKey([plan, risks]), pagesKey([risks, plan]), 'a reorder is not seen as a change');
  assert.equal(pagesKey([{ ...plan, title: ' Plan ' }]), pagesKey([plan]));
});

// The dot on the Notes tab and the session list read the joined text; it must
// follow the backend's rule exactly, so it agrees with what is stored.
test('the joined text follows the backend rule', () => {
  assert.equal(notePagesText([{ id: 'x', title: '', text: 'only' }]), 'only');
  assert.equal(
    notePagesText([plan, { id: 'c', title: 'Empty', text: '  ' }, risks, { id: 'd', title: '', text: 'loose' }]),
    '# Plan\nship on friday\n\n# Risks\nthe migration\n\nloose');
  assert.equal(notePagesText([{ id: 'x', title: 'Only a title', text: '' }]), '');
  assert.match(goPages, /notePageTitlePrefix\s*= "# "/);
  assert.match(goPages, /notePageJoinSeparator = "\\n\\n"/);
  assert.equal(hasNoteText([{ id: 'x', title: 'T', text: ' \n' }]), false);
  assert.equal(hasNoteText([{ id: 'x', title: '', text: '' }, risks]), true);
});

test('pages are added, renamed, moved and deleted', () => {
  const added = addPage([plan, risks], null, '  New\n page ');
  assert.equal(added.pages.length, 3);
  assert.equal(added.pages[2].id, added.id, 'a new page goes at the end');
  assert.equal(added.pages[2].title, 'New page');
  assert.equal(addPage([plan, risks], 'a').pages[1].text, '', 'a page after another lands beside it');

  assert.deepEqual(renamePage([plan], 'a', ' Road\tmap ')[0], { ...plan, title: 'Road map' });
  assert.equal(cleanTitle('  a \n b  '), 'a b');
  assert.deepEqual(setPageText([plan, risks], 'b', 'x')[1], { ...risks, text: 'x' });

  const three = [plan, risks, { id: 'c', title: 'C', text: '' }];
  assert.deepEqual(movePage(three, 'a', 2).map((p) => p.id), ['b', 'c', 'a']);
  assert.deepEqual(movePage(three, 'c', 0).map((p) => p.id), ['c', 'a', 'b']);
  // Dropped after the last page, or before the first: the dragged page leaves
  // its place first, so the index allows for it.
  assert.deepEqual(movePage(three, 'a', dropIndex(three, 'a', 'c', true)).map((p) => p.id), ['b', 'c', 'a']);
  assert.deepEqual(movePage(three, 'c', dropIndex(three, 'c', 'a', false)).map((p) => p.id), ['c', 'a', 'b']);
  assert.deepEqual(movePage(three, 'a', dropIndex(three, 'a', 'b', true)).map((p) => p.id), ['b', 'a', 'c']);

  // The open page stays open unless it is the one deleted; then its
  // neighbour opens. The last page is never deleted.
  assert.deepEqual(deletePage(three, 'b', 'a'), { pages: [plan, three[2]], activeId: 'a' });
  assert.equal(deletePage(three, 'b', 'b').activeId, 'c');
  assert.equal(deletePage(three, 'c', 'c').activeId, 'b');
  assert.deepEqual(deletePage([plan], 'a', 'a').pages, [plan]);

  assert.equal(stepPage(three, 'c', 1), 'a', 'stepping past the last page wraps round');
  assert.equal(stepPage(three, 'a', -1), 'c');
  assert.equal(resolveActivePage(three, 'gone'), 'a');
});

test('an untitled page is named by its place', () => {
  assert.equal(pageLabel('Plan', 0, 3, t), 'Plan');
  assert.equal(pageLabel('', 0, 1, t), 'Note');
  assert.equal(pageLabel('', 1, 3, t), 'Note 2');
});

// A search result names its page, and opening it asks for that page.
test('a note result leads to its page', () => {
  const sessions = [{ id: 's1', mainWindowIndex: 0, followedWindows: [{ id: 't-a', index: 2 }] }];
  assert.deepEqual(
    resolveNoteTarget({ kind: 'note', sessionId: 's1', noteScope: 'tab', tabId: 't-a', pageId: 'b' }, sessions),
    { sessionId: 's1', windowIdx: 2, scope: 'tab', pageId: 'b' });
  assert.deepEqual(
    resolveNoteTarget({ kind: 'note', sessionId: 's1', noteScope: 'session' }, sessions),
    { sessionId: 's1', windowIdx: null, scope: 'session' });
  assert.equal(notePageName({ pageTitle: 'Risks', pageIndex: 1, pageCount: 2 }, t), 'Risks');
  assert.equal(notePageName({ pageIndex: 1, pageCount: 2 }, t), 'Note 2');
  assert.equal(notePageName({ pageIndex: 0, pageCount: 1 }, t), '', 'a single untitled page is named');
});

// The whole note is saved at once, whichever page changed: a page switch is
// not a change of target, so it cannot drop the page left.
test('the notes view saves every page of the note at once', () => {
  assert.match(notes, /else await App\.SetTabNotePages\(sessionId, windowIdx, snapshot, projectId\);/);
  const input = notes.slice(notes.indexOf('function handleInput'));
  assert.match(input.slice(0, 300), /pages = setPageText\(pages, activePageId, notes\);\s*scheduleSave\(\);/,
    'typing does not write the open page back into the note');
  const schedule = notes.slice(notes.indexOf('function scheduleSave'));
  assert.match(schedule.slice(0, 600), /const snapshot = pages;/, 'a save is not of the whole note');
  // Selecting a page changes nothing in the note and must not reload it.
  const show = notes.slice(notes.indexOf('function showPage'), notes.indexOf('function selectPage'));
  assert.doesNotMatch(show, /loadNotes|saveNow/);
  assert.match(notes, /function changePages[\s\S]*?scheduleSave\(\);/, 'page edits are not saved');
});

test('the presence dots count every page', () => {
  assert.match(notes, /openText: notePagesText\(pages\),/);
  assert.match(notes, /otherDraft: draftText\(draftsByTarget\.get\(/);
  assert.match(notes, /dispatch\('notesChange', \{ sessionId, windowIdx, notes: notePagesText\(snapshot\) \}\)/);
});

// Ctrl+PgUp / Ctrl+PgDn switch the session's tabs everywhere; the pages take
// the Alt pair so the notes do not trap the user.
//
// They are registered shortcuts, so they can be rebound in the settings and
// are listed in the help.
test('pages are stepped with Alt+PgUp / Alt+PgDn, not the tab keys', async () => {
  const { SHORTCUTS, shortcutById } = await import('../src/lib/utils/shortcuts.ts');
  assert.deepEqual(shortcutById('notes.prevPage')?.defaults, [{ key: 'pageup', alt: true }]);
  assert.deepEqual(shortcutById('notes.nextPage')?.defaults, [{ key: 'pagedown', alt: true }]);
  for (const id of ['notes.prevPage', 'notes.nextPage']) {
    const shortcut = shortcutById(id);
    assert.equal(shortcut.fixed, undefined, `${id} cannot be rebound`);
    assert.equal(shortcut.category, 'navigation');
  }
  // No other default answers to the same keys.
  const seen = new Map();
  for (const shortcut of SHORTCUTS) {
    for (const b of shortcut.defaults) {
      const key = `${b.key}|${!!b.ctrl}|${!!b.shift}|${!!b.alt}`;
      assert.ok(!seen.has(key), `${shortcut.id} and ${seen.get(key)} share a default binding`);
      seen.set(key, shortcut.id);
    }
  }

  const step = notes.slice(notes.indexOf('function handlePageStepKey'));
  const body = step.slice(0, step.indexOf('\n  }\n'));
  assert.match(body, /matchesShortcut\(event, 'notes\.nextPage'\)/);
  assert.match(body, /matchesShortcut\(event, 'notes\.prevPage'\)/);
  assert.doesNotMatch(body, /PageUp|PageDown|altKey/, 'the page keys are still hard-coded');
  assert.match(notes, /if \(handlePageStepKey\(event\)\) return;\s*const mod = event\.ctrlKey/,
    'the editor does not step pages');

  // Acted on in the notes only: the app's own handler has nothing to do for
  // them, and the terminal neither refuses nor captures them.
  const app = read('../src/App.svelte');
  assert.doesNotMatch(app, /case 'notes\.(prev|next)Page'/);
  const stepKeys = read('../src/lib/utils/sessionStepKeys.ts');
  assert.match(stepKeys, /return id === 'session\.prev' \|\| id === 'session\.next';/);
  const terminal = read('../src/lib/components/MainPanel/Terminal.svelte');
  assert.match(terminal, /if \(e\.shiftKey && \(e\.key === 'PageUp' \|\| e\.key === 'PageDown'\)\) \{/,
    'the terminal captures other page keys than Shift+PgUp/PgDn');
});

test('the page keys are named as they are bound, and translated everywhere', () => {
  assert.match(notes, /\$: pageStepKeys = \['notes\.prevPage', 'notes\.nextPage'\]\s*\.flatMap\(\(id\) => \$effectiveBindings\.get\(id\) \?\? \[\]\)/);
  assert.match(notes, /\$t\('notes\.pageStepHint', \{ keys: pageStepKeys \}\)/);
  const dir = new URL('../src/lib/i18n/locales/', import.meta.url);
  const english = JSON.parse(readFileSync(new URL('en.json', dir), 'utf8'));
  for (const name of readdirSync(dir).filter((n) => n.endsWith('.json'))) {
    const strings = JSON.parse(readFileSync(new URL(name, dir), 'utf8'));
    for (const key of ['help.notesPrevPage', 'help.notesNextPage', 'notes.pageStepHint']) {
      assert.ok(strings[key]?.trim(), `${name} has no ${key}`);
      if (name !== 'en.json') assert.notEqual(strings[key], english[key], `${name} leaves ${key} in English`);
    }
    assert.match(strings['notes.pageStepHint'], /\{keys\}/, `${name} does not name the keys`);
    assert.doesNotMatch(strings['notes.pageHint'], /Alt\+/, `${name} names fixed keys in the page hint`);
  }
});

test('the page menu is the shared kind', () => {
  assert.match(notes, /class="page-menu"[\s\S]*?use:portal[\s\S]*?use:menuPosition=/);
  assert.match(notes, /claimMenu\(closePageMenu\)/);
  assert.match(notes, /releaseMenu\(closePageMenu\)/);
  assert.match(notes, /if \(page\.text\.trim\(\) === ''\) \{\s*removePage\(id\);\s*\} else \{\s*pendingDeletePageId = id;/,
    'a page with text is deleted without asking');
});

test('the page strings are translated everywhere', () => {
  const dir = new URL('../src/lib/i18n/locales/', import.meta.url);
  const keys = ['notes.pages', 'notes.untitledPage', 'notes.untitledPageN', 'notes.addPage', 'notes.pageTitle',
    'notes.pageHint', 'notes.renamePage', 'notes.movePageLeft', 'notes.movePageRight', 'notes.deletePage',
    'notes.deletePageTitle', 'notes.deletePageMessage'];
  // "Note" is French for a note too.
  const mayMatchEnglish = new Set(['notes.untitledPage', 'notes.untitledPageN']);
  const english = JSON.parse(readFileSync(new URL('en.json', dir), 'utf8'));
  for (const name of readdirSync(dir).filter((n) => n.endsWith('.json'))) {
    const strings = JSON.parse(readFileSync(new URL(name, dir), 'utf8'));
    for (const key of keys) {
      assert.ok(strings[key]?.trim(), `${name} has no ${key}`);
      if (name !== 'en.json' && !mayMatchEnglish.has(key)) {
        assert.notEqual(strings[key], english[key], `${name} leaves ${key} in English`);
      }
    }
    assert.match(strings['notes.untitledPageN'], /\{n\}/, `${name} drops the page number`);
    assert.match(strings['notes.deletePageMessage'], /\{title\}/, `${name} drops the page title`);
  }
});
