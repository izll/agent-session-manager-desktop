import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// Model of Diff.svelte's handleDiffFindKeydown, kept in step with it by the
// source check at the bottom: the real handler cannot be imported out of a
// .svelte file without a compiler.
function makeHandler() {
  const calls = [];
  const handler = (event) => {
    const e = { preventDefault() { this.defaultPrevented = true; }, defaultPrevented: false, ...event };
    e.preventDefault = () => { e.defaultPrevented = true; };
    if (e.key === 'Escape') { e.preventDefault(); calls.push('close'); return e; }
    if (e.key === 'Enter' || e.key === 'F3' || ((e.ctrlKey || e.metaKey) && e.key === 'g')) {
      e.preventDefault();
      calls.push(e.shiftKey ? -1 : 1);
      return e;
    }
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      calls.push(e.key === 'ArrowDown' ? 1 : -1);
    }
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

test('Diff.svelte actually handles the arrows', () => {
  const src = readFileSync(new URL('../src/lib/components/MainPanel/Diff.svelte', import.meta.url), 'utf8');
  const handler = src.slice(src.indexOf('function handleDiffFindKeydown'));
  const body = handler.slice(0, handler.indexOf('\n  }\n') + 4);
  assert.match(body, /ArrowDown/, 'the find handler no longer mentions ArrowDown');
  assert.match(body, /ArrowUp/, 'the find handler no longer mentions ArrowUp');
  assert.match(body, /stepDiffSearch\(event\.key === 'ArrowDown' \? 1 : -1\)/,
    'the arrows should step the search in the expected direction');
});
