import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const src = readFileSync(
  new URL('../src/lib/utils/backendError.ts', import.meta.url), 'utf8');
const en = JSON.parse(readFileSync(
  new URL('../src/lib/i18n/locales/en.json', import.meta.url), 'utf8'));
const hu = JSON.parse(readFileSync(
  new URL('../src/lib/i18n/locales/hu.json', import.meta.url), 'utf8'));
const instanceGo = readFileSync(
  new URL('../../session/instance.go', import.meta.url), 'utf8');
const tabBar = readFileSync(
  new URL('../src/lib/components/MainPanel/TabBar.svelte', import.meta.url), 'utf8');

// The Go side reported failures as English sentences written where they
// happen, and the frontend printed them with String(e). The user read the
// programmer's own words, in a language they may not have.
//
// Errors the user is expected to act on are now keys, resolved here.
test('errors the user must act on are reported as keys', () => {
  for (const key of ['error.agentFoundOffPath', 'error.agentNotOnServerPath']) {
    assert.ok(instanceGo.includes(key),
      `${key} is no longer reported by the backend`);
    assert.ok(en[key], `${key} has no English text`);
    assert.ok(hu[key], `${key} has no Hungarian text`);
    assert.notEqual(en[key], hu[key], `${key} was never actually translated`);
  }
});

// Values travel with the key so the backend names the agent and the directory
// without knowing any language.
test('a key can carry the values its message needs', () => {
  assert.match(src, /text\.split\(`\|`\)|text\.split\('\|'\)|split\("\|"\)/,
    'values are no longer carried with the key');
  assert.match(en['error.agentFoundOffPath'], /\{0\}/, 'the agent name has no place in the message');
  assert.match(en['error.agentFoundOffPath'], /\{1\}/, 'the directory has no place in the message');
});

// Anything that is not a key passes through: an English sentence is still
// better than an empty message or a bare identifier.
test('a plain message is passed through unchanged', () => {
  assert.match(src, /if \(!text\.startsWith\('error\.'\)\)/,
    'non-key errors are no longer passed through');
});

// The wrappers around those errors were English too — "Failed to restart tab"
// was as untranslated as the sentence it wrapped.
test('the messages wrapping backend errors are translated', () => {
  assert.ok(!/errorMessage = `Failed to/.test(tabBar),
    'a hardcoded English error prefix is back in the tab bar');
  for (const key of [
    'tabBar.restartTabFailed', 'tabBar.deleteTabFailed', 'tabBar.startSessionFailed',
  ]) {
    assert.ok(en[key] && hu[key], `${key} is missing`);
  }
});

const bridge = readFileSync(
  new URL('../src/lib/utils/backendErrorBridge.ts', import.meta.url), 'utf8');
const mainTs = readFileSync(
  new URL('../src/main.ts', import.meta.url), 'utf8');

// Around a hundred and fifty places render an error with String(e). Resolving
// the key at each of them is a hundred and fifty chances to forget, and a
// forgotten one shows a bare identifier like "error.agentNotOnServerPath|agy".
//
// So it happens once, where every call already passes: the bindings.
test('backend errors are translated at the binding layer, not at each caller', () => {
  assert.match(mainTs, /translateBackendErrors\(\)/,
    'nothing installs the translation, so keys reach the screen raw');

  const installAt = mainTs.indexOf('translateBackendErrors()');
  const mountAt = mainTs.indexOf('mount(App');
  assert.ok(installAt < mountAt,
    'the app is mounted before errors are translated, so early failures leak keys');
});

test('the wrapper preserves the failure, changing only its text', () => {
  assert.match(bridge, /error instanceof Error/,
    'an Error is no longer recognised, so its message is not rewritten');
  assert.match(bridge, /error\.message = described/,
    'the message is not replaced with the translation');
  assert.match(bridge, /\.catch\(/,
    'a rejected promise is not handled, which is how bindings fail');
});

// A binding that is not there at all — a browser preview, a test — must not
// bring the app down on startup.
test('missing bindings are survivable', () => {
  assert.match(bridge, /if \(!go \|\| typeof go !== 'object'\)/,
    'the bridge assumes the bindings exist');
});
