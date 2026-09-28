import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { loadSidebarStores } from './sidebarStores.mjs';

const {
  matchesActivityFilter,
  activityFilterFrom,
  isActivityFilterOn,
  activityTime,
  ACTIVE_WITHIN_CHOICES,
} = await import('../src/lib/utils/sessionFilter.ts');

const treeSrc = readFileSync(
  new URL('../src/lib/components/Sidebar/SessionTree.svelte', import.meta.url), 'utf8');
const en = JSON.parse(readFileSync(
  new URL('../src/lib/i18n/locales/en.json', import.meta.url), 'utf8'));

const HOUR = 60 * 60 * 1000;
const DAY = 24 * HOUR;
const NOW = Date.parse('2026-09-28T12:00:00Z');
const running = { status: 'running' };
const stopped = { status: 'stopped' };
const paused = { status: 'paused' };
const f = (hideInactive, activeWithinDays) => ({ hideInactive, activeWithinDays });

// ---- The predicate, at a fixed "now" ----------------------------------------

test('no filter shows everything, stamped or not', () => {
  for (const s of [running, stopped, paused]) {
    assert.equal(matchesActivityFilter(s, 0, f(false, 0), NOW), true);
    assert.equal(matchesActivityFilter(s, NOW - 400 * DAY, f(false, 0), NOW), true);
  }
});

test('hide inactive hides everything that is not running', () => {
  assert.equal(matchesActivityFilter(running, 0, f(true, 0), NOW), true);
  assert.equal(matchesActivityFilter(stopped, NOW, f(true, 0), NOW), false);
  assert.equal(matchesActivityFilter(paused, NOW, f(true, 0), NOW), false);
});

test('each time window keeps what was active inside it, edge included', () => {
  for (const days of [1, 7, 30]) {
    const within = f(false, days);
    assert.equal(matchesActivityFilter(stopped, NOW - days * DAY, within, NOW), true, `${days}d: the edge`);
    assert.equal(matchesActivityFilter(stopped, NOW - days * DAY - 1, within, NOW), false, `${days}d: just past`);
    assert.equal(matchesActivityFilter(stopped, NOW - HOUR, within, NOW), true, `${days}d: an hour ago`);
  }
  assert.equal(matchesActivityFilter(stopped, NOW - 3 * DAY, f(false, 1), NOW), false);
  assert.equal(matchesActivityFilter(stopped, NOW - 3 * DAY, f(false, 7), NOW), true);
  assert.equal(matchesActivityFilter(stopped, NOW - 10 * DAY, f(false, 7), NOW), false);
  assert.equal(matchesActivityFilter(stopped, NOW - 10 * DAY, f(false, 30), NOW), true);
});

test('a session with no recorded activity is never inside a window', () => {
  assert.equal(matchesActivityFilter(running, 0, f(false, 30), NOW), false);
});

// The window goes by activity, not by being up: a session left running and
// idle for weeks was not active this week. Otherwise "hide inactive" plus a
// window would be the same as "hide inactive" alone.
test('running alone does not put a session inside a window', () => {
  assert.equal(matchesActivityFilter(running, NOW - 20 * DAY, f(false, 7), NOW), false);
});

test('the two halves combine: running AND active in the window', () => {
  const both = f(true, 7);
  assert.equal(matchesActivityFilter(running, NOW - 2 * DAY, both, NOW), true);
  assert.equal(matchesActivityFilter(running, NOW - 20 * DAY, both, NOW), false, 'running but idle for 20 days');
  assert.equal(matchesActivityFilter(stopped, NOW - 2 * DAY, both, NOW), false, 'active this week but stopped');
});

test('the settings are read with unknown windows dropped', () => {
  assert.deepEqual(activityFilterFrom(undefined), f(false, 0));
  assert.deepEqual(activityFilterFrom({ sidebarHideInactive: true, sidebarActiveWithinDays: 7 }), f(true, 7));
  assert.deepEqual(activityFilterFrom({ sidebarActiveWithinDays: 3 }), f(false, 0));
  assert.equal(isActivityFilterOn(f(false, 0)), false);
  assert.equal(isActivityFilterOn(f(true, 0)), true);
  assert.equal(isActivityFilterOn(f(false, 1)), true);
  assert.deepEqual([...ACTIVE_WITHIN_CHOICES], [0, 1, 7, 30]);
});

