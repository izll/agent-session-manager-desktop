import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import ts from 'typescript';

let stored = [{
  id: 'task', title: 'parent', description: '', status: 'pending', priority: 'medium',
  tags: [], dependencies: [], subtasks: [
    { id: '1', title: 'one', status: 'pending', done: false },
    { id: '4', title: 'four', status: 'pending', done: false },
    { id: 'legacy-id', title: 'legacy', status: 'pending', done: false },
  ],
}];
const updates = [];
globalThis.__taskApp = {
  GetTasks: async () => structuredClone(stored),
  UpdateTask: async (_sessionId, taskId, patch) => {
    updates.push(structuredClone(patch));
    stored = stored.map((task) => task.id === taskId ? { ...task, ...structuredClone(patch) } : task);
  },
};
globalThis.__taskModels = {};

const source = readFileSync(new URL('../src/lib/stores/tasks.ts', import.meta.url), 'utf8');
const dir = mkdtempSync(join(tmpdir(), 'local-subtask-runtime-'));
const output = join(dir, 'tasks.mjs');
const transpiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
}).outputText
  .replace("from 'svelte/store'", `from '${import.meta.resolve('svelte/store')}'`)
  .replace("import * as App from '../../../wailsjs/go/main/App';", 'const App = globalThis.__taskApp;')
  .replace("import { main } from '../../../wailsjs/go/models';", 'const main = globalThis.__taskModels;')
  .replace("import { activeProjectId } from './projects';", "const activeProjectId = writable('project-a');");
writeFileSync(output, transpiled);

const store = await import(output);
store.useMCPMode.set(false);
await store.loadTasks('session-one');

await store.addSubtask('session-one', 'task', 'new child', 'description');
let subtasks = stored[0].subtasks;
assert.equal(subtasks.at(-1).id, '5', 'max numeric ID + 1 avoids the surviving ID 4');
assert.equal(new Set(subtasks.map((subtask) => subtask.id)).size, subtasks.length, 'IDs remain unique with non-numeric siblings');

await Promise.all([
  store.addSubtask('session-one', 'task', 'rapid A'),
  store.addSubtask('session-one', 'task', 'rapid B'),
]);
subtasks = stored[0].subtasks;
assert.ok(subtasks.some((subtask) => subtask.title === 'rapid A'));
assert.ok(subtasks.some((subtask) => subtask.title === 'rapid B'), 'queued local RMW must retain both rapid additions');
assert.equal(new Set(subtasks.map((subtask) => subtask.id)).size, subtasks.length, 'queued additions allocate from the latest backend snapshot');

await store.setSubtaskStatus('session-one', 'task.1', 'done', 'local');
await store.setSubtaskStatus('session-one', 'task.1', 'done', 'local');
let first = stored[0].subtasks.find((subtask) => subtask.id === '1');
assert.equal(first.status, 'done');
assert.equal(first.done, true, 'repeating set(done) must not toggle back to false');

await store.setSubtaskStatus('session-one', 'task.1', 'pending', 'local');
await store.setSubtaskStatus('session-one', 'task.1', 'pending', 'local');
first = stored[0].subtasks.find((subtask) => subtask.id === '1');
assert.equal(first.status, 'pending');
assert.equal(first.done, false, 'repeating set(pending) must remain false');
assert.ok(updates.every((patch) => Array.isArray(patch.subtasks)), 'local mutations replace one exact subtask snapshot');

console.log('localSubtaskRuntime: ok');
