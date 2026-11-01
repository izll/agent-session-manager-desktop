import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const source = readFileSync(new URL('../src/lib/utils/tokenUsage.ts', import.meta.url), 'utf8');
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText;
const { groupUsage, usageGroupKey, usageValue, formatTokens, formatShare, shortDirectory, filterUsageGroups, searchFindsConversation, usageConversationRows } =
  await import(`data:text/javascript,${encodeURIComponent(compiled)}`);

function item(overrides = {}) {
  return {
    machine: 'local', machineName: '', conversation: 'c1', agent: 'claude', cwd: '/work/api', lastUnix: 100,
    model: 'opus', subagent: false, input: 0, output: 0, cacheRead: 0, cacheWrite: 0, responses: 1,
    name: '', title: '', background: false, continues: '', projectId: '', projectName: '', sessionId: 's1', sessionName: 'API', tabName: '',
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

const searchable = () => groupUsage([
  item({ sessionId: 'mad', sessionName: 'MADisoCAD_v1', tabName: 'claude tab', conversation: '27632e09-68b7', cwd: '/work/sparrow', output: 75 }),
  item({ sessionId: 'site', sessionName: 'Site', conversation: 'bbbb', cwd: '/work/site', model: 'gpt-6', output: 25 }),
], 'session', 'new').groups;

test('the filter finds a session by anything that identifies it', () => {
  for (const query of ['madisocad', 'CLAUDE TAB', 'sparrow', '27632e09', '  madisocad  ']) {
    assert.deepEqual(filterUsageGroups(searchable(), query).map((g) => g.key), ['session:mad'], query);
  }
  assert.deepEqual(filterUsageGroups(searchable(), 'gpt-6').map((g) => g.key), ['session:site']);
  assert.equal(filterUsageGroups(searchable(), 'nothing like it').length, 0);
  assert.equal(filterUsageGroups(searchable(), '').length, 2);
});

// Worked out again from the filtered rows, whatever is found would read as 100%.
test('a filtered row keeps its share of everything', () => {
  assert.equal(filterUsageGroups(searchable(), 'site')[0].share, 0.25);
});

test('only a search for a conversation itself opens the row', () => {
  const [mad] = searchable();
  assert.equal(searchFindsConversation(mad, '27632E09'), true);
  assert.equal(searchFindsConversation(mad, 'claude tab'), true);
  assert.equal(searchFindsConversation(mad, 'madisocad'), false);
  assert.equal(searchFindsConversation(mad, ''), false);
});

const shape = (rows) => rows.map((row) => row.kind === 'tab' ? `# ${row.tabName}` : `${' '.repeat(row.depth)}${row.conversation.conversation}`);

test('conversations read by tab, the largest tab first, children under their parent', () => {
  const { groups } = groupUsage([
    item({ conversation: 'small', tabName: 'review', output: 10 }),
    item({ conversation: 'main', tabName: 'build', output: 20 }),
    item({ conversation: 'bg', tabName: 'build', continues: 'main', background: true, output: 70 }),
  ], 'session', 'new');
  assert.deepEqual(shape(usageConversationRows(groups[0], true)), ['# build', 'main', ' bg', '# review', 'small']);
  assert.equal(usageConversationRows(groups[0], true)[0].share, 0.9);
});

// The continuation may name no tab of its own; it goes where its parent is.
test('a child stands under its parent whatever tab it claims', () => {
  const { groups } = groupUsage([
    item({ conversation: 'main', tabName: 'build', output: 5 }),
    item({ conversation: 'bg', tabName: '', continues: 'main', output: 5 }),
  ], 'session', 'new');
  assert.deepEqual(shape(usageConversationRows(groups[0], true)), ['# build', 'main', ' bg']);
});

test('a child whose parent is out of the period stands alone', () => {
  const { groups } = groupUsage([item({ conversation: 'bg', continues: 'gone', output: 5 })], 'session', 'new');
  assert.deepEqual(shape(usageConversationRows(groups[0], false)), ['bg']);
});

test('without tabs the headings stay out', () => {
  const { groups } = groupUsage([item({ conversation: 'a', tabName: 'x', output: 1 })], 'model', 'new');
  assert.deepEqual(shape(usageConversationRows(groups[0], false)), ['a']);
});

test('logs that name each other in a circle lose nothing and do not hang', () => {
  const { groups } = groupUsage([
    item({ conversation: 'a', continues: 'b', output: 1 }),
    item({ conversation: 'b', continues: 'a', output: 1 }),
  ], 'session', 'new');
  const rows = usageConversationRows(groups[0], true).filter((row) => row.kind === 'conversation');
  assert.equal(rows.length, 2);
});

test('an agent can be searched for by its name, and that opens its row', () => {
  const { groups } = groupUsage([item({ conversation: 'c', name: 'legacy-form', output: 1 })], 'session', 'new');
  assert.equal(filterUsageGroups(groups, 'legacy').length, 1);
  assert.equal(searchFindsConversation(groups[0], 'legacy'), true);
});

test('an automatic title can be searched for too', () => {
  const { groups } = groupUsage([item({ conversation: 'c', title: 'Admin felület', output: 1 })], 'session', 'new');
  assert.equal(filterUsageGroups(groups, 'admin').length, 1);
  assert.equal(searchFindsConversation(groups[0], 'admin'), true);
});

