import test from 'node:test';
import assert from 'node:assert/strict';

/**
 * One press of Ctrl+Shift+T must open one terminal.
 *
 * The handler guarded only on `document.querySelector('.dialog-overlay')`.
 * That element does not exist until Svelte renders, which happens after the
 * current task — so a key held long enough to auto-repeat runs the handler
 * again while the DOM still shows no dialog, and two tabs appear.
 *
 * This models the two guards the fix relies on: the component's own state, and
 * the event's repeat flag.
 */

function makeHandler() {
  const state = { showQuickTerminalDialog: false, opens: 0 };
  // The DOM lags the state by design: the overlay appears only when flushed.
  const dom = { overlayPresent: false };
  const flush = () => { dom.overlayPresent = state.showQuickTerminalDialog; };

  const handler = (event) => {
    const dialogOpen = state.showQuickTerminalDialog || dom.overlayPresent;
    if (dialogOpen) return;
    if (event.repeat) return;
    state.showQuickTerminalDialog = true;
    state.opens++;
  };
  return { handler, state, flush };
}

test('a repeated keydown does not open the quick terminal twice', () => {
  const { handler, state } = makeHandler();
  handler({ repeat: false });
  handler({ repeat: true });
  handler({ repeat: true });
  assert.equal(state.opens, 1, 'auto-repeat opened more than one dialog');
});

test('a second distinct press while the dialog is open is ignored', () => {
  const { handler, state } = makeHandler();
  handler({ repeat: false });
  // Not flushed: the overlay is not in the DOM yet, which is the whole trap.
  handler({ repeat: false });
  assert.equal(state.opens, 1, 'state guard did not hold before the render');
});

test('the dialog can be opened again after it closes', () => {
  const { handler, state, flush } = makeHandler();
  handler({ repeat: false });
  flush();
  state.showQuickTerminalDialog = false;
  flush();
  handler({ repeat: false });
  assert.equal(state.opens, 2, 'the guard outlived the dialog it guarded');
});
