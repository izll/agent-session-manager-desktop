import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * Wiring the resume into the diff view.
 *
 * The state itself is tested in diffViewState.test.mjs. What is checked here is
 * that the component actually saves on the way out and restores on the way in —
 * and that the restore is not immediately undone by the reset that runs when a
 * file is opened, which is what made the position vanish in the first place.
 */
const diff = readFileSync(
  new URL('../src/lib/components/MainPanel/Diff.svelte', import.meta.url),
  'utf8',
);

// Saved on the way out. The component is destroyed by the tab switch, so there
// is no later moment to do it in.
assert.match(diff, /onDestroy\(\(\) => \{[\s\S]*?savePlace\(\);/, 'the place must be saved on destroy');

/**
 * But the offset cannot be READ on the way out.
 *
 * Svelte nulls a `bind:this` reference before destroying the child — the
 * compiled output is literally `ctx[1](null); destroy_component(...)`, and
 * onDestroy runs inside that second call. So asking the scrollers where they
 * were returns nothing, the saved offset was always 0, and 0 is
 * indistinguishable from "at the top": nothing was ever restored.
 *
 * The position is therefore tracked as it scrolls, while it is still readable.
 */
assert.match(diff, /function trackScroll\(\)/, 'the offset has to be tracked while scrolling');
assert.match(
  diff,
  /on:scroll=\{trackScroll\}/,
  'the hunk list scroller must report its position',
);
assert.match(
  diff,
  /on:viewscroll=\{trackScroll\}/,
  'the child renderers must report theirs',
);

// The tracked value belongs to one file in one renderer; switching either
// leaves a reading from the previous one behind.
assert.match(
  diff,
  /lastOffsetFor = offsetOwner\(selectedPath, viewMode\)/,
  'the tracked offset must record what it belongs to',
);
const save = diff.match(/function savePlace\(\)([\s\S]*?)\n {2}\}/);
assert.ok(save, 'savePlace is missing');
assert.match(
  save[0],
  /const fresh = lastOffsetFor === offsetOwner\(selectedPath, viewMode\)/,
  'a reading from another file or renderer must not be saved as this one',
);
/**
 * Which reading wins is decided on the ANSWER, not on whether the component
 * reference still exists.
 *
 * At teardown the reference is still set while the DOM element inside it is
 * already gone: the scroller reports 0 and looks perfectly bound. Choosing on
 * the reference therefore threw away a tracked 2816 in favour of that 0 — the
 * saved position was 0 every time, and 0 is indistinguishable from "at the
 * top", so nothing was restored. Observed in a running build, not deduced.
 *
 * A live 0 is genuinely at the top, where the tracked value would be 0 too, so
 * preferring the tracked one costs nothing.
 */
assert.match(
  save[0],
  /const scrollTop = currentScrollOffset\(\) \|\| \(fresh \? lastOffset : 0\)/,
  'the live reading wins only if it is non-zero; a bound-looking 0 must not beat the tracked value',
);
assert.doesNotMatch(
  save[0],
  /!!sideBySideView/,
  'the component reference outlives the element inside it, so it cannot decide this',
);

// Both children have to announce their scrolling, or there is nothing to track.
for (const [name, file] of [
  ['VirtualLines', 'VirtualLines.svelte'],
  ['SideBySideDiff', 'SideBySideDiff.svelte'],
]) {
  const child = readFileSync(new URL(`../src/lib/components/MainPanel/${file}`, import.meta.url), 'utf8');
  assert.match(child, /dispatch\('viewscroll'\)/, `${name} must announce its scrolling`);
}

// And when another file takes over: on destroy alone the position was lost the
// moment a different file was picked.
assert.match(
  diff,
  /function selectFile\(path: string\) \{\s*\n(?:\s*\/\/[^\n]*\n)*\s*savePlace\(\);/,
  'the file being left should be saved before another replaces it',
);

// Restored where the reset happens, not instead of it: a file opened for the
// first time still has to start before its first change.
const reset = diff.match(/\$: if \(selectedPath && selectedPath !== hunkCursorFor[\s\S]*?\n {2}\}/);
assert.ok(reset, 'the open-a-file reset is missing');
assert.match(reset[0], /currentHunk = -1;/, 'a file opened afresh starts before its first change');
assert.match(reset[0], /restorePlace\(selectedPath, viewMode\)/, 'a file seen before should resume');

// Svelte orders reactive statements by READING them, and it cannot see through
// the call to restorePlace — which reads viewMode. Without the mention here
// this block can run before viewMode is assigned, and the lookup goes to a key
// that was never written: the position silently never comes back.
assert.match(
  reset[0],
  /&& viewMode\)/,
  'the block must read viewMode, or Svelte may order it first',
);

const restore = diff.match(/async function restorePlace\(([\s\S]*?)\n {2}\}/);
assert.ok(restore, 'restorePlace is missing');

// An offset means nothing until the content is there: restoring into an empty
// list is clamped to nothing and lands at the top — which looks exactly like a
// restore that never ran.
assert.match(
  restore[0],
  /await waitForFileLoad\(path\)/,
  'the content must be in place before an offset can be applied',
);
assert.match(
  restore[0],
  /if \(selectedPath !== path \|\| viewMode !== mode\) return;/,
  'a file or renderer that took over while loading owns the position now',
);

// Three renderers, three scrollers, and no shared coordinate system between
// them — the columns pair lines up, the whole-file view holds every line, the
// hunk list only the changes.
assert.match(restore[0], /sideBySideView\?\.restoreOffset/, 'the column view needs restoring');
assert.match(restore[0], /virtualLines\?\.restoreOffset/, 'the whole-file view needs restoring');
assert.match(restore[0], /hunkListEl\.scrollTop = place\.scrollTop/, 'the hunk list needs restoring');

const offset = diff.match(/function currentScrollOffset\(\)([\s\S]*?)\n {2}\}/);
assert.ok(offset, 'currentScrollOffset is missing');
assert.match(offset[0], /sideBySide/, 'reading back has to match the renderer showing');
assert.match(offset[0], /wholeFileView/);

