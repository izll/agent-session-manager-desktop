/**
 * A session's tabs with one per window index — the first of each.
 *
 * An old store can hold two or three records for one index (see
 * selectFollowedWindowForRestart in the backend, which settles which is real
 * from the multiplexer's window name). The UI addresses a tab by its index, so
 * the others cannot be reached from a list anyway, and a keyed list given two
 * rows under one key throws: the command palette failed to open, and with its
 * open flag stuck every shortcut after it went quiet. `skip` leaves out the
 * session's own main index, which a stray record can also claim.
 */
export function firstPerIndex<T>(tabs: T[] | null | undefined, indexOf: (tab: T) => number, skip?: number): T[] {
  const seen = new Set<number>();
  if (skip !== undefined) seen.add(skip);
  const out: T[] = [];
  for (const tab of tabs ?? []) {
    const index = indexOf(tab);
    if (seen.has(index)) continue;
    seen.add(index);
    out.push(tab);
  }
  return out;
}
