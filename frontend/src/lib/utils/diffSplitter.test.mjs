// The splitter's floors, which are what stop the layout getting into a state
// the mouse cannot get it out of.
//
// Mirrors startSplitterDrag in MainPanel.svelte.
import { test } from 'node:test';
import assert from 'node:assert/strict';

const DIFF_ABOVE_MIN = 120;
const BELOW_MIN = 140;

/** Where the drag lands, given a pointer position and the space available. */
function clampHeight(wanted, stackHeight) {
  const ceiling = Math.max(DIFF_ABOVE_MIN, stackHeight - BELOW_MIN);
  return Math.min(Math.max(wanted, DIFF_ABOVE_MIN), ceiling);
}

test('an ordinary drag lands where the pointer is', () => {
  assert.equal(clampHeight(300, 800), 300);
});

test('the diff cannot be dragged shut', () => {
  // At zero height there is nothing to grab the splitter by from above, and
  // the pane looks broken rather than closed — there is a button for closing.
  assert.equal(clampHeight(0, 800), DIFF_ABOVE_MIN);
  assert.equal(clampHeight(-200, 800), DIFF_ABOVE_MIN);
});

test('the view below keeps a usable height', () => {
  // Dragging to the bottom must not leave a terminal too small to drag back.
  const h = clampHeight(10_000, 800);
  assert.equal(h, 800 - BELOW_MIN);
  assert.ok(800 - h >= BELOW_MIN);
});

test('a window too short for both floors still yields a grabbable splitter', () => {
  // 200px of space cannot satisfy 120 + 140. The diff wins its minimum, the
  // splitter stays on screen, and the pane below is clipped rather than the
  // layout collapsing.
  const h = clampHeight(500, 200);
  assert.equal(h, DIFF_ABOVE_MIN);
});

test('the ceiling never falls below the floor', () => {
  // Math.max in the ceiling is what prevents an inverted range, where clamping
  // would return the smaller of two contradictory bounds and jump the pane.
  for (const stack of [0, 50, 150, 260, 1000]) {
    const h = clampHeight(400, stack);
    assert.ok(h >= DIFF_ABOVE_MIN, `height ${h} fell below the floor at stack ${stack}`);
  }
});
