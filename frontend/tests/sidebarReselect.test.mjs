import test from 'node:test';
import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';
import { build } from 'esbuild';

// The sidebar's click and step, run for real against stand-in stores: what
// matters is which of selectSession and showSessionView each one reaches.
const root = new URL('../', import.meta.url);

const mocks = {
  sessions: `
    import { writable } from 'svelte/store';
    export const selectedSessionId = writable(null);
    export const favorites = writable([{ id: 'F' }]);
    export const groups = writable([{ id: 'G', collapsed: false }]);
    export const sessionsByGroup = writable(new Map([['G', [{ id: 'F' }, { id: 'A' }]]]));
    export const ungroupedSessions = writable([]);
    export const sessionsByActivity = writable([]);
    export function selectSession(id) {
      globalThis.__calls.push('select:' + id);
      selectedSessionId.set(id);
      globalThis.__view = 'session';
    }
  `,
  settings: `
    import { writable } from 'svelte/store';
    export const settings = writable({ sortByActivity: false });
  `,
  navigation: `
    export function showSessionView() {
      globalThis.__calls.push('show');
      globalThis.__view = 'session';
    }
  `,
};

const result = await build({
  entryPoints: [fileURLToPath(new URL('src/lib/stores/sidebarOrder.ts', root))],
  bundle: true,
  write: false,
  format: 'esm',
  platform: 'node',
  plugins: [{
    name: 'sidebar-order-mocks',
    setup(api) {
      api.onResolve({ filter: /^\.\/(sessions|settings|navigation)$/ }, (args) =>
        ({ path: args.path.slice(2), namespace: 'mock' }));
      api.onLoad({ filter: /.*/, namespace: 'mock' }, (args) => ({
        contents: mocks[args.path],
        loader: 'js',
        resolveDir: fileURLToPath(root),
      }));
    },
  }],
});

globalThis.__calls = [];
const order = await import(
  `data:text/javascript;base64,${Buffer.from(result.outputFiles[0].text).toString('base64')}`);

function reset(view, selected) {
  globalThis.__calls = [];
  globalThis.__view = view;
  if (selected !== undefined) order.selectSidebarEntry(selected, 'list');
  globalThis.__calls = [];
  globalThis.__view = view;
}

// The reported bug: from the dashboard, clicking the session that was already
// selected did nothing, because the click was skipped as "no change" and the
// switch to the session view went with it.
test('clicking the selected session leaves the dashboard', () => {
  reset('dashboard', 'A');
  order.selectSidebarEntry('A', 'list');
  assert.equal(globalThis.__view, 'session', 'the dashboard stays up over a click on its session');
  assert.deepEqual(globalThis.__calls, ['show'],
    'the selection itself is re-made, rewriting the stored session and tab for nothing');
});

test('clicking another session still selects it', () => {
  reset('tasks', 'A');
  order.selectSidebarEntry('F', 'favorites');
  assert.deepEqual(globalThis.__calls, ['select:F']);
});

// Both copies of a favourite are the same session, and here they sit next to
// each other: ★F, then F in its group, then A. Stepping from one copy to the
// other moves the cursor, so the next step continues from there — and does
// nothing else: no reselection, and no leaving the dashboard.
test('stepping between the two copies of a favourite only moves the cursor', () => {
  reset('dashboard', 'F');
  order.selectSidebarEntry('F', 'favorites');
  globalThis.__calls = [];
  globalThis.__view = 'dashboard';

  order.selectNextSession(); // ★F -> F
  assert.deepEqual(globalThis.__calls, [], 'a step onto the same session re-selected or switched view');
  assert.equal(globalThis.__view, 'dashboard');

  order.selectNextSession(); // F -> A, only if the cursor did move
  assert.deepEqual(globalThis.__calls, ['select:A']);
});
