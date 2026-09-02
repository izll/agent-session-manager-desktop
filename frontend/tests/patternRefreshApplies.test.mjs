import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const dialog = readFileSync(
  new URL('../src/lib/components/Dialogs/SettingsDialog.svelte', import.meta.url), 'utf8');

test('a successful pattern refresh asks for detection to run now', () => {
  const fn = dialog.slice(dialog.indexOf('async function refreshPatterns'));
  const body = fn.slice(0, fn.indexOf('\n  }\n') + 4);
  assert.match(body, /result\?\.updated/, 'the updated flag is no longer consulted');
  assert.match(body, /loadActivities\(\)/,
    'nothing asks detection to re-run, so the effect waits out the poll');
});

test('it is not asked for when nothing changed', () => {
  const fn = dialog.slice(dialog.indexOf('async function refreshPatterns'));
  const body = fn.slice(0, fn.indexOf('\n  }\n') + 4);
  const call = body.indexOf('loadActivities()');
  const guard = body.lastIndexOf('if (result?.updated)', call);
  assert.ok(guard > 0 && guard < call,
    're-running detection should be guarded by the updated flag');
});

// The message has to say the patterns are already live. Offering a restart, or
// staying silent about it, both leave the user wondering.
test('the updated message says no restart is needed', () => {
  const en = JSON.parse(readFileSync(
    new URL('../src/lib/i18n/locales/en.json', import.meta.url), 'utf8'));
  const msg = en['settings.refreshPatternsUpdated'];
  assert.ok(msg, 'the message is missing');
  assert.match(msg, /restart/i, 'the message does not mention restarting at all');
  assert.match(msg, /\{version\}/, 'the version placeholder was lost');
});

test('the Hungarian message says the same', () => {
  const hu = JSON.parse(readFileSync(
    new URL('../src/lib/i18n/locales/hu.json', import.meta.url), 'utf8'));
  assert.match(hu['settings.refreshPatternsUpdated'], /újraindítás/i);
});
