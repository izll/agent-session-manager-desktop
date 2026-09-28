import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { build } from 'esbuild';

// The real stores, bundled with only the backend stubbed out: the bug lived in
// which derived list the search reached, so the derivations have to run.
const root = new URL('../', import.meta.url);
const rootDir = fileURLToPath(root);
const treeSrc = readFileSync(new URL('src/lib/components/Sidebar/SessionTree.svelte', root), 'utf8');

const bindings = readFileSync(new URL('wailsjs/go/main/App.d.ts', root), 'utf8');
const appMethods = [...new Set([...bindings.matchAll(/export function ([A-Za-z0-9_]+)/g)].map(m => m[1]))];

const result = await build({
  stdin: {
    contents: `
      export { get } from 'svelte/store';
      export * from './src/lib/stores/sidebarOrder.ts';
      export { sessions, groups, searchFilter, sessionsByActivity, selectedSessionId } from './src/lib/stores/sessions.ts';
      export { settings } from './src/lib/stores/settings.ts';
      export { lastActive } from './src/lib/stores/statusLines.ts';
    `,
    resolveDir: rootDir,
    loader: 'ts',
  },
  bundle: true,
  write: false,
  format: 'esm',
  platform: 'node',
  plugins: [{
    name: 'sidebar-backend-stub',
    setup(api) {
      api.onResolve({ filter: /wailsjs\/go\/main\/App$/ }, () => ({ path: 'app', namespace: 'stub' }));
      api.onResolve({ filter: /utils\/terminal$/ }, () => ({ path: 'terminal', namespace: 'stub' }));
      api.onLoad({ filter: /^app$/, namespace: 'stub' }, () => ({
        contents: appMethods.map(n => `export const ${n} = async () => undefined;`).join('\n'),
        loader: 'js',
      }));
      api.onLoad({ filter: /^terminal$/, namespace: 'stub' }, () => ({
        contents: `export const defaultTerminalRenderer = () => 'dom';`,
        loader: 'js',
      }));
    },
  }],
});
const s = await import(
  `data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`);

const session = (id, name, extra = {}) => ({
  id, name, notes: '', groupId: '', favorite: false, status: 'stopped', updatedAt: '', ...extra,
});

function setup(sortByActivity) {
  s.settings.update(v => ({ ...v, sortByActivity }));
  s.groups.set([{ id: 'g', name: 'G', collapsed: false }]);
  s.sessions.set([
    session('a', 'alpha api', { updatedAt: '2026-09-01T10:00:00Z' }),
    session('b', 'beta', { updatedAt: '2026-09-03T10:00:00Z', notes: 'touches the API' }),
    session('c', 'gamma', { updatedAt: '2026-09-02T10:00:00Z', groupId: 'g' }),
    session('d', 'delta', { updatedAt: '2026-09-04T10:00:00Z', favorite: true }),
  ]);
  s.searchFilter.set('');
}

// The reported bug: with the list sorted by activity the search box filtered
// nothing, because the sorted list was the one list the search never reached.
test('the search filters the activity-sorted list', () => {
  setup(true);
  s.searchFilter.set('api');
  assert.deepEqual(s.get(s.sessionsByActivity).map(x => x.id), ['b', 'a'],
    'the sorted list ignores the search');
});

test('the search matches the notes in the sorted list too', () => {
  setup(true);
  s.searchFilter.set('TOUCHES');
  assert.deepEqual(s.get(s.sessionsByActivity).map(x => x.id), ['b']);
});

// Stepping follows what is on screen, so it must skip what the search hid.
test('stepping in the sorted list walks only the matches', () => {
  setup(true);
  s.searchFilter.set('api');
  assert.deepEqual(s.get(s.sidebarOrder).map(e => e.id), ['b', 'a']);
  s.selectedSessionId.set('b');
  s.selectNextSession();
  assert.equal(s.get(s.selectedSessionId), 'a');
  s.selectNextSession();
  assert.equal(s.get(s.selectedSessionId), 'a', 'stepped onto a session the search hid');
});

test('the search still filters the ordinary list', () => {
  setup(false);
  s.searchFilter.set('a');
  // Favourite delta first, then group g's gamma, then ungrouped alpha, beta.
  assert.deepEqual(s.get(s.sidebarOrder).map(e => e.id), ['d', 'c', 'a', 'b']);
  s.searchFilter.set('api');
  assert.deepEqual(s.get(s.sidebarOrder).map(e => e.id), ['a', 'b']);
});

test('the sorted list says so when nothing matches', () => {
  const at = treeSrc.indexOf('{#if $settings?.sortByActivity}');
  const branch = treeSrc.slice(at, treeSrc.indexOf('{:else}', at));
  assert.match(branch, /no-matches/, 'an empty sorted list gives no hint that the search emptied it');
});
