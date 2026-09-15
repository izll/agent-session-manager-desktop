import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const sessionSrc = readFileSync(
  new URL('../src/lib/components/Dialogs/NewSessionDialog.svelte', import.meta.url), 'utf8');
const tabSrc = readFileSync(
  new URL('../src/lib/components/Dialogs/NewTabDialog.svelte', import.meta.url), 'utf8');
const storeSrc = readFileSync(
  new URL('../src/lib/stores/agents.ts', import.meta.url), 'utf8');
const en = JSON.parse(readFileSync(
  new URL('../src/lib/i18n/locales/en.json', import.meta.url), 'utf8'));

// The webview will not follow a plain link, so the host has to open it. A
// target="_blank" here would do nothing at all.
for (const [name, src] of [['new-session', sessionSrc], ['new-tab', tabSrc]]) {
  test(`${name} opens the install page in the real browser`, () => {
    assert.match(src, /import \{ BrowserOpenURL \}/,
      'the dialog cannot open a URL outside the webview');
    assert.match(src, /BrowserOpenURL\(chosenAgent\?\.installUrl/,
      'the offer does not open the agent’s own install page');
  });

  test(`${name} offers the page before the failure, not only after`, () => {
    assert.match(src, /\$: agentMissing =/,
      'nothing tracks whether the chosen agent is missing');
    assert.match(src, /\{#if agentMissing\}/,
      'the offer is never rendered');
  });

  // A missing agent that looks identical to an installed one sends the user
  // through create → fail → read error, when the list already knew.
  test(`${name} marks uninstalled agents in the picker`, () => {
    assert.match(src, /class:not-installed=\{agent\.installed === false\}/,
      'the grid gives no sign which agents are missing');
  });
}

test('the agent list carries the install state from the backend', () => {
  assert.match(storeSrc, /installed: boolean;/, 'the store drops the installed flag');
  assert.match(storeSrc, /installUrl\?: string;/, 'the store drops the install URL');
});

test('the offer has translated strings', () => {
  for (const key of ['agentInstall.missing', 'agentInstall.open', 'agentInstall.notInstalled']) {
    assert.ok(en[key], `${key} is missing from en.json`);
  }
  assert.match(en['agentInstall.missing'], /\{name\}/,
    'the message does not name the agent, so it reads the same for all of them');
});
