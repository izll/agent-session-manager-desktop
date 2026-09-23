import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const storeSrc = readFileSync(
  new URL('../src/lib/stores/sessions.ts', import.meta.url), 'utf8');
const treeSrc = readFileSync(
  new URL('../src/lib/components/Sidebar/SessionTree.svelte', import.meta.url), 'utf8');

// The comparator is a plain function the store calls, so it runs here as it is.
const { compareByActivity, RECENTLY_ACTIVE_MS } = await import('../src/lib/utils/activityOrder.ts');

// Well after every timestamp below, so none counts as working now unless a
// test says so.
const NOW = Date.parse('2026-10-01T00:00:00Z');
const timeOf = (s) => {
  const parsed = s.updatedAt ? Date.parse(s.updatedAt) : 0;
  return Number.isFinite(parsed) ? parsed : 0;
};
const comparator = (a, b) =>
  compareByActivity({ name: a.name, time: timeOf(a) }, { name: b.name, time: timeOf(b) }, NOW);

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

// The session list is reloaded only on events, so its updatedAt is a startup
// snapshot: a session could show a live activity dot while the ordering still
// placed it by a timestamp minutes or hours old. The sidebar poll supplies
// fresh times every tick and they have to win.
test('the live poll time takes precedence over the loaded one', () => {
  const at = storeSrc.indexOf('export const sessionsByActivity');
  const decl = storeSrc.slice(at, storeSrc.indexOf('\n);', at));

  assert.match(decl, /\[sessions, lastActive\]/,
    'the ordering does not depend on the live activity times, so it goes stale');

  const timeOfAt = storeSrc.indexOf('const timeOf =', at);
  const timeOf = storeSrc.slice(timeOfAt, storeSrc.indexOf('\n    };', timeOfAt));
  assert.match(timeOf, /\$lastActive\[s\.id\] \|\| s\.updatedAt/,
    'the loaded timestamp is not overridden by the live one');
});

// Sessions working at the same time swapped places whenever one of them got no
// busy reading in a tick: its time fell a second behind and it dropped below
// the others until the next tick, moving rows under the cursor while stepping.
test('sessions working now keep a stable order by name', () => {
  const now = Date.parse('2026-09-23T12:00:00Z');
  const entry = (name, secondsAgo) => ({ name, time: now - secondsAgo * 1000 });
  const tickA = [entry('beta', 0), entry('alpha', 2), entry('old', 3600)];
  const tickB = [entry('beta', 2), entry('alpha', 0), entry('old', 3600)];
  const order = (list) => [...list].sort((x, y) => compareByActivity(x, y, now)).map(e => e.name);
  assert.deepEqual(order(tickA), ['alpha', 'beta', 'old']);
  assert.deepEqual(order(tickB), ['alpha', 'beta', 'old'],
    'a session that missed one busy reading swapped places with the other');
});

test('a session that finished a while ago falls back to its place by time', () => {
  const now = Date.parse('2026-09-23T12:00:00Z');
  const recent = { name: 'zulu', time: now - 1000 };
  const earlier = { name: 'alpha', time: now - RECENTLY_ACTIVE_MS - 5000 };
  const older = { name: 'beta', time: now - 3 * RECENTLY_ACTIVE_MS };
  const sorted = [older, earlier, recent].sort((x, y) => compareByActivity(x, y, now));
  assert.deepEqual(sorted.map(e => e.name), ['zulu', 'alpha', 'beta']);
});
