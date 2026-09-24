/**
 * Which tab a task is assigned to, and the panel's "This tab" filter.
 *
 * A task names its tab by the tab's stable ID — a followed window's `id`, or
 * "main" for the session's own window — never by window index: indexes change
 * on restart, on renumbering and on a restore from the trash, and a task
 * pointing at "window 3" would quietly move to whatever tab took that number.
 *
 * Kept free of imports so node can test it directly.
 */

/** The session's own window; it is not a followed window and has no stored ID. */
export const MAIN_TAB_ID = 'main';

export interface StoredTab {
  id?: string;
  index?: number;
  name?: string;
}

export interface SessionWithTabs {
  id?: string;
  name?: string;
  mainWindowIndex?: number;
  followedWindows?: StoredTab[] | null;
}

export interface TaskTab {
  id: string;
  name: string;
  windowIdx: number;
}

export interface TabAssignable {
  sessionId?: string;
  tabId?: string;
}

export type TaskTabFilter = 'all' | 'tab';

/**
 * The tabs a task can be given to: the main tab first, then every followed tab
 * that has an ID. Names fall back the way the tab bar's do, so the dialog and
 * the bar call an unnamed tab the same thing.
 */
export function sessionTabs(session: SessionWithTabs | null | undefined): TaskTab[] {
  if (!session) return [];
  const tabs: TaskTab[] = [{
    id: MAIN_TAB_ID,
    name: session.name || MAIN_TAB_ID,
    windowIdx: session.mainWindowIndex ?? 0,
  }];
  for (const window of session.followedWindows || []) {
    if (!window?.id || typeof window.index !== 'number') continue;
    tabs.push({ id: window.id, name: window.name || `Tab ${window.index}`, windowIdx: window.index });
  }
  return tabs;
}

/**
 * The tab a task is assigned to in this session, or null.
 *
 * Null as well when the tab no longer exists, or when the task belongs to
 * another session sharing the same task list — every session has a "main", and
 * another session's main tab is not this one. Such a task is shown and sent as
 * unassigned; its stored ID is left alone, so nothing is lost if the tab comes
 * back from the trash.
 */
export function resolveTaskTab(
  task: TabAssignable,
  sessionId: string | null | undefined,
  tabs: TaskTab[],
): TaskTab | null {
  if (!task.tabId || !sessionId || task.sessionId !== sessionId) return null;
  return tabs.find(tab => tab.id === task.tabId) ?? null;
}

/** The stable ID of the tab at a window index, or null for an untracked window. */
export function tabIdAtWindow(tabs: TaskTab[], windowIdx: number): string | null {
  return tabs.find(tab => tab.windowIdx === windowIdx)?.id ?? null;
}

/**
 * Narrow the list to the current tab's tasks when "This tab" is chosen.
 *
 * A window with no stable ID (one the app does not track) has no tasks of its
 * own, so the list is empty there rather than falling back to all of them —
 * falling back would look like the filter had stopped working.
 */
export function filterTasksByTab<T extends TabAssignable>(
  tasks: T[],
  filter: TaskTabFilter,
  currentTabId: string | null,
  sessionId: string | null | undefined,
  tabs: TaskTab[],
): T[] {
  if (filter !== 'tab') return tasks;
  if (!currentTabId) return [];
  return tasks.filter(task => resolveTaskTab(task, sessionId, tabs)?.id === currentTabId);
}

/** Anything unrecognised — nothing stored yet, an older value — reads as "all". */
export function parseTaskTabFilter(raw: unknown): TaskTabFilter {
  return raw === 'tab' ? 'tab' : 'all';
}

/**
 * Which halves of the [All | This tab] switch have unfinished tasks behind
 * them, for the dots on it. Unfinished is anything not done, as the backend
 * counts it (deferred included). Unfinished rather than any: a finished list
 * is not worth opening, and "All" would otherwise be marked nearly always.
 */
export function openTaskPresence<T extends TabAssignable & { status?: string }>(
  tasks: T[],
  currentTabId: string | null,
  sessionId: string | null | undefined,
  tabs: TaskTab[],
): Record<TaskTabFilter, boolean> {
  const open = tasks.filter(task => task.status !== 'done');
  return {
    all: open.length > 0,
    tab: filterTasksByTab(open, 'tab', currentTabId, sessionId, tabs).length > 0,
  };
}
