import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * Coming back to a tab asks the multiplexer to repaint, not just the browser.
 *
 * Three things happen on a switch and none of them covered this case: the
 * client repaints its own buffer, the backend replays what the pane produced
 * while the tab was away, and the size is re-announced. All of them are honest
 * about state they already hold — none helps when the buffer itself is what
 * went wrong, which is the tab that occasionally comes back looking stale.
 *
 * RedrawWindow closes that gap. RefreshWindow would too, but it sends Ctrl-L,
 * and an agent's prompt takes that as text — it stays on the button where the
 * user asks for it.
 */
const pool = readFileSync(
  new URL('../src/lib/utils/terminalPool.ts', import.meta.url),
  'utf8',
);

assert.match(pool, /import \{ LogFrontend, RedrawWindow \}/, 'the pool needs RedrawWindow');

// Never the input-sending one, however convenient it looks. Checked against
// the code alone: the comments explain why it is avoided, and matching prose
// is how a test starts failing for being right.
const poolCode = pool.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '');
assert.doesNotMatch(
  poolCode,
  /\bRefreshWindow\b/,
  "the input-sending refresh must not be called automatically",
);

// Both settle paths repaint, so a switch that lands on the timeout branch is
// not left out. Counted within the show path rather than across the file: the
// idle repaint calls the same helper for its own reasons, and folding the two
// together would let one of these disappear unnoticed.
const showPath = pool.slice(pool.indexOf('const settle ='), pool.indexOf('private startIdleRepaint'));
const calls = showPath.match(/requestRedraw\(entry\)/g) ?? [];
assert.equal(calls.length, 2, 'both the settle and the settle-timeout paths must ask for a repaint');

// Debounced: the two paths can both run for one switch, and a second redraw a
// few milliseconds later buys nothing.
assert.match(
  pool,
  /now - previous < \d+/,
  'repeated redraws for one pane must be debounced',
);

// A failure here is routine — a session stopped between the switch and the
// call — and must not break the switch.
assert.match(
  pool,
  /RedrawWindow\(sessionId, windowIdx, entry\.projectId\)\.catch\(/,
  'a failed redraw must not propagate',
);

console.log('switchRedraw: ok');
