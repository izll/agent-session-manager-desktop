import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8').replace(/\r\n/g, '\n');

// The session list drew its per-tab rows in the order the tabs were created,
// while the tab bar shows the order they were dragged into; a reordered tab
// sat in a different place in each.

test('tabs follow the saved order, and ones it does not name come last', async () => {
  const { sortByTabOrder } = await import('../src/lib/utils/tabOrder.ts');
  const tabs = [{ windowIdx: 0 }, { windowIdx: 1 }, { windowIdx: 2 }, { windowIdx: 3 }];
  const order = (list) => list.map((tab) => tab.windowIdx);

  assert.deepEqual(order(sortByTabOrder(tabs, [2, 0, 1], (tab) => tab.windowIdx)), [2, 0, 1, 3]);
  assert.deepEqual(order(sortByTabOrder(tabs, [], (tab) => tab.windowIdx)), [0, 1, 2, 3]);
  assert.deepEqual(order(sortByTabOrder(tabs, undefined, (tab) => tab.windowIdx)), [0, 1, 2, 3]);
  assert.deepEqual(order(tabs), [0, 1, 2, 3], 'the input is not reordered in place');
});

test('the session list and the tab bar sort tabs the same way', () => {
  const item = read('../src/lib/components/Sidebar/SessionItem.svelte');
  assert.match(item, /sortByTabOrder\(tabStatuses, session\.tabOrder/,
    'the session list does not sort its tab rows by the tab order');
  assert.match(item, /\{#each orderedTabStatuses as tab\}/,
    'the per-tab rows are drawn from the unsorted list');

  const bar = read('../src/lib/components/MainPanel/TabBar.svelte');
  assert.match(bar, /sortByTabOrder\(/, 'the tab bar sorts with its own copy');
});
