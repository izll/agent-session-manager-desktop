// Stepping through a review moves change by change, and runs past the end of a
// file into the next one — stopping at every file boundary is exactly the
// friction this exists to remove.
//
// Mirrors stepChange/stepFile in MainPanel/Diff.svelte.
import { test } from 'node:test';
import assert from 'node:assert/strict';

/** A review: several files, each with some number of changes. */
function makeReview(changesPerFile) {
  return {
    files: changesPerFile.map((n, i) => ({ path: `f${i}`, changes: n })),
    fileIndex: 0,
    hunk: -1,
    get current() { return { file: this.fileIndex, hunk: this.hunk }; },

    step(delta) {
      const hunks = this.files[this.fileIndex].changes;
      const next = this.hunk + delta;
      if (next >= 0 && next < hunks) {
        this.hunk = next;
        return true;
      }
      return this.stepFile(delta);
    },

    stepFile(delta) {
      // Wraps: a review is a loop, gone round until nothing needs another look.
      const count = this.files.length;
      const next = ((this.fileIndex + delta) % count + count) % count;
      this.fileIndex = next;
      // Entering a file backwards means its LAST change is the one wanted.
      this.hunk = delta > 0 ? 0 : Math.max(0, this.files[next].changes - 1);
      return true;
    },
  };
}

test('the first press lands on the first change, not the second', () => {
  const r = makeReview([3]);
  r.step(1);
  assert.deepEqual(r.current, { file: 0, hunk: 0 });
});

test('stepping walks the changes in order', () => {
  const r = makeReview([3]);
  r.step(1); r.step(1);
  assert.deepEqual(r.current, { file: 0, hunk: 1 });
});

test('the end of a file runs on into the next', () => {
  const r = makeReview([2, 2]);
  r.step(1); r.step(1);            // both changes in file 0
  assert.deepEqual(r.current, { file: 0, hunk: 1 });
  r.step(1);
  assert.deepEqual(r.current, { file: 1, hunk: 0 }, 'the review stopped at the file boundary');
});

test('stepping back enters the previous file at its LAST change', () => {
  // Otherwise going back lands at the top of the file and the change just
  // reviewed is off-screen above.
  const r = makeReview([3, 1]);
  r.stepFile(1);                    // now in file 1
  r.step(-1);
  assert.deepEqual(r.current, { file: 0, hunk: 2 });
});

test('past the last file the walk returns to the first', () => {
  const r = makeReview([1, 1]);
  r.step(1);                       // f0's only change
  r.step(1);                       // on to f1
  assert.deepEqual(r.current, { file: 1, hunk: 0 });
  r.step(1);                       // past the end
  assert.deepEqual(r.current, { file: 0, hunk: 0 }, 'the walk stopped instead of wrapping');
});

test('back past the first file lands on the last', () => {
  const r = makeReview([2, 1]);
  r.step(1);                       // f0, first change
  r.step(-1);                      // back past the start
  assert.equal(r.current.file, 1, 'going back from the first file did not wrap');
});

test('a file with no changes is still passed through', () => {
  // An empty file in the list should not trap the walk.
  const r = makeReview([1, 0, 1]);
  r.step(1);
  r.step(1);                        // into f1, which has none
  r.step(1);
  assert.equal(r.current.file, 2, `stalled at file ${r.current.file}`);
});

// Stepping into another file has to wait for that file's content.
//
// The changes are not known until the diff is fetched, so a step that acts
// immediately finds no changes and stops — which looked exactly like "the
// button does nothing at the end of a file", when in fact it had already moved
// and had nothing to move to yet.
function makeAsyncReview(changesPerFile) {
  return {
    files: changesPerFile.map((n, i) => ({ path: `f${i}`, changes: n })),
    loaded: new Set([0]),          // only the open file starts loaded
    fileIndex: 0,
    hunk: -1,
    pendingEntry: null,

    /** What a step does when it does NOT wait: the bug. */
    stepWithoutWaiting(delta) {
      const next = this.fileIndex + delta;
      if (next < 0 || next >= this.files.length) return false;
      this.fileIndex = next;
      const known = this.loaded.has(next) ? this.files[next].changes : 0;
      if (!known) { this.hunk = -1; return false; }
      this.hunk = delta > 0 ? 0 : known - 1;
      return true;
    },

    /** What it does when it waits. */
    async stepWaiting(delta) {
      const next = this.fileIndex + delta;
      if (next < 0 || next >= this.files.length) return false;
      this.fileIndex = next;
      this.pendingEntry = delta > 0 ? 'first' : 'last';
      this.loaded.add(next);                       // the fetch resolves
      const count = this.files[next].changes;
      if (!count) { this.pendingEntry = null; return false; }
      this.hunk = this.pendingEntry === 'first' ? 0 : count - 1;
      this.pendingEntry = null;
      return true;
    },
  };
}

