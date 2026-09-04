import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const src = readFileSync(
  new URL('../src/lib/components/MainPanel/TabBar.svelte', import.meta.url), 'utf8');

const sendBuffer = (() => {
  const at = src.indexOf('async function sendBuffer(');
  assert.ok(at > 0, 'sendBuffer is gone');
  const rest = src.slice(at);
  return rest.slice(0, rest.indexOf('\n  }\n') + 4);
})();

// Focus used to return to the terminal only when the panel closed itself. With
// the panel kept open the caret stayed in it — and sending without Enter leaves
// a prompt in the agent's composer waiting for exactly that key, which then went
// to the dictation buffer instead.
test('focus returns to the terminal whether or not the panel closes', () => {
  const focusAt = sendBuffer.indexOf('focusTerminalAfterSend(');
  assert.ok(focusAt > 0, 'nothing returns focus to the terminal any more');

  const closeAt = sendBuffer.indexOf('if (bufferCloseOnSend)');
  assert.ok(closeAt > 0, 'the close-on-send branch is gone');

  // The focus call must sit after that branch closes, not inside it. Measured
  // by brace depth rather than by matching indentation, which moves.
  let depth = 0;
  let branchEnd = -1;
  for (let i = closeAt; i < sendBuffer.length; i++) {
    if (sendBuffer[i] === '{') depth++;
    else if (sendBuffer[i] === '}') {
      depth--;
      if (depth === 0) { branchEnd = i; break; }
    }
  }
  assert.ok(branchEnd > 0, 'the close-on-send branch never closes');
  assert.ok(branchEnd < focusAt,
    'the focus call is still inside the close-on-send branch, so leaving the ' +
    'panel open leaves the caret in it');
});

test('closing on send still stops dictation', () => {
  const closeBranch = sendBuffer.slice(sendBuffer.indexOf('if (bufferCloseOnSend)'));
  assert.match(closeBranch.slice(0, 200), /dictationListening = false/);
  assert.match(closeBranch.slice(0, 200), /ToggleDictation\(\)/);
});

// The real cause, found after a retry loop failed to fix it: the 150ms buffer
// poll calls updateEditorDisplay, which places the caret inside the editor.
// Selecting inside a contenteditable focuses it, so the poll pulled focus back
// out of the terminal after the send had handed it over. Where the poll landed
// decided whether it happened, which is what made it look intermittent.
test('the buffer poll does not select inside an unfocused editor', () => {
  const at = src.indexOf('function updateEditorDisplay');
  assert.ok(at > 0, 'updateEditorDisplay is gone');
  const fn = src.slice(at, src.indexOf('\n  }\n', at) + 4);

  const guardAt = fn.indexOf('document.activeElement !== bufferEditor');
  assert.ok(guardAt > 0,
    'nothing stops the poll from selecting inside the editor while the caret ' +
    'is in the terminal, which drags focus back into the panel');

  // The guard has to come before the selection work, not after it.
  const selectAt = fn.indexOf('sel.addRange');
  assert.ok(selectAt > 0, 'the caret placement is gone');
  assert.ok(guardAt < selectAt, 'the guard runs after the selection it guards');

  // Text still has to update while unfocused: the panel shows what is being
  // dictated, and freezing it would be a worse bug than the one being fixed.
  const textAt = fn.indexOf('bufferEditor.textContent = bufferText');
  assert.ok(textAt > 0 && textAt < guardAt,
    'the guard also skips the text update, so the panel stops showing speech');
});

test('the caret is still placed while the editor is focused', () => {
  const at = src.indexOf('function updateEditorDisplay');
  const fn = src.slice(at, src.indexOf('\n  }\n', at) + 4);
  assert.match(fn, /sel\.removeAllRanges\(\)/, 'the caret placement was dropped');
  assert.match(fn, /range\.collapse\(true\)/, 'the caret no longer goes to the end');
});

// Closing the panel is not sending it. The user's habit is the hotkey: press to
// show, press again to hide — which never goes through sendBuffer at all, so
// the send-path fix above did nothing for it. Every close route ends in the
// dictation:state handler's stopped branch, and it returned focus to nothing.
test('closing the panel hands the caret back to the terminal', () => {
  const at = src.indexOf("EventsOn('dictation:state'");
  assert.ok(at > 0, 'the dictation state handler is gone');
  const handler = src.slice(at, src.indexOf('\n    });\n', at));

  const stoppedAt = handler.indexOf('} else {');
  assert.ok(stoppedAt > 0, 'the stopped branch is gone');
  const stopped = handler.slice(stoppedAt);

  assert.match(stopped, /focusTerminalAfterSend\(/,
    'closing the panel leaves the caret nowhere, so the terminal ignores keys');
});

// Dictation into a notes field has its own focus target; the terminal must not
// take the caret away from it when that dictation stops.
test('stopping notes dictation does not steal focus for the terminal', () => {
  const at = src.indexOf("EventsOn('dictation:state'");
  const handler = src.slice(at, src.indexOf('\n    });\n', at));
  const stopped = handler.slice(handler.indexOf('} else {'));

  const focusAt = stopped.indexOf('focusTerminalAfterSend(');
  const line = stopped.slice(stopped.lastIndexOf('\n', focusAt) + 1, focusAt + 30);
  assert.match(line, /bufferMode/,
    'the terminal grabs focus even when the dictation was into a notes field');
});

// The terminal pool keeps every opened tab's xterm instance in the DOM, hidden
// ones included. querySelector('.xterm-helper-textarea') returns the first in
// document order, which need not be the visible pane — so focus could be handed
// to a hidden terminal while the one on screen stayed dead to the keyboard.
test('focus goes through the terminal component, not the first textarea found', () => {
  const at = src.indexOf('function focusTerminalAfterSend');
  const fn = src.slice(at, src.indexOf('\n  }\n', at) + 4);

  assert.match(fn, /dispatchEvent\(new CustomEvent\('terminal:focus'\)\)/,
    'focus no longer asks the component which terminal is actually active');

  const call = fn.replace(/\/\/[^\n]*/g, '');
  assert.ok(!/querySelector\(['"]\.xterm-helper-textarea/.test(call),
    'still grabbing the first xterm textarea in the document');
});

// utils/focus.focusTerminal() bails out while the caret sits in a textarea.
// After dictation that is precisely where it sits, and precisely what has to
// move, so this path must not be routed through it.
test('the dictation path does not use the textarea-averse helper', () => {
  const at = src.indexOf('function focusTerminalAfterSend');
  const fn = src.slice(at, src.indexOf('\n  }\n', at) + 4);
  const call = fn.replace(/\/\/[^\n]*/g, '');
  assert.ok(!/\bfocusTerminal\(\)/.test(call),
    'focusTerminal() declines when the caret is in a textarea, which is the case here');
});

// Escape and the close button hide the panel themselves, before the backend
// state event that normally returns focus can arrive — and if that event is
// late, or never comes because listening was already off, nothing hands the
// caret back.
test('the close path returns focus without waiting for the backend event', () => {
  const at = src.indexOf('function closeDictationPanel');
  assert.ok(at > 0, 'closeDictationPanel is gone');
  const fn = src.slice(at, src.indexOf('\n  }\n', at) + 4);
  assert.match(fn, /focusTerminalAfterSend\(/,
    'closing relies entirely on an event that may not arrive');
});
