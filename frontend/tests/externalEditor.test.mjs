import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const diffSrc = readFileSync(
  new URL('../src/lib/components/MainPanel/Diff.svelte', import.meta.url), 'utf8');
const browserSrc = readFileSync(
  new URL('../src/lib/components/MainPanel/FileBrowser.svelte', import.meta.url), 'utf8');
const settingsSrc = readFileSync(
  new URL('../src/lib/components/Dialogs/SettingsDialog.svelte', import.meta.url), 'utf8');

// Opening at the file's first change is not the same as opening where the
// reader is: in a long file those can be thousands of lines apart. The diff
// already works this out for its own browser jump, and the editor button has
// to use the same answer rather than a second, simpler one.
test('the editor opens at the line being read', () => {
  const at = diffSrc.indexOf('async function openFileInEditor');
  assert.ok(at > 0, 'openFileInEditor is gone');
  const fn = diffSrc.slice(at, diffSrc.indexOf('\n  }\n', at));

  assert.match(fn, /lineForFile\(path\)/,
    'the editor button ignores where the reader is and opens at the top');
});

test('the shared line lookup keeps the fallback to the first hunk', () => {
  const at = diffSrc.indexOf('function lineForFile');
  assert.ok(at > 0, 'lineForFile is gone');
  const fn = diffSrc.slice(at, diffSrc.indexOf('\n  }\n', at));

  assert.match(fn, /topVisibleNewLine/, 'the side-by-side viewport is no longer consulted');
  assert.match(fn, /firstVisibleLine/, 'the whole-file viewport is no longer consulted');
  assert.match(fn, /parseHunkHeader/,
    'a file with no viewport of its own no longer falls back to its first hunk');
});

// expectedRoot is what stops a tab changing directory mid-flight from
// redirecting the open at a file with the same relative path somewhere else.
for (const [name, src, fn] of [
  ['the diff file button', diffSrc, 'openFileInEditor'],
  ['the diff view button', diffSrc, 'openDiffInEditor'],
  ['the browser button', browserSrc, 'openSelectedInEditor'],
]) {
  test(`${name} passes the root it loaded`, () => {
    const at = src.indexOf(`async function ${fn}`);
    assert.ok(at > 0, `${fn} is gone`);
    const body = src.slice(at, src.indexOf('\n  }\n', at));

    assert.match(body, /expectedRoot/,
      'the call does not pin the root, so a tab cwd change can retarget it');
    assert.match(body, /if \(!sessionId \|\| !expectedRoot/,
      'the call proceeds without a session or a root');
  });
}

// A missing editor is a setting the user can fix. A button that silently does
// nothing reads as a broken button.
test('a failure to open is shown, not swallowed', () => {
  const at = diffSrc.indexOf('async function openFileInEditor');
  const fn = diffSrc.slice(at, diffSrc.indexOf('\n  }\n', at));
  assert.match(fn, /catch/, 'the call has no error path at all');
  assert.match(fn, /editorError = String\(e\)/, 'the error is discarded');

  assert.match(diffSrc, /\{#if editorError\}/, 'nothing renders the error');
});

test('the diff view button says which side it is comparing', () => {
  const at = diffSrc.indexOf('async function openDiffInEditor');
  const fn = diffSrc.slice(at, diffSrc.indexOf('\n  }\n', at));
  // "session" vs the uncommitted view: the backend resolves the base commit
  // from this, and sending the wrong one compares against the wrong thing.
  assert.match(fn, /diffMode/,
    'the editor diff ignores which of the two diff tabs is showing');
});

// Both diff layouts carry the buttons: the tree and the flat list are the same
// file rows drawn differently, and having them in one is a gap nobody notices
// until they switch layouts.
test('both diff layouts carry the editor buttons', () => {
  const file = (re) => [...diffSrc.matchAll(re)].length;
  assert.equal(file(/openDiffInEditor\(file\.path\)/g), 2,
    'the editor diff button is missing from one of the two layouts');
  assert.equal(file(/openFileInEditor\(file\.path\)/g), 2,
    'the editor open button is missing from one of the two layouts');
});

// The browser view cannot show binary or truncated files at all, which makes
// them the ones most worth handing to an editor.
test('the browser offers the editor for files it cannot show', () => {
  const at = browserSrc.indexOf('openSelectedInEditor}');
  assert.ok(at > 0, 'the browser button is gone');
  const before = browserSrc.slice(Math.max(0, at - 400), at);
  assert.match(before, /\{#if selectedFile\}/,
    'the button is nested inside the editable-file branch, so binary and ' +
    'truncated files — the ones this view cannot show — do not get it');
});

test('the editor setting is offered with the detected editor as its hint', () => {
  assert.match(settingsSrc, /settings\.externalEditor/, 'the setting is not in the dialog');
  assert.match(settingsSrc, /placeholder=\{detectedEditor \|\| 'code'\}/,
    'an empty field gives no clue which editor would be used');
  assert.match(settingsSrc, /App\.DetectedEditor\(\)/, 'nothing asks what was detected');
});

// The file list is not where someone looks for a control acting on the file
// they have open: that row of buttons above the diff is. Having them only in
// the list was the first thing reported about this feature.
test('the selected file carries the editor buttons too', () => {
  assert.match(diffSrc, /openDiffInEditor\(selectedFile\.path\)/,
    'the open diff button is missing from the header row');
  assert.match(diffSrc, /openFileInEditor\(selectedFile\.path\)/,
    'the open file button is missing from the header row');
});

// The glyph buttons sit next to a text button ("Whole file"). Matching its
// 11px made the search and side-by-side marks too small to read or aim at.
test('the glyph buttons are sized apart from the text one', () => {
  const at = diffSrc.indexOf('.nav-btn:not(.wide)');
  assert.ok(at > 0, 'the glyph sizing rule is gone');
  const rule = diffSrc.slice(at, diffSrc.indexOf('}', at));
  assert.match(rule, /font-size:\s*14px/, 'the glyphs are back at the text size');
  assert.match(rule, /min-width/, 'nothing keeps the glyph buttons wide enough to hit');
});

// Per-file buttons answer "this file". Opening the directory as a project is
// the one that answers "everything I have touched": the editor's own
// source-control view then lists every change at once.
test('the folder can be opened as a project from both views', () => {
  for (const [name, src] of [['the diff', diffSrc], ['the browser', browserSrc]]) {
    const at = src.indexOf('async function openFolderInEditor');
    assert.ok(at > 0, `openFolderInEditor is missing from ${name}`);
    const body = src.slice(at, src.indexOf('\n  }\n', at));
    assert.match(body, /App\.OpenFolderInEditor/, `${name} does not call through`);
    assert.match(body, /expectedRoot/,
      `${name} does not pin the root, so a tab cwd change can retarget it`);
  }
});

// The button acts on the whole tree, so it belongs in the header rather than on
// a file row — and it has nothing to open before a root is known.
test('the folder button is disabled until a root is loaded', () => {
  assert.match(diffSrc, /on:click=\{openFolderInEditor\}[\s\S]{0,80}disabled=\{!loadedRoot\}/,
    'the diff folder button is enabled with no root, so it opens nothing');
  assert.match(browserSrc, /on:click=\{openFolderInEditor\}[\s\S]{0,80}disabled=\{!rootAbsPath\}/,
    'the browser folder button is enabled with no root');
});
