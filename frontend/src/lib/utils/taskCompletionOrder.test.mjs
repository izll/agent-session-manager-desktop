// Finished tasks are a record of what was done, so they read best in the order
// they were ticked off — most recent first. They used to fall back to priority
// and then to the task id, which orders them by nothing the user chose.
//
// Mirrors compareCompletion in stores/tasks.ts.
import { test } from 'node:test';
import assert from 'node:assert/strict';

function compareCompletion(a, b) {
  const ca = a.completedAt || '';
  const cb = b.completedAt || '';
  if (ca && cb) return cb.localeCompare(ca);
  if (ca) return -1;
  if (cb) return 1;
  return 0;
}

const at = (iso) => ({ completedAt: iso });

test('the most recently finished comes first', () => {
  const older = at('2026-08-05T09:00:00Z');
  const newer = at('2026-08-05T15:00:00Z');
  assert.ok(compareCompletion(newer, older) < 0);
  assert.ok(compareCompletion(older, newer) > 0);
});

test('a task with a completion time outranks one without', () => {
  // Tasks finished before the field was recorded, or through a path that does
  // not set it. The one we know something about goes first.
  assert.ok(compareCompletion(at('2026-08-05T09:00:00Z'), {}) < 0);
  assert.ok(compareCompletion({}, at('2026-08-05T09:00:00Z')) > 0);
});

test('two tasks with no time are left to the caller', () => {
  // Zero rather than an arbitrary pick, so the existing priority-then-id
  // ordering still decides instead of the two shuffling between renders.
  assert.equal(compareCompletion({}, {}), 0);
});

test('ISO timestamps compare correctly as strings', () => {
  // The whole comparison rests on this: same length, same field order, zero
  // padded — so no date parsing is needed per comparison.
  const list = [
    at('2026-08-05T09:05:00Z'),
    at('2026-08-05T09:00:00Z'),
    at('2026-08-04T23:59:00Z'),
    at('2026-12-01T00:00:00Z'),
  ];
  const sorted = [...list].sort(compareCompletion).map((t) => t.completedAt);
  assert.deepEqual(sorted, [
    '2026-12-01T00:00:00Z',
    '2026-08-05T09:05:00Z',
    '2026-08-05T09:00:00Z',
    '2026-08-04T23:59:00Z',
  ]);
});

test('a task reopened loses its place among the finished', () => {
  // The backend clears completedAt when the status leaves "done", so an
  // un-ticked task cannot keep sorting as though it were still complete.
  const reopened = { completedAt: undefined };
  assert.equal(compareCompletion(reopened, {}), 0);
});
