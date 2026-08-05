// A timer that redraws a panel nobody is looking at is pure cost: this app has
// one WebKit main thread, and everything competes for it — the same budget that
// made a background tab's output starve typing in the foreground one.
//
// So the task panel's "3 minutes ago" clock runs only while the panel is on
// screen. It starts when the panel becomes active, stops when it stops being
// active, and is cleared again on destroy because a component can be torn down
// while still visible (switching session, closing the window) and an interval
// outlives its component.
//
// Mirrors the reactive block and onDestroy in MainPanel/TaskPanel.svelte.
import { test } from 'node:test';
import assert from 'node:assert/strict';

/** The panel's timer lifecycle, as a state machine. */
function makePanel() {
  let clock = null;
  let ticks = 0;
  let now = 1_000_000;
  return {
    get running() { return clock !== null; },
    get now() { return now; },
    setActive(active) {
      if (active && !clock) {
        now = 2_000_000;            // re-read at once, not at the first tick
        clock = { fire: () => { ticks++; now += 60_000; } };
      } else if (!active && clock) {
        clock = null;
      }
    },
    destroy() { if (clock) clock = null; },
    tick() { if (clock) clock.fire(); },
    get ticks() { return ticks; },
  };
}

test('no timer runs before the panel is shown', () => {
  const panel = makePanel();
  assert.equal(panel.running, false);
});

test('the clock runs only while the panel is active', () => {
  const panel = makePanel();
  panel.setActive(true);
  assert.ok(panel.running);
  panel.setActive(false);
  assert.equal(panel.running, false, 'the panel was hidden but its timer kept going');
});

test('hiding and showing does not leave a second timer behind', () => {
  const panel = makePanel();
  panel.setActive(true);
  panel.setActive(true);   // a re-render with active still true
  panel.setActive(false);
  assert.equal(panel.running, false,
    'a duplicate interval survived — each redraw would then cost more than the last');
});

test('the time is re-read on becoming visible, not only on the first tick', () => {
  // Otherwise a panel reopened an hour later shows the time it was closed at
  // until a whole minute has passed.
  const panel = makePanel();
  const before = panel.now;
  panel.setActive(true);
  assert.notEqual(panel.now, before);
});

test('destroy stops a clock that was still running', () => {
  const panel = makePanel();
  panel.setActive(true);
  panel.destroy();
  panel.tick();
  assert.equal(panel.ticks, 0, 'the interval outlived the component');
});