test('stepping without waiting lands nowhere — the reported bug', () => {
  const r = makeAsyncReview([1, 2]);
  assert.equal(r.stepWithoutWaiting(1), false,
    'this is the failure: the file changed but no change was found in it');
  assert.equal(r.hunk, -1);
});

test('waiting for the file lands on its first change', () => {
  const r = makeAsyncReview([1, 2]);
  return r.stepWaiting(1).then((moved) => {
    assert.ok(moved);
    assert.equal(r.fileIndex, 1);
    assert.equal(r.hunk, 0);
  });
});

test('the pending entry is cleared even when the file has no changes', () => {
  // Left set, it would suppress the reset that puts a newly opened file before
  // its first change, and the next step would resume from a stale position.
  const r = makeAsyncReview([1, 0]);
  return r.stepWaiting(1).then(() => {
    assert.equal(r.pendingEntry, null);
  });
});

// A scroll container keeps its position when its contents are replaced, so
// opening a file after a longer one leaves it scrolled to where the previous
// file had been. Most visible when the walk wraps from the last file to the
// first: it arrived showing that file's final lines instead of its top.
//
// Mirrors the lines-changed reset in VirtualLines.svelte.
function makeViewport() {
  return {
    scrollTop: 0,
    lines: null,
    setLines(next) {
      // Identity, not contents: a new file means a new array, and comparing
      // thousands of lines would cost more than the scroll it corrects.
      if (next !== this.lines) {
        this.lines = next;
        this.scrollTop = 0;
      }
    },
  };
}

test('replacing the content scrolls back to the top', () => {
  const v = makeViewport();
  const longFile = new Array(3000).fill('x');
  v.setLines(longFile);
  v.scrollTop = 52_000;              // read to the end of it

  v.setLines(new Array(40).fill('y'));
  assert.equal(v.scrollTop, 0, 'the new file opened part-way down');
});

test('re-rendering the same content does not lose the position', () => {
  // A resize or a highlight arriving late must not throw away where the user
  // had scrolled to.
  const v = makeViewport();
  const file = new Array(500).fill('x');
  v.setLines(file);
  v.scrollTop = 4_000;
  v.setLines(file);
  assert.equal(v.scrollTop, 4_000);
});

// The button says which file a press would move to, so leaving the current one
// is never a surprise. Silent while the step stays inside this file.
//
// Mirrors fileAfterStep in Diff.svelte.
function fileAfterStep(delta, hunk, hunkCount, files, currentIndex) {
  const staysHere = hunkCount && hunk + delta >= 0 && hunk + delta < hunkCount;
  if (staysHere) return null;
  if (!files.length) return null;
  const count = files.length;
  const next = ((currentIndex + delta) % count + count) % count;
  if (next === currentIndex) return null;
  return files[next];
}

test('no file is named while the step stays in this one', () => {
  // Three changes, sitting on the first: the next press moves within the file.
  assert.equal(fileAfterStep(1, 0, 3, ['a', 'b'], 0), null);
});

test('the last change names the next file', () => {
  assert.equal(fileAfterStep(1, 2, 3, ['a', 'b'], 0), 'b');
});

test('the first change names the previous file', () => {
  assert.equal(fileAfterStep(-1, 0, 3, ['a', 'b'], 1), 'a');
});

test('at the end of the review it names the first file, because the walk wraps', () => {
  assert.equal(fileAfterStep(1, 2, 3, ['a', 'b'], 1), 'a');
});

test('a review of one file names nothing', () => {
  // Wrapping would land back where it started, so there is no move to announce.
  assert.equal(fileAfterStep(1, 2, 3, ['only'], 0), null);
});

test('a file with no changes still names its neighbour', () => {
  // Nothing to step to inside it, so any press leaves — and should say so.
  assert.equal(fileAfterStep(1, -1, 0, ['a', 'b'], 0), 'b');
});

