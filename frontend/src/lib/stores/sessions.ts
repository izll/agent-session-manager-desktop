import { writable, derived, get } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';
import type { main } from '../../../wailsjs/go/models';

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

export async function createSession(name: string, path: string, agent: string, autoYes: boolean = false) {
  try {
    const session = await App.CreateSession(name, path, agent, autoYes);
    if (session) {
      sessions.update(s => [...s, session as Session]);
      selectedSessionId.set(session.id);
    }
    return session;
  } catch (e) {
    error.set(String(e));
    throw e;
  }
}

export async function startSession(id: string, resumeId?: string) {
  try {
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
  selectedSessionId.set(id);
  selectedWindowIdx.set(0);
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
    selectedSessionId.set(currentSessions[currentIdx - 1].id);
  }
}

export function selectNextSession() {
  const currentSessions = get(sessions);
  const currentId = get(selectedSessionId);
  if (!currentId || currentSessions.length === 0) return;

  const currentIdx = currentSessions.findIndex(s => s.id === currentId);
  if (currentIdx < currentSessions.length - 1) {
    selectedSessionId.set(currentSessions[currentIdx + 1].id);
  }
}
