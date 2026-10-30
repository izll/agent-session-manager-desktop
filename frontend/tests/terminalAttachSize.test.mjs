import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// The terminal attaches at its own size: without one the pane shrank to 80x24
// for a moment, an agent redrew at that width, and the scrollback kept it.
test('the attach URL carries the terminal size', () => {
  const src = readFileSync(new URL('../src/lib/utils/terminal.ts', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
  assert.match(src, /`&token=\$\{encodeURIComponent\(token\)\}` \+ attachSizeQuery\(terminalInstance\);/);
  assert.match(src, /return `&cols=\$\{Math\.floor\(size\.cols\)\}&rows=\$\{Math\.floor\(size\.rows\)\}`;/);
  assert.match(src, /if \(!\(size\.cols >= 20\) \|\| !\(size\.rows >= 5\)\) return '';/,
    'a terminal not laid out yet must send no size rather than a tiny one');
});

// A pooled terminal attaches while still hidden, before its first fit. Its own
// cols and rows are then xterm's default 80x24 — sent as the viewer's size, it
// dropped the window to 80x24 on every fresh attach.
test('an unmeasured terminal does not attach at xterm\'s default size', () => {
  const src = readFileSync(new URL('../src/lib/utils/terminal.ts', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
  const body = src.slice(src.indexOf('export function attachSizeQuery('), src.indexOf('\n}\n', src.indexOf('export function attachSizeQuery(')));
  assert.doesNotMatch(body, /instance\.terminal\.(cols|rows)/,
    'the terminal\'s own cols/rows are xterm\'s default until the first fit');
  assert.match(body, /instance\.lastSentSize/, 'the size it last announced is what stands in');
});
