import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

import { execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

// The real key rules (utils/diffFind.ts), driven the way DiffFindBar.svelte
// drives them: a non-null action is prevented and acted on. The source check at
// the bottom keeps the component on those rules.
const root = new URL('..', import.meta.url);
const dir = mkdtempSync(join(tmpdir(), 'diff-find-keys-'));
const js = join(dir, 'diffFind.mjs');
writeFileSync(js, execFileSync('npx', ['esbuild', '--loader=ts', '--format=esm'], {
  input: readFileSync(new URL('src/lib/utils/diffFind.ts', root), 'utf8'),
  encoding: 'utf8',
  cwd: root.pathname,
}));
const { findKeyAction } = await import(js);

function makeHandler() {
  const calls = [];
  const handler = (event) => {
    const e = { ...event, defaultPrevented: false };
    e.preventDefault = () => { e.defaultPrevented = true; };
    const action = findKeyAction(e);
    if (action === null) return e;
    e.preventDefault();
    calls.push(action);
    return e;
  };
  return { handler, calls };
}

const press = (key, mods = {}) => ({ key, ctrlKey: false, metaKey: false, shiftKey: false, ...mods });

test('the down arrow steps to the next match', () => {
  const { handler, calls } = makeHandler();
  const e = handler(press('ArrowDown'));
  assert.deepEqual(calls, [1]);
  assert.equal(e.defaultPrevented, true, 'the field must not also scroll or move a cursor');
});

test('the up arrow steps to the previous match', () => {
  const { handler, calls } = makeHandler();
  handler(press('ArrowUp'));
  assert.deepEqual(calls, [-1]);
});

test('Enter still steps forward and Shift+Enter back', () => {
  const { handler, calls } = makeHandler();
  handler(press('Enter'));
  handler(press('Enter', { shiftKey: true }));
  assert.deepEqual(calls, [1, -1], 'the arrows must not have displaced Enter');
});

test('F3 and Ctrl+G keep working', () => {
  const { handler, calls } = makeHandler();
  handler(press('F3'));
  handler(press('g', { ctrlKey: true }));
  assert.deepEqual(calls, [1, 1]);
});

test('Escape still closes the bar', () => {
  const { handler, calls } = makeHandler();
  handler(press('Escape'));
  assert.deepEqual(calls, ['close']);
});

test('ordinary typing is left alone', () => {
  const { handler, calls } = makeHandler();
  const e = handler(press('a'));
  assert.deepEqual(calls, []);
  assert.equal(e.defaultPrevented, false, 'typing into the field must not be blocked');
});

test('left and right arrows still move the caret in the field', () => {
  const { handler, calls } = makeHandler();
  const left = handler(press('ArrowLeft'));
  const right = handler(press('ArrowRight'));
  assert.deepEqual(calls, [], 'only up and down step matches');
  assert.equal(left.defaultPrevented, false);
  assert.equal(right.defaultPrevented, false);
});

test('the find bar actually uses those rules', () => {
  const src = readFileSync(new URL('../src/lib/components/MainPanel/DiffFindBar.svelte', import.meta.url), 'utf8')
    .replace(/\r\n/g, '\n');
  const handler = src.slice(src.indexOf('function handleKeydown'));
  const body = handler.slice(0, handler.indexOf('\n  }\n') + 4);
  assert.match(body, /findKeyAction\(event\)/, 'the bar no longer takes its keys from findKeyAction');
  assert.match(body, /event\.preventDefault\(\)/);
  assert.match(body, /dispatch\('step', action\)/, 'a step key should step the search in its direction');
  assert.match(src, /on:keydown=\{handleKeydown\}/);
});
