import { writable, derived, get, readable } from 'svelte/store';
import { lastActive } from './statusLines';
import { compareByActivity } from '../utils/activityOrder';
import {
  activityFilterFrom,
  activityTime,
  isActivityFilterOn,
  matchesActivityFilter,
  matchesSearch,
} from '../utils/sessionFilter';
import * as App from '../../../wailsjs/go/main/App';
import type { main } from '../../../wailsjs/go/models';
import { showSessionView } from './navigation';
import { settings, saveSettings } from './settings';
import { invalidateSidebarProject } from './sidebarPolling';
import { activeProjectId } from './projects';

// Types
export interface Session {
  id: string;
  name: string;
  path: string;
  status: 'running' | 'paused' | 'stopped';
  agent: string;
  color: string;
  bgColor: string;
  fullRowColor: boolean;
  groupId: string;
  autoYes: boolean;
  hideStatusLine: boolean;
  notes: string;
  /** The main tab's own note; notes is the session's. */
  mainTabNotes?: string;
  favorite: boolean;
  resumeSessionId: string;
  /** RFC3339 of the last activity, or empty when never recorded. */
  updatedAt?: string;
  followedWindows: any[];
  tabOrder: number[];
  mainWindowStopped: boolean;
  serverId?: string;
  serverName?: string;
  extraArgs: string;
  tabTextColor: string;
  tabBackgroundColor: string;
  terminalTheme: string;
  /** Per-session font size override; 0 inherits the global setting. */
  terminalFontSize: number;
  /** View bar: 0 follow the global setting, 1 hide, 2 show. */
  hideViewBar: number;
  /** Bottom status bar, same tri-state. */
  hideStatusBar: number;
  /** Main window's tmux index — not always 0. */
  mainWindowIndex: number;
  /** Tab the session was last left on; may no longer exist. */
  lastWindowIndex: number;
  /** False outside a git repository — there is nothing to diff. */
  isGitRepo: boolean;
}

export interface Group {
  id: string;
  name: string;
  collapsed: boolean;
  color: string;
  bgColor: string;
  fullRowColor: boolean;
}

// Stores
export const sessions = writable<Session[]>([]);
export const groups = writable<Group[]>([]);
export const selectedSessionId = writable<string | null>(null);
export const selectedWindowIdx = writable<number>(0);

// Per-session tab memory (sessionId -> last active window index)
const sessionTabMemory = new Map<string, number>();
export const searchFilter = writable<string>('');
export const isLoading = writable<boolean>(false);
export const error = writable<string | null>(null);

// Derived stores
export const selectedSession = derived(
  [sessions, selectedSessionId],
  ([$sessions, $selectedSessionId]) =>
    $sessions.find(s => s.id === $selectedSessionId) || null
);

/**
 * A minute-by-minute clock for the activity filter's time window.
 *
 * The session list and the live activity times change only when something
 * happens, so without this a session that crossed the edge of "last 1 day"
 * while nothing else moved would stay listed until the next change.
 */
const minuteClock = readable(Date.now(), (set) => {
  const timer = setInterval(() => set(Date.now()), 60_000);
  // Never the reason a process (a test run) stays alive.
  (timer as { unref?: () => void }).unref?.();
  return () => clearInterval(timer);
});

/** The sidebar's activity filter, from the settings it is saved in. */
export const sidebarActivityFilter = derived(settings, ($settings) => activityFilterFrom($settings));

/**
 * Whether a session is shown in the sidebar list: it matches the search and
 * passes the activity filter. Every list below goes through this one test, so
 * the sort order cannot change what is shown.
 *
 * The time is the live poll's where there is one: the loaded updatedAt is a
 * snapshot from the last reload and goes stale within seconds.
 */
export const sessionVisible = derived(
  [searchFilter, sidebarActivityFilter, lastActive, minuteClock],
  ([$searchFilter, $filter, $lastActive, $now]) => {
    const now = Math.max($now, Date.now());
    return (s: Session) =>
      matchesSearch(s, $searchFilter) &&
      matchesActivityFilter(s, activityTime($lastActive[s.id] || s.updatedAt), $filter, now);
  },
);

/**
 * Every session in one flat list, most recently active first.
 *
 * No groups and no favourites section: this view answers "where was I", and a
 * session pinned to the top for being important is not an answer to that. The
 * ordinary list is still there behind the toggle for everything else.
 *
 * Filtered like the grouped lists are. It used to leave the search out, so
 * with the list sorted the search box filtered nothing.
 */
