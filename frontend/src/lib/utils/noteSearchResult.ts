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
  /** The page of the note the match is on. */
  pageId?: string;
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
  /** The page to open, when the result names one. */
  pageId?: string;
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
    return { sessionId: session.id, windowIdx: null, scope: 'session', ...page(entry) };
  }
  if (entry.tabId === 'main') {
    return { sessionId: session.id, windowIdx: session.mainWindowIndex ?? 0, scope: 'tab', ...page(entry) };
  }
  const tab = session.followedWindows?.find((w) => !!entry.tabId && w.id === entry.tabId)
    // Stored tabs all carry an ID by now; the index is only the fallback.
    ?? (entry.tabId ? undefined : session.followedWindows?.find((w) => w.index === entry.windowIdx));
  if (!tab) return null;
  return { sessionId: session.id, windowIdx: tab.index, scope: 'tab', ...page(entry) };
}

function page(entry: NoteResultFields): { pageId?: string } {
  return entry.pageId ? { pageId: entry.pageId } : {};
}

type Translate = (key: string, params?: Record<string, string | number>) => string;

/**
 * The page a note result is on, as the notes view names it: its title, or
 * "Note 2" for an untitled page among several. Empty for a note that is a
 * single untitled page — there is no page to name.
 */
export function notePageName(
  entry: { pageTitle?: string; pageIndex?: number; pageCount?: number },
  t: Translate,
): string {
  if (entry.pageTitle) return entry.pageTitle;
  if ((entry.pageCount ?? 0) > 1) return t('notes.untitledPageN', { n: (entry.pageIndex ?? 0) + 1 });
  return '';
}
