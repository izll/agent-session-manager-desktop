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

const sessionTree = read('../src/lib/components/Sidebar/SessionTree.svelte');
const dropZone = read('../src/lib/components/Sidebar/NewSessionDropZone.svelte');
const prompts = read('../src/lib/components/Dialogs/TabMovePrompts.svelte');
const nameDialog = read('../src/lib/components/Dialogs/MoveToNewSessionDialog.svelte');
const tabMoveUtil = read('../src/lib/utils/tabMove.ts');

const {
  TAB_DRAG_MIME, isMovableTab, isOnlyTab, encodeTabDrag, decodeTabDrag, tabDropState, newSessionDropState,
  uniqueSessionName, moveTargets,
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

// ── The only tab of a session, and the "new session" target ───────────────

test('the only tab of a session is told apart from the own window beside other tabs', () => {
  assert.equal(isOnlyTab({ followedWindows: [] }, 0), true);
  assert.equal(isOnlyTab({ followedWindows: [], mainWindowIndex: 0 }, 0), true);
  assert.equal(isOnlyTab({ followedWindows: null, mainWindowIndex: 1 }, 1), true, 'base-index 1: its own window is 1');
  assert.equal(isOnlyTab({ followedWindows: [], mainWindowIndex: 0 }, 3), false,
    'a window the session does not follow is not the session');
  assert.equal(isOnlyTab({ followedWindows: [{ index: 1 }] }, 0), false);
  assert.equal(isOnlyTab({ followedWindows: [{ index: 1 }] }, 1), false);
  assert.equal(isOnlyTab(null, 0), false);
  assert.equal(isOnlyTab({ followedWindows: [] }, null), false);
});

test('a drag says whether it carries the only tab, and nothing else claims to', () => {
  const payload = { sessionId: 's1', windowIdx: 0, projectId: 'p', name: 'solo', onlyTab: true };
  assert.deepEqual(decodeTabDrag(encodeTabDrag(payload)), payload);
  assert.equal(decodeTabDrag(encodeTabDrag({ ...payload, onlyTab: false })).onlyTab, undefined);
  assert.equal(decodeTabDrag('{"sessionId":"s","windowIdx":0,"projectId":"p","onlyTab":"yes"}').onlyTab, undefined);
});

test('the "new session" target answers a dragged tab by what the backend said', () => {
  const drag = { sessionId: 'src', windowIdx: 2, projectId: 'p', name: 't', refusals: {}, newSessionRefusal: '' };
  assert.equal(newSessionDropState(drag, 'p'), 'ok');
  assert.equal(newSessionDropState({ ...drag, newSessionRefusal: 'error.tabSplitOnlyTab' }, 'p'), 'refused');
  assert.equal(newSessionDropState({ ...drag, newSessionRefusal: null }, 'p'), 'ok', 'still being asked');
  assert.equal(newSessionDropState(drag, 'other-project'), 'none');
  assert.equal(newSessionDropState(null, 'p'), 'none');
});

test('the name offered for the new session is the one the backend would give', () => {
  const sessions = [{ name: 'build' }, { name: 'build 2' }, { name: 'web' }];
  assert.equal(uniqueSessionName('build', sessions), 'build 3');
  assert.equal(uniqueSessionName('web', sessions), 'web 2');
  assert.equal(uniqueSessionName('docs', sessions), 'docs');
});

test('the only tab is offered to the sidebar too, marked as such', () => {
  const start = body(tabBar, 'function handleTabDragStart');
  assert.match(start, /isMovableTab\(sess, win\.Index\) \|\| isOnlyTab\(sess, win\.Index\)/);
  assert.match(start, /onlyTab: isOnlyTab\(sess, win\.Index\)/);
  assert.match(tabBar, /\$: tabContextMenuOnlyTab = isOnlyTab\(\$selectedSession, tabContextMenuIndex\)/);
  assert.match(tabBar, /\$: tabContextMenuMovable = isMovableTab\(\$selectedSession, tabContextMenuIndex\) \|\| tabContextMenuOnlyTab/);
  assert.match(body(tabBar, 'class:disabled={tabContextMenuOnlyTab}', 300),
    /aria-disabled=\{tabContextMenuOnlyTab\}[\s\S]*error\.tabSplitOnlyTab[\s\S]*data-menu-action="move-to-new-session"/,
    '"Move to new session" is inert for the only tab, and says why');
  assert.match(body(tabBar, 'function tabContextMoveToNewSession', 120), /if \(tabContextMenuOnlyTab\) return;/);
  assert.match(body(tabBar, 'function tabContextMoveToNewSession', 500), /askNewSessionFor\(sess\.id, windowIdx, name\)/,
    'the menu asks for the name, as the drop does');
  assert.match(tabBar, /onlyTab=\{moveTabTarget\.onlyTab\}/);
  assert.match(tabBar, /<TabMovePrompts \/>/);
  assert.match(body(tabBar, 'function tabBarDialogOpen', 400), /get\(tabMovePrompt\) !== null/);
});

test('moving the only tab is asked first, wherever it comes from', () => {
  assert.match(body(sessionItem, 'function handleTabDrop', 900),
    /requestTabMove\(payload\.sessionId, payload\.windowIdx, session\.id, payload\.name, !!payload\.onlyTab\)/);
  const request = body(tabMoveUtil, 'export function requestTabMove', 500);
  assert.match(request, /if \(!onlyTab\) \{\s*void moveTabWithNotice/);
  assert.match(request, /tabMovePrompt\.set\(\{ kind: 'onlyTab'/);
  assert.match(body(dialog, 'async function confirm', 400), /if \(mode === 'tab' && onlyTab\)[\s\S]*requestTabMove\(sourceId, windowIdx, targetId, name, true\)/);
  assert.match(body(tabMoveUtil, 'export async function moveOnlyTabWithNotice', 400), /mergeSessionInto\(sourceId, targetId\)/,
    'the only tab moves by merging its session');
  assert.match(prompts, /<ConfirmDialog[\s\S]*tabMove\.onlyTabMessage[\s\S]*on:confirm=\{confirmOnlyTab\}[\s\S]*on:cancel=\{done\}/);
  assert.match(prompts, /<MoveToNewSessionDialog/);
});

test('the "new session" target is there only while a tab of this project is dragged', () => {
  assert.match(sessionTree, /<NewSessionDropZone \/>/);
  assert.match(dropZone, /\$: shown = !!\$tabDrag && \$tabDrag\.projectId === \$activeProjectId;/);
  const over = body(dropZone, 'function handleDragOver', 400);
  assert.match(over, /types\.includes\(TAB_DRAG_MIME\)/);
  assert.match(over, /if \(over === 'ok'\) \{\s*e\.preventDefault\(\)/, 'a refusing target must not accept the drop');
  assert.match(body(dropZone, 'function handleDrop', 700), /endTabDrag\(\);[\s\S]*askNewSessionFor\(payload\.sessionId, payload\.windowIdx, payload\.name\)/);
  assert.match(body(tabMoveUtil, 'export function beginTabDrag', 900), /App\.TabSplitRefusal\(payload\.sessionId, payload\.windowIdx\)/);
});

test('the name prompt offers the tab\'s name, refuses what the backend refuses, and splits with the typed one', () => {
  assert.match(body(nameDialog, 'async function open', 300), /name = uniqueSessionName\(tabName, get\(sessions\)\)/);
  assert.match(body(nameDialog, 'async function open', 400), /App\.TabSplitRefusal\(sourceId, windowIdx\)/);
  assert.match(body(nameDialog, 'async function confirm', 400), /splitTabWithNotice\(sourceId, windowIdx, tabName, name\.trim\(\)\)/);
  assert.match(nameDialog, /return !!value\.trim\(\) && !refused && !working;/);
  assert.match(nameDialog, /claimKeyForDialog\(\);\s*e\.stopPropagation\(\);\s*close\(\);/);
  assert.match(body(tabMoveUtil, 'export async function splitTabWithNotice', 300), /moveTabToNewSession\(sourceId, windowIdx, name\)/);
});
