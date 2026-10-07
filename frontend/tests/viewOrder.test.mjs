import test from 'node:test';
import assert from 'node:assert/strict';

const order = await import('../src/lib/utils/viewOrder.ts');

test('a stored order keeps every view once, and only the ones the app has', () => {
  assert.deepEqual(order.normalizeViewOrder(['diff', 'terminal']),
    ['diff', 'terminal', 'notes', 'tasks', 'browser']);
  assert.deepEqual(order.normalizeViewOrder(['notes', 'inbox', 'notes', 42, 'terminal']),
    ['notes', 'terminal', 'tasks', 'browser', 'diff']);
  assert.deepEqual(order.normalizeViewOrder(null), [...order.DEFAULT_VIEW_ORDER]);
  assert.deepEqual(order.normalizeViewOrder([]), [...order.DEFAULT_VIEW_ORDER]);
});

test('a view the stored order lacks is added at the end', () => {
  // Saved before "browser" existed: shown, after the views the user placed.
  assert.deepEqual(order.normalizeViewOrder(['tasks', 'terminal', 'notes', 'diff']),
    ['tasks', 'terminal', 'notes', 'diff', 'browser']);
});

test('a tab\'s own order wins, and an empty one follows the global', () => {
  assert.deepEqual(order.resolveViewOrder(['diff'], ['notes']),
    ['diff', 'terminal', 'notes', 'tasks', 'browser']);
  assert.deepEqual(order.resolveViewOrder([], ['notes']),
    ['notes', 'terminal', 'tasks', 'browser', 'diff']);
  assert.deepEqual(order.resolveViewOrder(undefined, undefined), [...order.DEFAULT_VIEW_ORDER]);
});

test('moving a view', () => {
  const start = [...order.DEFAULT_VIEW_ORDER];
  assert.deepEqual(order.moveView(start, 4, 0), ['diff', 'terminal', 'notes', 'tasks', 'browser']);
  assert.deepEqual(order.moveView(start, 0, 2), ['notes', 'tasks', 'terminal', 'browser', 'diff']);
  assert.deepEqual(order.moveView(start, 0, 9), start);
  assert.equal(order.isDefaultViewOrder(start), true);
  assert.equal(order.isDefaultViewOrder(order.moveView(start, 1, 2)), false);
});
