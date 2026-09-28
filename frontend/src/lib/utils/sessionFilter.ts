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
