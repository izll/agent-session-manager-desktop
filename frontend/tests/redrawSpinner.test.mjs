import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * The "refreshing" spinner has to come down when the pane is up to date.
 *
 * It ended only on the next byte from the backend, which is not the same thing.
 * A tab whose program has nothing to say — an agent that has finished, a shell
 * at its prompt — returns to a correct screen and no traffic at all, and the
 * spinner stayed up over a pane that had already redrawn. Clicking cleared it
 * only because the click itself made the terminal send something, which is why
 * it looked intermittent rather than broken.
 */

const terminal = readFileSync(
  new URL('../src/lib/utils/terminal.ts', import.meta.url),
  'utf8',
);
const pool = readFileSync(
  new URL('../src/lib/utils/terminalPool.ts', import.meta.url),
  'utf8',
);

// One place decides what "stop waiting" means. Three callers need it — the
// first byte, the repaint, and the timeout — and three copies of the same
// two-line dance is how one of them ends up forgetting the timer.
assert.match(
  terminal,
  /export function clearAwaitingRedraw\(/,
  'clearing the wait should be a named function, not repeated inline',
);

// The repaint in the settle step is proof the pane is current: it has just
// been redrawn from a cleared glyph cache. Waiting for bytes after that is
// waiting for something that may never come.
const settleRepaints = pool.match(/term\.refresh\(0, term\.rows - 1\);\s*(?:\/\/[^\n]*\n\s*)*clearAwaitingRedraw\(/g) ?? [];
assert.equal(
  settleRepaints.length,
  2,
  'both repaint paths (settle and settle-timeout) must end the wait',
);

// The backstop. Without it a silent tab waits forever, because the only other
// signals are bytes that are not coming and a repaint that already happened.
assert.match(
  pool,
  /awaitingRedrawTimer = setTimeout\(\(\) => clearAwaitingRedraw\(ti\), \d+\)/,
  'a wait with no other way out needs a timeout',
);

// The callback closes over `key`. Left from an earlier tab it compares
// activeKey against the wrong pane, and then nothing can ever take the
// spinner down — the failure that outlives the one being fixed here.
const assignsBeforeGuard = /ti\.onAwaitingRedraw = \([\s\S]{0,400}?if \(!ti\.awaitingRedraw\) \{/;
assert.match(
  pool,
  assignsBeforeGuard,
  'the callback must be reassigned on every switch, not only when starting a new wait',
);

// A torn-down tab must not fire a spinner update at a pane that is gone.
assert.match(
  pool,
  /private teardownEntry\(entry: PoolEntry\): void \{\s*(?:\/\/[^\n]*\n\s*)*clearAwaitingRedraw\(entry\.terminalInstance\)/,
  'teardown must cancel the timer',
);

console.log('redrawSpinner: ok');
