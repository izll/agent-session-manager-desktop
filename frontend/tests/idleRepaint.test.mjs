import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const QUIET_MS = 30_000;

// The decision the interval makes each tick, modelled so the conditions can be
// exercised; the source assertions below keep it honest.
function shouldRepaint({ disposed = false, hidden = false, activeKey = 'k',
                         entry = {}, now = 100_000 } = {}) {
  if (disposed) return false;
  if (hidden) return false;
  if (!activeKey) return false;
  if (!entry) return false;
  if (entry.awaitingRedrawTimer !== undefined) return false;
  if (now - (entry.lastOutputAt ?? 0) < QUIET_MS) return false;
  return true;
}

test('a pane silent for longer than the quiet period is repainted', () => {
  assert.equal(shouldRepaint({ entry: { lastOutputAt: 100_000 - QUIET_MS - 1 } }), true);
});

test('a pane that is actively drawing is left alone', () => {
  assert.equal(shouldRepaint({ entry: { lastOutputAt: 99_000 } }), false,
    'repainting a pane mid-render is how a TUI is made to flicker');
});

test('a pane that has never produced output is repainted', () => {
  assert.equal(shouldRepaint({ entry: {} }), true,
    'a pane that drew nothing at all is exactly the stale case');
});

test('nothing happens while the window is hidden', () => {
  assert.equal(shouldRepaint({ hidden: true, entry: {} }), false);
});

test('nothing happens after dispose', () => {
  assert.equal(shouldRepaint({ disposed: true, entry: {} }), false);
});

test('nothing happens with no visible pane', () => {
  assert.equal(shouldRepaint({ activeKey: null, entry: {} }), false);
});

test('a pane already awaiting a repaint is not asked twice', () => {
  assert.equal(shouldRepaint({ entry: { awaitingRedrawTimer: 1 } }), false);
});

test('the pool implements these guards', () => {
  const src = readFileSync(new URL('../src/lib/utils/terminalPool.ts', import.meta.url), 'utf8');
  const start = src.indexOf('private startIdleRepaint');
  assert.ok(start > 0, 'startIdleRepaint is gone');
  const body = src.slice(start, src.indexOf('private stopIdleRepaint'));
  assert.match(body, /this\.disposed/, 'no disposed guard');
  assert.match(body, /document\.hidden/, 'no hidden-window guard');
  assert.match(body, /awaitingRedrawTimer/, 'no in-flight-repaint guard');
  assert.match(body, /lastOutputAt/, 'no quiet-pane guard');
  assert.match(body, /requestRedraw\(entry\)/, 'it no longer repaints anything');
  // RedrawWindow sends no input; RefreshWindow's Ctrl-L would reach the agent.
  assert.doesNotMatch(body, /RefreshWindow/,
    'the idle path must never use the Ctrl-L refresh');
});

test('the timer is cleared on dispose so it cannot outlive the pool', () => {
  const src = readFileSync(new URL('../src/lib/utils/terminalPool.ts', import.meta.url), 'utf8');
  const dispose = src.slice(src.indexOf('async dispose()'));
  assert.match(dispose.slice(0, 400), /stopIdleRepaint\(\)/);
});
