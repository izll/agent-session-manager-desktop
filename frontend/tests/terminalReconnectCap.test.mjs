import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const terminal = readFileSync(
  new URL('../src/lib/utils/terminal.ts', import.meta.url), 'utf8');

// A tab whose window is gone refuses every attach — an agent that is not
// installed dies the instant it opens, and the window goes with it. Retrying
// on a timer made the multiplexer's own "can't find window N" reappear every
// 750ms, which is the flicker the user sees.
//
// A refusal the backend can name arrives as a close with a reason and is never
// retried (terminalRefusal.test.mjs). What is left closed without one, so a
// dropped client and a window that vanished mid-attach look identical from
// here. A few attempts cover the first and stop short of a loop.
test('reconnecting gives up rather than retrying for ever', () => {
  const close = terminal.slice(terminal.indexOf('ws.onclose'));
  assert.match(close, /reconnectFailures/,
    'nothing counts failed reconnects, so they repeat indefinitely');
  assert.match(close, /maxReconnects/,
    'there is no limit on the number of attempts');
});

// A long-running tab that drops once an hour must not exhaust the budget.
test('a successful connection clears the budget', () => {
  const open = terminal.slice(terminal.indexOf('ws.onopen'), terminal.indexOf('ws.onopen') + 400);
  assert.match(open, /reconnectFailures = 0/,
    'the failure count is never reset, so a long session eventually stops reconnecting');
});
