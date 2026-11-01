import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const source = readFileSync(new URL('../src/lib/utils/passwordPicker.ts', import.meta.url), 'utf8');
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText;
const { filterPasswords, tabRunsAgent } = await import(`data:text/javascript,${encodeURIComponent(compiled)}`);

const entries = [
  { source: 'local', id: '1', name: 'Prod DB', username: 'root', group: '' },
  { source: 'keepassxc', id: '2', name: 'Mail', username: '', group: 'Work' },
];

test('a search finds by name, user and group, in any case', () => {
  assert.deepEqual(filterPasswords(entries, 'PROD').map((e) => e.id), ['1']);
  assert.deepEqual(filterPasswords(entries, 'root').map((e) => e.id), ['1']);
  assert.deepEqual(filterPasswords(entries, ' work ').map((e) => e.id), ['2']);
  assert.equal(filterPasswords(entries, '').length, 2);
  assert.equal(filterPasswords(entries, 'nothing').length, 0);
});

test('a tab running an agent is told from a shell', () => {
  const session = { agent: 'claude', followedWindows: [{ index: 1, agent: 'terminal' }, { index: 2, agent: 'codex' }, { index: 3, agent: 'custom' }] };
  assert.equal(tabRunsAgent(session, 0), true);
  assert.equal(tabRunsAgent(session, 1), false);
  assert.equal(tabRunsAgent(session, 2), true);
  assert.equal(tabRunsAgent(session, 3), false);
  assert.equal(tabRunsAgent({ agent: 'terminal' }, 0), false);
});

// Not knowing is the case to be careful in.
test('a tab that cannot be found counts as an agent', () => {
  assert.equal(tabRunsAgent(null, 0), true);
  assert.equal(tabRunsAgent(undefined, 0), true);
});

// The design rests on this: the interface holds names only.
test('the dialogs ask for no secret from the backend', () => {
  for (const name of ['PasswordPickerDialog', 'PasswordManagerDialog']) {
    const src = readFileSync(new URL(`../src/lib/components/Dialogs/${name}.svelte`, import.meta.url), 'utf8');
    assert.doesNotMatch(src, /App\.\w*Secret/);
    assert.doesNotMatch(src, /console\.|localStorage|sessionStorage/);
  }
});
