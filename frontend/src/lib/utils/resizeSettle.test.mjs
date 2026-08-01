// The size announcement waits for layout to stop moving. It counts FRAMES,
// not milliseconds, and this is the reason: a fixed delay silently encodes a
// frame rate. Tuned on an idle machine, 80ms looks like five frames; on a
// loaded WebKitGTK where a frame takes 100ms it is less than one, so the wait
// expires mid-burst and sends exactly the intermediate size it exists to
// suppress. That failure appears only on slow machines — the hardest place to
// notice it, and the likeliest place for layout to be slow in the first place.
//
// Mirrors the settle loop in terminal.ts (sendResizeIfChanged).
import { test } from 'node:test';
import assert from 'node:assert/strict';

const SETTLE_FRAMES = 3;
const MAX_MS = 1000;

/**
 * Run the settle loop over a scripted sequence of per-frame sizes.
 * Returns what would be announced, or null if nothing would be.
 */
function settle(frames, { frameMs = 16 } = {}) {
  let stable = 0;
  let lastSeen = frames[0];
  let elapsed = 0;

  for (let i = 1; i < frames.length; i++) {
    const now = frames[i];
    stable = now === lastSeen ? stable + 1 : 0;
    lastSeen = now;
    elapsed += frameMs;
    if (stable >= SETTLE_FRAMES || elapsed >= MAX_MS) return now;
  }
  return null;
}

test('a burst settles on the final size, not an intermediate one', () => {
  // The observed case: a too-small size measured mid-layout, then the real one.
  const frames = ['168x48', '168x48', '221x60', '221x60', '221x60', '221x60'];
  assert.equal(settle(frames), '221x60');
});

test('the same burst on a slow machine still settles on the final size', () => {
  // 100ms frames. A fixed 80ms wait would have fired during the first frame
  // and announced 168x48; counting frames does not care how long they take.
  const frames = ['168x48', '168x48', '221x60', '221x60', '221x60', '221x60'];
  assert.equal(settle(frames, { frameMs: 100 }), '221x60');
});

test('a size that never moves is announced promptly', () => {
  const frames = ['221x60', '221x60', '221x60', '221x60'];
  assert.equal(settle(frames), '221x60');
});

test('a change restarts the count, so a long drag keeps up', () => {
  // Each step of a drag replaces the last; only the value it lands on is sent.
  const frames = ['100x30', '120x36', '140x42', '160x48', '160x48', '160x48', '160x48'];
  assert.equal(settle(frames), '160x48');
});

test('the wall-clock ceiling releases a size when frames are pathologically slow', () => {
  // 600ms frames: the ceiling fires before three frames accumulate, so the
  // announcement is late rather than never.
  const frames = ['80x24', '221x60', '221x60'];
  assert.equal(settle(frames, { frameMs: 600 }), '221x60');
});
