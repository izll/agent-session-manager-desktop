import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// --- the change announcement -------------------------------------------------

function collectChanges(previous, next) {
  const fired = [];
  for (const [sessionId, to] of Object.entries(next)) {
    const from = previous[sessionId];
    if (from === undefined || from === to) continue;
    fired.push({ sessionId, from, to });
  }
  return fired;
}

test('finishing work is announced', () => {
  assert.deepEqual(collectChanges({ a: 'busy' }, { a: 'idle' }),
    [{ sessionId: 'a', from: 'busy', to: 'idle' }]);
});

test('an unchanged session is not announced', () => {
  assert.deepEqual(collectChanges({ a: 'busy' }, { a: 'busy' }), []);
});

test('a session seen for the first time is not a change', () => {
  assert.deepEqual(collectChanges({}, { a: 'idle', b: 'busy' }), [],
    'the first poll after a project loads would otherwise fire for everything');
});

test('several sessions changing are each announced', () => {
  const fired = collectChanges({ a: 'busy', b: 'idle' }, { a: 'waiting', b: 'busy' });
  assert.equal(fired.length, 2);
});

// --- what the pool does with it ---------------------------------------------

function shouldRepaint({ detail, activeKey = 'k', entry = { sessionId: 'a' }, disposed = false }) {
  if (disposed) return false;
  if (!detail || detail.from !== 'busy') return false;
  if (!activeKey) return false;
  if (!entry) return false;
  if (entry.sessionId !== detail.sessionId) return false;
  if (entry.awaitingRedrawTimer !== undefined) return false;
  return true;
}

test('the visible session leaving busy is repainted', () => {
  assert.equal(shouldRepaint({ detail: { sessionId: 'a', from: 'busy', to: 'idle' } }), true);
});

test('a session becoming busy is left alone', () => {
  assert.equal(shouldRepaint({ detail: { sessionId: 'a', from: 'idle', to: 'busy' } }), false,
    'a pane about to draw for itself must not be resized mid-render');
});

test('a change in a session that is not on screen is ignored', () => {
  assert.equal(shouldRepaint({
    detail: { sessionId: 'other', from: 'busy', to: 'idle' },
    entry: { sessionId: 'a' },
  }), false);
});

test('nothing happens after dispose', () => {
  assert.equal(shouldRepaint({
    disposed: true, detail: { sessionId: 'a', from: 'busy', to: 'idle' },
  }), false);
});

// --- the source really does this --------------------------------------------

test('activities announces changes and forgets them when polling stops', () => {
  const src = readFileSync(new URL('../src/lib/stores/activities.ts', import.meta.url), 'utf8');
  assert.match(src, /ACTIVITY_CHANGED_EVENT/, 'the event is gone');
  assert.match(src, /from === undefined \|\| from === to/, 'first-sight sessions are no longer skipped');
  const stop = src.slice(src.indexOf('export function stopActivityPolling'));
  assert.match(stop, /lastActivities = \{\}/,
    'stale state across a project switch would report changes that never happened');
});

test('the pool listens for it and only on the way out of busy', () => {
  const src = readFileSync(new URL('../src/lib/utils/terminalPool.ts', import.meta.url), 'utf8');
  const body = src.slice(src.indexOf('private watchActivityChanges'),
                         src.indexOf('private stopWatchingActivityChanges'));
  assert.match(body, /detail\.from !== 'busy'/, 'it no longer filters on leaving busy');
  assert.match(body, /sessionId !== detail\.sessionId/, 'it would repaint for another session');
  assert.match(body, /requestRedraw\(entry\)/, 'it repaints nothing');
  assert.doesNotMatch(body, /RefreshWindow/, 'the Ctrl-L refresh must never run automatically');
  const dispose = src.slice(src.indexOf('async dispose()'));
  assert.match(dispose.slice(0, 500), /stopWatchingActivityChanges\(\)/,
    'the listener would outlive the pool');
});
