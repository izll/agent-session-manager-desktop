import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { transformSync } from 'esbuild';

// A tab whose agent has an update waiting shows a small arrow in the session
// list and the tab bar; its tooltip says which agent, which versions and what
// to do. Nothing is updated on the user's behalf.

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8').replace(/\r\n/g, '\n');

const { code } = transformSync(read('../src/lib/utils/updateBadge.ts'), { loader: 'ts', format: 'esm' });
const { updateBadge, updateTooltip } = await import(`data:text/javascript,${encodeURIComponent(code)}`);

test('the badge for each notice', () => {
  assert.equal(updateBadge({ update: { kind: 'available', blocking: true } }), 'blocking');
  assert.equal(updateBadge({ update: { kind: 'available', blocking: false } }), 'available');
  assert.equal(updateBadge({ update: { kind: 'installed-restart', blocking: false } }), 'restart');
  assert.equal(updateBadge({}), '');
  assert.equal(updateBadge({ update: null }), '');
  assert.equal(updateBadge(undefined), '');
});

test('the tooltip names the agent, the versions and what to do', () => {
  assert.deepEqual(
    updateTooltip({ update: { kind: 'available', blocking: true, current: '0.155.1', version: '0.156.1' } }, 'Codex'),
    [
      { key: 'sessionItem.updateAvailableFrom', params: { agent: 'Codex', version: '0.156.1', current: '0.155.1' } },
      { key: 'sessionItem.updateHintPrompt', params: {} },
    ],
  );
  assert.deepEqual(
    updateTooltip({ update: { kind: 'installed-restart', blocking: false } }, 'Claude'),
    [
      { key: 'sessionItem.updateInstalled', params: { agent: 'Claude' } },
      { key: 'sessionItem.updateHintRestart', params: {} },
    ],
  );
  assert.deepEqual(
    updateTooltip({ update: { kind: 'available', blocking: false, version: '2.24.0', command: 'q update' } }, 'Amazon Q'),
    [
      { key: 'sessionItem.updateAvailableVersion', params: { agent: 'Amazon Q', version: '2.24.0' } },
      { key: 'sessionItem.updateHintCommand', params: { command: 'q update' } },
    ],
  );
  assert.deepEqual(
    updateTooltip({ update: { kind: 'available', blocking: false } }, 'Cursor').map((m) => m.key),
    ['sessionItem.updateAvailable', 'sessionItem.updateHintAvailable'],
  );
  assert.deepEqual(
    updateTooltip({ update: { kind: 'installed-restart', blocking: false, version: '1.3.0' } }, 'OpenCode')[0],
    { key: 'sessionItem.updateInstalledVersion', params: { agent: 'OpenCode', version: '1.3.0' } },
  );
  assert.deepEqual(updateTooltip({}, 'Codex'), []);
});

test('every locale has every string, with the same placeholders', () => {
  const keys = [
    'settings.showUpdateBadge', 'settings.showUpdateBadgeDesc',
    'sessionItem.updateAvailable', 'sessionItem.updateAvailableVersion', 'sessionItem.updateAvailableFrom',
    'sessionItem.updateInstalled', 'sessionItem.updateInstalledVersion',
    'sessionItem.updateHintPrompt', 'sessionItem.updateHintRestart',
    'sessionItem.updateHintCommand', 'sessionItem.updateHintAvailable',
  ];
  const placeholders = (s) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort().join(',');
  const dir = new URL('../src/lib/i18n/locales/', import.meta.url);
  const locales = readdirSync(dir).filter((name) => name.endsWith('.json'));
  assert.ok(locales.length >= 20, `only ${locales.length} locales found`);
  const en = JSON.parse(read('../src/lib/i18n/locales/en.json'));
  for (const name of locales) {
    const strings = JSON.parse(read(`../src/lib/i18n/locales/${name}`));
    for (const key of keys) {
      assert.equal(typeof strings[key], 'string', `${name} lacks ${key}`);
      assert.equal(placeholders(strings[key]), placeholders(en[key]), `${name} ${key} placeholders`);
    }
  }
});

const item = read('../src/lib/components/Sidebar/SessionItem.svelte');

test('every tab row and a single tab\'s name show it, when switched on', () => {
  const rows = item.match(/\{#if updateBadge\(tab\) && showUpdate\}.*\{\/if\}/g) ?? [];
  assert.equal(rows.length, 3, 'busy, waiting and idle rows each draw the badge');
  for (const row of rows) assert.match(row, /title=\{updateTitle\(tab\)\}/);
  assert.match(item, /\{#if updateBadge\(nameUpdateTab\) && showUpdate\}/);
  assert.match(item, /showUpdate = !!\$settings\?\.showUpdateBadge/,
    'the session list shows the badge without being asked to');
  assert.match(item, /tabRowVisible = [^\n]*\n[^\n]*updateBadge\(tab\) && showUpdate/,
    'a tab row with only the update badge is still drawn');
  // The tooltip follows the language: the helper is rebuilt from $t.
  assert.match(item, /\$: updateTitle = [^\n]*\n[^\n]*\$t\(/);
});

// The session list is busy enough: the marker there is opt-in. The tab bar,
// where it sits beside the tab it is about, always shows it.
test('the tab bar always shows it', () => {
  const bar = read('../src/lib/components/MainPanel/TabBar.svelte');
  assert.match(bar, /!win\.Dead && tabUpdateByIdx\[win\.Index\]\}/);
  assert.doesNotMatch(bar, /UpdateBadge/, 'the tab bar follows the session-list setting');
});

test('the session-list setting is off by default and has a toggle', () => {
  const store = read('../src/lib/stores/settings.ts');
  assert.match(store, /showUpdateBadge: boolean;/);
  assert.match(store, /showUpdateBadge: false,/);
  assert.doesNotMatch(store, /hideUpdateBadge/);
  const dialog = read('../src/lib/components/Dialogs/SettingsDialog.svelte');
  assert.match(dialog, /saveSettings\(\{ showUpdateBadge: !\$settings\.showUpdateBadge \}\)/);
  assert.match(dialog, /\$t\('settings\.showUpdateBadge'\)/);
});
