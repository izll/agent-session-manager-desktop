import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';

// After a reboot the app offers back the sessions that were running, each with
// only the tabs that were running. The rules for what the dialog says and what
// a choice sends run here as they are; the wiring is read from the source.

const { tabsLine, splitChoice, summarize } = await import('../src/lib/utils/interruptedWork.ts');

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const appSrc = read('../src/App.svelte');
const storeSrc = read('../src/lib/stores/sessions.ts');
const dialogSrc = read('../src/lib/components/Dialogs/InterruptedWorkDialog.svelte');
const settingsSrc = read('../src/lib/components/Dialogs/SettingsDialog.svelte');
const settingsStoreSrc = read('../src/lib/stores/settings.ts');

// ── What a row says ─────────────────────────────────────────────────────────

test('a session with some tabs stopped says how many come back', () => {
  assert.deepEqual(tabsLine({ reopenTabs: 3, totalTabs: 5 }),
    { key: 'interrupted.someTabs', values: { count: 3, total: 5 } });
});

test('a session with every tab running says all of them', () => {
  assert.deepEqual(tabsLine({ reopenTabs: 4, totalTabs: 4 }),
    { key: 'interrupted.allTabs', values: { count: 4 } });
});

test('a single-tab session says nothing about tabs', () => {
  assert.equal(tabsLine({ reopenTabs: 1, totalTabs: 1 }), null);
});

// ── What a choice sends ─────────────────────────────────────────────────────

test('ticked sessions are reopened and the rest forgotten', () => {
  const sessions = [{ id: 'a' }, { id: 'b' }, { id: 'c' }];
  assert.deepEqual(splitChoice(sessions, new Set(['a', 'c'])), { reopen: ['a', 'c'], dismiss: ['b'] });
  assert.deepEqual(splitChoice(sessions, new Set()), { reopen: [], dismiss: ['a', 'b', 'c'] });
});

test('a run is summarised as reopened and failed', () => {
  assert.deepEqual(summarize([{ id: 'a', ok: true }, { id: 'b', ok: false, error: 'x' }, { id: 'c', ok: true }]),
    { ok: 2, failed: 1 });
  assert.deepEqual(summarize([]), { ok: 0, failed: 0 });
});

// ── Wiring ──────────────────────────────────────────────────────────────────

function body(src, signature, end = '\n  }\n') {
  const at = src.indexOf(signature);
  assert.ok(at >= 0, `${signature} is gone`);
  return src.slice(at, src.indexOf(end, at));
}

test('the launch asks once the project is known, without holding up the rest', () => {
  const mount = appSrc.slice(appSrc.indexOf('onMount(async'), appSrc.indexOf('onDestroy('));
  const loaded = mount.indexOf('await loadProjects()');
  const check = mount.indexOf('checkInterruptedWork(get(activeProjectId))');
  assert.ok(loaded >= 0 && check > loaded, 'the check runs before the active project is loaded');
  assert.doesNotMatch(mount, /await checkInterruptedWork/,
    'the launch waits for servers to answer before the app becomes usable');
});

