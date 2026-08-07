import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * What counts as "a change" when stepping through a diff.
 *
 * The step targets used to be one per hunk — the hunk's first changed line.
 * That is fine in the hunks-only view, where git splits the file at every
 * change. It is wrong in the whole-file view, which asks git for the entire
 * file as a SINGLE hunk: one target for the whole file, whatever it contains.
 *
 * Three symptoms, one cause. Any change below the first was unreachable and
 * unmarked; the first press ran out of targets and left for the next file, so
 * every file cost two presses; and the marker covered whichever run began
 * first rather than the one stepped to.
 */
const source = readFileSync(
  new URL('../src/lib/components/MainPanel/Diff.svelte', import.meta.url),
  'utf8',
);

const starts = source.match(/\$: hunkStarts = ([\s\S]*?)\}, \[\] as number\[\]\);/);
assert.ok(starts, 'hunkStarts is gone');

// Derived from the lines, not from a per-hunk flag.
assert.match(
  starts[1],
  /line\.type === 'add' \|\| line\.type === 'remove'/,
  'step targets should be found from the lines themselves',
);
assert.match(
  starts[1],
  /flatLines\[index - 1\]/,
  'a run starts where the previous line is not a change',
);
assert.doesNotMatch(
  starts[1],
  /isHunkStart/,
  'one target per hunk is one target for the whole file in whole-file view',
);

// A removal followed by its replacement reads as one change on screen, and
// stepping should treat it that way rather than stopping twice.
assert.match(
  starts[1],
  /previous\.type !== 'add' && previous\.type !== 'remove'/,
  'adjacent added and removed lines belong to the same run',
);

// The flag it replaced should be gone entirely, not left to rot.
assert.doesNotMatch(source, /isHunkStart/, 'the unused flag should be removed');
assert.doesNotMatch(source, /firstChange/, 'the variable that fed it should be removed');

// The marker spans the run, not the hunk — same reason.
const marked = source.match(/\$: markedBlock = \(\(\) => \{([\s\S]*?)\}\)\(\);/);
assert.ok(marked, 'markedBlock is gone');
assert.match(
  marked[1],
  /flatLines\[to \+ 1\]\.type === 'add' \|\| flatLines\[to \+ 1\]\.type === 'remove'/,
  'the marker should run to the end of the changed lines',
);

console.log('diffChangeRuns: ok');
