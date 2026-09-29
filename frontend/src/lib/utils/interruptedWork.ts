// The "continue where you left off" offer after a restart: what the dialog
// shows for each session, and how a choice turns into backend calls. Kept apart
// from the component so the rules can be tested without mounting it.

export interface InterruptedSession {
  id: string;
  name: string;
  path: string;
  agent: string;
  color: string;
  serverId: string;
  agents: string[];
  reopenTabs: number;
  totalTabs: number;
}

export interface ReopenResult {
  id: string;
  ok: boolean;
  error?: string;
}

/** The restart setting: ask, reopen on its own, or leave it. */
export type RestartReopenMode = 'ask' | 'auto' | 'off';

/**
 * How many tabs will come back, as a translation key and its values.
 *
 * A session with one tab says nothing — "1 of 1 tabs" is noise. With more it
 * says how many of them, because only the ones that were running come back
 * and the user should see that before choosing.
 */
export function tabsLine(session: Pick<InterruptedSession, 'reopenTabs' | 'totalTabs'>):
  { key: string; values: Record<string, number> } | null {
  if (session.totalTabs <= 1) return null;
  if (session.reopenTabs >= session.totalTabs) {
    return { key: 'interrupted.allTabs', values: { count: session.totalTabs } };
  }
  return { key: 'interrupted.someTabs', values: { count: session.reopenTabs, total: session.totalTabs } };
}

/**
 * The sessions to start and the ones to forget. Everything offered is one or
 * the other: a session left unticked was a decision too, and asking about it
 * again on the next launch would make the dialog a chore.
 */
export function splitChoice(sessions: Pick<InterruptedSession, 'id'>[], selected: Set<string>):
  { reopen: string[]; dismiss: string[] } {
  const reopen: string[] = [];
  const dismiss: string[] = [];
  for (const session of sessions) {
    (selected.has(session.id) ? reopen : dismiss).push(session.id);
  }
  return { reopen, dismiss };
}

/** How a finished run went, for the toast and for keeping the dialog open. */
export function summarize(results: ReopenResult[]): { ok: number; failed: number } {
  let ok = 0;
  for (const result of results) if (result.ok) ok++;
  return { ok, failed: results.length - ok };
}