test('an empty or broken stamp is no activity', () => {
  assert.equal(activityTime(''), 0);
  assert.equal(activityTime(undefined), 0);
  assert.equal(activityTime('not a date'), 0);
  assert.equal(activityTime('2026-09-28T12:00:00Z'), NOW);
});

// ---- The live stores ----------------------------------------------------------

const s = await loadSidebarStores();
const ago = (ms) => new Date(Date.now() - ms).toISOString();

const session = (id, name, extra = {}) => ({
  id, name, notes: '', groupId: '', favorite: false, status: 'stopped', updatedAt: '', ...extra,
});

// run: running now, 2 hours ago        | fav: favourite, stopped, 3 days ago
// old: running, idle for 20 days       | grp: in group g, stopped, 5 days ago
// wk:  stopped, 6 days ago             | never: stopped, no activity recorded
function setup({ sortByActivity = false, hideInactive = false, days = 0, search = '' } = {}) {
  s.settings.update(v => ({
    ...v, sortByActivity, sidebarHideInactive: hideInactive, sidebarActiveWithinDays: days,
  }));
  s.lastActive.set({});
  s.groups.set([{ id: 'g', name: 'G', collapsed: false }, { id: 'h', name: 'H', collapsed: false }]);
  s.sessions.set([
    session('run', 'run api', { status: 'running', updatedAt: ago(2 * HOUR) }),
    session('old', 'old api', { status: 'running', updatedAt: ago(20 * DAY) }),
    session('wk', 'weekly', { updatedAt: ago(6 * DAY) }),
    session('fav', 'fav api', { favorite: true, updatedAt: ago(3 * DAY) }),
    session('grp', 'grouped', { groupId: 'g', updatedAt: ago(5 * DAY) }),
    session('hh', 'h-only', { groupId: 'h', status: 'running', updatedAt: ago(40 * DAY) }),
    session('never', 'never'),
  ]);
  s.searchFilter.set(search);
}
const order = () => s.get(s.sidebarOrder).map(e => e.id);

test('no filter: the list is unchanged in both sort modes', () => {
  setup();
  assert.deepEqual(order(), ['fav', 'grp', 'hh', 'run', 'old', 'wk', 'never']);
  setup({ sortByActivity: true });
  assert.deepEqual(order(), ['run', 'fav', 'grp', 'wk', 'old', 'hh', 'never']);
});

test('hide inactive, in both sort modes', () => {
  setup({ hideInactive: true });
  assert.deepEqual(order(), ['hh', 'run', 'old']);
  setup({ hideInactive: true, sortByActivity: true });
  assert.deepEqual(order(), ['run', 'old', 'hh']);
});

test('a time window, in both sort modes', () => {
  setup({ days: 1 });
  assert.deepEqual(order(), ['run']);
  setup({ days: 7 });
  assert.deepEqual(order(), ['fav', 'grp', 'run', 'wk']);
  setup({ days: 7, sortByActivity: true });
  assert.deepEqual(order(), ['run', 'fav', 'grp', 'wk']);
  setup({ days: 30, sortByActivity: true });
  assert.deepEqual(order(), ['run', 'fav', 'grp', 'wk', 'old']);
});

test('hide inactive and a window together', () => {
  setup({ hideInactive: true, days: 7 });
  assert.deepEqual(order(), ['run']);
  setup({ hideInactive: true, days: 30, sortByActivity: true });
  assert.deepEqual(order(), ['run', 'old']);
});

test('the filter and the search combine, in both sort modes', () => {
  setup({ days: 7, search: 'api' });
  assert.deepEqual(order(), ['fav', 'run']);
  setup({ days: 7, search: 'api', sortByActivity: true });
  assert.deepEqual(order(), ['run', 'fav']);
  setup({ hideInactive: true, search: 'api', sortByActivity: true });
  assert.deepEqual(order(), ['run', 'old']);
});

// The loaded updatedAt is a snapshot; the poll's time is what is current.
test('the live activity time decides the window', () => {
  setup({ days: 1 });
  assert.ok(!order().includes('old'));
  s.lastActive.set({ old: ago(HOUR) });
  assert.deepEqual(order(), ['run', 'old']);
});

