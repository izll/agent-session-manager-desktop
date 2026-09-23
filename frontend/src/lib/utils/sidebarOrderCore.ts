/**
 * The sidebar's row order and the previous/next step through it, as plain
 * functions. No stores and no imports, so the rules can be run under plain
 * node; stores/sidebarOrder.ts wires them to the live data.
 */

/** Which part of the list a row sits in. */
export type SidebarSection = 'favorites' | 'list';

export interface SidebarEntry {
  id: string;
  section: SidebarSection;
}

interface Identified {
  id: string;
}

interface GroupLike {
  id: string;
  collapsed: boolean;
}

/**
 * Every row of the sidebar, top to bottom, in the order SessionTree renders
 * them. Each argument is what the corresponding store already holds — filtered
 * by the search and sorted — so this only has to lay them out.
 */
export function buildSidebarOrder(
  sortByActivity: boolean,
  byActivity: Identified[],
  favorites: Identified[],
  groups: GroupLike[],
  byGroup: Map<string, Identified[]>,
  ungrouped: Identified[],
): SidebarEntry[] {
  const entries: SidebarEntry[] = [];
  if (sortByActivity) {
    // One flat list, no favourites section and no groups.
    for (const session of byActivity) entries.push({ id: session.id, section: 'list' });
    return entries;
  }
  for (const session of favorites) entries.push({ id: session.id, section: 'favorites' });
  for (const group of groups) {
    // A collapsed group shows its header only; its sessions are not on
    // screen, so the shortcut must not land on them.
    if (group.collapsed) continue;
    for (const session of byGroup.get(group.id) ?? []) {
      entries.push({ id: session.id, section: 'list' });
    }
  }
  for (const session of ungrouped) entries.push({ id: session.id, section: 'list' });
  return entries;
}

export function sameEntry(a: SidebarEntry | null, b: SidebarEntry | null): boolean {
  return !!a && !!b && a.id === b.id && a.section === b.section;
}

/**
 * The row that stands for the selected session: the cursor while it still
 * describes the selection and is on screen, the session's first row otherwise
 * — after it was chosen from somewhere that is not the list, such as quick
 * jump, or after its row was collapsed away.
 */
export function resolveActiveEntry(
  cursor: SidebarEntry | null,
  selectedId: string | null,
  order: SidebarEntry[],
): SidebarEntry | null {
  if (!selectedId) return null;
  if (cursor && cursor.id === selectedId && order.some(entry => sameEntry(entry, cursor))) {
    return cursor;
  }
  return order.find(entry => entry.id === selectedId) ?? null;
}

/**
 * The row one step from the active one, or null when there is nowhere to go.
 * No wrap-around: holding the key stops at the end of the list.
 */
export function stepEntry(
  order: SidebarEntry[],
  active: SidebarEntry | null,
  delta: 1 | -1,
): SidebarEntry | null {
  if (order.length === 0) return null;
  const at = active ? order.findIndex(entry => sameEntry(entry, active)) : -1;
  if (at < 0) {
    // Nothing on screen is selected: start from the end being stepped from.
    return order[delta > 0 ? 0 : order.length - 1];
  }
  const next = at + delta;
  if (next < 0 || next >= order.length) return null;
  return order[next];
}
