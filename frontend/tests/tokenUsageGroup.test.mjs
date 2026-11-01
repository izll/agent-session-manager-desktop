import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const source = readFileSync(new URL('../src/lib/utils/tokenUsage.ts', import.meta.url), 'utf8');
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText;
const { groupUsage, usageGroupKey, usageValue, formatTokens, formatShare, shortDirectory } =
  await import(`data:text/javascript,${encodeURIComponent(compiled)}`);

function item(overrides = {}) {
  return {
    machine: 'local', machineName: '', conversation: 'c1', agent: 'claude', cwd: '/work/api', lastUnix: 100,
    model: 'opus', subagent: false, input: 0, output: 0, cacheRead: 0, cacheWrite: 0, responses: 1,
    projectId: '', projectName: '', sessionId: 's1', sessionName: 'API', tabName: '',
    ...overrides,
  };
}

test('cache reads stay out of the default share and go in on request', () => {
  const totals = { input: 1, output: 2, cacheRead: 100, cacheWrite: 3 };
  assert.equal(usageValue(totals, 'new'), 6);
  assert.equal(usageValue(totals, 'all'), 106);
});

test('groups are sorted by use and their shares add up', () => {
  const { groups, value, totals } = groupUsage([
    item({ sessionId: 'small', conversation: 'a', output: 25 }),
    item({ sessionId: 'big', conversation: 'b', output: 75 }),
  ], 'session', 'new');
  assert.deepEqual(groups.map((g) => g.key), ['session:big', 'session:small']);
  assert.equal(groups[0].share, 0.75);
  assert.equal(groups[1].share, 0.25);
  assert.equal(value, 100);
  assert.equal(totals.output, 100);
});

test('a session adds up its conversations and models', () => {
  const { groups } = groupUsage([
    item({ conversation: 'a', model: 'opus', output: 10 }),
    item({ conversation: 'a', model: 'sonnet', output: 10 }),
    item({ conversation: 'b', model: 'opus', output: 60, tabName: 'review' }),
  ], 'session', 'new');
  assert.equal(groups.length, 1);
  const [largest, other] = groups[0].conversations;
  assert.equal(largest.conversation, 'b');
  assert.equal(largest.tabName, 'review');
  assert.equal(largest.share, 0.75);
  assert.deepEqual(other.models, ['opus', 'sonnet']);
});

test('the subagent share is measured on the same basis as the group', () => {
  const { groups } = groupUsage([
    item({ output: 30 }),
    item({ output: 70, subagent: true }),
  ], 'session', 'new');
  assert.equal(groups[0].subagentShare, 0.7);
});

test('nothing used means no share, not a division by zero', () => {
  const { groups } = groupUsage([item()], 'session', 'new');
  assert.equal(groups[0].share, 0);
  assert.equal(groups[0].subagentShare, 0);
});

test('a conversation in no session groups by machine and directory', () => {
  const outside = item({ sessionId: '', machine: 'srv', cwd: '/x' });
  assert.equal(usageGroupKey(outside, 'session'), 'dir:srv:/x');
  assert.notEqual(usageGroupKey(item({ sessionId: '', cwd: '/x' }), 'session'), 'dir:srv:/x');
});

// Both have an empty project id: only one of them is in the default project.
test('no session is not the same as the default project', () => {
  assert.equal(usageGroupKey(item({ sessionId: 's1', projectId: '' }), 'project'), 'project:');
  assert.equal(usageGroupKey(item({ sessionId: '', projectId: '' }), 'project'), 'outside');
});

test('machine and model groups cross sessions', () => {
  const items = [
    item({ sessionId: 'a', machine: 'local', model: 'opus', output: 1 }),
    item({ sessionId: 'b', machine: 'local', model: 'opus', output: 1 }),
    item({ sessionId: 'c', machine: 'srv', model: 'sonnet', output: 1 }),
  ];
  assert.equal(groupUsage(items, 'machine', 'new').groups.length, 2);
  assert.equal(groupUsage(items, 'model', 'new').groups.length, 2);
  assert.equal(groupUsage(items, 'session', 'new').groups.length, 3);
});

test('token counts read at a glance', () => {
  assert.equal(formatTokens(0), '0');
  assert.equal(formatTokens(999), '999');
  assert.equal(formatTokens(1500), '1.5 k');
  assert.equal(formatTokens(26852239), '26.9 M');
  assert.equal(formatTokens(150000000), '150 M');
  assert.equal(formatTokens(7533986121), '7.5 G');
});

test('a very small share does not read as nothing', () => {
  assert.equal(formatShare(0), '0%');
  assert.equal(formatShare(0.0004), '<0.1%');
  assert.equal(formatShare(0.51), '51.0%');
});

test('a directory shortens to what identifies it', () => {
  assert.equal(shortDirectory('/home/me/work/api'), 'work/api');
  assert.equal(shortDirectory('C:\\Users\\me\\api'), 'me/api');
  assert.equal(shortDirectory('/'), '/');
  assert.equal(shortDirectory(''), '');
});