export const sessionsByActivity = derived(
  [sessions, lastActive, sessionVisible],
  ([$sessions, $lastActive, $visible]) => {
    // A session with no recorded activity sorts last rather than first.
    const timeOf = (s: Session) => activityTime($lastActive[s.id] || s.updatedAt);
    const now = Date.now();
    return $sessions
      .filter($visible)
      .sort((a, b) =>
        compareByActivity({ name: a.name, time: timeOf(a) }, { name: b.name, time: timeOf(b) }, now));
  },
);

export const favorites = derived(
  [sessions, sessionVisible],
  ([$sessions, $visible]) => $sessions.filter(s => s.favorite && $visible(s)),
);

/** Is the sidebar's favourites section folded down to its header? */
export const favoritesCollapsed = derived(settings, ($settings) => !!$settings?.favoritesCollapsed);

export function toggleFavoritesCollapsed() {
  return saveSettings({ favoritesCollapsed: !get(favoritesCollapsed) });
}

/**
 * The sessions shown below the groups.
 *
 * A favourite without a group is normally shown only in the favourites
 * section. While that section is folded it comes back here, in its usual
 * place: folding the section hides the extra copies, not the sessions.
 */
export const ungroupedSessions = derived(
  [sessions, sessionVisible, favoritesCollapsed],
  ([$sessions, $visible, $collapsed]) =>
    $sessions.filter(s => !s.groupId && (!s.favorite || $collapsed) && $visible(s)),
);

export const sessionsByGroup = derived(
  [sessions, groups, sessionVisible],
  ([$sessions, $groups, $visible]) => {
    const result: Map<string, Session[]> = new Map();
    for (const group of $groups) {
      result.set(group.id, $sessions.filter(s => s.groupId === group.id && $visible(s)));
    }
    return result;
  }
);

/**
 * Is anything narrowing the sidebar list — the search or the activity filter?
 * While it is, groups with nothing left to show are hidden rather than left as
 * empty headers, and an empty list says it was filtered empty.
 */
export const sidebarFiltered = derived(
  [searchFilter, sidebarActivityFilter],
  ([$searchFilter, $filter]) => !!$searchFilter.trim() || isActivityFilterOn($filter),
);

// Actions
let sessionProjectGeneration = 0;
let sessionsLoadGeneration = 0;

type ProjectOperationTarget = { generation: number; projectId: string };

function projectTarget(): ProjectOperationTarget {
  return { generation: sessionProjectGeneration, projectId: get(activeProjectId) };
}

function projectTargetIsCurrent(target: ProjectOperationTarget): boolean {
  return target.generation === sessionProjectGeneration && target.projectId === get(activeProjectId);
}

/** Called before the backend's active project changes. */
export function invalidateSessionProject() {
  sessionProjectGeneration++;
  sessionsLoadGeneration++;
  // Session ids are only unique inside a project. Do not let the previous
  // project's selected id/tab memory flow into a replacement project, and do
  // not use selectSession(null) here: that helper persists the old session's
  // tab through the backend whose active project may already be changing.
  sessionTabMemory.clear();
  selectedSessionId.set(null);
  selectedWindowIdx.set(0);
  isLoading.set(false);
  error.set(null);
  // Session ids are project-scoped. Reusing an id in the next project must
  // not briefly inherit the old project's busy/waiting text or tab badges.
  invalidateSidebarProject();
}

export async function loadSessions() {
  const target = projectTarget();
  const generation = ++sessionsLoadGeneration;
  isLoading.set(true);
  error.set(null);
  try {
    const [sessionsData, groupsData] = await Promise.all([
      App.GetSessions(),
      App.GetGroups()
    ]);
    if (!projectTargetIsCurrent(target) || generation !== sessionsLoadGeneration) return;
    sessions.set(sessionsData as Session[]);
    groups.set(groupsData as Group[]);
  } catch (e) {
    if (!projectTargetIsCurrent(target) || generation !== sessionsLoadGeneration) return;
    console.error('Failed to load sessions:', e);
    error.set(String(e));
  } finally {
    if (projectTargetIsCurrent(target) && generation === sessionsLoadGeneration) isLoading.set(false);
  }
}

