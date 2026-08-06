import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * A shortcut that opens something must not also reach the terminal.
 *
 * preventDefault only cancels the browser's own action. xterm attaches its own
 * key listener, so a shortcut handled in the capture phase still arrives there
 * unless propagation is stopped — Ctrl+J opened the quick-jump window and sent
 * LF to the agent's composer at the same time, adding a newline on every use.
 *
 * The keys carrying a control character are the ones this matters for, and
 * they are exactly the ones a user is most likely to bind: Ctrl+letter.
 */
const source = readFileSync(new URL('../src/App.svelte', import.meta.url), 'utf8');

/** The body of a `case '<id>':` arm, up to its `return`. */
function caseBody(id) {
  const marker = "case '" + id + "':";
  const start = source.indexOf(marker);
  assert.notEqual(start, -1, 'no handler found for ' + id);
  const end = source.indexOf('return;', start);
  assert.notEqual(end, -1, 'handler for ' + id + ' never returns');
  return source.slice(start + marker.length, end);
}

for (const id of ['quickJump.open', 'quickJump.add']) {
  const body = caseBody(id);
  assert.match(
    body,
    /e\.preventDefault\(\)/,
    id + " must cancel the browser's default action",
  );
  assert.match(
    body,
    /e\.stopPropagation\(\)/,
    id + ' must stop propagation, or xterm receives the key as well',
  );
}

console.log('shortcutNoLeak: ok');
