// Shutting down is not instant: the app saves where each session left off,
// detaches its mirrors and reaps the processes it started. With several busy
// sessions that is long enough to look like a hang, so an overlay says what is
// happening.
//
// The ordering is the whole point, and it has two parts.
//
// Setting the flag only QUEUES a DOM update — Svelte applies it in a
// microtask, which can run after a frame callback. So waiting for a frame
// without first awaiting tick() waits for a frame that paints the old DOM,
// with no overlay in it. That is why the first attempt showed nothing.
//
// Then Quit() blocks the main thread for the length of the teardown, so the
// overlay has to be on screen before it is called: tick(), two frames, quit.
//
// Mirrors confirmQuit in App.svelte.
import { test } from 'node:test';
import assert from 'node:assert/strict';

/** A confirm-and-quit sequence, recording what happened in what order. */
function runQuit({ awaitTick, deferFrames }) {
  const events = [];
  let frame = 0;
  let domHasOverlay = false;

  events.push('flag-set');
  // The DOM only gains the overlay once Svelte has flushed.
  if (awaitTick) {
    domHasOverlay = true;
    events.push('dom-updated');
  }
  for (let i = 0; i < deferFrames; i++) {
    frame++;
    events.push(domHasOverlay ? `painted-with-overlay:${frame}` : `painted-empty:${frame}`);
  }
  events.push('quit');
  return events;
}

test('quitting in the same frame paints nothing at all', () => {
  const events = runQuit({ awaitTick: true, deferFrames: 0 });
  assert.equal(events.findIndex((e) => e.startsWith('painted')), -1);
});

test('waiting for frames without awaiting the DOM paints an empty overlay', () => {
  // The first attempt at this: frames were waited for, but the flag had not
  // been flushed to the DOM yet, so the frame painted the page as it was.
  const events = runQuit({ awaitTick: false, deferFrames: 2 });
  assert.ok(events.some((e) => e.startsWith('painted-empty')),
    'the overlay was in the DOM sooner than Svelte would have put it there');
  assert.equal(events.some((e) => e.startsWith('painted-with-overlay')), false);
});

test('awaiting the DOM update then a frame paints the overlay before quitting', () => {
  const events = runQuit({ awaitTick: true, deferFrames: 2 });
  const painted = events.findIndex((e) => e.startsWith('painted-with-overlay'));
  const quit = events.indexOf('quit');
  assert.ok(painted >= 0, 'the overlay never painted');
  assert.ok(painted < quit, 'the quit was requested before the overlay was on screen');
});

test('the flag is set before anything else happens', () => {
  const events = runQuit({ awaitTick: true, deferFrames: 2 });
  assert.equal(events[0], 'flag-set');
});
