import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const src = readFileSync(
  new URL('../src/lib/components/MainPanel/TabBar.svelte', import.meta.url), 'utf8');

// The dictation buffer lives in the backend and outlived the session that
// filled it: stopping dictation only stopped the polling, so a sentence spoken
// and abandoned reappeared the next time dictation was switched on — ready to
// send, from an hour ago.

test('stopping dictation discards the buffer', () => {
  const handler = src.slice(src.indexOf("EventsOn('dictation:state'"));
  const body = handler.slice(0, handler.indexOf('\n    });') + 8);
  const stopBranch = body.slice(body.lastIndexOf('} else {'));
  assert.match(stopBranch, /discardBuffer\(\)/,
    'the stop path leaves the buffer holding text that was never sent');
});

// Clearing only the local copy leaves the backend holding the words, and the
// next poll puts them straight back.
test('discarding clears both the local text and the backend buffer', () => {
  const fn = src.slice(src.indexOf('async function discardBuffer('));
  const body = fn.slice(0, fn.indexOf('\n  }\n') + 4);
  assert.match(body, /bufferText = ''/, 'the local text is not cleared');
  assert.match(body, /lastGoText = ''/,
    'without resetting lastGoText the poll sees no change and never refreshes');
  assert.match(body, /ClearBuffer\(\)/, 'the backend buffer is left holding the text');
});

// A failure to reach the backend must not leave the editor showing text the
// user thinks is gone.
test('a failed clear is reported rather than swallowed', () => {
  const fn = src.slice(src.indexOf('async function discardBuffer('));
  const body = fn.slice(0, fn.indexOf('\n  }\n') + 4);
  assert.match(body, /catch/, 'a clear that throws would break the stop path');
  assert.match(body, /console\.error/, 'a failed clear leaves no trace at all');
});
