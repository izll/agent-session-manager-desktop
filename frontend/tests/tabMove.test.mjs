import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// Moving tabs between sessions: the rules the UI decides with, and the wiring
// that connects the tab bar, the sidebar rows and the backend.

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const tabBar = read('../src/lib/components/MainPanel/TabBar.svelte');
const sessionItem = read('../src/lib/components/Sidebar/SessionItem.svelte');
const sessionsStore = read('../src/lib/stores/sessions.ts');
const dialog = read('../src/lib/components/Dialogs/MoveTabDialog.svelte');

const {
  TAB_DRAG_MIME, isMovableTab, encodeTabDrag, decodeTabDrag, tabDropState, moveTargets,
} = await import('../src/lib/utils/tabMoveRules.ts');

const body = (text, start, length = 1500) => {
  const at = text.indexOf(start);
  assert.ok(at >= 0, `${start} not found`);
  return text.slice(at, at + length);
};

test('only a tab of the session, never its own window, can be moved', () => {
  const session = { followedWindows: [{ index: 3 }, { index: 7 }] };
  assert.equal(isMovableTab(session, 3), true);
  assert.equal(isMovableTab(session, 7), true);
  // The own window is the one that is not a stored tab, whatever its index:
  // with base-index 1 it is 1, and 0 is no tab at all.
  assert.equal(isMovableTab(session, 0), false);
  assert.equal(isMovableTab(session, 1), false);
  assert.equal(isMovableTab({ followedWindows: null }, 0), false);
  assert.equal(isMovableTab(null, 3), false);
  assert.equal(isMovableTab(session, null), false);
});

test('a tab drag survives the trip through the drag data, and junk is refused', () => {
  const payload = { sessionId: 's1', windowIdx: 4, projectId: 'p', name: 'build' };
  assert.deepEqual(decodeTabDrag(encodeTabDrag(payload)), payload);
  assert.equal(decodeTabDrag('2'), null, 'the tab bar\'s own reorder data is not a tab move');
  assert.equal(decodeTabDrag('{"id":"s1","index":0}'), null, 'a session row being reordered is not one either');
  assert.equal(decodeTabDrag('not json'), null);
});

test('a session row answers a dragged tab by what the backend said', () => {
  const drag = { sessionId: 'src', windowIdx: 2, projectId: 'p', name: 't',
    refusals: { ok: '', remote: 'error.tabMoveLocalTabToServer' } };
  assert.equal(tabDropState(drag, 'ok', 'p'), 'ok');
  assert.equal(tabDropState(drag, 'remote', 'p'), 'refused');
  assert.equal(tabDropState(drag, 'src', 'p'), 'none', 'its own session is no target');
  assert.equal(tabDropState(drag, 'ok', 'other-project'), 'none');
  assert.equal(tabDropState(null, 'ok', 'p'), 'none');
  // Still being asked: accepted, the move checks again.
  assert.equal(tabDropState({ ...drag, refusals: null }, 'remote', 'p'), 'ok');
});

test('the picker lists every other session, narrowed by name or path', () => {
  const all = [
    { id: 'a', name: 'Alpha', path: '/work/api' },
    { id: 'b', name: 'Beta', path: '/work/web' },
    { id: 'c', name: 'Gamma', path: '/srv/api' },
  ];
  assert.deepEqual(moveTargets(all, 'a', '').map(s => s.id), ['b', 'c']);
  assert.deepEqual(moveTargets(all, 'a', 'API').map(s => s.id), ['c']);
  assert.deepEqual(moveTargets(all, 'b', 'gam').map(s => s.id), ['c']);
});

test('the tab bar offers a tab to the sidebar only when it can leave its session', () => {
  const start = body(tabBar, 'function handleTabDragStart');
  assert.match(start, /isMovableTab\(sess, win\.Index\)[\s\S]*beginTabDrag\(/,
    'the drag must be marked movable only for a tab that is not the session itself');
  assert.match(body(tabBar, 'function handleTabDragEnd', 300), /endTabDrag\(\)/);
  assert.match(tabBar, /\$: tabContextMenuMovable = isMovableTab\(\$selectedSession, tabContextMenuIndex\)/,
    'the menu has to follow the session and the tab it was opened on');
  const menu = body(tabBar, '{#if tabContextMenuMovable}', 1200);
  assert.match(menu, /tabContextMoveToSession/);
  assert.match(menu, /tabContextMoveToNewSession/);
  assert.match(body(tabBar, 'function tabBarDialogOpen', 400), /showMoveTabDialog/,
    'the picker is one of the tab bar\'s dialogs, for the keyboard and focus guards');
});

test('a session row tells a dragged tab apart from a session being reordered', () => {
  const over = body(sessionItem, 'function handleDragOver(e: DragEvent) {', 200);
  assert.match(over, /if \(handleTabDragOver\(e\)\) return;/,
    'a tab over a row must not light it up as a session reorder target');
  const drop = body(sessionItem, 'function handleDrop(e: DragEvent) {', 200);
  assert.match(drop, /if \(handleTabDrop\(e\)\) return;/);
  const tabOver = body(sessionItem, 'function handleTabDragOver', 600);
  assert.match(tabOver, /types\.includes\(TAB_DRAG_MIME\)/, 'only the drag type is readable before the drop');
  assert.match(tabOver, /if \(tabDropOver === 'ok'\) \{\s*e\.preventDefault\(\)/,
    'a refusing row must not accept the drop');
  assert.match(sessionItem, /data-menu-action="merge-into"/);
  assert.equal(TAB_DRAG_MIME, 'application/x-asmgr-tab');
});

test('after a move the view follows the tab, and the old terminal is let go', () => {
  const follow = body(sessionsStore, 'async function followMovedTab', 1800);
  assert.match(follow, /dropPoolForWindow\(sourceId, windowIdx\)/);
  assert.match(follow, /dropPoolForSession\(sourceId\)/, 'a merged session\'s terminals all go');
  assert.match(follow, /selectSession\(result\.sessionId\);\s*selectWindow\(result\.windowIdx\);/);
  assert.match(follow, /tasks:refresh/, 'moved tasks must show up where they went');
});

test('the picker refuses what the backend refuses, and is a proper dialog', () => {
  assert.match(dialog, /App\.SessionMergeRefusals\(sourceId\)/);
  assert.match(dialog, /App\.TabMoveRefusals\(sourceId, windowIdx\)/);
  assert.match(dialog, /!!id && list.some\(s => s.id === id\) && !refused\[id\]/);
  assert.match(dialog, /claimKeyForDialog\(\);\s*e\.stopPropagation\(\);\s*close\(\);/);
});
