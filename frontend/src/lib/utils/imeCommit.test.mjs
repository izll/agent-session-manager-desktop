/**
 * Accented characters from the input method reach the pane exactly once.
 *
 * The event sequences below are the ones WebKitGTK produced with IBus and the
 * Hungarian layout, traced in a nested X server: an accented key is a 229
 * keydown, an insertFromComposition input and a compositionend with no
 * compositionstart; a dead key is a full composition.
 *
 *   cd frontend && npm test
 */
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, writeFileSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { transformSync } from 'esbuild';

const here = dirname(fileURLToPath(import.meta.url));
const dir = mkdtempSync(join(tmpdir(), 'asmgr-imecommit-'));
const jsPath = join(dir, 'imeCommit.mjs');
writeFileSync(
  jsPath,
  transformSync(readFileSync(join(here, 'imeCommit.ts'), 'utf8'), { loader: 'ts', format: 'esm' }).code
);
const { guardImeCommits } = await import(pathToFileURL(jsPath).href);
process.on('exit', () => rmSync(dir, { recursive: true, force: true }));

/** A container, its xterm textarea, and what the guard sent to the pane. */
function setup({ accepts = () => true } = {}) {
  const listeners = new Map();
  const container = {
    addEventListener: (type, fn) => listeners.set(type, fn),
    removeEventListener: (type) => listeners.delete(type),
  };
  const textarea = { value: '' };
  const sent = [];
  const terminal = { textarea, input: (data) => sent.push(data) };
  const unguard = guardImeCommits(container, terminal, accepts);

  /** Dispatch to the guard; returns whether it stopped the event reaching xterm. */
  const fire = (type, props = {}) => {
    let stopped = false;
    const ev = { type, target: textarea, stopImmediatePropagation: () => { stopped = true; }, ...props };
    listeners.get(type)?.(ev);
    return stopped;
  };

  /** One accented key as IBus delivers it; the browser inserts the text. */
  const commit = (ch) => {
    fire('keydown', { keyCode: 229 });
    textarea.value += ch;
    const inputStopped = fire('input', { inputType: 'insertFromComposition', data: ch });
    const endStopped = fire('compositionend', { data: ch });
    return { inputStopped, endStopped };
  };

  return { fire, commit, textarea, sent, listeners, unguard };
}

test('each accented character is sent once and kept from xterm', () => {
  const { commit, sent, textarea } = setup();
  for (const ch of 'éáőű') {
    const { inputStopped, endStopped } = commit(ch);
    assert.equal(inputStopped, true);
    assert.equal(endStopped, true, 'the orphaned compositionend must not reach xterm');
    assert.equal(textarea.value, '', 'xterm must find nothing left to send');
  }
  assert.deepEqual(sent, ['é', 'á', 'ő', 'ű']);
});

test('text left in the textarea by plain keys is cleared on an IME keydown', () => {
  const { fire, textarea } = setup();
  textarea.value = 'abc';
  fire('keydown', { keyCode: 229 });
  assert.equal(textarea.value, '');
});

test('a plain keydown leaves the textarea alone', () => {
  const { fire, textarea } = setup();
  textarea.value = 'abc';
  fire('keydown', { keyCode: 65 });
  assert.equal(textarea.value, 'abc');
});

test('a compositionend whose commit was not taken over still reaches xterm', () => {
  const { fire, sent, textarea } = setup();
  fire('keydown', { keyCode: 229 });
  textarea.value = 'á';
  assert.equal(fire('input', { inputType: 'insertFromComposition', data: null }), false);
  assert.equal(fire('compositionend', { data: 'á' }), false, 'xterm is the only one left to send it');
  assert.equal(textarea.value, 'á');
  assert.deepEqual(sent, []);
});

test('a taken-over commit does not swallow a later, unrecognised one', () => {
  const { fire, commit, sent } = setup();
  // Taken over, but no compositionend follows (the insertText form).
  fire('keydown', { keyCode: 229 });
  assert.equal(fire('input', { inputType: 'insertText', data: 'ö' }), true);
  // The next commit is not recognised, so its compositionend must get through.
  fire('keydown', { keyCode: 229 });
  assert.equal(fire('input', { inputType: 'insertReplacementText', data: 'ü' }), false);
  assert.equal(fire('compositionend', { data: 'ü' }), false);
  assert.deepEqual(sent, ['ö']);
  // And a recognised one after that is handled as before.
  assert.deepEqual(commit('é'), { inputStopped: true, endStopped: true });
  assert.deepEqual(sent, ['ö', 'é']);
});

test('plain keys are left to xterm', () => {
  const { fire, sent } = setup();
  fire('keydown', { keyCode: 69 });
  assert.equal(fire('input', { inputType: 'insertText', data: 'é' }), false);
  assert.deepEqual(sent, []);
});

test('an insertText commit after a 229 keydown is taken over too', () => {
  const { fire, sent } = setup();
  fire('keydown', { keyCode: 229 });
  assert.equal(fire('input', { inputType: 'insertText', data: 'ö' }), true);
  assert.deepEqual(sent, ['ö']);
});

test('a dead-key composition is left entirely to xterm', () => {
  const { fire, sent, textarea } = setup();
  fire('keydown', { keyCode: 229 });
  assert.equal(fire('compositionstart', { data: '' }), false);
  textarea.value = '^';
  assert.equal(fire('input', { inputType: 'insertCompositionText', data: '^' }), false);
  fire('keydown', { keyCode: 229 });
  assert.equal(textarea.value, '^', 'mid-composition text must survive a keydown');
  textarea.value = 'ô';
  assert.equal(fire('input', { inputType: 'insertFromComposition', data: 'ô' }), false);
  assert.equal(fire('compositionend', { data: 'ô' }), false);
  // xterm reads the composed text a tick later; a keydown in between must not wipe it.
  fire('keydown', { keyCode: 229 });
  assert.equal(textarea.value, 'ô');
  assert.deepEqual(sent, []);
});

test('a commit while a dialog owns the keyboard is swallowed, not sent', () => {
  const { commit, sent } = setup({ accepts: () => false });
  const { inputStopped } = commit('á');
  assert.equal(inputStopped, true);
  assert.deepEqual(sent, []);
});

test('events from elsewhere in the container are ignored', () => {
  const { listeners, sent } = setup();
  const other = { value: 'x' };
  listeners.get('input')({ target: other, inputType: 'insertFromComposition', data: 'á',
    stopImmediatePropagation: () => assert.fail('must not stop a foreign event') });
  assert.deepEqual(sent, []);
});

test('unguarding removes every listener', () => {
  const { listeners, unguard } = setup();
  unguard();
  assert.equal(listeners.size, 0);
});
