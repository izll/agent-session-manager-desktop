import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const src = readFileSync(
  new URL('../src/lib/components/MainPanel/TabBar.svelte', import.meta.url), 'utf8');

function bodyOf(marker, end = '\n  }\n') {
  const at = src.indexOf(marker);
  assert.ok(at > 0, `${marker} is gone`);
  const rest = src.slice(at);
  return rest.slice(0, rest.indexOf(end) + end.length);
}

// Escape used to clear the text and leave the panel sitting there, so dismissing
// a thought one had decided against still meant reaching for the mouse.
test('Escape closes the dictation panel', () => {
  const handler = bodyOf('function handleBufferKeydown');
  const escBranch = handler.slice(handler.indexOf("e.key === 'Escape'"));
  assert.match(escBranch, /closeDictationPanel\(\)/,
    'Escape no longer closes the panel');
});

// Escape is read by the pane too — xterm sees keys first, and an Escape that
// reaches an agent means something there.
test('Escape does not travel on to the terminal', () => {
  const handler = bodyOf('function handleBufferKeydown');
  const escBranch = handler.slice(handler.indexOf("e.key === 'Escape'"));
  assert.match(escBranch, /stopPropagation\(\)/,
    'the Escape that closes the panel would also reach the agent behind it');
  assert.match(escBranch, /preventDefault\(\)/);
});

// Two ways to close, one behaviour: they must not drift apart.
test('the close button and Escape do the same thing', () => {
  assert.match(src, /on:click=\{closeDictationPanel\}/,
    'the close button no longer shares the close path');
});

// Leaving dictation running behind a closed panel keeps the microphone open
// with nowhere for the text to go.
test('closing stops listening as well as clearing', () => {
  const fn = bodyOf('function closeDictationPanel');
  assert.match(fn, /clearBuffer\(\)/, 'the text is left behind');
  assert.match(fn, /dictationListening = false/, 'the panel would reopen as listening');
  assert.match(fn, /ToggleDictation\(\)/, 'the microphone is left running');
});
