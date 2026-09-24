import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

/**
 * A new or deleted file is read in one column even with two chosen.
 *
 * Two columns for a file with one side put the whole file in half the width
 * beside a pane of nothing. The choice itself is kept — the toggle shows it,
 * and the next modified file gets it — only what is rendered changes.
 */
const source = readFileSync(new URL('../src/lib/utils/sideBySide.ts', import.meta.url), 'utf8');
const dir = mkdtempSync(join(tmpdir(), 'one-side-'));
const js = join(dir, 'sideBySide.mjs');
writeFileSync(js, execFileSync('npx', ['esbuild', '--loader=ts', '--format=esm'], {
  input: source,
  encoding: 'utf8',
  cwd: new URL('..', import.meta.url).pathname,
}));
const { hasOneSide } = await import(js);

test('a file git calls added or deleted has one side', () => {
  assert.equal(hasOneSide({ status: 'added', hunks: [] }), true);
  assert.equal(hasOneSide({ status: 'deleted' }), true);
});

test('a modified or renamed file has two', () => {
  assert.equal(hasOneSide({ status: 'modified', hunks: [{ header: '@@ -1,3 +1,4 @@' }] }), false);
  assert.equal(hasOneSide({ status: 'renamed', hunks: [{ header: '@@ -10,2 +10,2 @@ fn' }] }), false);
  assert.equal(hasOneSide({ status: 'modified' }), false);
  assert.equal(hasOneSide(null), false);
  assert.equal(hasOneSide(undefined), false);
});

test('without a status the hunk ranges decide', () => {
  // Nothing before: every hunk starts at -0,0.
  assert.equal(hasOneSide({ hunks: [{ header: '@@ -0,0 +1,12 @@' }] }), true);
  // Nothing after: every hunk ends at +0,0.
  assert.equal(hasOneSide({ status: '', hunks: [{ header: '@@ -1,7 +0,0 @@' }] }), true);
  // A change at the top of a file is not a new file: -0,0 needs the count.
  assert.equal(hasOneSide({ hunks: [{ header: '@@ -1 +1 @@' }] }), false);
  assert.equal(hasOneSide({ hunks: [{ header: '@@ -0,0 +1,2 @@' }, { header: '@@ -5,3 +7,4 @@' }] }), false);
  // Nothing to go on, or something unreadable: keep the chosen layout.
  assert.equal(hasOneSide({ hunks: [] }), false);
  assert.equal(hasOneSide({ hunks: [{ header: 'not a header' }] }), false);
});

// Both diff views gate the rendered layout on it, and keep the toggle on the
// setting — otherwise the button would switch off for a new file, and pressing
// it there would turn two columns off for every other file too.
for (const [name, path] of [
  ['working-tree diff', '../src/lib/components/MainPanel/Diff.svelte'],
  ['commit history diff', '../src/lib/components/Dialogs/GitHistoryDialog.svelte'],
]) {
  test(`${name}: a one-sided file renders in one column, the toggle keeps the choice`, () => {
    const code = readFileSync(new URL(path, import.meta.url), 'utf8');
    assert.match(code, /\$: sideBySideChosen = \$settings\.diffSideBySide === true;/);
    assert.match(code, /\$: sideBySide = sideBySideChosen &&\s*!hasOneSide\(/);
    assert.match(code, /class:active=\{sideBySideChosen\}/);
    assert.match(code, /diffSideBySide: !sideBySideChosen|setSideBySide\(!sideBySideChosen\)/);
  });
}
