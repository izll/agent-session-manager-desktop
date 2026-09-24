import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  MAIN_TAB_ID,
  sessionTabs,
  resolveTaskTab,
  tabIdAtWindow,
  filterTasksByTab,
  parseTaskTabFilter,
} from '../src/lib/utils/taskTabs.ts';

const session = {
  id: 's1',
  name: 'api',
  mainWindowIndex: 0,
  followedWindows: [
    { id: 'codex-id', index: 4, name: 'Codex' },
    { id: 'shell-id', index: 7, name: '' },
    { index: 9, name: 'no id yet' },
  ],
};
const tabs = sessionTabs(session);

const tasks = [
  { id: '1', sessionId: 's1', tabId: 'codex-id' },
  { id: '2', sessionId: 's1', tabId: MAIN_TAB_ID },
  { id: '3', sessionId: 's1' },
  { id: '4', sessionId: 's1', tabId: 'closed-tab' },
  { id: '5', sessionId: 's2', tabId: MAIN_TAB_ID },
  { id: '6' },
];
const ids = list => list.map(task => task.id);

test('the main tab comes first and followed tabs are named like the tab bar names them', () => {
  assert.deepEqual(tabs, [
    { id: MAIN_TAB_ID, name: 'api', windowIdx: 0 },
    { id: 'codex-id', name: 'Codex', windowIdx: 4 },
    { id: 'shell-id', name: 'Tab 7', windowIdx: 7 },
  ]);
});

test('"All" shows every task', () => {
  assert.deepEqual(ids(filterTasksByTab(tasks, 'all', 'codex-id', 's1', tabs)), ids(tasks));
});

test('"This tab" shows only the tasks assigned to the selected tab', () => {
  assert.deepEqual(ids(filterTasksByTab(tasks, 'tab', 'codex-id', 's1', tabs)), ['1']);
});

// Every session has a "main"; another session's main tab is not this one.
test('another session\'s task on its main tab is not this session\'s main tab task', () => {
  assert.deepEqual(ids(filterTasksByTab(tasks, 'tab', MAIN_TAB_ID, 's1', tabs)), ['2']);
  assert.equal(resolveTaskTab(tasks[4], 's1', tabs), null);
});

test('a task whose tab is gone is unassigned, not lost', () => {
  assert.equal(resolveTaskTab(tasks[3], 's1', tabs), null);
  assert.ok(ids(filterTasksByTab(tasks, 'all', null, 's1', tabs)).includes('4'));
});

test('a window the app does not track has no tasks of its own', () => {
  assert.equal(tabIdAtWindow(tabs, 9), null);
  assert.deepEqual(filterTasksByTab(tasks, 'tab', tabIdAtWindow(tabs, 9), 's1', tabs), []);
});

test('the selected window index maps to the tab\'s stable ID', () => {
  assert.equal(tabIdAtWindow(tabs, 0), MAIN_TAB_ID);
  assert.equal(tabIdAtWindow(tabs, 7), 'shell-id');
});

test('a stored filter that is not "tab" reads as "all"', () => {
  assert.equal(parseTaskTabFilter('tab'), 'tab');
  for (const raw of [null, undefined, '', 'all', 'This tab', 3]) {
    assert.equal(parseTaskTabFilter(raw), 'all');
  }
});

// The [All | This tab] switch marks which half has unfinished tasks, as the
// notes switch marks which note has text.
test('the task switch marks which half has unfinished tasks', async () => {
  const { openTaskPresence } = await import('../src/lib/utils/taskTabs.ts');
  const tabs = [{ id: 'main', name: 'Main' }, { id: 't1', name: 'Codex' }];
  const task = (status, tabId) => ({ status, sessionId: 's', tabId });

  assert.deepEqual(openTaskPresence([task('done', 't1')], 't1', 's', tabs), { all: false, tab: false },
    'finished tasks mark a half as having work in it');
  assert.deepEqual(openTaskPresence([task('backlog', 'main')], 't1', 's', tabs), { all: true, tab: false });
  assert.deepEqual(openTaskPresence([task('deferred', 't1')], 't1', 's', tabs), { all: true, tab: true },
    'deferred is unfinished, as the backend counts it');
});
