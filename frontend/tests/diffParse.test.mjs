import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

/**
 * Classifying diff lines, and rationing how many get drawn.
 *
 * This runs the real functions rather than reading the source and matching
 * patterns: what matters is what they return for a given diff, and a regex over
 * the file cannot tell you that. Compiled with esbuild, the same way
 * blockPatch.test.mjs does it.
 */
const source = readFileSync(new URL('../src/lib/utils/diffParse.ts', import.meta.url), 'utf8');
const dir = mkdtempSync(join(tmpdir(), 'dp-'));
const js = join(dir, 'diffParse.mjs');
writeFileSync(js, execFileSync('npx', ['esbuild', '--loader=ts', '--format=esm'], {
  input: source,
  encoding: 'utf8',
  cwd: new URL('..', import.meta.url).pathname,
}));
const { parseDiff, buildHunkViews } = await import(js);

const typesOf = (text) => parseDiff(text).map((line) => line.type);

// The ordinary case: one added line, one removed, the rest context.
assert.deepEqual(
  typesOf('@@ -1,3 +1,3 @@\n context\n-gone\n+added'),
  ['header', 'context', 'remove', 'add'],
);

// File headers are NOT an added and a removed line. Reading them as changes
// puts a bright green and a bright red bar at the top of every file.
assert.deepEqual(
  typesOf('diff --git a/f b/f\nindex 111..222 100644\n--- a/f\n+++ b/f'),
  ['meta', 'meta', 'meta', 'meta'],
);

// A line of exactly "+" or "-" is a change to an empty line, not a header.
assert.deepEqual(typesOf('+\n-'), ['add', 'remove']);

// Empty input is one empty context line, not zero lines: split('\n') on '' is
// [''] and the view draws it. Worth pinning — the alternative silently changes
// how an empty hunk renders.
assert.deepEqual(typesOf(''), ['context']);

// The text survives untouched, including leading whitespace, which is the part
// a diff is often about.
assert.deepEqual(
  parseDiff('+    indented').map((l) => l.text),
  ['+    indented'],
);

// --- the budget -----------------------------------------------------------

const hunk = (body) => ({ body, header: '', oldStart: 1, newStart: 1 });

// Spent in order: the first hunks are drawn in full and later ones report what
// they left out. Truncating every hunk to the same length instead would show a
// little of everything and all of nothing.
{
  const file = { hunks: [hunk('a\nb\nc'), hunk('d\ne\nf')] };
  const views = buildHunkViews(file, 4);
  assert.equal(views[0].lines.length, 3, 'the first hunk fits and is drawn whole');
  assert.equal(views[0].hidden, 0);
  assert.equal(views[1].lines.length, 1, 'the second gets what is left');
  assert.equal(views[1].hidden, 2, 'and reports the rest as hidden');
}

// Once the budget is gone, later hunks draw nothing — but still say how much
// they are hiding, which is what the notice in the view is counting.
{
  const file = { hunks: [hunk('a\nb'), hunk('c\nd\ne')] };
  const views = buildHunkViews(file, 2);
  assert.equal(views[1].lines.length, 0);
  assert.equal(views[1].hidden, 3);
}

// A budget of zero draws nothing at all rather than going negative and slicing
// from the end, which is what a naive slice(0, budget) would do.
{
  const views = buildHunkViews({ hunks: [hunk('a\nb')] }, 0);
  assert.equal(views[0].lines.length, 0);
  assert.equal(views[0].hidden, 2);
}

// A file with no hunks is not an error — a rename with no content change is
// exactly that.
assert.deepEqual(buildHunkViews({ hunks: [] }, 100), []);

console.log('diffParse: ok');