// Entering a file costs the same either way round.
//
// A file entered backwards used to land ON its last change, so a single press
// left again — working back through a review passed over files without ever
// stopping in them, while going forward stopped in every one. The position is
// set one step before the change in the direction of travel, so the first press
// lands on the change already scrolled to and the second crosses out.
//
// Mirrors the landing calculation in stepFile.
function pressesToLeave(changeCount, entry) {
  const landing = entry === 'first' ? 0 : changeCount - 1;
  let hunk = entry === 'first' ? landing - 1 : landing + 1;
  const delta = entry === 'first' ? 1 : -1;
  for (let presses = 1; presses <= 50; presses++) {
    const next = hunk + delta;
    if (next < 0 || next >= changeCount) return presses;
    hunk = next;
  }
  throw new Error('never left the file');
}

test('leaving costs the same forwards and backwards', () => {
  for (const changes of [1, 2, 3, 8]) {
    assert.equal(
      pressesToLeave(changes, 'first'),
      pressesToLeave(changes, 'last'),
      `a file with ${changes} change(s) is asymmetric`);
  }
});

test('a file with one change takes two presses to pass through', () => {
  // One to land on the change, one to leave — in both directions.
  assert.equal(pressesToLeave(1, 'first'), 2);
  assert.equal(pressesToLeave(1, 'last'), 2);
});

test('every change is visited on the way through', () => {
  // The count is changes + 1: one press each, plus the one that leaves.
  for (const changes of [1, 4, 9]) {
    assert.equal(pressesToLeave(changes, 'first'), changes + 1);
  }
});

// A file opened by clicking it in the list sits "before the first change" — a
// position that only means "before" when moving forward. Read literally it is
// -1, so stepping UP from it computed -2, fell outside the file, and left for
// the previous one on the very first press: the up arrow skipped whole files
// instead of walking their changes.
//
// Mirrors the `from` adjustment in stepChange.
function stepFrom(hunk, delta, changeCount) {
  const from = hunk === -1 && delta < 0 ? changeCount : hunk;
  const next = from + delta;
  return next < 0 || next >= changeCount ? 'leaves' : next;
}

test('the down arrow enters an untouched file at its first change', () => {
  assert.equal(stepFrom(-1, 1, 3), 0);
});

test('the up arrow enters an untouched file at its LAST change', () => {
  assert.equal(stepFrom(-1, -1, 3), 2, 'the up arrow left the file instead of entering it');
});

test('the up arrow then walks back through every change', () => {
  let hunk = -1;
  const visited = [];
  for (let i = 0; i < 5; i++) {
    const next = stepFrom(hunk, -1, 3);
    if (next === 'leaves') break;
    visited.push(next);
    hunk = next;
  }
  assert.deepEqual(visited, [2, 1, 0], 'changes were skipped on the way back');
});

test('a file with one change is entered from either direction', () => {
  assert.equal(stepFrom(-1, 1, 1), 0);
  assert.equal(stepFrom(-1, -1, 1), 0);
});

// Only the hint ahead of you is shown. Both at once is noise: at the end of a
// small file each arrow would leave it, so two labels appear announcing
// opposite destinations, and neither tells you what the key you are about to
// press will do.
//
// Mirrors hintAbove/hintBelow in Diff.svelte.
function hints(direction, nextFile, prevFile) {
  return {
    below: direction === 1 ? nextFile : null,
    above: direction === -1 ? prevFile : null,
  };
}

test('stepping down shows only the next file, below', () => {
  const h = hints(1, 'b.ts', 'a.ts');
  assert.equal(h.below, 'b.ts');
  assert.equal(h.above, null, 'the previous file was announced while moving forward');
});

test('stepping up shows only the previous file, above', () => {
  const h = hints(-1, 'b.ts', 'a.ts');
  assert.equal(h.above, 'a.ts');
  assert.equal(h.below, null);
});

test('a file just opened shows neither', () => {
  // No direction yet: picking a file from the list is not stepping, and a hint
  // would point somewhere the user was not heading.
  const h = hints(null, 'b.ts', 'a.ts');
  assert.equal(h.above, null);
  assert.equal(h.below, null);
});

test('a hint stays absent when there is no file to move to', () => {
  // Direction alone is not enough — the step still has to leave the file.
  const h = hints(1, null, 'a.ts');
  assert.equal(h.below, null);
});
