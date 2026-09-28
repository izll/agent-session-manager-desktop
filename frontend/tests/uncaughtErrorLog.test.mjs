import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// An error that broke something in the webview left no trace outside a
// devtools build: Ctrl+K doing nothing had nothing in the log to go on.

test('errors are described with their stack, anything else as text', async () => {
  const { describeUncaught } = await import('../src/lib/utils/uncaughtErrorLog.ts');
  const error = new TypeError('x is undefined');
  assert.match(describeUncaught(error), /TypeError: x is undefined/);
  assert.equal(describeUncaught('plain'), 'plain');
  assert.equal(describeUncaught({ a: 1 }), '{"a":1}');
});

test('a failure repeating in a loop cannot flood the log', async () => {
  const { throttle } = await import('../src/lib/utils/uncaughtErrorLog.ts');
  let now = 0;
  const limit = throttle(3, 1000, () => now);
  const results = Array.from({ length: 5 }, () => limit.allow().ok);
  assert.deepEqual(results, [true, true, true, false, false]);
  now = 1000;
  assert.deepEqual(limit.allow(), { ok: true, dropped: 2 }, 'the next window says how many were dropped');
});

test('it is installed before the app mounts', () => {
  const main = readFileSync(new URL('../src/main.ts', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
  const installed = main.indexOf('installUncaughtErrorLog(LogFrontend)');
  assert.ok(installed > 0, 'uncaught errors are not logged');
  assert.ok(installed < main.indexOf('mount(App'), 'installed only after the app mounts');
});
