import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';

const refusal = await import('../src/lib/utils/terminalRefusal.ts');
const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');

// The placeholder for a tab that could not be attached always said
// "WebSocket connection failed": every refusal was an HTTP error sent before
// the upgrade, and a browser's WebSocket reports all of those alike. The
// backend now completes the handshake and closes with a reason instead.

test('a refusal is read from the close', () => {
  assert.deepEqual(refusal.refusalFromClose(4001, 'session-not-running'),
    { reason: 'session-not-running', detail: '' });
  assert.deepEqual(refusal.refusalFromClose(4003, 'project-locked'),
    { reason: 'project-locked', detail: '' });
  assert.deepEqual(refusal.refusalFromClose(4004, 'attach-failed: ssh: handshake failed'),
    { reason: 'attach-failed', detail: 'ssh: handshake failed' },
    'the detail after the key is the only clue to what went wrong on a server');
});

// An ordinary close is a connection problem, and those are reconnected to.
test('only the application range is a refusal', () => {
  for (const code of [1000, 1001, 1006, 1011, 3000, 5000]) {
    assert.equal(refusal.refusalFromClose(code, 'session-not-running'), null, `code ${code}`);
  }
});

test('a reason this build does not know still explains itself', () => {
  assert.deepEqual(refusal.refusalFromClose(4099, 'quota-exceeded'),
    { reason: 'attach-failed', detail: 'quota-exceeded' });
});

// The keys are a contract with terminal_ws.go. A reason the backend sends that
// the frontend does not know would fall back to the generic sentence.
test('every reason the backend sends is one the frontend knows', () => {
  const backend = read('../../terminal_ws.go');
  const sent = [...backend.matchAll(/(?:refuseAttach\([^)]*|closeWithReason\([^,]+,\s*\w+),\s*(?:fmt\.Sprintf\()?"([a-z-]+)/g)]
    .map(match => match[1]);
  assert.ok(sent.length >= 4, `found only ${sent.length} refusals in terminal_ws.go`);
  for (const reason of sent) {
    assert.equal(refusal.refusalFromClose(4000, reason).reason, reason,
      `the backend refuses with "${reason}", which the frontend does not know`);
  }
});

// A refusal is a verdict. Retrying it is a loop: the handshake completes each
// time, which resets the failure budget, so the cap would never be reached.
test('a refused attach is not retried', () => {
  const terminal = read('../src/lib/utils/terminal.ts');
  const close = terminal.slice(terminal.indexOf('ws.onclose = (ev)'));
  const refused = close.indexOf('refusalFromClose(ev.code, ev.reason)');
  const budget = close.indexOf('reconnectFailures');
  assert.ok(refused > 0, 'the close is not checked for a refusal');
  assert.ok(refused < budget, 'a refusal is only noticed after a reconnect was counted');
  const branch = close.slice(refused, budget);
  assert.match(branch, /dispatchEvent\(new CustomEvent\(TERMINAL_REFUSED_EVENT/,
    'the pane is never told why');
  assert.match(branch, /return;/, 'the refusal falls through to a reconnect');
});

test('the pane showing the tab takes the refusal down and says why', () => {
  const pane = read('../src/lib/components/MainPanel/Terminal.svelte');
  assert.match(pane, /poolContainerEl\.addEventListener\(TERMINAL_REFUSED_EVENT, handleAttachRefused/);
  const at = pane.indexOf('function handleAttachRefused');
  const body = pane.slice(at, pane.indexOf('\n  }\n', at));
  assert.match(body, /refusal\.sessionId !== currentTargetSessionId\(\)/,
    'another tab\'s refusal would take this one down');
  assert.match(body, /refusal\.windowIdx !== currentTargetWindowIdx\(\)/);
  assert.match(body, /status !== 'running'\) return;/,
    'a session the user stopped is reported as a tab that failed to open');
  assert.match(body, /poolChangeGeneration\+\+/,
    'a show still settling marks the refused tab attached again');
  assert.match(body, /isAttached = false/);
  assert.match(body, /\$t\(refusalMessageKey\(refusal\)\)/, 'the reason is shown untranslated');
});

test('every refusal is translated', () => {
  const dir = new URL('../src/lib/i18n/locales/', import.meta.url);
  const locales = readdirSync(dir).filter(name => name.endsWith('.json'));
  assert.ok(locales.length >= 20);
  const en = JSON.parse(readFileSync(new URL('en.json', dir), 'utf8'));
  for (const name of locales) {
    const strings = JSON.parse(readFileSync(new URL(name, dir), 'utf8'));
    for (const key of refusal.refusalMessageKeys) {
      assert.ok(strings[key]?.trim(), `${name} has no ${key}`);
      if (name !== 'en.json') assert.notEqual(strings[key], en[key], `${name} left ${key} in English`);
    }
  }
});
