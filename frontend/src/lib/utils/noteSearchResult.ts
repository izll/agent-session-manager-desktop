/**
 * Note results of the global search: telling them from conversation results,
 * and finding the tab and note a result should open. Plain data in and out,
 * so the rules can be run under plain node.
 */

export type NoteResultScope = 'tab' | 'session';

/** The fields of a global search result that concern notes. */
export interface NoteResultFields {
  kind?: string;
  sessionId: string;
  noteScope?: string;
  tabId?: string;
  windowIdx?: number;
}

/** The parts of a session the lookup reads. */
export interface NoteResultSession {
  id: string;
  mainWindowIndex?: number;
  followedWindows?: { id?: string; index: number }[];
}

export interface NoteTarget {
  sessionId: string;
  /** The tab to select, or null to leave the session on the tab it remembers. */
  windowIdx: number | null;
  scope: NoteResultScope;
}

export function isNoteResult(entry: { kind?: string }): boolean {
  return entry.kind === 'note';
}

/**
 * Where a note result leads, read against the session list as it is now.
 *
 * A tab is found by its ID rather than the window index the search saw: the
 * index changes when tabs are reordered or tmux renumbers them, the ID does
 * not. The main tab's index the backend does not even know without asking
 * tmux; the session list already carries it. The session's own note belongs
 * to no tab, so opening it leaves whichever tab the session was on.
 *
 * Null when the session or tab has gone since the search ran.
 */
export function resolveNoteTarget(
  entry: NoteResultFields,
  sessions: NoteResultSession[],
): NoteTarget | null {
  if (!isNoteResult(entry)) return null;
  const session = sessions.find((s) => s.id === entry.sessionId);
  if (!session) return null;
  if (entry.noteScope === 'session') {
    return { sessionId: session.id, windowIdx: null, scope: 'session' };
  }
  if (entry.tabId === 'main') {
    return { sessionId: session.id, windowIdx: session.mainWindowIndex ?? 0, scope: 'tab' };
  }
  const tab = session.followedWindows?.find((w) => !!entry.tabId && w.id === entry.tabId)
    // Stored tabs all carry an ID by now; the index is only the fallback.
    ?? (entry.tabId ? undefined : session.followedWindows?.find((w) => w.index === entry.windowIdx));
  if (!tab) return null;
  return { sessionId: session.id, windowIdx: tab.index, scope: 'tab' };
}
