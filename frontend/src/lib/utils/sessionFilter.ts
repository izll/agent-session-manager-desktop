/**
 * Which sessions the sidebar list shows, as plain functions so the rules can
 * be run under plain node; stores/sessions.ts applies them to every list the
 * sidebar draws from.
 *
 * One predicate for all of them on purpose. The search used to be written out
 * in each grouped list and was simply missing from the activity-sorted one, so
 * with the list sorted the search box did nothing.
 */

interface Searchable {
  name: string;
  notes?: string;
}

/** Does the session match the sidebar search? An empty query matches all. */
export function matchesSearch(session: Searchable, query: string): boolean {
  if (!query) return true;
  const lower = query.toLowerCase();
  return session.name.toLowerCase().includes(lower) ||
    !!session.notes?.toLowerCase().includes(lower);
}

/**
 * Milliseconds since the epoch of an activity stamp, 0 for none.
 *
 * An empty stamp is "never", not "the beginning of time", and an unparseable
 * one is NaN — which in a comparison silently leaves an array in whatever
 * order it started in — so both come out as 0.
 */
export function activityTime(stamp: string | undefined): number {
  const parsed = stamp ? Date.parse(stamp) : 0;
  return Number.isFinite(parsed) ? parsed : 0;
}

/** The time windows the filter menu offers, in days; 0 is no window. */
export const ACTIVE_WITHIN_CHOICES = [0, 1, 7, 30] as const;

const DAY_MS = 24 * 60 * 60 * 1000;

/**
 * The sidebar's activity filter. Two independent parts that combine: "last 7
 * days" with "hide inactive" leaves the running sessions that did something
 * in the last 7 days.
 */
export interface ActivityFilter {
  /** Hide the sessions that are not running. */
  hideInactive: boolean;
  /** Hide the sessions whose last activity is older than this; 0 is off. */
  activeWithinDays: number;
}

/** The filter as the settings store holds it, with unknown values dropped. */
export function activityFilterFrom(settings: {
  sidebarHideInactive?: boolean;
  sidebarActiveWithinDays?: number;
} | null | undefined): ActivityFilter {
  const days = settings?.sidebarActiveWithinDays ?? 0;
  return {
    hideInactive: !!settings?.sidebarHideInactive,
    activeWithinDays: (ACTIVE_WITHIN_CHOICES as readonly number[]).includes(days) ? days : 0,
  };
}

export function isActivityFilterOn(filter: ActivityFilter): boolean {
  return filter.hideInactive || filter.activeWithinDays > 0;
}

/**
 * Does the session pass the activity filter?
 *
 * The time window goes by the session's last recorded activity — when an
 * agent in it was last seen working — and not by whether it is running: a
 * session left running and idle for a month is not one that was active this
 * week, and "running" is what the other half of the filter is for. A session
 * with no recorded activity never falls inside a window.
 */
export function matchesActivityFilter(
  session: { status: string },
  lastActiveMs: number,
  filter: ActivityFilter,
  now: number,
): boolean {
  if (filter.hideInactive && session.status !== 'running') return false;
  if (filter.activeWithinDays > 0) {
    if (lastActiveMs <= 0) return false;
    if (now - lastActiveMs > filter.activeWithinDays * DAY_MS) return false;
  }
  return true;
}
