import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const core = await import('../src/lib/utils/sidebarOrderCore.ts');
const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');

const s = (id) => ({ id });
const rows = (order) => order.map(e => `${e.section === 'favorites' ? '★' : ''}${e.id}`).join(' ');

// A list as it looks on screen: favourite F is also in group G1, which is
// open; group G2 is collapsed; U is ungrouped.
const favorites = [s('F'), s('U-fav')];
const groups = [{ id: 'G1', collapsed: false }, { id: 'G2', collapsed: true }];
const byGroup = new Map([['G1', [s('A'), s('F'), s('B')]], ['G2', [s('HIDDEN')]]]);
const ungrouped = [s('U')];
const order = core.buildSidebarOrder(false, [], favorites, groups, byGroup, ungrouped);

test('the order is the one on screen', () => {
  // Favourites first, then open groups, then ungrouped — and nothing from the
  // collapsed group, whose sessions are not visible.
  assert.equal(rows(order), '★F ★U-fav A F B U');
});

test('sorted by activity, it is one flat list', () => {
  const flat = core.buildSidebarOrder(true, [s('X'), s('Y')], favorites, groups, byGroup, ungrouped);
  assert.equal(rows(flat), 'X Y');
});

// Walk it with the keyboard from the top.
test('stepping walks the rows in order, both copies of a favourite included', () => {
  let active = core.resolveActiveEntry(null, 'F', order); // first appearance
  const seen = [rows([active])];
  for (;;) {
    const next = core.stepEntry(order, active, 1);
    if (!next) break;
    active = next;
    seen.push(rows([active]));
  }
  assert.deepEqual(seen, ['★F', '★U-fav', 'A', 'F', 'B', 'U']);
});

// The reported bug: clicking F inside its group and pressing "next" went to
// the favourites' neighbour, because F was resolved to its first appearance.
test('stepping continues from the copy that was clicked', () => {
  const clicked = { id: 'F', section: 'list' };
  const active = core.resolveActiveEntry(clicked, 'F', order);
  assert.deepEqual(active, clicked);
  assert.deepEqual(core.stepEntry(order, active, 1), { id: 'B', section: 'list' });
  assert.deepEqual(core.stepEntry(order, active, -1), { id: 'A', section: 'list' });
});

test('a selection made elsewhere starts from its first row', () => {
  // A stale cursor for another session, as after quick jump picked B.
  const active = core.resolveActiveEntry({ id: 'F', section: 'list' }, 'B', order);
  assert.deepEqual(active, { id: 'B', section: 'list' });
});

test('the ends do not wrap, and a hidden selection starts from an end', () => {
  const last = { id: 'U', section: 'list' };
  assert.equal(core.stepEntry(order, last, 1), null);
  assert.equal(core.stepEntry(order, { id: 'F', section: 'favorites' }, -1), null);
  // In a collapsed group: not on screen, so there is no row to step from.
  const hidden = core.resolveActiveEntry(null, 'HIDDEN', order);
  assert.equal(hidden, null);
  assert.deepEqual(core.stepEntry(order, hidden, 1), { id: 'F', section: 'favorites' });
  assert.deepEqual(core.stepEntry(order, hidden, -1), { id: 'U', section: 'list' });
});

// The order is rebuilt from the same stores the sidebar draws from; if the
// sidebar changed and this did not, the shortcut would drift again.
test('the order is built from what SessionTree renders', () => {
  const tree = read('../src/lib/components/Sidebar/SessionTree.svelte');
  for (const store of ['$sessionsByActivity', '$favorites', '$groups', '$sessionsByGroup', '$ungroupedSessions']) {
    assert.ok(tree.includes(store), `SessionTree no longer renders ${store}`);
  }
  assert.match(tree, /\{#each \$favorites as session[^}]*\}\s*<SessionItem \{session\} section="favorites"/,
    'the favourites rows do not say which section they are in');

  const store = read('../src/lib/stores/sidebarOrder.ts');
  assert.match(store, /settings, sessionsByActivity, favorites, groups, sessionsByGroup, ungroupedSessions/);
});

test('clicking a row records which copy it was, and the cursor row stays in view', () => {
  const item = read('../src/lib/components/Sidebar/SessionItem.svelte');
  assert.match(item, /on:click=\{\(\) => selectSidebarEntry\(session\.id, section\)\}/,
    'a click selects the session without saying which copy was clicked');
  assert.match(item, /scrollIntoView\(\{ block: 'nearest' \}\)/,
    'stepping past the visible part of the list selects rows out of sight');
  assert.match(item, /class:echo=\{isSelected && !isCursor\}/,
    'both copies of a selected favourite look the same, hiding where the next step goes');

  const app = read('../src/App.svelte');
  assert.match(app, /import \{ selectPrevSession, selectNextSession \} from '\.\/lib\/stores\/sidebarOrder'/,
    'the shortcuts still step in stored order');
});

// One Alt+Down in the terminal moved two or three sessions. The app's shortcut
// handler listens on the window in the capture phase, so it had already
// stepped; the xterm key handler then stepped again with an event of its own,
// and it runs for keyup as well as keydown.
test('one press steps once, with the terminal focused too', () => {
  const terminal = read('../src/lib/utils/terminal.ts');
  const at = terminal.indexOf('if (isSessionStepKey(event))');
  assert.ok(at > 0, 'the terminal no longer refuses the step keys, so the pane would receive them');
  const branch = terminal.slice(at, terminal.indexOf('}', at));
  assert.match(branch, /return false/);
  assert.doesNotMatch(branch, /dispatchEvent/, 'the terminal steps a second time');

  const app = read('../src/App.svelte');
  assert.doesNotMatch(app, /terminal-nav/, 'something still steps on the terminal\'s event');
  assert.match(app, /addEventListener\('keydown', handleKeydown, true\)/,
    'the shortcut handler no longer runs before the terminal sees the key');
});