test('stepping walks only the sessions the filter left', () => {
  setup({ hideInactive: true, days: 30, sortByActivity: true });
  s.selectedSessionId.set('run');
  s.selectNextSession();
  assert.equal(s.get(s.selectedSessionId), 'old');
  s.selectNextSession();
  assert.equal(s.get(s.selectedSessionId), 'old', 'stepped onto a session the filter hid');
  s.selectPrevSession();
  assert.equal(s.get(s.selectedSessionId), 'run');
});

test('the filter counts as filtering, for hiding empty groups', () => {
  setup();
  assert.equal(s.get(s.sidebarFiltered), false);
  setup({ days: 7 });
  assert.equal(s.get(s.sidebarFiltered), true);
  assert.deepEqual(s.get(s.sessionsByGroup).get('h'), [], 'group h should have nothing left to show');
  setup({ hideInactive: true });
  assert.equal(s.get(s.sidebarFiltered), true);
  setup({ search: '  ' });
  assert.equal(s.get(s.sidebarFiltered), false, 'blank search is not a filter');
});

// What the menu writes is what the list reads: each half saved on its own
// leaves the other as it was.
test('a saved half takes effect and keeps the other', async () => {
  setup();
  await s.saveSettings({ sidebarActiveWithinDays: 7 });
  await s.saveSettings({ sidebarHideInactive: true });
  assert.deepEqual(s.get(s.sidebarActivityFilter), f(true, 7));
  assert.deepEqual(order(), ['run']);
  await s.saveSettings({ sidebarActiveWithinDays: 0 });
  assert.deepEqual(s.get(s.sidebarActivityFilter), f(true, 0));
});

// ---- Wiring --------------------------------------------------------------------

test('the filter button sits beside the sort toggle and opens a menu', () => {
  const sort = treeSrc.indexOf('class="sort-toggle"');
  const filter = treeSrc.indexOf('class="sort-toggle filter-toggle"');
  assert.ok(sort > 0 && filter > sort, 'no filter button after the sort toggle');
  assert.match(treeSrc, /claimMenu\(closeFilterMenu\)/, 'the menu does not take the single menu slot');
  assert.match(treeSrc, /releaseMenu\(closeFilterMenu\)/);
  assert.match(treeSrc, /use:portal/, 'the menu is not moved out of the sidebar layer');
});

test('the button shows the filter is on and names it', () => {
  assert.match(treeSrc, /class:active=\{activityFilterOn\}/);
  assert.match(treeSrc, /filter-dot/);
  assert.match(treeSrc, /title=\{filterButtonTitle\}/);
  assert.match(treeSrc, /filterTitle\(\$sidebarActivityFilter, \$t\)/,
    'the tooltip does not follow the filter or the language');
});

test('both halves are saved in the settings', () => {
  assert.match(treeSrc, /saveSettings\(\{ sidebarHideInactive: !\$sidebarActivityFilter\.hideInactive \}\)/);
  assert.match(treeSrc, /saveSettings\(\{ sidebarActiveWithinDays: days \}\)/);
});

test('groups hide when the filter empties them, not only the search', () => {
  assert.match(treeSrc, /\{#if !\$sidebarFiltered \|\| groupSessions\.length > 0\}/);
  assert.ok(!treeSrc.includes('{#if !$searchFilter.trim() || groupSessions.length > 0}'));
});

test('an emptied list says so and offers to clear, in both sort modes', () => {
  const branches = treeSrc.split('{#if $settings?.sortByActivity}')[1];
  const [sorted, grouped] = [branches.slice(0, branches.indexOf('{:else}')), branches.slice(branches.indexOf('{:else}'))];
  for (const [name, src] of [['sorted', sorted], ['grouped', grouped]]) {
    assert.match(src, /sidebar\.filterNoMatches/, `${name}: no filtered-empty line`);
    assert.match(src, /on:click=\{clearFilters\}/, `${name}: no way to clear the filter`);
  }
});

test('an empty filtered list is not mistaken for an empty project', () => {
  assert.match(treeSrc, /\{:else if \$sessions\.length === 0 && \$groups\.length === 0\}/,
    'the "no sessions yet" state is decided from the filtered lists');
});

test('every new string exists in English', () => {
  for (const key of ['filter', 'filterActive', 'filterHideInactive', 'filterLastActivity', 'filterAnyTime',
    'filterDay', 'filterWeek', 'filterMonth', 'filterClear', 'filterNoMatches']) {
    assert.ok(en[`sidebar.${key}`], `sidebar.${key} is missing`);
  }
  assert.match(en['sidebar.filterActive'], /\{filters\}/);
});
