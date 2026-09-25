import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { transformSync } from 'esbuild';

// A Codex tab's YOLO badge follows what Codex reports, and says so when YOLO
// was asked for but is not in effect — its background server ignores the flag.

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8').replace(/\r\n/g, '\n');

const { code } = transformSync(read('../src/lib/utils/yoloBadge.ts'), { loader: 'ts', format: 'esm' });
const { yoloBadge } = await import(`data:text/javascript,${encodeURIComponent(code)}`);

test('the badge for each reading', () => {
  assert.equal(yoloBadge({ yolo: true }), 'on');
  assert.equal(yoloBadge({ yolo: true, yoloNotInEffect: true }), 'on');
  assert.equal(yoloBadge({ yolo: false, yoloNotInEffect: true }), 'notInEffect');
  assert.equal(yoloBadge({ yolo: false }), '');
  assert.equal(yoloBadge(undefined), '');
});

const item = read('../src/lib/components/Sidebar/SessionItem.svelte');

test('every tab row shows the warning badge, not only the plain one', () => {
  const rows = item.match(/\{#if yoloBadge\(tab\) && showYolo\}.*\{\/if\}/g) ?? [];
  assert.equal(rows.length, 3, 'busy, waiting and idle rows each draw the badge');
  for (const row of rows) {
    assert.match(row, /class:not-in-effect=\{yoloBadge\(tab\) === 'notInEffect'\}/);
    assert.match(row, /'sessionItem\.yoloNotInEffect'/);
  }
  assert.doesNotMatch(item, /\{#if tab\.yolo && showYolo\}/, 'a row still ignores the warning');
});

test('a single-tab session shows it next to its name', () => {
  assert.match(item, /singleTabYolo = tabStatuses\.length === 1 \? yoloBadge\(tabStatuses\[0\]\)/);
  assert.match(item, /class:not-in-effect=\{nameYolo === 'notInEffect'\}/);
  assert.match(item, /\.badge\.yolo\.not-in-effect \{[^}]*line-through/);
});

test('a tab row with only the warning is still drawn', () => {
  assert.match(item, /!!yoloBadge\(tab\) && showYolo/);
});

test('every locale explains the warning', () => {
  const dir = new URL('../src/lib/i18n/locales/', import.meta.url);
  for (const file of readdirSync(dir)) {
    const locale = JSON.parse(readFileSync(new URL(file, dir), 'utf8'));
    assert.ok(locale['sessionItem.yoloNotInEffect'], `${file} has no sessionItem.yoloNotInEffect`);
  }
});

// The YOLO button shows what a click changes. On a Codex tab whose YOLO the
// background server ignored, the button looked off; clicking it to switch YOLO
// on switched the session's YOLO off.
const { yoloButtonState } = await import(`data:text/javascript,${encodeURIComponent(code)}`);

test('the button on a Codex tab shows the setting a click toggles', () => {
  const ignored = yoloButtonState({
    running: true, tabAgent: 'codex', sessionAutoYes: true, tabAutoYes: false,
    tab: { yolo: false, yoloNotInEffect: true },
  });
  assert.deepEqual(ignored, { active: true, notInEffect: true });

  const tabOnly = yoloButtonState({
    running: true, tabAgent: 'codex', sessionAutoYes: false, tabAutoYes: true, tab: { yolo: true },
  });
  assert.deepEqual(tabOnly, { active: true, notInEffect: false });

  const off = yoloButtonState({
    running: true, tabAgent: 'codex', sessionAutoYes: false, tabAutoYes: false, tab: { yolo: false },
  });
  assert.deepEqual(off, { active: false, notInEffect: false });
});

test('the button on a Claude tab still follows the pane', () => {
  const cycledAway = yoloButtonState({
    running: true, tabAgent: 'claude', sessionAutoYes: true, tabAutoYes: false, tab: { yolo: false },
  });
  assert.deepEqual(cycledAway, { active: false, notInEffect: false });
  const stopped = yoloButtonState({
    running: false, tabAgent: 'claude', sessionAutoYes: true, tabAutoYes: false, tab: undefined,
  });
  assert.equal(stopped.active, true);
});

test('the main panel uses it', () => {
  const panel = read('../src/lib/components/MainPanel/MainPanel.svelte');
  assert.match(panel, /\$: liveYolo = yoloButton\.active;/);
  assert.match(panel, /class:not-in-effect=\{yoloButton\.notInEffect\}/);
});

// A click on a tab toggles what makes that tab YOLO: its own flag, or the
// session's it inherits (CycleYoloMode / tabYoloToggle). The button shows
// exactly that, so a tab on by its own flag alone reads on, and reads off once
// the click has cleared it. Before, the click set the session's flag and the
// button could never be turned off from there.
test('the button on a tab follows the flags a click toggles', () => {
  const base = { running: true, tabAgent: 'codex', tab: { yolo: false } };
  assert.equal(yoloButtonState({ ...base, sessionAutoYes: false, tabAutoYes: true }).active, true);
  assert.equal(yoloButtonState({ ...base, sessionAutoYes: false, tabAutoYes: false }).active, false,
    "after the click cleared the tab's own flag");
  assert.equal(yoloButtonState({ ...base, sessionAutoYes: true, tabAutoYes: false }).active, true);
});

test('a live reading no click can change does not keep the button on', () => {
  const state = yoloButtonState({
    running: true, tabAgent: 'codex', sessionAutoYes: false, tabAutoYes: false, tab: { yolo: true },
  });
  assert.equal(state.active, false);
});

test('a stopped tab has no pane to cycle, so even a Claude one shows its flags', () => {
  const stoppedClaude = yoloButtonState({
    running: true, tabAgent: 'claude', sessionAutoYes: false, tabAutoYes: true, tabStopped: true, tab: undefined,
  });
  assert.deepEqual(stoppedClaude, { active: true, notInEffect: false });
  const stoppedSession = yoloButtonState({
    running: false, tabAgent: 'codex', sessionAutoYes: false, tabAutoYes: true, tab: undefined,
  });
  assert.equal(stoppedSession.active, true);
});

test('the main panel says whether the tab is stopped, and reloads after a click', () => {
  const panel = read('../src/lib/components/MainPanel/MainPanel.svelte');
  assert.match(panel, /tabStopped: isTab && !!fw\?\.stopped/);
  assert.match(panel, /tabAutoYes: isTab && !!fw\?\.auto_yes/);
  const store = read('../src/lib/stores/sessions.ts');
  const from = store.indexOf('export async function cycleYoloMode');
  const body = store.slice(from, store.indexOf('\n}\n', from));
  assert.match(body, /if \(restartedTab\) dropPoolForWindow\(id, windowIdx\);[\s\S]*await loadSessions\(\);/);
});
