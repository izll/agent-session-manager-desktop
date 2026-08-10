import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * A large diff must not be syntax-coloured before it is drawn.
 *
 * Colouring a line costs a parse. Measured against a real grammar: about 500ms
 * for 10,000 lines, and under a millisecond for the fifty a virtualised view
 * actually shows. Doing it when the file is opened meant paying for all of it
 * before anything appeared, however little of it was ever seen — the whole
 * point of virtualising, undone one layer down.
 *
 * So the flat line list holds TEXT, and the renderers colour what they draw.
 * This is easy to undo by accident: `html: highlightLine(...)` in a map over
 * every line looks perfectly reasonable in isolation.
 */
const diff = readFileSync(
  new URL('../src/lib/components/MainPanel/Diff.svelte', import.meta.url),
  'utf8',
);
const virtual = readFileSync(
  new URL('../src/lib/components/MainPanel/VirtualLines.svelte', import.meta.url),
  'utf8',
);
const util = readFileSync(
  new URL('../src/lib/utils/highlightLine.ts', import.meta.url),
  'utf8',
);

// The flat list holds text, not HTML.
const build = diff.match(/function buildFlatLines\(([\s\S]*?)\n {2}\}/);
assert.ok(build, 'buildFlatLines is missing');
assert.match(
  build[0],
  /out\.push\(\{ type: line\.type, text: line\.text, hunkIndex \}\)/,
  'the flat list must hold the line text, not coloured HTML',
);
assert.doesNotMatch(
  build[0],
  /highlightLine/,
  'colouring here pays for the whole file before anything is drawn',
);

// It must not be rebuilt when a grammar arrives, either: that would throw the
// list away — and the scroll position with it — for a change that only affects
// how the visible lines are drawn.
assert.match(
  diff,
  /\$: flatLines = selectedFile && shouldRender\s*\n\s*\? buildFlatLines\(selectedFile\)/,
  'buildFlatLines should not take the language',
);

// The virtual renderer colours its slice, which is the screenful it draws.
const slice = virtual.match(/\$: slice = lines\.slice\(first, last\)([\s\S]*?);\n/);
assert.ok(slice, 'the visible slice is missing');
assert.match(
  slice[0],
  /memoHighlightLine\(line\.text, language\)/,
  'the visible lines are where colouring belongs',
);
assert.match(virtual, /export let language/, 'the renderer needs the grammar to colour with');

/**
 * And it must be the memoising form.
 *
 * Colouring on render moves the cost into scrolling: every frame re-colours the
 * lines still on screen, and a line coming back into view is parsed again.
 */
for (const [name, source] of [['Diff', diff], ['VirtualLines', virtual]]) {
  const calls = source.match(/(?<!memo)\bhighlightLine\(/g) ?? [];
  assert.equal(
    calls.length,
    0,
    `${name} should call memoHighlightLine, not highlightLine (${calls.length} bare calls)`,
  );
}

// The cache is bounded: a diff view left open all day would otherwise hold
// every line of every file ever scrolled through.
assert.match(util, /const CACHE_LIMIT = \d+/, 'the cache must have a limit');
assert.match(
  util,
  /if \(language !== cacheLanguage\)/,
  'a different grammar makes every stored answer wrong, not stale',
);

console.log('diffHighlightLazy: ok');
