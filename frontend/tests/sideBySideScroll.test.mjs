import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * Two scrollers rather than one grid.
 *
 * A single grid has to pad the shorter side to keep the pairs level, so a long
 * insertion shows a screenful of blank rows beside it. Split into two columns
 * that are kept in step by scrolling, the side with nothing to show simply
 * waits while the other runs through the insertion — which is what IntelliJ
 * does, and what makes a large insertion readable.
 *
 * The arithmetic that does the waiting is tested against real data in
 * sideBySide.test.mjs. What is checked here is the wiring: that the component
 * actually has two scrollers, that they are kept in step, and that the loop
 * this obviously invites is guarded.
 */
const source = readFileSync(
  new URL('../src/lib/components/MainPanel/SideBySideDiff.svelte', import.meta.url),
  'utf8',
);

// Two scrollable panes, not one.
assert.match(source, /bind:this=\{leftEl\}/, 'the left pane needs its own element');
assert.match(source, /bind:this=\{rightEl\}/, 'the right pane needs its own element');
assert.match(
  source,
  /on:scroll=\{\(\) => syncFrom\('left'\)\}/,
  'scrolling the left pane must lead the right',
);
assert.match(
  source,
  /on:scroll=\{\(\) => syncFrom\('right'\)\}/,
  'scrolling the right pane must lead the left',
);

const sync = source.match(/function syncFrom\(([\s\S]*?)\n {2}\}/);
assert.ok(sync, 'syncFrom is missing');

// The waiting comes from matchingLine: where the following side has no line
// for the leading side's row, it stays on the last line it does have.
assert.match(
  sync[0],
  /matchingLine\(fromLines, toLines, topLine\)/,
  'the following side is placed by the pairing, which is what makes it wait',
);

