// The dropdown always opened downwards with no height limit, and the panel
// clipped its contents — so a select near the bottom of the window ran off the
// screen and what was below could not be scrolled to. Adding two more sort
// options was enough to make the list unusable.
//
// Mirrors positionDropdown in components/common/Select.svelte.
import { test } from 'node:test';
import assert from 'node:assert/strict';

const VIEWPORT_MARGIN = 8;
const MIN_USABLE_HEIGHT = 120;

/** Where the dropdown goes, given the trigger's position in the window. */
function place(rect, windowHeight) {
  const below = windowHeight - rect.bottom - VIEWPORT_MARGIN;
  const above = rect.top - VIEWPORT_MARGIN;
  const openUp = below < MIN_USABLE_HEIGHT && above > below;
  return openUp
    ? { direction: 'up', maxHeight: above }
    : { direction: 'down', maxHeight: below };
}

test('a select with room below opens downwards', () => {
  const p = place({ top: 100, bottom: 130 }, 900);
  assert.equal(p.direction, 'down');
  assert.ok(p.maxHeight > 700);
});

test('a select near the bottom opens upwards', () => {
  // The reported case: the list ran off the screen instead.
  const p = place({ top: 840, bottom: 870 }, 900);
  assert.equal(p.direction, 'up');
  assert.ok(p.maxHeight > MIN_USABLE_HEIGHT);
});

test('the height is always bounded, in either direction', () => {
  // Without a limit the panel is as tall as its contents, which is what put
  // options past the edge of the window in the first place.
  for (const rect of [{ top: 10, bottom: 40 }, { top: 500, bottom: 530 }, { top: 860, bottom: 890 }]) {
    const p = place(rect, 900);
    assert.ok(Number.isFinite(p.maxHeight), 'unbounded height');
    assert.ok(p.maxHeight >= 0);
  }
});

test('a cramped window still picks the roomier side', () => {
  // Neither side fits comfortably; the taller one wins rather than defaulting
  // down and showing almost nothing.
  const p = place({ top: 200, bottom: 230 }, 300);
  assert.equal(p.direction, 'up');
  assert.equal(p.maxHeight, 192);
});

test('the dropdown keeps clear of the window edge', () => {
  const p = place({ top: 100, bottom: 130 }, 900);
  assert.equal(p.maxHeight, 900 - 130 - VIEWPORT_MARGIN);
});

test('plenty of room below wins even with more room above', () => {
  // Opening upwards is the exception, not a preference: it is only worth doing
  // when downwards would be cramped.
  const p = place({ top: 600, bottom: 630 }, 900);
  assert.equal(p.direction, 'down');
});
