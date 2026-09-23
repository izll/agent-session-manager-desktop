import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const termSrc = readFileSync(
  new URL('../src/lib/components/MainPanel/Terminal.svelte', import.meta.url), 'utf8');

const start = termSrc.indexOf('async function handlePoolChange(');
const poolChange = termSrc.slice(start, termSrc.indexOf('\n  }\n', start));

// The attach error is one variable for the whole pane, and only a successful
// attach cleared it. After tab A failed, selecting a stopped session B showed
// A's failure over B, and a running tab C showed it while its own attach was
// still under way.
test('a new target starts without the previous one\'s error', () => {
  assert.ok(start > 0, 'handlePoolChange is gone; this test needs to follow it');
  const firstAwait = poolChange.indexOf('await ');
  const clear = poolChange.search(
    /if \(projectChanged \|\| sessionChanged \|\| windowChanged\) error = '';/);
  assert.ok(clear > 0, 'switching project, session or tab keeps the last tab\'s error');
  assert.ok(clear < firstAwait,
    'the error is cleared only after an await, so the old one shows in between');
});

test('every hide in handlePoolChange clears the error with it', () => {
  const hides = [...poolChange.matchAll(/pool\.hideAll\(\);\s*isAttached = false;([^\n]*\n[^\n]*)/g)];
  // The project switch, the stop, and the not-running branch. The project
  // switch is covered by the target-change clear above.
  assert.ok(hides.length >= 3, `expected three hides, found ${hides.length}`);
  const withoutClear = hides.filter(([, next]) => !/error = '';/.test(next));
  assert.ok(withoutClear.length <= 1,
    'a hidden pane keeps an error from the tab it no longer shows');
  const stopped = poolChange.slice(poolChange.indexOf("// Don't destroy yet, just hide"));
  assert.match(stopped.slice(0, 200), /error = '';/, 'the stop path keeps a stale error');
  const notRunning = poolChange.slice(poolChange.lastIndexOf('pool.hideAll();'));
  assert.match(notRunning, /error = '';/, 'the not-running path keeps a stale error');
});

test('a new attach starts with a clean slate', () => {
  const show = poolChange.indexOf('await pool.show(');
  assert.ok(show > 0);
  const before = poolChange.slice(poolChange.lastIndexOf('try {', show) - 120, show);
  assert.match(before, /error = '';/,
    'while the show is pending, the last attempt\'s error is shown for this one');
});

test('the error is not cleared twice in a row', () => {
  assert.doesNotMatch(termSrc, /error = '';\s*error = '';/);
});

// A tab on a server that is not answering also fails to attach. The failure
// took over the placeholder and replaced "the server is not answering" — the
// one explanation the user can act on — with a bare connection error.
test('an unreachable server keeps its own explanation', () => {
  const line = termSrc.slice(termSrc.indexOf('$: attachFailed ='));
  assert.match(line.slice(0, line.indexOf('\n')), /!remoteUnreachable/,
    'a failed attach still hides the unreachable-server text');
});
