import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

/**
 * Pairing a unified diff into two aligned columns.
 *
 * The alignment is the whole point: where one side has more lines than the
 * other, the shorter side needs blank rows to keep the pairs level. Without
 * them the columns drift, and every comparison after the first change is
 * against the wrong line — which is worse than no side-by-side view at all,
 * because it looks right.
 *
 * The module is TypeScript, so it is compiled to a temporary file first rather
 * than being reimplemented here.
 */
const source = readFileSync(new URL('../src/lib/utils/sideBySide.ts', import.meta.url), 'utf8');
const dir = mkdtempSync(join(tmpdir(), 'sbs-'));
const js = join(dir, 'sideBySide.mjs');
// The file has no imports and only type-level syntax to remove, so stripping
// the annotations is enough to run it.
writeFileSync(js, execFileSync('npx', ['esbuild', '--loader=ts', '--format=esm'], {
  input: source,
  encoding: 'utf8',
  cwd: new URL('..', import.meta.url).pathname,
}));

const { buildSideBySide, parseHunkHeader } = await import(js);

// The header is where both columns get their first line number.
{
  const { oldStart, newStart } = parseHunkHeader('@@ -2799,8 +2799,9 @@ type SettingsInfo struct {');
  assert.equal(oldStart, 2799);
  assert.equal(newStart, 2799);

  // A single-line hunk omits the count.
  const single = parseHunkHeader('@@ -1 +1 @@');
  assert.equal(single.oldStart, 1);
  assert.equal(single.newStart, 1);

  // Malformed input must not throw: a diff is whatever the repository holds.
  const broken = parseHunkHeader('not a header');
  assert.equal(broken.oldStart, 1);
  assert.equal(broken.newStart, 1);
}

const line = (type, html) => ({ type, html });

// A line replaced by another shares a row, so "this became that" is read
// across rather than reconstructed by counting.
{
  const rows = buildSideBySide('@@ -10,3 +10,3 @@', [
    line('context', 'before'),
    line('remove', 'old'),
    line('add', 'new'),
    line('context', 'after'),
  ]);

  assert.equal(rows.length, 3, 'a replacement is one row, not two');
  assert.deepEqual(
    [rows[1].oldHtml, rows[1].newHtml],
    ['old', 'new'],
    'the replaced line and its replacement belong on the same row',
  );
  assert.equal(rows[1].oldNumber, 11);
  assert.equal(rows[1].newNumber, 11);
}

// Uneven runs are where alignment earns its keep: three lines becoming one has
// to read as three rows against one, not as three rows sliding the rest of the
// file out of step.
{
  const rows = buildSideBySide('@@ -1,4 +1,2 @@', [
    line('remove', 'a'),
    line('remove', 'b'),
    line('remove', 'c'),
    line('add', 'x'),
    line('context', 'tail'),
  ]);

  assert.equal(rows.length, 4);
  assert.deepEqual(rows.map((r) => r.oldHtml), ['a', 'b', 'c', 'tail']);
  assert.deepEqual(rows.map((r) => r.newHtml), ['x', null, null, 'tail']);
  assert.deepEqual(
    rows.map((r) => r.newNumber),
    [1, null, null, 2],
    'the shorter side must not consume line numbers for rows it has no line on',
  );
}

// Line numbers advance per side, which is the thing a reader checks against
// their editor.
{
  const rows = buildSideBySide('@@ -100,3 +200,4 @@', [
    line('context', 'ctx'),
    line('add', 'inserted'),
    line('context', 'ctx2'),
  ]);

  assert.deepEqual(rows.map((r) => r.oldNumber), [100, null, 101]);
  assert.deepEqual(rows.map((r) => r.newNumber), [200, 201, 202]);
}

// An addition with nothing removed leaves the old side blank rather than
// borrowing the next context line.
{
  const rows = buildSideBySide('@@ -1,1 +1,3 @@', [
    line('add', 'one'),
    line('add', 'two'),
    line('context', 'kept'),
  ]);

  assert.deepEqual(rows.map((r) => r.oldHtml), [null, null, 'kept']);
  assert.deepEqual(rows.map((r) => r.newHtml), ['one', 'two', 'kept']);
}

console.log('sideBySide: ok');
