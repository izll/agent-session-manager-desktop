import test from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { loadSidebarStores } from './sidebarStores.mjs';

const core = await import('../src/lib/utils/sidebarOrderCore.ts');
const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');

const s = (id) => ({ id });
const rows = (order) => order.map(e => `${e.section === 'favorites' ? '★' : ''}${e.id}`).join(' ');

// ---- The order, as plain functions ------------------------------------------

// Favourite F is also in the open group G1. The ungrouped favourite U-fav is,
// while the section is folded, back among the ungrouped sessions.
const groups = [{ id: 'G1', collapsed: false }];
const byGroup = new Map([['G1', [s('A'), s('F')]]]);

test('a folded favourites section has no rows to step onto', () => {
  const open = core.buildSidebarOrder(false, [], [s('F'), s('U-fav')], groups, byGroup, [s('U')], false);
  assert.equal(rows(open), '★F ★U-fav A F U');
  const folded = core.buildSidebarOrder(false, [], [s('F'), s('U-fav')], groups, byGroup, [s('U-fav'), s('U')], true);
  assert.equal(rows(folded), 'A F U-fav U');
});

test('the activity order has no favourites section to fold', () => {
  const flat = core.buildSidebarOrder(true, [s('X'), s('F')], [s('F')], groups, byGroup, [], true);
  assert.equal(rows(flat), 'X F');
});

test('a selection made in the favourites continues from its other copy once folded', () => {
  const folded = core.buildSidebarOrder(false, [], [s('F')], groups, byGroup, [s('U')], true);
  const active = core.resolveActiveEntry({ id: 'F', section: 'favorites' }, 'F', folded);
  assert.deepEqual(active, { id: 'F', section: 'list' });
  assert.deepEqual(core.stepEntry(folded, active, 1), { id: 'U', section: 'list' });
  assert.deepEqual(core.stepEntry(folded, active, -1), { id: 'A', section: 'list' });
});

// ---- The real stores --------------------------------------------------------

const st = await loadSidebarStores();
const session = (id, extra = {}) => ({
  id, name: id, notes: '', groupId: '', favorite: false, status: 'stopped', updatedAt: '', ...extra,
});

function setup(favoritesCollapsed, search = '') {
  st.settings.update(v => ({ ...v, sortByActivity: false, favoritesCollapsed }));
  st.groups.set([{ id: 'g', name: 'G', collapsed: false }]);
  st.sessions.set([
    session('plain'),
    session('fav-loose', { favorite: true }),
    session('fav-grouped', { favorite: true, groupId: 'g' }),
    session('grouped', { groupId: 'g' }),
  ]);
  st.searchFilter.set(search);
}
const order = () => rows(st.get(st.sidebarOrder));
const ids = (store) => st.get(store).map(x => x.id);

test('folding hides the favourites rows but never the sessions', () => {
  setup(false);
  assert.equal(order(), '★fav-loose ★fav-grouped fav-grouped grouped plain');
  assert.deepEqual(ids(st.ungroupedSessions), ['plain']);

  setup(true);
  // The ungrouped favourite comes back to the ungrouped list, in stored order.
  assert.equal(order(), 'fav-grouped grouped plain fav-loose');
  assert.deepEqual(ids(st.ungroupedSessions), ['plain', 'fav-loose']);
  // The section still knows its members: the header counts them.
  assert.deepEqual(ids(st.favorites), ['fav-loose', 'fav-grouped']);
});

test('a search keeps the section folded, and it still counts what matches', () => {
  setup(true, 'loose');
  assert.equal(order(), 'fav-loose');
  assert.deepEqual(ids(st.favorites), ['fav-loose']);
});

test('the header toggle saves the setting, and stepping follows it', async () => {
  setup(false);
  st.selectedSessionId.set('fav-loose');
  assert.deepEqual(st.get(st.activeSidebarEntry), { id: 'fav-loose', section: 'favorites' });

  await st.toggleFavoritesCollapsed();
  assert.equal(st.get(st.settings).favoritesCollapsed, true);
  assert.equal(st.get(st.favoritesCollapsed), true);
  // The copy that was selected went; its other copy stands for it.
  assert.deepEqual(st.get(st.activeSidebarEntry), { id: 'fav-loose', section: 'list' });

  await st.toggleFavoritesCollapsed();
  assert.equal(st.get(st.settings).favoritesCollapsed, false);
  assert.equal(order(), '★fav-loose ★fav-grouped fav-grouped grouped plain');
});

// ---- Wiring -----------------------------------------------------------------

test('the sidebar folds the section from its header', () => {
  const tree = read('../src/lib/components/Sidebar/SessionTree.svelte');
  assert.match(tree, /on:click=\{\(\) => void toggleFavoritesCollapsed\(\)\}/);
  assert.match(tree, /aria-expanded=\{!\$favoritesCollapsed\}/);
  assert.match(tree, /\{#if !\$favoritesCollapsed\}\s*\{#each \$favorites as session/,
    'the favourites rows are drawn while the section is folded');
  assert.match(tree, /class="favorites-count">\{\$favorites\.length\}/);

  const order = read('../src/lib/stores/sidebarOrder.ts');
  assert.match(order, /!!\$settings\?\.favoritesCollapsed/, 'stepping ignores the folded section');
});

test('the setting starts open and is carried to the backend', () => {
  const settings = read('../src/lib/stores/settings.ts');
  assert.match(settings, /favoritesCollapsed: boolean;/);
  assert.match(settings, /favoritesCollapsed: false,/);
  const models = read('../wailsjs/go/models.ts');
  assert.match(models, /this\.favoritesCollapsed = source\["favoritesCollapsed"\]/);
});

// The Ctrl+Shift+N slots are counted from every favourite, not from what the
// sidebar shows, so a folded section cannot take them away.
test('the favourite shortcuts do not depend on the section being open', () => {
  const app = read('../src/App.svelte');
  assert.match(app, /\$: favouriteTargets = \$sessions\.filter\(s => s\.favorite\);/);
  assert.doesNotMatch(app, /favoritesCollapsed/);
});

test('every language has the header tooltips', () => {
  const dir = new URL('../src/lib/i18n/locales/', import.meta.url);
  const files = readdirSync(dir).filter(f => f.endsWith('.json'));
  assert.equal(files.length, 20);
  for (const file of files) {
    const strings = JSON.parse(readFileSync(new URL(file, dir), 'utf8'));
    for (const key of ['sidebar.collapseFavorites', 'sidebar.expandFavorites']) {
      assert.ok(strings[key], `${file} has no ${key}`);
    }
  }
});
