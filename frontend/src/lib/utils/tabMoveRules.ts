/**
 * The rules for moving tabs between sessions, kept free of stores and imports
 * so they can be tested on their own. The backend checks all of them again;
 * these only decide what the UI offers and how a drop target looks.
 */

/** The drag type a tab carries when it can be dropped on another session. */
export const TAB_DRAG_MIME = 'application/x-asmgr-tab';

export interface TabDragPayload {
  sessionId: string;
  windowIdx: number;
  projectId: string;
  name: string;
}

interface SessionTabs {
  followedWindows?: Array<{ index: number }> | null;
}

/**
 * Whether a tab can leave its session: any tab but the session's own window,
 * which is the session — moving that is merging the session.
 *
 * Answered from the stored tabs, as the backend does: the own window is the
 * one that is not among them, whatever index it has.
 */
export function isMovableTab(session: SessionTabs | null | undefined, windowIdx: number | null): boolean {
  if (!session || windowIdx === null) return false;
  return (session.followedWindows ?? []).some(fw => fw.index === windowIdx);
}

export function encodeTabDrag(payload: TabDragPayload): string {
  return JSON.stringify(payload);
}

export function decodeTabDrag(raw: string): TabDragPayload | null {
  try {
    const data = JSON.parse(raw);
    if (typeof data?.sessionId !== 'string' || typeof data?.windowIdx !== 'number' ||
        typeof data?.projectId !== 'string') {
      return null;
    }
    return { sessionId: data.sessionId, windowIdx: data.windowIdx, projectId: data.projectId,
      name: typeof data.name === 'string' ? data.name : '' };
  } catch {
    return null;
  }
}

/** A tab being dragged, with what each session would say to it. */
export interface TabDrag extends TabDragPayload {
  /** Session ID to refusal key ('' = accepts); null while still being asked. */
  refusals: Record<string, string> | null;
}

export type TabDropState = 'none' | 'ok' | 'refused';

/**
 * How a session row answers a tab dragged over it: not at all for the tab's
 * own session or another project's drag, refused where the backend said it
 * would refuse, and accepted otherwise — including while the answer is still
 * on its way, since the move checks again.
 */
export function tabDropState(drag: TabDrag | null, targetId: string, projectId: string): TabDropState {
  if (!drag || drag.sessionId === targetId || drag.projectId !== projectId) return 'none';
  const refusal = drag.refusals?.[targetId];
  return refusal ? 'refused' : 'ok';
}

export interface PickableSession {
  id: string;
  name: string;
  path?: string;
}

/**
 * The sessions a tab (or a whole session) can be sent to: every other one,
 * narrowed by what is typed, matching the name or the path.
 */
export function moveTargets<T extends PickableSession>(sessions: T[], sourceId: string, filter: string): T[] {
  const needle = filter.trim().toLowerCase();
  return sessions.filter(session => session.id !== sourceId && (!needle ||
    session.name.toLowerCase().includes(needle) || (session.path ?? '').toLowerCase().includes(needle)));
}
