import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * A subtask's completion is spelled two different ways by the two backends.
 *
 * Task Master sends a `status` string; the app's own storage sends a `done`
 * boolean and has no status field on subtasks at all. Reading only
 * `status === 'done'` left every checkbox unticked outside MCP mode, and
 * writing `status` back stored a field nothing reads — so a tick saved
 * "successfully" and came back empty, which looks exactly like a lost click.
 */
const panel = readFileSync(
  new URL('../src/lib/components/MainPanel/TaskPanel.svelte', import.meta.url),
  'utf8',
);

// Comments may still describe the old form; only code counts. The helper's own
// body is excluded too — it is the one place that legitimately reads both
// spellings, and matching it would flag the fix as the bug.
const code = panel.split('\n')
  .filter((line) => !/^\s*(\/\/|\*|\/\*|<!--)/.test(line))
  .filter((line) => !/subtask\.done === true/.test(line))
  .join('\n');

assert.doesNotMatch(
  code,
  /subtask\.status === 'done'/,
  'reading status alone misses the boolean the local store sends',
);
assert.doesNotMatch(
  code,
  /s\.status === 'done'/,
  'the subtask counter has the same problem as the checkbox',
);
assert.match(
  panel,
  /function isSubtaskDone/,
  'both spellings should be resolved in one place, not at each call site',
);
assert.match(
  panel,
  /subtask\.done === true \|\| subtask\.status === 'done'/,
  'the helper has to accept both the boolean and the status string',
);

/**
 * And the write path has to use the operation the storage actually has.
 *
 * Writing `{...sub, status}` into the local store produced a subtask carrying
 * a field nothing reads. ToggleSubtask is what that storage exposes, and it
 * flips the boolean the reader looks at.
 */
const store = readFileSync(
  new URL('../src/lib/stores/tasks.ts', import.meta.url),
  'utf8',
);

const setStatus = store.match(/export async function setSubtaskStatus[\s\S]*?\n}/);
assert.ok(setStatus, 'setSubtaskStatus is missing');
assert.match(
  setStatus[0],
  /App\.ToggleSubtask\(/,
  'the local branch must toggle through storage rather than write a status field',
);
assert.doesNotMatch(
  setStatus[0],
  /\{ \.\.\.sub, status \}/,
  'that is the write that produced a field nothing reads',
);

console.log('subtaskDone: ok');
