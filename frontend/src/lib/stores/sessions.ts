import { writable, derived, get } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';
import type { main } from '../../../wailsjs/go/models';
import { showSessionView } from './navigation';

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
  notes: string;
  favorite: boolean;
  resumeSessionId: string;
  followedWindows: any[];
  tabOrder: number[];
  mainWindowStopped: boolean;
  extraArgs: string;
  tabTextColor: string;
  tabBackgroundColor: string;
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

export const favorites = derived(sessions, ($sessions) =>
  $sessions.filter(s => s.favorite)
);

export const ungroupedSessions = derived(
  [sessions, searchFilter],
  ([$sessions, $searchFilter]) => {
    let filtered = $sessions.filter(s => !s.groupId && !s.favorite);
    if ($searchFilter) {
      const lower = $searchFilter.toLowerCase();
      filtered = filtered.filter(s =>
        s.name.toLowerCase().includes(lower) ||
        s.notes?.toLowerCase().includes(lower)
      );
    }
    return filtered;
  }
);

export const sessionsByGroup = derived(
  [sessions, groups, searchFilter],
  ([$sessions, $groups, $searchFilter]) => {
    const result: Map<string, Session[]> = new Map();

    for (const group of $groups) {
      let groupSessions = $sessions.filter(s => s.groupId === group.id);
      if ($searchFilter) {
        const lower = $searchFilter.toLowerCase();
        groupSessions = groupSessions.filter(s =>
          s.name.toLowerCase().includes(lower) ||
          s.notes?.toLowerCase().includes(lower)
        );
      }
      result.set(group.id, groupSessions);
    }

    return result;
  }
);

// Actions
export async function loadSessions() {
  isLoading.set(true);
  error.set(null);
  try {
    const [sessionsData, groupsData] = await Promise.all([
      App.GetSessions(),
      App.GetGroups()
    ]);
    sessions.set(sessionsData as Session[]);
    groups.set(groupsData as Group[]);
  } catch (e) {
    console.error('Failed to load sessions:', e);
    error.set(String(e));
  } finally {
    isLoading.set(false);
  }
}

