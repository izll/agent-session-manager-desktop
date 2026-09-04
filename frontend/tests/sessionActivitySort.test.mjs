import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const storeSrc = readFileSync(
  new URL('../src/lib/stores/sessions.ts', import.meta.url), 'utf8');
const treeSrc = readFileSync(
  new URL('../src/lib/components/Sidebar/SessionTree.svelte', import.meta.url), 'utf8');

// The comparator, lifted out of the store so it can be run here. The store
// itself pulls in Svelte and the Wails bindings, neither of which exists under
// plain node.
const comparator = (() => {
  const at = storeSrc.indexOf('export const sessionsByActivity');
  assert.ok(at > 0, 'sessionsByActivity is gone');
  const from = storeSrc.indexOf('.sort((a, b) => {', at);
  assert.ok(from > 0, 'the sort call is gone');
  const open = from + '.sort('.length;
  // Walk to the arrow function's matching brace rather than guessing at a
  // closing token: the body contains both braces and parentheses.
  let depth = 0, end = -1;
  for (let i = storeSrc.indexOf('{', open); i < storeSrc.length; i++) {
    if (storeSrc[i] === '{') depth++;
    else if (storeSrc[i] === '}' && --depth === 0) { end = i + 1; break; }
  }
  assert.ok(end > 0, 'the comparator body is unbalanced');
  return eval(`(${storeSrc.slice(open, end)})`);
})();

const at = (name, updatedAt, extra = {}) => ({ name, updatedAt, ...extra });

test('most recent activity comes first', () => {
  const sorted = [
    at('old', '2026-09-01T10:00:00Z'),
    at('newest', '2026-09-03T14:00:00Z'),
    at('middle', '2026-09-02T09:30:00Z'),
  ].sort(comparator);
  assert.deepEqual(sorted.map(s => s.name), ['newest', 'middle', 'old']);
});

// An empty timestamp is "never ran", not "ran at the epoch". Parsed as a date
// it would be NaN, and a NaN comparison silently leaves the array in whatever
// order it started in.
test('sessions with no recorded activity sort last, not first', () => {
  const sorted = [
    at('never', ''),
    at('recent', '2026-09-03T14:00:00Z'),
    at('missing', undefined),
    at('older', '2026-09-01T10:00:00Z'),
  ].sort(comparator);
  // The two that never ran keep the tie-break among themselves: by name.
  assert.deepEqual(sorted.map(s => s.name), ['recent', 'older', 'missing', 'never']);
});

// Ties are what an unstable order shows up as: two sessions touched in the same
// second must not swap places between repaints.
test('equal timestamps fall back to the name', () => {
  const same = '2026-09-03T14:00:00Z';
  const sorted = [at('zulu', same), at('alpha', same), at('mike', same)].sort(comparator);
  assert.deepEqual(sorted.map(s => s.name), ['alpha', 'mike', 'zulu']);
});

test('the source list is not sorted in place', () => {
  const at2 = storeSrc.indexOf('export const sessionsByActivity');
  const body = storeSrc.slice(at2, storeSrc.indexOf('\n);', at2));
  assert.ok(body.includes('[...$sessions]'),
    'sorting $sessions directly mutates the store array every derivation');
});

// The activity view answers "where was I". A session pinned to the top for
// being important is not an answer to that, so it must not be lifted out.
test('the activity view is one flat list with no favourites section', () => {
  const at3 = treeSrc.indexOf('{#if $settings?.sortByActivity}');
  assert.ok(at3 > 0, 'the activity branch is gone');
  const branch = treeSrc.slice(at3, treeSrc.indexOf('{:else}', at3));
  assert.ok(branch.includes('$sessionsByActivity'), 'the branch does not use the sorted list');
  assert.ok(!branch.includes('$favorites'), 'favourites are separated out in the activity view');
  assert.ok(!branch.includes('$groups'), 'groups are still rendered in the activity view');
});

test('the toggle persists through settings rather than local state', () => {
  assert.ok(treeSrc.includes('saveSettings({ sortByActivity:'),
    'the toggle does not write the setting, so it is lost on restart');
});
