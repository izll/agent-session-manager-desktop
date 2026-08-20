import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import ts from 'typescript';

const source = readFileSync(new URL('../src/lib/stores/undo.ts', import.meta.url), 'utf8');
const dir = mkdtempSync(join(tmpdir(), 'undo-runtime-'));
const output = join(dir, 'undo.mjs');
const transpiled = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.ESNext,
    target: ts.ScriptTarget.ES2022,
  },
}).outputText;
writeFileSync(
  output,
  transpiled.replace("from 'svelte/store'", `from '${import.meta.resolve('svelte/store')}'`),
);

const { offerUndo, runUndo, undoState, dismissUndo } = await import(output);
const readStore = (store) => {
  let value;
  const unsubscribe = store.subscribe((next) => { value = next; });
  unsubscribe();
  return value;
};

let attempts = 0;
offerUndo({
  message: 'deleted',
  undo: async () => {
    attempts++;
    if (attempts === 1) throw new Error('backend refused');
  },
});

assert.equal(await runUndo(), false, 'a failed backend action is not reported as undone');
let state = readStore(undoState);
assert.equal(state.action?.message, 'deleted', 'the failed action remains retryable');
assert.match(state.error, /backend refused/, 'the failure is visible');

assert.equal(await runUndo(), true, 'the same action can be retried');
state = readStore(undoState);
assert.equal(state.action, null);
assert.equal(state.error, null);
assert.equal(attempts, 2);

let rejectOldUndo;
offerUndo({
  message: 'old action',
  undo: () => new Promise((_, reject) => { rejectOldUndo = reject; }),
});
const oldRun = runUndo();
offerUndo({ message: 'new action', undo: async () => {} });
rejectOldUndo(new Error('old action failed late'));
assert.equal(await oldRun, false);
state = readStore(undoState);
assert.equal(state.action?.message, 'new action', 'a late failure must not overwrite a newer undo offer');

dismissUndo();
console.log('undoRuntime: ok');
