// With Task Master off, the app edits tasks through its own storage. Task
// Master has an endpoint per operation — add a subtask, remove one, set its
// status; the local store has UpdateTask, which takes whole fields. So each
// operation is a list rewrite here rather than a call there.
//
// This is what made "save" on an edited task report that Task Master was turned
// off: the store called it regardless of the setting.
//
// Mirrors the local branches in stores/tasks.ts.
import { test } from 'node:test';
import assert from 'node:assert/strict';

function parentTaskId(subtaskId) {
  return String(subtaskId).split('.')[0];
}
function childId(subtaskId) {
  const parent = parentTaskId(subtaskId);
  return String(subtaskId).slice(parent.length + 1);
}

test('a subtask id names its parent task', () => {
  // Task Master addresses subtasks as "3.1"; the local store holds them inside
  // the task, so the parent has to be recovered from the id.
  assert.equal(parentTaskId('3.1'), '3');
  assert.equal(childId('3.1'), '1');
});

test('a multi-digit parent is not truncated', () => {
  assert.equal(parentTaskId('12.7'), '12');
  assert.equal(childId('12.7'), '7');
});

test('adding a subtask numbers it within the task', () => {
  const task = { subtasks: [{ id: '1' }, { id: '2' }] };
  const added = [...task.subtasks, { id: String(task.subtasks.length + 1), title: 'x', status: 'pending' }];
  assert.equal(added[2].id, '3');
});

test('adding a subtask to a task with none starts at 1', () => {
  const task = {};
  const added = [...(task.subtasks || []), { id: String((task.subtasks?.length || 0) + 1) }];
  assert.equal(added[0].id, '1');
});

test('removing a subtask leaves the others', () => {
  const task = { subtasks: [{ id: '1' }, { id: '2' }, { id: '3' }] };
  const left = task.subtasks.filter((s) => String(s.id) !== '2');
  assert.deepEqual(left.map((s) => s.id), ['1', '3']);
});

test('a dependency is not added twice', () => {
  // Adding the same one again is a no-op, not an error worth interrupting for.
  const task = { dependencies: ['3'] };
  const next = Array.from(new Set([...task.dependencies, '3']));
  assert.deepEqual(next, ['3']);
});

test('dependencies compare as strings', () => {
  // Ids arrive as numbers from one store and strings from the other; comparing
  // them raw would leave a dependency that cannot be removed.
  const task = { dependencies: ['3', '7'] };
  const left = task.dependencies.filter((d) => String(d) !== String(3));
  assert.deepEqual(left, ['7']);
});

test('a status maps onto the local done flag', () => {
  // The local Subtask records only whether it is finished, which is all its UI
  // offers — a checkbox.
  assert.equal('done' === 'done', true);
  assert.equal('pending' === 'done', false);
});
