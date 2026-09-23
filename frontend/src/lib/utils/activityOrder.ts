/**
 * The order of the activity-sorted session list, as a plain function so the
 * rules can be run under plain node.
 */

/**
 * How recently a session must have worked to count as working now.
 *
 * Sessions working at the same time used to be ordered by their exact last
 * activity, which the poll stamps to the second. A session that got no busy
 * reading in one tick — a capture that timed out, a poll that ran late — fell
 * a second behind the others and swapped places with them until the next
 * tick, moving rows under the cursor while stepping through the list. Within
 * this window they are simply "working now", in a stable order by name.
 *
 * Long enough to cover a missed tick or several, short enough that a session
 * that finished a while ago drops back into its place by time.
 */
export const RECENTLY_ACTIVE_MS = 60_000;

export interface ActivityEntry {
  name: string;
  /** Milliseconds since the epoch; 0 for a session with no recorded activity. */
  time: number;
}

/** Sort comparator: working-now first by name, then most recent first. */
export function compareByActivity(a: ActivityEntry, b: ActivityEntry, now: number): number {
  const aRecent = a.time > 0 && now - a.time <= RECENTLY_ACTIVE_MS;
  const bRecent = b.time > 0 && now - b.time <= RECENTLY_ACTIVE_MS;
  if (aRecent !== bRecent) return aRecent ? -1 : 1;
  if (!aRecent && a.time !== b.time) return b.time - a.time;
  return a.name.localeCompare(b.name);
}