export async function createSession(name: string, path: string, agent: string, autoYes: boolean = false, extraArgs: string = '', serverId: string = '', worktree: boolean = false) {
  const target = projectTarget();
  try {
    const session = await App.CreateSessionWithWorktree(
      name, path, agent, autoYes, extraArgs, target.projectId, serverId, worktree);
    // The session was durably created in the project that owned the bridge
    // call, but callers must not continue a multi-step create/assign/start
    // workflow in the replacement project using the same generated id.
    if (!projectTargetIsCurrent(target)) return;
    if (session) {
      sessions.update(s => [...s, session as Session]);
      selectedSessionId.set(session.id);
      selectedWindowIdx.set(0);
      showSessionView();
    }
    return session;
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

// Tell the Terminal pool that every cached PoolEntry for this session is
// stale and must be dropped. Necessary on every start/stop because the
// backend kills/recreates the whole tmux session (which also wipes any
// grouped gui_* mirrors); a cached WebSocket would point at nothing.
// The 3-second grace-period in Terminal.svelte's handlePoolChange can
// race against a quick stop+start and leave the old entry in place,
// which is the bug this guards against.
function dropPoolForSession(id: string) {
  try {
    window.dispatchEvent(new CustomEvent('terminal:destroy-session', {
      detail: { sessionId: id },
    }));
  } catch { /* no-op outside browser context */ }
}

export async function startSession(id: string, resumeId?: string) {
  const target = projectTarget();
  try {
    // Drop BEFORE the backend call: by the time it returns the new tmux
    // session is up, and any subsequent pool.show() must create a fresh
    // WebSocket against it.
    dropPoolForSession(id);
    if (resumeId) {
      await App.StartSessionWithResume(id, resumeId, target.projectId);
    } else {
      await App.StartSession(id, target.projectId);
    }
    if (!projectTargetIsCurrent(target)) return;
    // Reset to window 0 when session starts (windows are recreated with potentially new indices)
    if (get(selectedSessionId) === id) {
      selectedWindowIdx.set(0);
    }
    await loadSessions();
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

export async function stopSession(id: string) {
  const target = projectTarget();
  try {
    await App.StopSession(id, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    dropPoolForSession(id);
    // Reset to window 0 when session stops
    if (get(selectedSessionId) === id) {
      selectedWindowIdx.set(0);
    }
    await loadSessions();
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

export async function stopTab(id: string, windowIdx: number) {
  const target = projectTarget();
  try {
    await App.StopTab(id, windowIdx, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    // Reset to window 0 when main tab stops (kills entire session)
    if (windowIdx === 0 && get(selectedSessionId) === id) {
      selectedWindowIdx.set(0);
    }
    await loadSessions();
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

// Drop the cached PoolEntry for a specific (sessionId, windowIdx) — used
// after the backend respawns the pane in that window so the next
// pool.show() builds a fresh WebSocket + xterm against the new process.
function dropPoolForWindow(id: string, windowIdx: number) {
  try {
    window.dispatchEvent(new CustomEvent('terminal:destroy-window', {
      detail: { sessionId: id, windowIdx },
    }));
  } catch { /* no-op */ }
}

export async function restartTab(id: string, windowIdx: number) {
  const target = projectTarget();
  try {
    await App.RestartTab(id, windowIdx, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    dropPoolForWindow(id, windowIdx);
    await loadSessions();
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

// Starting one tab of a *stopped* session is a different backend call from
// restarting one tab of a running session: there is no pane to respawn yet.
export async function startTabOnly(id: string, windowIdx: number) {
  const target = projectTarget();
  try {
    await App.StartTabOnly(id, windowIdx, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    dropPoolForWindow(id, windowIdx);
    await loadSessions();
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

// What a restart interrupted, for the active project. Asked on launch and
// after a project switch; empty on an ordinary relaunch.
export async function getInterruptedWork(): Promise<main.InterruptedWork | null> {
  const target = projectTarget();
  const work = await App.GetInterruptedWork();
  if (!projectTargetIsCurrent(target) || work.projectId !== target.projectId) return null;
  return work;
}

// Brings the chosen sessions back and forgets the rest. Each session's result
// comes back rather than one error for all: one that fails must not hide the
// ones that came back.
export async function reopenInterruptedSessions(reopen: string[], dismiss: string[]) {
  const target = projectTarget();
  if (dismiss.length > 0) await App.DismissInterruptedWork(dismiss, target.projectId);
  if (reopen.length === 0) return [];
  for (const id of reopen) dropPoolForSession(id);
  const results = await App.ReopenInterruptedSessions(reopen, target.projectId);
  if (projectTargetIsCurrent(target)) await loadSessions();
  return results;
}

export async function restartTabWithResume(id: string, windowIdx: number, resumeId: string) {
  const target = projectTarget();
  try {
    await App.RestartTabWithResume(id, windowIdx, resumeId, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    dropPoolForWindow(id, windowIdx);
    await loadSessions();
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

export async function deleteTab(id: string, windowIdx: number) {
  const target = projectTarget();
  try {
    await App.DeleteTab(id, windowIdx, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    dropPoolForWindow(id, windowIdx);
    // Switch to window 0 if the deleted tab was selected
    if (get(selectedSessionId) === id && get(selectedWindowIdx) === windowIdx) {
      selectedWindowIdx.set(0);
    }
    const currentSettings = get(settings);
    if (currentSettings.splitView && currentSettings.markedSessionId === id &&
        currentSettings.markedWindowIdx === windowIdx) {
      await saveSettings({ splitView: false, markedSessionId: '', markedWindowIdx: 0 }, target.projectId);
      if (!projectTargetIsCurrent(target)) return;
    }
    await loadSessions();
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

export async function renameSession(id: string, name: string) {
  const target = projectTarget();
  try {
    await App.RenameSession(id, name, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    sessions.update(s => s.map(sess =>
      sess.id === id ? { ...sess, name } : sess
    ));
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

export async function renameTab(sessionId: string, windowIdx: number, name: string) {
  const target = projectTarget();
  try {
    await App.RenameTab(sessionId, windowIdx, name, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    await loadSessions();
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

export async function deleteSession(id: string) {
  const target = projectTarget();
  try {
    await App.DeleteSession(id, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    dropPoolForSession(id);
    const currentSettings = get(settings);
    if (currentSettings.splitView && currentSettings.markedSessionId === id) {
      await saveSettings({ splitView: false, markedSessionId: '', markedWindowIdx: 0 }, target.projectId);
      if (!projectTargetIsCurrent(target)) return;
    }
    sessionTabMemory.delete(id);
    sessions.update(s => s.filter(sess => sess.id !== id));
    if (get(selectedSessionId) === id) {
      selectedSessionId.set(null);
    }
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

export async function toggleFavorite(id: string) {
  const target = projectTarget();
  try {
    await App.ToggleFavorite(id, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    // A refresh can land after the backend has toggled but before this Wails
    // promise resolves. Inverting that already-fresh store value would put the
    // UI back on the old state while disk is on the new one. Re-read backend
    // truth instead; loadSessions also drops an older overlapping refresh.
    await loadSessions();
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

export async function toggleAutoYes(id: string) {
  const target = projectTarget();
  try {
    await App.ToggleAutoYes(id, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    await loadSessions();
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

// Cycle the YOLO/permission mode of a running Claude window by sending Shift+Tab
// to its pane (no restart). The live indicator updates on the next poll. Falls
// back to the stored flag for stopped/non-Claude windows: the session's for the
// main window (restarting the session), the tab's own for a tab (restarting just
// that tab, whose terminal is then rebuilt as after any tab restart).
export async function cycleYoloMode(id: string, windowIdx: number) {
  const target = projectTarget();
  try {
    const restartedTab = await App.CycleYoloMode(id, windowIdx, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    if (restartedTab) dropPoolForWindow(id, windowIdx);
    // The stored flags changed; the button reads them from the session list.
    await loadSessions();
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

export async function setSessionColor(id: string, color: string, bgColor: string, fullRow: boolean) {
  const target = projectTarget();
  try {
    await App.SetSessionColor(id, color, bgColor, fullRow, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    sessions.update(s => s.map(sess =>
      sess.id === id ? { ...sess, color, bgColor, fullRowColor: fullRow } : sess
    ));
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

export async function assignToGroup(sessionId: string, groupId: string) {
  const target = projectTarget();
  try {
    await App.AssignToGroup(sessionId, groupId, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    sessions.update(s => s.map(sess =>
      sess.id === sessionId ? { ...sess, groupId } : sess
    ));
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

export async function createGroup(name: string) {
  const target = projectTarget();
  try {
    const group = await App.CreateGroup(name, target.projectId);
    if (!projectTargetIsCurrent(target)) return group;
    if (group) {
      groups.update(g => [...g, group as Group]);
    }
    return group;
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

export async function deleteGroup(id: string) {
  const target = projectTarget();
  try {
    await App.DeleteGroup(id, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    groups.update(g => g.filter(group => group.id !== id));
    // Unassign sessions from deleted group
    sessions.update(s => s.map(sess =>
      sess.groupId === id ? { ...sess, groupId: '' } : sess
    ));
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

export async function renameGroup(id: string, name: string) {
  const target = projectTarget();
  try {
    await App.RenameGroup(id, name, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    groups.update(g => g.map(group =>
      group.id === id ? { ...group, name } : group
    ));
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

/**
 * Reorders a group. Unlike the other group helpers we reload instead of
 * patching the store: order IS the array order on the Go side, and newIndex is
 * clamped there, so replaying the move locally could drift from what was saved.
 */
export async function moveGroup(id: string, newIndex: number) {
  const target = projectTarget();
  try {
    await App.MoveGroup(id, newIndex, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    await loadSessions();
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

export async function setGroupColor(id: string, color: string, bgColor: string, fullRow: boolean) {
  const target = projectTarget();
  try {
    await App.SetGroupColor(id, color, bgColor, fullRow, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    groups.update(g => g.map(group =>
      group.id === id ? { ...group, color, bgColor, fullRowColor: fullRow } : group
    ));
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

export async function toggleGroupCollapse(id: string) {
  const target = projectTarget();
  try {
    await App.ToggleGroupCollapse(id, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    await loadSessions();
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

/** Tabs a session currently has: its main window plus every followed one. */
function availableWindowIndexes(session: Session | undefined): number[] {
  if (!session) return [];
  const main = session.mainWindowIndex ?? 0;
  const followed = (session.followedWindows || [])
    .map((w: any) => w?.index)
    .filter((i: any): i is number => typeof i === 'number');
  return [main, ...followed];
}

/**
 * The tab to open a session on: the one it was left on, if it still exists.
 * Anything stale (tab closed, session restarted with fewer windows) falls
 * back to the main window rather than leaving a blank pane.
 */
function resolveInitialWindow(id: string): number {
  const remembered = sessionTabMemory.get(id);
  const session = get(sessions).find(s => s.id === id);
  const available = availableWindowIndexes(session);
  const main = session?.mainWindowIndex ?? 0;

  // Within this run, in-memory memory wins; across restarts it's empty and
  // the persisted value takes over.
  const candidate = remembered ?? session?.lastWindowIndex;
  if (typeof candidate !== 'number') return main;

  // Can't validate without a loaded session — trust it, the tab bar clamps.
  if (available.length === 0) return candidate;
  return available.includes(candidate) ? candidate : main;
}

export function selectSession(id: string | null) {
  // Save current tab for the session we're leaving
  const prevId = get(selectedSessionId);
  if (prevId) {
    sessionTabMemory.set(prevId, get(selectedWindowIdx));
    persistLastWindow(prevId, get(selectedWindowIdx));
  }

  selectedSessionId.set(id);
  // Restore the remembered tab, validated against the tabs that still exist
  selectedWindowIdx.set(id ? resolveInitialWindow(id) : 0);
  // Persist which session this is, so the next launch opens it. Best-effort:
  // failing to record it must never get in the way of switching.
  if (id) void persistLastSession(id);
  if (id) showSessionView();
}

/**
 * Record the tab for the next launch. Best-effort: remembering a tab is a
 * convenience, so a storage failure is logged and otherwise ignored rather
 * than surfaced as an error the user has to dismiss.
 */
function persistLastWindow(id: string, idx: number) {
  const projectId = get(activeProjectId);
  // The bridge call is intentionally fire-and-forget, but the backend project
  // is process-global. Pin the write to the project whose tab is being left so
  // an immediately-following project switch cannot write the same session id
  // in its replacement project.
  void App.SetLastWindowIndex(id, idx, projectId).catch(e => {
    console.warn('could not persist last tab', id, e);
  });
}

// Record which session is selected, so the next launch opens it. The tab within
// it is stored separately, on the session itself.
//
// Written on every switch rather than on shutdown: the app can be closed in
// ways that run no exit handler, and a preference that only survives a clean
// quit is one the user cannot rely on.
function persistLastSession(id: string) {
  if (get(settings).lastSessionId === id) return;
  void saveSettings({ lastSessionId: id });
}

export function selectWindow(idx: number) {
  selectedWindowIdx.set(idx);
  const id = get(selectedSessionId);
  if (id) {
    sessionTabMemory.set(id, idx);
    persistLastWindow(id, idx);
  }
}

export async function reorderSession(id: string, direction: number) {
  const target = projectTarget();
  try {
    await App.ReorderSession(id, direction, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    await loadSessions();
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

export async function reorderTab(sessionId: string, fromIdx: number, toIdx: number) {
  const target = projectTarget();
  try {
    await App.ReorderTab(sessionId, fromIdx, toIdx, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    await loadSessions();
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}

export async function moveSessionToIndex(id: string, targetIndex: number) {
  const target = projectTarget();
  try {
    await App.MoveSessionToIndex(id, targetIndex, target.projectId);
    if (!projectTargetIsCurrent(target)) return;
    await loadSessions();
  } catch (e) {
    if (!projectTargetIsCurrent(target)) return;
    error.set(String(e));
    throw e;
  }
}
