import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * Stepping between changes in the history dialog, and out into the next file.
 *
 * Two things went wrong here, and both are easier to reason about in a test
 * than to see by clicking: crossing into a file only opened it, leaving the
 * cursor before its first change, so it took two presses to move on and a
 * press upwards then left the file again immediately. And the "next file"
 * hint keyed off that same cursor, so on a freshly opened file it appeared on
 * the wrong edge.
 */
const source = readFileSync(
  new URL('../src/lib/components/Dialogs/GitHistoryDialog.svelte', import.meta.url),
  'utf8',
);

function body(name) {
  const marker = 'async function ' + name + '(';
  const start = source.indexOf(marker);
  assert.notEqual(start, -1, name + ' is missing');
  const end = source.indexOf('\n  }', start);
  return source.slice(start, end);
}

const stepFile = body('stepFile');

// Crossing a boundary lands on the change at the end it came in from — first
// going forward, last going back — so entering a file backwards does not start
// at its top with the change just left behind off-screen above.
//
// The cursor lands ON it, not one step before. Set before it, the next press
// moved onto the change already on screen: a press that appeared to do
// nothing, doubling every step through a review.
assert.match(
  stepFile,
  /const landing = delta > 0 \? 0 : changeStarts\.length - 1/,
  'the change to land on is the one at the end we came in from',
);
assert.match(
  stepFile,
  /currentChange = landing/,
  'the cursor lands on the change, not one step before it',
);

// The marker names the block on screen. Sharing the stepping cursor left the
// block unmarked until a later press.
assert.match(
  stepFile,
  /markedChange = landing/,
  'the marker should name the block that was scrolled to',
);
assert.match(
  stepFile,
  /revealChange/,
  'the change it lands on must be scrolled to, or the jump is invisible',
);

// Two renderers, two coordinate systems. The columns pair lines up, so their
// rows do not line up with the unified positions changeStarts holds — asked
// to scroll by a unified position the columns went to the wrong place, or in
// whole-file view (one hunk covering everything) to the top of the file, which
// looks exactly like a scroll that never happened.
const reveal = source.match(/function revealChange\(([\s\S]*?)\n {2}\}/);
assert.ok(reveal, 'revealChange is missing');
assert.match(
  reveal[0],
  /sideBySideView\?\.changeRows\(\)/,
  'the column view must be asked where its own changes are',
);
assert.match(
  reveal[0],
  /scrollToRow\(run\.from, run\.to - run\.from \+ 1\)/,
  'the column scroll needs the run, so a tall change is not pushed off the bottom',
);

// A third of the way down, not centred: a change is read downwards, so the
// room below it is worth more than the room above, and centring spends half
// the screen on context already read.
const scroller = source.match(/function scrollToThird\([\s\S]*?\n {2}\}/);
assert.ok(scroller, 'scrollToThird is missing');
assert.match(
  scroller[0],
  /view \/ 3/,
  'the change should sit a third of the way down the viewport',
);

// Unless it is too tall for that: a block longer than the room below a third
// would run off the bottom, and the part scrolled past is the part being read.
assert.match(
  scroller[0],
  /blockHeight > view - view \/ 3/,
  'a block taller than the space below a third needs the whole viewport',
);

// Measured across the whole change, not from its first row. Handed the row
// alone this always came out as one line, so a long block never met the
// condition above and ran off the bottom of the screen.
assert.match(
  scroller[0],
  /element\.offsetHeight \* Math\.max\(1, lines\)/,
  'the height must span the change, not just its first line',
);
assert.match(
  source,
  /function blockLength\(/,
  'something has to work out how far the change runs',
);

// selectFile resets the cursor and the new file's changes are derived
// reactively, so the landing has to wait for that to settle.
assert.match(
  stepFile,
  /await selectFile[\s\S]*await tick\(\)[\s\S]*changeStarts/,
  'the new file\'s changes must be awaited before landing on one',
);

// Wrapping keeps a review moving: stopping at the last file means going back
// to the tree to start again.
assert.match(
  stepFile,
  /% order\.length/,
  'stepping past either end should wrap around the file list',
);

const stepChange = body('stepChange');

// A file opened by clicking sits at -1, "before the first change" — which is
// only "before" when moving forward.
assert.match(
  stepChange,
  /currentChange === -1 && delta < 0 \? changeStarts\.length : currentChange/,
  'stepping up from a freshly opened file should reach its last change',
);


// The hint shows on the edge the step is heading towards. Both at once would
// be two answers to a question with one, over the code either side of what is
// being read.
// Running out of changes stops and offers the next file rather than going
// there: the name and the jump used to land on the same press, so the file
// being entered was replaced by its own contents before the name could be
// read. The press after the offer accepts it.
assert.match(
  source,
  /atFileEdge = delta > 0 \? 1 : -1;\s*\n\s*return;/,
  'running out of changes should pause at the edge, not move on',
);
assert.match(
  source,
  /if \(atFileEdge === delta\)[\s\S]{0,120}stepFile\(delta\)/,
  'pressing again in the same direction should accept the offer',
);
assert.match(
  source,
  /hintBelow = atFileEdge === 1 \? nextFileName/,
  'the lower hint shows while the offer to move down stands',
);
assert.match(
  source,
  /hintAbove = atFileEdge === -1 \? prevFileName/,
  'the upper hint shows while the offer to move up stands',
);

assert.match(
  stepChange,
  /stepDirection = delta > 0 \? 1 : -1/,
  'the direction must be recorded, or the hint cannot pick a side',
);

// A commit touching one file wraps onto itself. Re-reading it throws away the
// position and lands at the far end, so the arrow looked like it jumped to the
// other side of the file for no reason.
assert.match(
  stepFile,
  /if \(next === at\) return;/,
  'a single-file commit has nowhere to step to and should stay put',
);

// Which lines to read, not merely where the block starts: in whole-file view a
// change is an island in a long file.
assert.match(
  source,
  /\$: currentBlock = /,
  'the whole current change block should be identified, not just its first line',
);
assert.match(
  source,
  /class:in-block=\{i >= currentBlock\.from && i <= currentBlock\.to\}/,
  'every line of the current block should be marked',
);

// The selection uses the accent, as the diff view and the tab bar do — a grey
// wash says nothing about which selection it is.
const treeSelected = source.match(/\n {2}\.tree-row\.selected \{([\s\S]*?)\n {2}\}/);
assert.ok(treeSelected, '.tree-row.selected rule is missing');
assert.match(
  treeSelected[1],
  /--accent-rgb/,
  'the selected file should use the accent tint, matching the diff view',
);

console.log('historyStepping: ok');