export async function createSession(name: string, path: string, agent: string, autoYes: boolean = false, extraArgs: string = '') {
  try {
    const session = await App.CreateSession(name, path, agent, autoYes, extraArgs);
    if (session) {
      sessions.update(s => [...s, session as Session]);
      selectedSessionId.set(session.id);
      selectedWindowIdx.set(0);
      showSessionView();
    }
    return session;
  } catch (e) {
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
  try {
    // Drop BEFORE the backend call: by the time it returns the new tmux
    // session is up, and any subsequent pool.show() must create a fresh
    // WebSocket against it.
    dropPoolForSession(id);
    if (resumeId) {
      await App.StartSessionWithResume(id, resumeId);
    } else {
      await App.StartSession(id);
    }
    // Reset to window 0 when session starts (windows are recreated with potentially new indices)
    if (get(selectedSessionId) === id) {
      selectedWindowIdx.set(0);
    }
    await loadSessions();
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export async function stopSession(id: string) {
  try {
    await App.StopSession(id);
    dropPoolForSession(id);
    // Reset to window 0 when session stops
    if (get(selectedSessionId) === id) {
      selectedWindowIdx.set(0);
    }
    await loadSessions();
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export async function stopTab(id: string, windowIdx: number) {
  try {
    await App.StopTab(id, windowIdx);
    // Reset to window 0 when main tab stops (kills entire session)
    if (windowIdx === 0 && get(selectedSessionId) === id) {
      selectedWindowIdx.set(0);
    }
    await loadSessions();
  } catch (e) {
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
  try {
    await App.RestartTab(id, windowIdx);
    dropPoolForWindow(id, windowIdx);
    await loadSessions();
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export async function restartTabWithResume(id: string, windowIdx: number, resumeId: string) {
  try {
    await App.RestartTabWithResume(id, windowIdx, resumeId);
    dropPoolForWindow(id, windowIdx);
    await loadSessions();
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export async function deleteTab(id: string, windowIdx: number) {
  try {
    await App.DeleteTab(id, windowIdx);
    // Switch to window 0 if the deleted tab was selected
    if (get(selectedSessionId) === id && get(selectedWindowIdx) === windowIdx) {
      selectedWindowIdx.set(0);
    }
    await loadSessions();
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export async function renameSession(id: string, name: string) {
  try {
    await App.RenameSession(id, name);
    sessions.update(s => s.map(sess =>
      sess.id === id ? { ...sess, name } : sess
    ));
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export async function renameTab(sessionId: string, windowIdx: number, name: string) {
  try {
    await App.RenameTab(sessionId, windowIdx, name);
    await loadSessions();
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export async function deleteSession(id: string) {
  try {
    await App.DeleteSession(id);
    sessions.update(s => s.filter(sess => sess.id !== id));
    if (get(selectedSessionId) === id) {
      selectedSessionId.set(null);
    }
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export async function toggleFavorite(id: string) {
  try {
    await App.ToggleFavorite(id);
    sessions.update(s => s.map(sess =>
      sess.id === id ? { ...sess, favorite: !sess.favorite } : sess
    ));
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export async function toggleAutoYes(id: string) {
  try {
    await App.ToggleAutoYes(id);
    sessions.update(s => s.map(sess =>
      sess.id === id ? { ...sess, autoYes: !sess.autoYes } : sess
    ));
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

// Cycle the YOLO/permission mode of a running Claude window by sending Shift+Tab
// to its pane (no restart). The live indicator updates on the next poll. Falls
// back to ToggleAutoYes (stored flag + restart) for stopped/non-Claude windows.
export async function cycleYoloMode(id: string, windowIdx: number) {
  try {
    await App.CycleYoloMode(id, windowIdx);
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export async function setSessionColor(id: string, color: string, bgColor: string, fullRow: boolean) {
  try {
    await App.SetSessionColor(id, color, bgColor, fullRow);
    sessions.update(s => s.map(sess =>
      sess.id === id ? { ...sess, color, bgColor, fullRowColor: fullRow } : sess
    ));
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export async function assignToGroup(sessionId: string, groupId: string) {
  try {
    await App.AssignToGroup(sessionId, groupId);
    sessions.update(s => s.map(sess =>
      sess.id === sessionId ? { ...sess, groupId } : sess
    ));
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export async function createGroup(name: string) {
  try {
    const group = await App.CreateGroup(name);
    if (group) {
      groups.update(g => [...g, group as Group]);
    }
    return group;
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export async function deleteGroup(id: string) {
  try {
    await App.DeleteGroup(id);
    groups.update(g => g.filter(group => group.id !== id));
    // Unassign sessions from deleted group
    sessions.update(s => s.map(sess =>
      sess.groupId === id ? { ...sess, groupId: '' } : sess
    ));
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export async function renameGroup(id: string, name: string) {
  try {
    await App.RenameGroup(id, name);
    groups.update(g => g.map(group =>
      group.id === id ? { ...group, name } : group
    ));
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export async function toggleGroupCollapse(id: string) {
  try {
    await App.ToggleGroupCollapse(id);
    groups.update(g => g.map(group =>
      group.id === id ? { ...group, collapsed: !group.collapsed } : group
    ));
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export function selectSession(id: string | null) {
  // Save current tab for the session we're leaving
  const prevId = get(selectedSessionId);
  if (prevId) {
    sessionTabMemory.set(prevId, get(selectedWindowIdx));
  }

  selectedSessionId.set(id);
  // Restore remembered tab for the session we're switching to
  selectedWindowIdx.set(id ? (sessionTabMemory.get(id) ?? 0) : 0);
  if (id) showSessionView();
}

export function selectWindow(idx: number) {
  selectedWindowIdx.set(idx);
}

export async function reorderSession(id: string, direction: number) {
  try {
    await App.ReorderSession(id, direction);
    await loadSessions();
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export async function reorderTab(sessionId: string, fromIdx: number, toIdx: number) {
  try {
    await App.ReorderTab(sessionId, fromIdx, toIdx);
    await loadSessions();
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export async function moveSessionToIndex(id: string, targetIndex: number) {
  try {
    await App.MoveSessionToIndex(id, targetIndex);
    await loadSessions();
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export function selectPrevSession() {
  const currentSessions = get(sessions);
  const currentId = get(selectedSessionId);
  if (!currentId || currentSessions.length === 0) return;

  const currentIdx = currentSessions.findIndex(s => s.id === currentId);
  if (currentIdx > 0) {
    selectSession(currentSessions[currentIdx - 1].id);
  }
}

export function selectNextSession() {
  const currentSessions = get(sessions);
  const currentId = get(selectedSessionId);
  if (!currentId || currentSessions.length === 0) return;

  const currentIdx = currentSessions.findIndex(s => s.id === currentId);
  if (currentIdx < currentSessions.length - 1) {
    selectSession(currentSessions[currentIdx + 1].id);
  }
}
