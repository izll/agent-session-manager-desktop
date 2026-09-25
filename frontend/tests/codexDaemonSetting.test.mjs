import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';

// Codex 0.157's background server ignores the YOLO flag and sometimes fails to
// start, so asmgr starts Codex with --no-daemon unless the user turns the
// server back on. The switch has to say why it is off.

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const dialog = read('../src/lib/components/Dialogs/SettingsDialog.svelte');
const store = read('../src/lib/stores/settings.ts');
const localesDir = new URL('../src/lib/i18n/locales/', import.meta.url);

const keys = [
  'settings.codexSection',
  'settings.codexUseDaemon',
  'settings.codexUseDaemonDesc',
  'settings.codexUseDaemonWarning',
];

test('the daemon is off unless chosen', () => {
  const defaults = store.slice(store.indexOf('function defaultSettings()'));
  assert.match(defaults, /codexUseDaemon: false,/,
    'a fresh install would let Codex use its background server');
});

test('the switch is on the Agents tab and shows the warning', () => {
  const agentsTab = dialog.slice(
    dialog.indexOf("{#if activeTab === 'agents'}"),
    dialog.indexOf("{#if activeTab === 'dictation'}"));
  assert.match(agentsTab, /toggle\('codexUseDaemon'\)/, 'the switch is not on the Agents tab');
  assert.match(agentsTab, /class:active=\{\$settings\.codexUseDaemon\}/,
    'the switch does not show the stored value');
  // Outside any {#if}: the warning is the reason for the default, and it
  // matters most right when someone is about to turn the daemon on.
  const section = agentsTab.slice(agentsTab.lastIndexOf('<div class="settings-section">'));
  assert.match(section, /\$t\('settings\.codexUseDaemonWarning'\)/, 'the bug is not mentioned');
  assert.doesNotMatch(section, /\{#if/, 'the warning is only shown some of the time');
});

test('every language has the texts, and they name the flag', () => {
  const files = readdirSync(localesDir).filter((name) => name.endsWith('.json'));
  assert.equal(files.length, 20);
  for (const file of files) {
    const locale = JSON.parse(readFileSync(new URL(file, localesDir), 'utf8'));
    for (const key of keys) {
      assert.ok(locale[key]?.trim(), `${file} has no ${key}`);
    }
    assert.match(locale['settings.codexUseDaemonDesc'], /--no-daemon/, `${file}: the flag is not named`);
    assert.match(locale['settings.codexUseDaemonWarning'], /YOLO/, `${file}: the YOLO problem is not named`);
  }
});