test('opening another project offers that project\'s interrupted work', () => {
  assert.match(appSrc,
    /\$: if \(interruptedCheckedProject !== null && \$activeProjectId !== interruptedCheckedProject\) \{\s*checkInterruptedWork\(\$activeProjectId\);/,
    'a project switch does not look for that project\'s interrupted work');
});

test('the setting decides between asking, reopening and nothing', () => {
  const fn = body(appSrc, 'async function checkInterruptedWork');
  assert.match(fn, /work\.mode === 'auto'[\s\S]*const ids = work\.sessions\.map\(\(s\) => s\.id\);[\s\S]*reopenInterruptedSessions\(ids, \[\]\)/,
    '"reopen automatically" does not reopen');
  assert.match(fn, /if \(work\.mode !== 'ask'\) return;/, '"do nothing" still shows the dialog');
  assert.match(fn, /showInterruptedWork = true/);
  assert.match(fn, /generation !== interruptedCheckGeneration/,
    'an answer for a project already left can still open the dialog');
});

test('the dialog is mounted and counts as an open dialog', () => {
  assert.match(appSrc, /<InterruptedWorkDialog\s+bind:show=\{showInterruptedWork\}/);
  assert.match(appSrc, /showResumeSessionPicker \|\| showInterruptedWork;/,
    'keyboard shortcuts would reach the terminal behind the dialog');
});

test('the store forgets the unticked sessions and pins the project', () => {
  const fn = body(storeSrc, 'export async function reopenInterruptedSessions', '\n}\n');
  assert.match(fn, /App\.DismissInterruptedWork\(dismiss, target\.projectId\)/);
  assert.match(fn, /App\.ReopenInterruptedSessions\(reopen, target\.projectId\)/);
  assert.match(fn, /dropPoolForSession\(id\)/,
    'a cached terminal for a reopened session points at the tmux session that died with the machine');
  assert.ok(fn.indexOf('DismissInterruptedWork') < fn.indexOf('ReopenInterruptedSessions'));
});

test('an answer from a project already left is dropped', () => {
  const fn = body(storeSrc, 'export async function getInterruptedWork', '\n}\n');
  assert.match(fn, /projectTargetIsCurrent\(target\)/);
});

test('the dialog uses the shared chrome and shows per-session progress', () => {
  assert.match(dialogSrc, /<DialogCloseButton on:click=\{notNow\} disabled=\{busy\} \/>/);
  assert.match(dialogSrc, /EventsOn\('interrupted:progress'/);
  assert.match(dialogSrc, /onDestroy\(\(\) => stopProgress\?\.\(\)\)/, 'the progress listener outlives the dialog');
  // Every offered session starts ticked.
  assert.match(dialogSrc, /selected = new Set\(sessions\.map\(\(s\) => s\.id\)\)/);
});

// ── The setting ─────────────────────────────────────────────────────────────

test('the setting defaults to asking', () => {
  assert.match(settingsStoreSrc, /restartReopen: 'ask',/);
  assert.match(settingsStoreSrc, /export type RestartReopen = 'ask' \| 'auto' \| 'off';/);
});

test('the setting sits on the General tab, before Experimental', () => {
  const general = settingsSrc.indexOf("{#if activeTab === 'general'}");
  const setting = settingsSrc.indexOf('data-setting="restart-reopen"');
  const experimental = settingsSrc.indexOf("$t('settings.experimental')");
  const terminalTab = settingsSrc.indexOf("{#if activeTab === 'terminal'}");
  assert.ok(general >= 0 && setting > general, 'the setting is not on the General tab');
  assert.ok(setting < experimental, 'Experimental must stay the last section of its tab');
  assert.ok(setting < terminalTab);
  assert.match(settingsSrc, /saveSettings\(\{ restartReopen: e\.detail as RestartReopen \}\)/);
});

test('every locale says all of it', () => {
  const dir = new URL('../src/lib/i18n/locales/', import.meta.url);
  const keys = [
    'interrupted.title', 'interrupted.intro', 'interrupted.introProject', 'interrupted.allTabs',
    'interrupted.someTabs', 'interrupted.reopenSelected', 'interrupted.notNow', 'interrupted.reopening',
    'interrupted.reopened', 'interrupted.failed', 'interrupted.settingHint', 'interrupted.toastDone',
    'interrupted.toastPartial', 'settings.restartReopen', 'settings.restartReopenDesc',
    'settings.restartReopenAsk', 'settings.restartReopenAuto', 'settings.restartReopenOff',
    'error.notInterrupted',
  ];
  const files = readdirSync(dir).filter((f) => f.endsWith('.json'));
  assert.equal(files.length, 20);
  for (const file of files) {
    const locale = JSON.parse(readFileSync(new URL(file, dir), 'utf8'));
    for (const key of keys) assert.ok(locale[key], `${file} lacks ${key}`);
    assert.match(locale['interrupted.someTabs'], /\{count\}/, `${file}: the count is missing`);
    assert.match(locale['interrupted.someTabs'], /\{total\}/, `${file}: the total is missing`);
    assert.match(locale['interrupted.introProject'], /\{project\}/, `${file}: the project is missing`);
  }
});

// Both launch dialogs go through one queue: the offer to reopen first, since it
// asks for an answer, and the release notes only after the check for it — the
// check is asynchronous, and asking first would have let the notes jump ahead.
test('the reopen offer comes before the release notes', () => {
  const app = readFileSync(new URL('../src/App.svelte', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
  assert.match(app, /launchDialogs\.request\('interrupted', \(\) => \{/);
  assert.match(app, /\}, 0\);\n  \}/, 'the reopen offer is not first in line');
  assert.match(app, /launchDialogs\.done\('interrupted'\);/);
  assert.match(app, /void interruptedChecked\.finally\(\(\) => checkWhatsNew\(\)\);/,
    'the release notes are asked for before the interrupted-work check has answered');
});
