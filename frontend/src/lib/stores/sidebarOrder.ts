import { derived, get, writable } from 'svelte/store';
import {
  favorites,
  groups,
  selectSession,
  selectedSessionId,
  sessionsByActivity,
  sessionsByGroup,
  ungroupedSessions,
} from './sessions';
import { settings } from './settings';
import {
  buildSidebarOrder,
  resolveActiveEntry,
  stepEntry,
  type SidebarEntry,
  type SidebarSection,
} from '../utils/sidebarOrderCore';

export type { SidebarEntry, SidebarSection };

/**
 * The session list as the sidebar shows it, for stepping through with the
 * previous/next shortcuts.
 *
 * Stepping used to walk the sessions in stored order, which is not what is on
 * screen: the list may be sorted by activity, favourites are lifted into a
 * section of their own, collapsed groups hide their sessions, and a search
 * filters them. The shortcut then jumped to a session somewhere else in the
 * list, or to one that was not visible at all.
 *
 * A favourite that belongs to a group appears twice — in the favourites and in
 * its group — so a position here is a session AND the section it is shown in.
 * Stepping continues from the copy the user is actually on, instead of always
 * resolving the session to its first appearance.
 */
export const sidebarOrder = derived(
  [settings, sessionsByActivity, favorites, groups, sessionsByGroup, ungroupedSessions],
  ([$settings, $byActivity, $favorites, $groups, $byGroup, $ungrouped]) =>
    buildSidebarOrder(!!$settings?.sortByActivity, $byActivity, $favorites, $groups, $byGroup, $ungrouped),
);

/** The copy the user last clicked or stepped onto. */
const cursor = writable<SidebarEntry | null>(null);

export const activeSidebarEntry = derived(
  [cursor, selectedSessionId, sidebarOrder],
  ([$cursor, $selectedId, $order]) => resolveActiveEntry($cursor, $selectedId, $order),
);

/** Select a session from one particular row of the list. */
export function selectSidebarEntry(id: string, section: SidebarSection) {
  cursor.set({ id, section });
  if (get(selectedSessionId) !== id) selectSession(id);
}

function step(delta: 1 | -1) {
  const entry = stepEntry(get(sidebarOrder), get(activeSidebarEntry), delta);
  // Moving between the two copies of one session keeps the selection and moves
  // only the cursor, which is what makes the next step continue from there.
  if (entry) selectSidebarEntry(entry.id, entry.section);
}

export function selectPrevSession() {
  step(-1);
}

export function selectNextSession() {
  step(1);
}
