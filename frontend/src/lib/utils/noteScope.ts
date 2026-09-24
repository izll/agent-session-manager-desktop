/**
 * Which of a tab's two notes — its own and the session's — have something in
 * them, for the marks on the notes view's switch. Plain data in and out, so
 * the rule can be run under plain node.
 */

export type NotesScope = 'tab' | 'session';

export interface NoteSources {
  /** The note open in the editor. */
  open: NotesScope;
  /** What the editor holds, saved or not. */
  openText: string;
  /**
   * The other note as this view last had it — a draft kept from an earlier
   * visit — or undefined when it has not been opened since loading.
   */
  otherDraft?: string;
  /** Both notes as stored, from the session list. */
  storedTab: string;
  storedSession: string;
}

/**
 * The open note is what the editor holds, so the mark follows typing. The
 * other one is this view's own copy if it has one — the session list is only
 * reloaded on events and would miss an edit made a moment ago — and what is
 * stored otherwise. Whitespace alone is not a note.
 */
export function notePresence(sources: NoteSources): Record<NotesScope, boolean> {
  const has = (text: string | undefined) => !!text && text.trim() !== '';
  const other: NotesScope = sources.open === 'tab' ? 'session' : 'tab';
  const stored = other === 'tab' ? sources.storedTab : sources.storedSession;
  return {
    [sources.open]: has(sources.openText),
    [other]: has(sources.otherDraft ?? stored),
  } as Record<NotesScope, boolean>;
}