// The mode is part of what is stored, for the same reason.
assert.match(
  diff,
  /\$: viewMode = sideBySide \? 'sbs' : wholeFileView \? 'whole' : 'hunks'/,
  'the renderer must be part of the key',
);

/**
 * A changed file list means the cached contents are stale, and so is any offset
 * into them — so the places are dropped with the cache.
 *
 * What that comparison is made against has to live OUTSIDE the component. Held
 * inside, it came back empty after a tab switch, so the first load always
 * looked like a change and forgot the very places the switch was meant to
 * preserve. Nothing about the code read wrong; the state simply was not there
 * any more. This is the bug that made the position never come back.
 */
assert.doesNotMatch(
  diff,
  /let listKey = ''/,
  'a per-component listKey comes back empty and makes every first load look changed',
);
assert.match(
  diff,
  /if \(noteListKey\(sessionId, nextKey, windowIdx, mode, root, projectId\)\)/,
  'the comparison must be made against state that survives the component',
);
assert.doesNotMatch(
  diff,
  /forgetSession\(/,
  'forgetting is noteListKey\'s job, so it cannot drift apart from the comparison',
);

/**
 * The two components that own a scroller have to be able to give it back.
 */
const virtual = readFileSync(
  new URL('../src/lib/components/MainPanel/VirtualLines.svelte', import.meta.url),
  'utf8',
);
assert.match(virtual, /export function scrollOffset\(\)/, 'VirtualLines must report its offset');
assert.match(virtual, /export async function restoreOffset\(/, 'VirtualLines must take one back');
// Rounded to a line, restoring would nudge the file every time.
assert.match(
  virtual,
  /export function scrollOffset\(\): number \{\s*\n\s*return scrollTop;/,
  'the exact offset, not the first visible line',
);
// The list is rendered from a slice the reset has just emptied, so without the
// tick the assignment is clamped to a height that does not exist yet.
assert.match(
  virtual.match(/export async function restoreOffset\([\s\S]*?\n {2}\}/)[0],
  /await tick\(\)/,
  'the height has to exist before an offset can be applied to it',
);

const sbs = readFileSync(
  new URL('../src/lib/components/MainPanel/SideBySideDiff.svelte', import.meta.url),
  'utf8',
);
assert.match(sbs, /export function scrollOffset\(\)/, 'the column view must report its offset');
const sbsRestore = sbs.match(/export async function restoreOffset\([\s\S]*?\n {2}\}/);
assert.ok(sbsRestore, 'the column view must take one back');
// Restoring is a lead: left armed, the guard would drop the left pane's answer
// and leave the two sides showing different parts of the file.
assert.match(sbsRestore[0], /echoFrom = null;/, 'restoring must not be mistaken for an echo');
assert.match(sbsRestore[0], /syncFrom\('right'\)/, 'the other side has to follow the restore');

console.log('diffResume: ok');
