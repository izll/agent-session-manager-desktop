/**
 * Tests for the checkpoints dialog's decisions.
 *
 * Same arrangement as fileMatch.test.mjs: Node's built-in runner, with the
 * TypeScript transpiled by the esbuild that Vite already pulls in.
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
const dir = mkdtempSync(join(tmpdir(), 'asmgr-checkpoints-'));
const jsPath = join(dir, 'checkpoints.mjs');
writeFileSync(
  jsPath,
  transformSync(readFileSync(join(here, 'checkpoints.ts'), 'utf8'), { loader: 'ts', format: 'esm' }).code
);
const { checkpointsUnavailable, checkpointName, checkpointDifference, sessionAgentBusy } =
  await import(pathToFileURL(jsPath).href);
process.on('exit', () => rmSync(dir, { recursive: true, force: true }));

const texts = { untitled: 'Checkpoint', beforeRestore: 'Before restore' };
const cp = (over = {}) => ({ label: '', kind: 'manual', files: 0, statsKnown: true, ...over });

test('a remote tab is reported as remote even though it is not a repository here', () => {
  assert.equal(checkpointsUnavailable({ remote: true, isGitRepo: false }), 'remote');
  assert.equal(checkpointsUnavailable({ remote: true, isGitRepo: true }), 'remote');
  assert.equal(checkpointsUnavailable({ remote: false, isGitRepo: false }), 'notRepository');
  assert.equal(checkpointsUnavailable({ remote: false, isGitRepo: true }), '');
});

test('a row is named by its label, else by what made it', () => {
  assert.equal(checkpointName(cp({ label: '  before refactor ' }), texts), 'before refactor');
  assert.equal(checkpointName(cp({ kind: 'beforeRestore' }), texts), 'Before restore');
  assert.equal(checkpointName(cp(), texts), 'Checkpoint');
  assert.equal(checkpointName(cp({ label: 'mine', kind: 'beforeRestore' }), texts), 'mine');
});

test('the difference from now is unknown, the same, or differs', () => {
  assert.equal(checkpointDifference(cp({ statsKnown: false, files: 3 })), 'unknown');
  assert.equal(checkpointDifference(cp({ files: 0 })), 'same');
  assert.equal(checkpointDifference(cp({ files: 2 })), 'differs');
});

test('any busy tab of the session counts as a busy agent', () => {
  assert.equal(sessionAgentBusy('s', { s: 'busy' }, {}), true);
  assert.equal(sessionAgentBusy('s', { s: 'idle' }, { s: [{ activity: 'idle' }, { activity: 'busy' }] }), true);
  assert.equal(sessionAgentBusy('s', { s: 'waiting' }, { s: [{ activity: 'idle' }] }), false);
  // Another session's agent is not this one's.
  assert.equal(sessionAgentBusy('s', { other: 'busy' }, { other: [{ activity: 'busy' }] }), false);
  assert.equal(sessionAgentBusy('', { '': 'busy' }, {}), false);
});
