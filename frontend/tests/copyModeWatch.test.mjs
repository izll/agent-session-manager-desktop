import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createCopyModeWatch } from '../src/lib/utils/copyModeWatch.ts';

const NONE = { inMode: false, scrolled: false };

// A hand-driven clock and backend: each call waits until the test answers it.
function harness() {
  const calls = [];
  const ticks = [];
  const states = [];
  const answer = (kind, s, w) => new Promise((resolve) => calls.push({ kind, s, w, resolve }));
  const watch = createCopyModeWatch(
    { read: (s, w) => answer('read', s, w), settle: (s, w) => answer('settle', s, w) },
    (state) => states.push(state),
    { every: (fn) => { ticks.push(fn); return ticks.length; }, stop: (id) => { ticks[id - 1] = null; } },
  );
  const flush = () => new Promise((r) => setTimeout(r, 0));
  const tick = () => ticks.forEach((fn) => fn && fn());
  return { calls, states, watch, flush, tick };
}

test('showing a tab settles it once and reports the answer', async () => {
  const h = harness();
  h.watch.show('s1', 0);
  h.watch.show('s1', 0);
  assert.deepEqual(h.calls.map((c) => c.kind), ['settle']);
  h.calls[0].resolve({ inMode: true, scrolled: true });
  await h.flush();
  assert.deepEqual(h.states.at(-1), { inMode: true, scrolled: true });
});

test('while shown it is read on every tick, never twice at once', async () => {
  const h = harness();
  h.watch.show('s1', 2);
  h.calls[0].resolve(NONE);
  await h.flush();
  h.tick();
  h.tick();
  assert.deepEqual(h.calls.map((c) => [c.kind, c.s, c.w]), [['settle', 's1', 2], ['read', 's1', 2]]);
  h.calls[1].resolve({ inMode: true, scrolled: false });
  await h.flush();
  assert.deepEqual(h.states.at(-1), { inMode: true, scrolled: false });
  h.tick();
  assert.equal(h.calls.length, 3);
});

test('an answer about the tab shown before is dropped', async () => {
  const h = harness();
  h.watch.show('s1', 0);
  h.watch.show('s2', 1);
  h.calls[0].resolve({ inMode: true, scrolled: true });
  await h.flush();
  assert.deepEqual(h.states.at(-1), NONE);
  h.calls[1].resolve({ inMode: true, scrolled: false });
  await h.flush();
  assert.deepEqual(h.states.at(-1), { inMode: true, scrolled: false });
});

test('hiding stops the reads and clears the state', async () => {
  const h = harness();
  h.watch.show('s1', 0);
  h.calls[0].resolve({ inMode: true, scrolled: true });
  await h.flush();
  h.watch.hide();
  h.tick();
  assert.equal(h.calls.length, 1);
  assert.deepEqual(h.states.at(-1), NONE);
});

test('a failed read changes nothing', async () => {
  const h = harness();
  const watch = createCopyModeWatch(
    { read: () => Promise.reject(new Error('gone')), settle: () => Promise.reject(new Error('gone')) },
    (state) => h.states.push(state),
    { every: () => 1, stop: () => {} },
  );
  watch.show('s1', 0);
  await h.flush();
  assert.deepEqual(h.states, [NONE]);
});
