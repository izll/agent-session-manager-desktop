import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const source = readFileSync(new URL('../src/lib/stores/unsavedChanges.ts', import.meta.url), 'utf8');
const dir = mkdtempSync(join(tmpdir(), 'unsaved-'));
const js = join(dir, 'unsavedChanges.mjs');
writeFileSync(js, execFileSync('npx', ['esbuild', '--loader=ts', '--format=esm'], {
  input: source,
  encoding: 'utf8',
  cwd: new URL('..', import.meta.url).pathname,
}));

const { registerUnsavedGuard, afterUnsavedChanges } = await import(js);

// Dirty editors approve one at a time; the destructive action cannot run
// before every visible confirmation has continued.
{
  const prompts = [];
  let acted = false;
  const unregisterA = registerUnsavedGuard({
    isDirty: () => true,
    requestDiscard: (next) => prompts.push(next),
  });
  const unregisterClean = registerUnsavedGuard({
    isDirty: () => false,
    requestDiscard: () => assert.fail('a clean editor must not prompt'),
  });
  const unregisterB = registerUnsavedGuard({
    isDirty: () => true,
    requestDiscard: (next) => prompts.push(next),
  });

  afterUnsavedChanges(() => { acted = true; });
  assert.equal(prompts.length, 1);
  assert.equal(acted, false);
  prompts.shift()();
  assert.equal(prompts.length, 1);
  assert.equal(acted, false);
  prompts.shift()();
  assert.equal(acted, true);

  unregisterA();
  unregisterClean();
  unregisterB();
}

// Cancelling is fail-closed: an editor simply withholds its continuation, so
// quit/navigation never reaches the destructive action.
{
  let acted = false;
  const unregister = registerUnsavedGuard({
    isDirty: () => true,
    requestDiscard: () => {},
  });
  afterUnsavedChanges(() => { acted = true; });
  assert.equal(acted, false);
  unregister();
}

console.log('unsavedChanges: ok');
