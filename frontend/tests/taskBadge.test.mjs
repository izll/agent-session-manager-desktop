import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

/**
 * The badge has to move when a task is ticked off.
 *
 * It counted on a five-minute poll alone, so a task marked done left the number
 * unchanged for up to five minutes — which reads as the badge being broken
 * rather than merely late.
 *
 * Every edit made in the app goes through the tasks store, so recounting when
 * that changes covers all of them without each caller having to remember.
 */
const alerts = readFileSync(
  new URL('../src/lib/stores/taskAlerts.ts', import.meta.url),
  'utf8',
);

assert.match(
  alerts,
  /tasks\.subscribe\(/,
  'the count must follow the store, not only the clock',
);

// The poll stays as a backstop: tasks also change outside this window, written
// by an agent or by another copy of the app.
assert.match(alerts, /setInterval/, 'the periodic check is still needed for outside changes');

/**
 * And the subscription must not fire a read per store write.
 *
 * A single tick writes the store twice — the optimistic update and the reload
 * after it — and answering means reading every project's task file. Without
 * coalescing, one click costs several full sweeps.
 */
assert.match(
  alerts,
  /clearTimeout\(pending\)/,
  'bursts of store writes should collapse into one recount',
);

// Subscribing fires immediately with the current value; that first call is
// already covered by the refresh at the top of the watcher.
assert.match(alerts, /if \(first\)/, 'the immediate first callback should be skipped');

// Cleanup has to undo both halves, or a closed window keeps reading files.
const watcher = alerts.match(/export function watchOpenCount[\s\S]*?\n}/);
assert.ok(watcher, 'watchOpenCount is missing');
assert.match(watcher[0], /clearInterval\(timer\)/, 'the poll must be stopped');
assert.match(watcher[0], /unsubscribe\(\)/, 'and the subscription dropped');

console.log('taskBadge: ok');
