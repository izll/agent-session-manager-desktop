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

// The registry must be checked again after every confirmation. A different
// editor can become dirty while the first editor's modal is open.
{
  const prompts = [];
  let bDirty = false;
  let acted = false;
  const unregisterA = registerUnsavedGuard({
    isDirty: () => true,
    requestDiscard: (next) => prompts.push(['a', next]),
  });
  const unregisterB = registerUnsavedGuard({
    isDirty: () => bDirty,
    requestDiscard: (next) => prompts.push(['b', next]),
  });

  afterUnsavedChanges(() => { acted = true; });
  assert.equal(prompts[0][0], 'a');
  bDirty = true;
  prompts.shift()[1]();
  assert.equal(prompts[0][0], 'b');
  assert.equal(acted, false);
  prompts.shift()[1]();
  assert.equal(acted, true);

  unregisterA();
  unregisterB();
}

// Cancelling is fail-closed: an editor simply withholds its continuation, so
// quit/navigation never reaches the destructive action.
{
  let acted = false;
  let cancelled = false;
  let cancelPrompt;
  const unregister = registerUnsavedGuard({
    isDirty: () => true,
    requestDiscard: (_next, cancel) => { cancelPrompt = cancel; },
  });
  afterUnsavedChanges(() => { acted = true; }, () => { cancelled = true; });
  assert.equal(acted, false);
  cancelPrompt();
  assert.equal(cancelled, true);
  assert.equal(acted, false);
  unregister();
}

// Approval covers one exact draft revision, not the guard forever. Editor A
// can become dirty again while editor B's confirmation is open (for example a
// delayed save failure restores its failed draft); the destructive action must
// then return to A instead of losing that new state.
{
  let aDirty = true;
  let aRevision = 1;
  let bDirty = true;
  let acted = false;
  let prompt = '';
  let continueA;
  let continueB;
  const unregisterA = registerUnsavedGuard({
    isDirty: () => aDirty,
    revision: () => aRevision,
    requestDiscard: (next) => {
      prompt = 'a';
      continueA = () => { aDirty = false; next(); };
    },
  });
  const unregisterB = registerUnsavedGuard({
    isDirty: () => bDirty,
    revision: () => Number(bDirty),
    requestDiscard: (next) => {
      prompt = 'b';
      continueB = () => { bDirty = false; next(); };
    },
  });

  afterUnsavedChanges(() => { acted = true; });
  assert.equal(prompt, 'a');
  continueA();
  assert.equal(prompt, 'b');
  aDirty = true;
  aRevision++;
  continueB();
  assert.equal(acted, false);
  assert.equal(prompt, 'a');
  continueA();
  assert.equal(acted, true);

  unregisterA();
  unregisterB();
}

console.log('unsavedChanges: ok');
