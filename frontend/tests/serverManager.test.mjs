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

// The connection test reports steps, not a verdict. "Could not connect" is the
// least useful thing to tell someone setting up a server: reaching the machine,
// signing in and finding tmux are three problems with three different fixes.
test('the test result is shown step by step', () => {
  assert.match(dialogSrc, /testResult\.steps/,
    'the dialog shows a single verdict rather than what was checked');
  assert.match(dialogSrc, /step\.status === 'ok' \? '✓'/,
    'the steps carry no pass/fail marks');
  assert.match(dialogSrc, /stepLabel\(\$t, step\.name\)/,
    'step names are shown raw, or the translator is not passed in — a helper ' +
    'that reads the store itself does not re-run when the language changes');
});

// Accepting a host key is a decision, so it is a deliberate second action —
// and it carries the fingerprint the user was shown, not one re-read later.
test('a new host key is confirmed, never accepted silently', () => {
  const at = dialogSrc.indexOf('async function acceptHostKey');
  assert.ok(at > 0, 'there is no way to accept a host key');
  const fn = dialogSrc.slice(at, dialogSrc.indexOf('function stepLabel'));

  assert.match(fn, /testResult\?\.hostKey/,
    'the accepted fingerprint is not the one that was displayed');
  assert.match(fn, /App\.AcceptServerHostKey\(srv\.id, testResult\.hostKey\)/,
    'the acceptance does not name the fingerprint, so a key that changed in ' +
    'between would be accepted on the strength of the earlier prompt');

  assert.match(dialogSrc, /\{#if testResult\.hostKeyChanged\}/,
    'a changed host key is not called out');
  assert.match(dialogSrc, /\{:else if testResult\.hostKeyIsNew\}/,
    'a first connection is not distinguished from a changed key');
});

// A changed key must not offer an "accept" button: the honest explanation and
// the dishonest one look identical, and one click would settle it wrongly.
test('a changed host key offers no accept button', () => {
  const at = dialogSrc.indexOf('{#if testResult.hostKeyChanged}');
  const branch = dialogSrc.slice(at, dialogSrc.indexOf('{:else if testResult.hostKeyIsNew}'));
  assert.ok(!/hostKeyAccept/.test(branch),
    'a changed host key can be accepted with one click');
});

test('the test strings are translated', () => {
  for (const key of ['servers.test', 'servers.testing', 'servers.hostKeyNew',
                     'servers.hostKeyChanged', 'servers.step.connect']) {
    assert.ok(en[key], `${key} is missing from en.json`);
    assert.ok(hu[key], `${key} is missing from hu.json`);
  }
  assert.notEqual(en['servers.hostKeyChanged'], hu['servers.hostKeyChanged'],
    'the host-key warning is still English in hu.json');
});

// Without a multiplexer nothing here works — sessions live in it. So a server
// without one is not degraded, it is unusable, and offering to fix it beats
// telling the user to go and do it themselves.
test('a server with no tmux is offered the install', () => {
  assert.match(dialogSrc, /multiplexerMissing/,
    'nothing notices that the server has no multiplexer');
  assert.match(dialogSrc, /step\.name === 'multiplexer' && step\.status === 'failed'/,
    'the offer is not tied to what the test actually found');
  assert.match(dialogSrc, /App\.PlanServerMultiplexerInstall/, 'there is no way to plan an install');
  assert.match(dialogSrc, /App\.InstallServerMultiplexer/, 'there is no way to run it');
});

// This installs software on someone's server. The command is shown first, and
// running it is a second, separate click.
test('the install command is shown before it runs', () => {
  const at = dialogSrc.indexOf('{#if multiplexerMissing}');
  const block = dialogSrc.slice(at, dialogSrc.indexOf('{#if testResult.hostKeyChanged}', at));

  assert.match(block, /\{installPlan\.command\}/,
    'the command that would run on the server is never displayed');

  // The plan and the run are different buttons: one call must not do both.
  assert.match(block, /planInstall\(srv\)/, 'there is no step that only looks');
  assert.match(block, /runInstall\(srv\)/, 'there is no step that runs it');

  const planIndex = block.indexOf('planInstall(srv)');
  const runIndex = block.indexOf('runInstall(srv)');
  assert.ok(runIndex < planIndex,
    'the run button is not inside the branch that has a plan to show');
});

test('the install strings are translated', () => {
  for (const key of ['servers.tmuxMissing', 'servers.tmuxInstallOffer',
                     'servers.tmuxWillRun', 'servers.tmuxCannotInstall']) {
    assert.ok(en[key], `${key} is missing from en.json`);
    assert.ok(hu[key], `${key} is missing from hu.json`);
    assert.notEqual(en[key], hu[key], `${key} is still English in hu.json`);
  }
});

// Creating a session has to say where it runs, and default to this computer —
// which is what every session was before, and what most still are.
test('a new session can be placed on a server', () => {
  const dialog = readFileSync(
    new URL('../src/lib/components/Dialogs/NewSessionDialog.svelte', import.meta.url), 'utf8');

  assert.match(dialog, /let serverId = '';/,
    'the dialog has no notion of which machine a session runs on');
  assert.match(dialog, /createSession\([^)]*serverId\)/s,
    'the chosen server is not passed to the creation call');

  // The field only appears when there is a choice: someone with no servers
  // should not have to read a control that always says the same thing.
  assert.match(dialog, /\{#if servers\.length > 0\}/,
    'the server field is shown even when there are no servers to choose from');

  // The default server is preselected, so someone who works mainly on one
  // does not pick it every time.
  assert.match(dialog, /servers\.find\(s => s\.isDefault\)\?\.id/,
    'the default server is not preselected');
});

// A session running elsewhere has to be visible as such in the list — but only
// those, since a marker on every session would say nothing.
test('remote sessions are marked in the sidebar', () => {
  const item = readFileSync(
    new URL('../src/lib/components/Sidebar/SessionItem.svelte', import.meta.url), 'utf8');

  assert.match(item, /\{#if session\.serverName\}/,
    'nothing marks a session that runs on a server');
  assert.match(item, /\$t\('servers\.onServer'\)\.replace\('\{server\}', session\.serverName\)/,
    'the marker does not name the server it points at');
});

test('the placement strings are translated', () => {
  for (const key of ['servers.thisComputer', 'servers.runsOn', 'servers.runsOnHint',
                     'servers.onServer']) {
    assert.ok(en[key], `${key} is missing from en.json`);
    assert.ok(hu[key], `${key} is missing from hu.json`);
    assert.notEqual(en[key], hu[key], `${key} is still English in hu.json`);
  }
});
