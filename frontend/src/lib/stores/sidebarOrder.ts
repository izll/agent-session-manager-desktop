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
import { showSessionView } from './navigation';
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

/**
 * Select a session from one particular row of the list.
 *
 * Clicking the session that is already selected still opens it. It used to be
 * skipped as a no-op, and with it went the switch to the session view that
 * selectSession makes — so from the dashboard or the task list, clicking the
 * highlighted row did nothing at all. Only the view is changed here, not the
 * selection: re-selecting would re-resolve the tab and write the same session
 * and tab back to storage, for a session nobody left.
 */
export function selectSidebarEntry(id: string, section: SidebarSection) {
  cursor.set({ id, section });
  if (get(selectedSessionId) !== id) selectSession(id);
  else showSessionView();
}

function step(delta: 1 | -1) {
  const entry = stepEntry(get(sidebarOrder), get(activeSidebarEntry), delta);
  if (!entry) return;
  // Moving between the two copies of one session moves only the cursor, which
  // is what makes the next step continue from there. It is not a click on the
  // session: the selection is unchanged, so neither is the view — a step taken
  // on the dashboard does not throw the user out of it just for passing over
  // the favourite's second row.
  if (entry.id === get(selectedSessionId)) cursor.set(entry);
  else selectSidebarEntry(entry.id, entry.section);
}

export function selectPrevSession() {
  step(-1);
}

export function selectNextSession() {
  step(1);
}
