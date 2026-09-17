import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const dialogSrc = readFileSync(
  new URL('../src/lib/components/Dialogs/ServerManagerDialog.svelte', import.meta.url), 'utf8');
const appSrc = readFileSync(new URL('../src/App.svelte', import.meta.url), 'utf8');
const settingsSrc = readFileSync(
  new URL('../src/lib/components/Dialogs/SettingsDialog.svelte', import.meta.url), 'utf8');
const en = JSON.parse(readFileSync(
  new URL('../src/lib/i18n/locales/en.json', import.meta.url), 'utf8'));
const hu = JSON.parse(readFileSync(
  new URL('../src/lib/i18n/locales/hu.json', import.meta.url), 'utf8'));

// The password lives in the system keyring. It reaches the dialog only when
// the user types a new one, and never travels back — so an empty field means
// "leave it alone", and the placeholder is what tells the two apart.
test('the editor never displays a stored password', () => {
  assert.ok(!/srv\.password|\.password\b\s*\|\|/.test(dialogSrc),
    'the dialog reads a password off the server entry; the backend sends a ' +
    'flag instead and the secret stays in the keyring');
  assert.match(dialogSrc, /hasStoredPassword/,
    'nothing distinguishes "no password" from "one is saved"');
  assert.match(dialogSrc, /placeholder=\{hasStoredPassword \? \$t\('servers\.passwordStored'\)/,
    'a saved password is not shown as such, so clearing the field looks like ' +
    'the way to keep it');
});

// Both are long, racing operations: a save that lands after the dialog closed,
// or after another edit started, would overwrite the newer state.
test('loads and saves are guarded against races', () => {
  assert.match(dialogSrc, /loadGeneration/, 'a slow load can overwrite a newer one');
  assert.match(dialogSrc, /operationGeneration/, 'a slow save can overwrite a newer edit');

  const save = dialogSrc.slice(dialogSrc.indexOf('async function save()'),
    dialogSrc.indexOf('function askDelete'));
  assert.match(save, /generation !== operationGeneration/,
    'the save applies its result without checking it is still the current one');
});

// Escape has to unwind one layer at a time: closing the whole manager from
// inside a half-finished edit loses what was typed.
test('Escape closes one layer at a time', () => {
  const handler = dialogSrc.slice(dialogSrc.indexOf('function handleKeydown'),
    dialogSrc.indexOf('function focusInput'));
  assert.match(handler, /claimKeyForDialog\(\)/,
    'the keystroke is not claimed, so it reaches the terminal behind the dialog');
  assert.match(handler, /if \(showDelete\)/, 'Escape skips the confirmation layer');
  assert.match(handler, /else if \(editing\)/,
    'Escape closes the manager from inside the editor, discarding the form');
});

// Offering a jump host that cannot be saved is worse than not offering it: the
// backend rejects loops, and the user would only find out on save.
test('a server is not offered as its own jump host', () => {
  assert.match(dialogSrc, /servers\s*\n?\s*\.filter\(s => s\.id !== editingId\)/,
    'the server being edited appears in its own jump-host list');
});

test('the manager is reachable from Settings', () => {
  assert.match(settingsSrc, /dispatch\('openServers'\)/,
    'Settings has no way to open the server manager');
  assert.match(appSrc, /on:openServers=\{\(\) => \{ showSettingsDialog = false; showServerManager = true; \}\}/,
    'the Settings event is not wired to the manager');
  assert.match(appSrc, /<ServerManagerDialog bind:show=\{showServerManager\} \/>/,
    'the dialog is never rendered');
});

// The shortcut guard lists every open dialog; one left out means keystrokes
// meant for it reach the session behind.
test('the dialog is listed in the shortcut guard', () => {
  const guard = appSrc.slice(appSrc.indexOf('showCommandPicker ||'), appSrc.indexOf('showCommandPicker ||') + 200);
  assert.match(guard, /showServerManager/,
    'the server manager is missing from the guard, so shortcuts fire behind it');
});

test('every string is translated, and Hungarian is not left in English', () => {
  const keys = [...dialogSrc.matchAll(/\$t\('([^']+)'\)/g)].map(m => m[1]);
  assert.ok(keys.length > 15, `only found ${keys.length} translated strings`);

  for (const key of keys) {
    assert.ok(en[key], `${key} is missing from en.json, so the raw key is shown`);
    assert.ok(hu[key], `${key} is missing from hu.json`);
  }

  // The ones that carry meaning rather than a label — if these are identical
  // the Hungarian file was filled from the English one.
  for (const key of ['servers.managerTitle', 'servers.extraPathHint', 'servers.noKeyringHint']) {
    assert.notEqual(en[key], hu[key], `${key} is still English in hu.json`);
  }
});

// The placeholder is substituted, not concatenated: a name with a brace or a
// percent sign in it would otherwise break the message.
test('the delete confirmation names the server', () => {
  assert.match(dialogSrc, /\$t\('servers\.deleteMessage'\)\.replace\('\{name\}'/,
    'the confirmation does not say which server is being deleted');
  assert.match(en['servers.deleteMessage'], /\{name\}/,
    'the English message has no placeholder to substitute');
  assert.match(hu['servers.deleteMessage'], /\{name\}/,
    'the Hungarian message has no placeholder to substitute');
});
