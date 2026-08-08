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

const { buildSideBySide, parseHunkHeader, splitSides, matchingLine } = await import(js);

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



// Two independent columns, kept in step by scrolling rather than by padding.
//
// Paired rows fill the shorter side with blanks so one grid can hold both —
// which is also why a long insertion shows a screenful of nothing on the left.
// Split into two columns, each holds only its own lines, and the side with
// nothing to show waits while the other runs through the insertion.
{
  const rows = buildSideBySide('@@ -1,2 +1,5 @@', [
    line('context', 'top'),
    line('add', 'new1'),
    line('add', 'new2'),
    line('add', 'new3'),
    line('context', 'bottom'),
  ]);
  const { left, right } = splitSides(rows);

  assert.deepEqual(
    left.map((l) => l.html),
    ['top', 'bottom'],
    'the left column holds only its own lines, with no blanks for the insertion',
  );
  assert.deepEqual(
    right.map((l) => l.html),
    ['top', 'new1', 'new2', 'new3', 'bottom'],
  );

  // Scrolling the right side down through the insertion: the left stays on
  // 'top' until the insertion ends, then moves on. That is the waiting.
  assert.equal(matchingLine(right, left, 0), 0, 'the shared first line matches');
  assert.equal(matchingLine(right, left, 1), 0, 'the left waits at the line before the insertion');
  assert.equal(matchingLine(right, left, 3), 0, 'it is still waiting at the end of the insertion');
  assert.equal(matchingLine(right, left, 4), 1, 'and moves on once the insertion is past');

  // And the other way: the left has fewer lines, so scrolling it maps to where
  // each line sits on the right.
  assert.equal(matchingLine(left, right, 0), 0);
  assert.equal(matchingLine(left, right, 1), 4, 'the line after the insertion is four rows down');
}

console.log('sideBySide: ok');

/**
 * Where a changed block's outline goes, per column.
 *
 * The rule is the neighbour WITHIN the column: a block that is three lines on
 * one side and one on the other has to be boxed as three there and one here,
 * and the paired-row boundary falls in the middle of nothing on the shorter
 * side. Row numbers separate blocks that are merely adjacent in this column
 * because the lines between them live on the other side.
 */
{
  const mark = (lines) => lines.map((line, at) => {
    const changed = line.kind === 'change';
    const previous = lines[at - 1];
    const next = lines[at + 1];
    return {
      html: line.html,
      first: changed && !(previous?.kind === 'change' && previous.row === line.row - 1),
      last: changed && !(next?.kind === 'change' && next.row === line.row + 1),
    };
  });

  // Three lines replaced by two: three boxed on the left, two on the right.
  {
    const { left, right } = splitSides(buildSideBySide('@@ -1,5 +1,4 @@', [
      line('context', 'a'),
      line('remove', 'o1'), line('remove', 'o2'), line('remove', 'o3'),
      line('add', 'n1'), line('add', 'n2'),
      line('context', 'b'),
    ]));
    assert.deepEqual(
      mark(left).map((l) => [l.first, l.last]),
      [[false, false], [true, false], [false, false], [false, true], [false, false]],
      'the left box spans all three removed lines',
    );
    assert.deepEqual(
      mark(right).map((l) => [l.first, l.last]),
      [[false, false], [true, false], [false, true], [false, false]],
      'the right box spans both added lines, not three',
    );
  }

  // A pure insertion leaves the left with no block to outline at all.
  {
    const { left } = splitSides(buildSideBySide('@@ -1,2 +1,4 @@', [
      line('context', 'a'), line('add', 'n1'), line('add', 'n2'), line('context', 'b'),
    ]));
    assert.deepEqual(
      mark(left).map((l) => l.first || l.last),
      [false, false],
      'nothing to box on a side the block does not appear on',
    );
  }

  // Two changes with one unchanged line between them are two boxes, not one.
  {
    const { left } = splitSides(buildSideBySide('@@ -1,5 +1,5 @@', [
      line('context', 'a'),
      line('remove', 'o1'), line('add', 'n1'),
      line('context', 'mid'),
      line('remove', 'o2'), line('add', 'n2'),
      line('context', 'b'),
    ]));
    assert.deepEqual(
      mark(left).map((l) => [l.first, l.last]),
      [[false, false], [true, true], [false, false], [true, true], [false, false]],
      'two separate one-line blocks',
    );
  }

  // The hard one: a deletion and an insertion with nothing between them are
  // adjacent in the right column but are two blocks.
  {
    const { right } = splitSides(buildSideBySide('@@ -1,5 +1,6 @@', [
      line('context', 'a'),
      line('remove', 'o1'), line('remove', 'o2'),
      line('add', 'n1'), line('add', 'n2'),
      line('context', 'b'),
      line('add', 'n3'),
      line('context', 'c'),
    ]));
    const marked = mark(right);
    assert.deepEqual(
      [marked[1].first, marked[2].last],
      [true, true],
      'the replacement is one box',
    );
    const lone = marked.find((l) => l.html === 'n3');
    assert.deepEqual([lone.first, lone.last], [true, true], 'the later insertion is its own box');
  }
}

console.log('sideBySide edges: ok');