// Two elements each answering the other's scroll is a loop that never settles,
// so the follower's own event has to be dropped.
//
// Identified by which side is expected to report next, NOT by a timer. A wheel
// sends events faster than a frame, so a guard held open for a frame swallows
// the next real scroll along with the echo — and the wheel stops working.
assert.match(
  sync[0],
  /if \(echoFrom === source\) \{\s*\n\s*echoFrom = null;\s*\n\s*return;/,
  'the follower\'s echo should be dropped by identity, and the guard cleared with it',
);
assert.doesNotMatch(
  source,
  /requestAnimationFrame/,
  'a frame-long guard eats the wheel events that arrive within it',
);

// Setting scrollTop to the value it already holds sends no event, so arming the
// guard for it would eat the next genuine scroll instead.
assert.match(
  sync[0],
  /Math\.round\(to\.scrollTop\) !== Math\.round\(wanted\)/,
  'only a move that will actually happen should arm the guard',
);

// Past its end an element simply stops, and asking for more leaves the two
// sides disagreeing about where they are.
assert.match(
  source,
  /function clampScroll\([\s\S]*?scrollHeight - element\.clientHeight/,
  'scroll positions must be clamped to what the element can take',
);

// The fraction of a row is carried across, or the sides jerk a row apart at
// every scroll step.
assert.match(
  sync[0],
  /const offset = from\.scrollTop - topLine \* ROW_HEIGHT/,
  'the position within the row must be carried across',
);

// Stepping has to lead from the side that holds the change. An insertion exists
// only on the right; led from the left it scrolls to the line the left is
// waiting on, leaving the insertion off-screen — a jump that looks like nothing
// happened.
const scroll = source.match(/export function scrollToRow\(([\s\S]*?)\n {2}\}/);
assert.ok(scroll, 'scrollToRow is missing');
assert.match(
  scroll[0],
  /block\.leftFrom === -1 \|\| rightLines >= leftLines/,
  'the side holding more of the change should lead the scroll',
);

// Near the end of the file the leading pane runs out of scroll before the
// change reaches its third, and leaves it sitting at the bottom of the view.
// The other side has a different number of lines below it and is not stuck in
// the same place, so it can pull the change up by leading instead.
assert.match(
  scroll[0],
  /const short = wanted - top > ROW_HEIGHT/,
  'a leader that cannot scroll far enough has to be noticed',
);
assert.match(
  scroll[0],
  /if \(otherTop - other\.scrollTop > top - lead\.scrollTop\)/,
  'the other side should lead only where it can actually get further',
);

// A step is a lead, not an echo: leaving the guard armed would drop the
// follower's answer to it and leave the two sides apart.
assert.match(scroll[0], /echoFrom = null;/, 'stepping must clear the echo guard');

// A block absent from one side still needs a place on it for the ribbon to
// land, or the ribbon has no bottom edge and the block is drawn from the top of
// the file.
assert.match(
  source,
  /if \(block\[from\] === -1\) block\[to\] = waiting;/,
  'a block missing from a side must record where that side waits',
);

// A ribbon joins a block's left end to its right end, and with the panes
// scrolled independently those ends move by different amounts — through an
// insertion the left stands still while the right runs on. Placed against one
// shared offset, every ribbon anchors to whichever pane it was measured from
// and they pile up at the top of the strip.
assert.match(source, /let leftScroll = 0;/, 'the left pane needs its own offset');
assert.match(source, /let rightScroll = 0;/, 'the right pane needs its own offset');
assert.doesNotMatch(source, /ribbonOffset/, 'one shared offset cannot place both ends');

const shapes = source.match(/\$: shapes = blocks([\s\S]*?)\n\n/);
assert.ok(shapes, 'the ribbon shapes are missing');
assert.match(shapes[0], /- leftScroll/, 'the left edge is placed against the left pane');
assert.match(shapes[0], /- rightScroll/, 'the right edge is placed against the right pane');

// A large file has thousands of blocks, and an off-screen path still costs to
// lay out.
assert.match(
  shapes[0],
  /\.filter\(\(shape\) =>/,
  'only the ribbons near the viewport should be drawn',
);

// Marking by hunk lights up the whole file in whole-file view, where git
// returns the file as a single hunk. The mark belongs on the block.
assert.match(
  source,
  /\$: markedBlock = currentChange >= 0 \? blocks\[currentChange\] : undefined;/,
  'the current change should be found by ordinal among the blocks',
);
assert.doesNotMatch(
  source,
  /hunkIndex === currentHunk/,
  'marking by hunk lights up every change sharing it',
);

// The line number goes ahead of the code it labels, in both panes. Trailing it,
// the ruler reads as part of the line rather than as a label on it — and on the
// left pane that put every number in the middle of the view, against the code
// it did not belong to.
for (const pane of source.matchAll(/<div\s+class="sbs-line"[\s\S]*?<\/div>/g)) {
  const gutter = pane[0].indexOf('class="gutter"');
  const code = pane[0].indexOf('class="code"');
  assert.ok(gutter !== -1 && code !== -1, 'a pane is missing its gutter or its code');
  assert.ok(gutter < code, 'the line number belongs before the code, as in an editor');
}

/**
 * The outline around a changed block.
 *
 * Decided per COLUMN, from the neighbour within that column — a block three
 * lines on one side and one on the other has to be boxed as three there and one
 * here, and the paired-row boundary falls in the middle of nothing on the
 * shorter side.
 *
 * Adjacency alone is not enough: two blocks separated only by lines living on
 * the other side are neighbours in this column while being separate blocks. The
 * row numbers settle it — consecutive rows are one block, a gap is two.
 */
const marker = source.match(/function mark\(lines: SideLine\[\], side: 'left' \| 'right'\)([\s\S]*?)\n {2}\}/);
assert.ok(marker, 'the block-edge marking is missing');
assert.match(
  marker[0],
  /previous\.row === line\.row - 1/,
  'a gap in row numbers separates two blocks that are adjacent in the column',
);
assert.match(marker[0], /next\.row === line\.row \+ 1/, 'and the same going down');
assert.match(source, /class:block-top=\{line\.first\}/, 'the outline needs its top');
assert.match(source, /class:block-bottom=\{line\.last\}/, 'and its bottom');

// box-shadow does not stack across rules — the most specific one wins outright,
// so an outline and a current-change stripe on the same row would erase each
// other. Composed through variables instead of enumerating the combinations.
assert.match(
  source,
  /box-shadow: inset var\(--edge-top\), inset var\(--edge-bottom\), inset var\(--edge-side\)/,
  'the edges must compose, or the outline and the stripe cancel each other',
);
assert.equal(
  (source.match(/^\s*box-shadow:/gm) || []).length,
  1,
  'a second box-shadow rule would override the composed one instead of adding to it',
);

// The revert arrow belongs at the top of the ribbon, which is the higher of its
// two ends. Tied to the right end alone it slid down to the waiting line
// whenever a block existed only on the left — a deletion put the arrow well
// below the lines it acts on.
assert.match(
  source,
  /class="arrow-slot" style="top: \{Math\.min\(shape\.y1, shape\.y2\)\}px"/,
  'the arrow goes at the top of the ribbon, not at one end of it',
);

/**
 * The columns can only scroll independently if nothing outside them scrolls.
 *
 * The diff's own container is a scroller, and the panes are sized to it. With
 * no definite height to fill they grew to their content and never scrolled at
 * all, so both columns rode the OUTER scrollbar together — a single shared
 * position, which is exactly what the two-scroller layout exists to avoid. The
 * alignment arithmetic was right the whole time and never got to run: the panes
 * emitted no scroll events for it to answer.
 *
 * On screen it looked like the columns had drifted, because after an insertion
 * a shared position shows line N beside line N when they are six lines apart.
 */
const parent = readFileSync(
  new URL('../src/lib/components/MainPanel/Diff.svelte', import.meta.url),
  'utf8',
);
assert.match(
  parent,
  /class="diff-content" class:columns=\{sideBySide\}/,
  'the outer scroller has to know when the columns are showing',
);
const columns = parent.match(/\.diff-content\.columns \{([\s\S]*?)\n {2}\}/);
assert.ok(columns, 'the .diff-content.columns rule is missing');
assert.match(columns[1], /overflow: hidden/, 'the outer scroller must stop scrolling');
assert.match(
  columns[1],
  /display: flex/,
  'and give the view a definite height, or it sizes to its content and cannot scroll within',
);

// The view itself has to fill whichever kind of parent it lands in.
const sbsBox = source.match(/\n {2}\.sbs \{([\s\S]*?)\n {2}\}/);
assert.ok(sbsBox, 'the .sbs rule is missing');
assert.match(sbsBox[1], /height: 100%/, 'for a block parent');
assert.match(sbsBox[1], /flex: 1/, 'for a flex one');
assert.match(sbsBox[1], /min-height: 0/, 'or a flex child refuses to shrink below its content');

/**
 * The band between the panes, drawn the way IntelliJ draws it.
 *
 * A filled shape whose top and bottom are stroked SEPARATELY. Stroking the
 * filled path instead outlines the whole thing, which lands a line down the
 * inner edge of each pane — right where the block's own outline already is —
 * and the result reads as two boxed regions with a decoration between them
 * rather than as one region spanning the strip.
 */
assert.match(source, /class="ribbon-fill \{shape\.kind\}"/, 'the band has to be filled');
assert.equal(
  (source.match(/class="ribbon-edge \{shape\.kind\}"/g) || []).length,
  2,
  'its top and bottom are two open curves, not the outline of the filled shape',
);
const fill = source.match(/\.ribbon-fill \{([\s\S]*?)\n {2}\}/);
assert.ok(fill, 'the .ribbon-fill rule is missing');
assert.match(fill[1], /stroke: none/, 'the filled shape must not be outlined');
const edge = source.match(/\.ribbon-edge \{([\s\S]*?)\n {2}\}/);
assert.ok(edge, 'the .ribbon-edge rule is missing');
assert.match(edge[1], /fill: none/, 'the edges are curves, not shapes');

// The band takes the colour of the change it joins, and so does the block
// outline: a band in a third colour reads as a thing in its own right, and an
// outline in a different colour from the band stops at the pane instead of
// running across.
for (const kind of ['added', 'removed']) {
  assert.match(source, new RegExp(`\\.ribbon-fill\\.${kind} \\{`), `the band needs an ${kind} colour`);
  assert.match(source, new RegExp(`\\.ribbon-edge\\.${kind} \\{`), `and so does its edge`);
  assert.match(
    source,
    new RegExp(`\\.sbs-line\\.${kind}\\.block-top \\{`),
    `the block outline must match the band, not be blue against a green one`,
  );
}
assert.match(
  source,
  /kind: block\.leftFrom === -1 \? 'added' : block\.rightFrom === -1 \? 'removed' : 'changed'/,
  'a block present on one side only is an insertion or a deletion, not a replacement',
);

// A tinted strip between the panes reads as a divider, and the band crossing it
// then looks like something laid over a gap.
const middle = source.match(/\n {2}\.middle \{([\s\S]*?)\n {2}\}/);
assert.ok(middle, 'the .middle rule is missing');
assert.doesNotMatch(middle[1], /background:/, 'the strip must not be tinted into a divider');

/**
 * A block with no lines on this side still has to be marked.
 *
 * An insertion has none on the left, a deletion none on the right, so there is
 * no row to outline — and the band arrives from the strip and stops dead at the
 * edge of the pane. The mark goes on the boundary instead: a rule along the top
 * of the line that side waits on.
 */
assert.match(marker[0], /const gapAbove = blocks\.find/, 'a block absent from this side needs marking');
assert.match(
  marker[0],
  /side === 'left' \? 'added' : 'removed'/,
  'the change is on the other side, so the left shows an addition and the right a deletion',
);
// At the end of the file the side waits PAST its last line, where there is no
// row to put a rule above.
assert.match(
  marker[0],
  /at === lines\.length - 1 &&/,
  'a block at the end of the file has no following line to mark',
);
assert.match(source, /class:gap-added=\{line\.gap === 'added'\}/, 'the rule needs its class');
assert.match(source, /class:gap-under-added=\{line\.gapUnder === 'added'\}/, 'and so does the end-of-file one');
assert.match(source, /\.sbs-line\.gap-added \{/, 'and its style');
assert.match(source, /\.sbs-line\.gap-under-added \{/);

// mark() reads blocks through a call Svelte cannot see into, so the dependency
// has to be named or these can run against an undefined value.
assert.match(
  source,
  /\$: left = blocks && mark\(sides\.left, 'left'\)/,
  'blocks must be named here, or Svelte may order this before it',
);

// The arrow sits ON the coloured band, where an accent-coloured one competes
// with the colour instead of reading as a control.
const arrow = source.match(/\.revert-arrow \{([\s\S]*?)\n {2}\}/);
assert.ok(arrow, 'the .revert-arrow rule is missing');
assert.doesNotMatch(arrow[1], /--accent/, 'the arrow should not take the accent over a coloured band');

/**
 * The left pane has no vertical scrollbar, and its wheel is forwarded.
 *
 * A vertical bar takes its width out of the pane's usable area, and the rows
 * end where that area ends — so scrolled fully right a bar-shaped stripe of
 * background sat beyond them, uncoloured (measured: 460px visible against 444px
 * usable). Painting the bar transparent does not help, the space is still
 * taken; reserving it with a border is worse, since a border is outside the
 * scrollable box and the tint stops before it too.
 *
 * overflow-y: hidden still allows scrollTop from the sync — it only stops the
 * user scrolling directly — but it also lets the wheel pass through to whatever
 * is behind, so that has to be handled.
 */
assert.match(source, /class="pane left"/, 'the left pane needs to be identifiable');
const leftPane = source.match(/\n {2}\.pane\.left \{([\s\S]*?)\n {2}\}/);
assert.ok(leftPane, 'the .pane.left rule is missing');
assert.match(leftPane[1], /overflow-y: hidden/, 'a vertical bar costs the width the rows need');
assert.doesNotMatch(
  source,
  /border-right: var\(--scrollbar-width/,
  'a border is outside the scrollable box, so the row tint stops before it',
);
assert.doesNotMatch(
  source,
  /scrollbar-width: none/,
  'that takes the horizontal bar too, and that one is how a long line is scrolled',
);

// The wheel has to be forwarded, or it scrolls whatever is behind the pane.
assert.match(source, /on:wheel=\{onLeftWheel\}/, 'the left pane needs its wheel handled');
const wheel = source.match(/function onLeftWheel\(([\s\S]*?)\n {2}\}/);
assert.ok(wheel, 'onLeftWheel is missing');
assert.match(wheel[0], /rightEl\.scrollTop = wanted/, 'the movement goes to the pane that can scroll');
assert.match(wheel[0], /syncFrom\('right'\)/, 'and the left is brought along by the sync');
// At the end of the file the wheel should still reach the panel behind, as it
// would in any other scroller.
assert.match(
  wheel[0],
  /if \(wanted === before\) return;/,
  'a wheel that would move nothing must not be swallowed',
);

// The pane still scrolls: the sync reads and writes scrollTop on it.
const paneRule = source.match(/\n {2}\.pane \{([\s\S]*?)\n {2}\}/);
assert.match(paneRule[1], /overflow: auto/, 'the pane must still scroll');

// The line numbers carry their own line's tint. Opaque so other lines do not
// show through while scrolling, but a changed line whose number sits on plain
// grey reads as a ruler beside the diff rather than part of the row.
assert.match(source, /\.sbs-line\.removed \.gutter \{/, 'a removed line tints its number too');
assert.match(source, /\.sbs-line\.added \.gutter \{/, 'and an added one');
assert.doesNotMatch(
  source,
  /scrollbar-width: none/,
  'that hides both axes, taking the horizontal bar with it',
);
// overflow stays auto — hiding a bar must not stop the pane scrolling, or the
// synchronisation has nothing to read.
const pane = source.match(/\n {2}\.pane \{([\s\S]*?)\n {2}\}/);
assert.match(pane[1], /overflow: auto/, 'the pane must still scroll');

/**
 * Sideways the panes move as one, with no pairing involved.
 *
 * Vertically the sides hold different lines and the pairing decides what
 * belongs beside what. Along a line there is nothing to work out: column 80 is
 * column 80 on both sides, and letting them drift apart is what makes a long
 * line impossible to compare.
 */
assert.match(
  sync[0],
  /const wantedLeft = Math\.max\(/,
  'the horizontal position has to be carried across too',
);
assert.match(
  sync[0],
  /to\.scrollWidth - to\.clientWidth/,
  'clamped to what the follower can take: the two rarely have the same longest line',
);
// Either axis moving produces the one echo event, so the guard has to be armed
// for both — armed only for a vertical move, a purely sideways scroll would
// have its echo treated as a lead and the two would fight.
assert.match(sync[0], /const movesDown =/, 'a vertical move has to be recognised');
assert.match(sync[0], /const movesAcross =/, 'and a horizontal one');
assert.match(
  sync[0],
  /if \(movesDown \|\| movesAcross\) \{/,
  'either axis moving arms the echo guard',
);

/**
 * A row is as wide as its text, not as wide as the pane.
 *
 * Sized to the pane, a row scrolled sideways ran out at the pane's original
 * right edge: the added/removed tint and the block outline stopped dead partway
 * along the line, with the code carrying on past them.
 */
const lines = source.match(/\n {2}\.lines \{([\s\S]*?)\n {2}\}/);
assert.ok(lines, 'the .lines container is missing');
assert.match(lines[1], /width: max-content/, 'the container takes the width of the longest line');
assert.match(lines[1], /min-width: 100%/, 'and fills the pane when the file is narrow');
// On the rows themselves this sizes each to its OWN text, so a short changed
// line's tint ends at the pane edge while longer lines carry on past it.
assert.doesNotMatch(
  source.match(/\n {2}\.sbs-line \{([\s\S]*?)\n {2}\}/)[1],
  /min-width: max-content/,
  'sized per row, a short row is narrower than the pane scrolls',
);
assert.match(source, /<div class="lines"/, 'both panes need the container');

/**
 * The container is stretched to what the pane actually scrolls.
 *
 * `width: max-content` gets close but not exact: the webview settled it at
 * 874px while its own contents came to 881, so scrolled fully right seven
 * pixels sat past the end of every row as a stripe the tint could not reach.
 * Nothing in the box model accounts for it — no padding, border or scrollbar —
 * and Chromium does not reproduce it, so it is measured rather than derived.
 */
assert.match(
  source,
  /min-width: \$\{leftWidth\}px/,
  'the left container needs the measured width',
);
assert.match(source, /min-width: \$\{rightWidth\}px/, 'and the right one');
const measure = source.match(/function measureWidths\(\)([\s\S]*?)\n {2}\}/);
assert.ok(measure, 'measureWidths is missing');
// Assigned unconditionally this runs away: the min-width widens the pane's
// scrollWidth, which is then measured again as the new target.
assert.match(
  measure[0],
  /leftEl\.scrollWidth > leftWidth/,
  'the width may only grow towards what the pane already scrolls',
);
// And a new file is a new width, or the widest line of the previous one keeps
// the panes scrolling past the end of this one.
assert.match(
  source,
  /leftWidth = 0;\s*\n\s*rightWidth = 0;/,
  'a new file must clear the measured widths',
);
assert.match(
  source,
  /tick\(\)\.then\(measureWidths\)/,
  'and measure again once it has been laid out',
);
// min-width: 0 on the code would let it shrink below its text and undo that.
const code = source.match(/\n {2}\.code \{([\s\S]*?)\n {2}\}/);
assert.ok(code, 'the .code rule is missing');
assert.doesNotMatch(code[1], /min-width: 0/, 'that would undo the row width');

// The ruler stays at the pane's edge while the code scrolls under it —
// otherwise the lines on screen are unnumbered exactly when the number is most
// wanted, reading a long line and asking which one it is.
const gutter = source.match(/\n {2}\.gutter \{([\s\S]*?)\n {2}\}/);
assert.ok(gutter, 'the .gutter rule is missing');
assert.match(gutter[1], /position: sticky/, 'the ruler should hold its place');
// And it has to be opaque, or the line's own tint slides past behind the
// numbers.
assert.match(
  gutter[1],
  /background: #[0-9a-f]{6}/,
  'a translucent ruler shows the scrolling tint through it',
);

// Row height is arithmetic here, not just styling: the sides are placed by it.
const rowHeight = source.match(/const ROW_HEIGHT = (\d+)/);
assert.ok(rowHeight, 'ROW_HEIGHT is missing');
assert.match(
  source,
  new RegExp(`\\.sbs-line \\{[\\s\\S]*?height: ${rowHeight[1]}px`),
  'the CSS row height must match ROW_HEIGHT, or the two sides drift apart',
);

console.log('sideBySideScroll: ok');
