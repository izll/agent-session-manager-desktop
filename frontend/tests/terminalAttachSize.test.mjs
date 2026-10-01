import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// The terminal attaches at its own size: without one the pane shrank to 80x24
// for a moment, an agent redrew at that width, and the scrollback kept it.
test('the attach URL carries the terminal size', () => {
  const src = readFileSync(new URL('../src/lib/utils/terminal.ts', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
  assert.match(src, /`&token=\$\{encodeURIComponent\(token\)\}` \+ attachSizeQuery\(terminalInstance\);/);
  assert.match(src, /return `&cols=\$\{Math\.floor\(size\.cols\)\}&rows=\$\{Math\.floor\(size\.rows\)\}`;/);
  assert.match(src, /if \(!size \|\| !\(size\.cols >= 20\) \|\| !\(size\.rows >= 5\)\) return '';/,
    'a terminal not laid out yet must send no size rather than a tiny one');
});
