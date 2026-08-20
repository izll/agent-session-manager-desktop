import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * Returning to the diff tab should not blink through an empty view.
 *
 * The component is unmounted on a tab switch — its two mount points are in
 * exclusive branches — so the field recording what is already loaded starts
 * empty every time. The list was therefore cleared and refetched on the way
 * back, which reads as the whole review restarting: the files blink away, the
 * selection goes, and the pane lands at the top before the new data arrives.
 *
 * The cache lives in diffViewState because that module outlives the component;
 * anything held inside the component cannot, which is the whole problem.
 */
const state = readFileSync(
  new URL('../src/lib/utils/diffViewState.ts', import.meta.url),
  'utf8',
);

assert.match(state, /export function cacheDiff/, 'the diff has to be cached outside the component');
assert.match(state, /export function cachedDiff/, 'and read back');
assert.match(state, /export function invalidateDiffCache/, 'and dropped when it stops being true');

// Keyed on session and mode together: the same session in another mode is a
// different diff, and showing one for the other is wrong rather than stale.
assert.match(
  state,
  /diffCache\.key !== key/,
  'a cache hit must require the same key, not merely any cached value',
);

// And it has to expire. A diff more than a few seconds old may not describe the
// working tree any more, and nothing on screen would say so.
assert.match(state, /DIFF_CACHE_TTL_MS/, 'the cache needs a lifetime');
assert.match(
  state,
  /now - diffCache\.at > DIFF_CACHE_TTL_MS/,
  'an entry past its lifetime must miss',
);

const diff = readFileSync(
  new URL('../src/lib/components/MainPanel/Diff.svelte', import.meta.url),
  'utf8',
);

// A revert rewrites the tree, so the cached list is not stale but wrong.
const revert = diff.match(/async function confirmRevert[\s\S]*?\n  \}/);
assert.ok(revert, 'confirmRevert is missing');
assert.match(
  revert[0],
  /invalidateDiffCache\(\)/,
  'a revert must drop the cache, or the reverted file shows as still changed',
);

// The load has to write the cache too, or nothing is ever there to return to.
assert.match(diff, /cacheDiff\(rootCacheKey/, 'a completed load should populate the root-bound cache');
assert.match(diff, /rootCacheKey = `\$\{requestedKey\}\\x1f\$\{root\}`/,
  'two repositories reached from the same tab must never share cached diff data');

console.log('diffCache: ok');
