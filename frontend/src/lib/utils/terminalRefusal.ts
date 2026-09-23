/**
 * An attach the backend turned down, and why.
 *
 * A refusal sent as an HTTP error before the WebSocket upgrade never reaches
 * the page — the browser reports every refused handshake as the same bare
 * failure — so the backend completes the handshake and closes straight away,
 * with a code from the application range (4000-4999) and a reason naming what
 * happened. See refuseAttach in terminal_ws.go.
 *
 * No imports, so the rules can be run under plain node.
 */

export type TerminalRefusalReason =
  | 'session-not-running'
  | 'session-not-found'
  | 'project-locked'
  | 'attach-failed';

export interface TerminalRefusal {
  reason: TerminalRefusalReason;
  /** What the backend added after the key, such as an SSH error. */
  detail: string;
}

const messageKeys: Record<TerminalRefusalReason, string> = {
  'session-not-running': 'terminal.refusedNotRunning',
  'session-not-found': 'terminal.refusedNotFound',
  'project-locked': 'terminal.refusedProjectLocked',
  'attach-failed': 'terminal.refusedAttachFailed',
};

/**
 * The refusal a close carries, or null for an ordinary close.
 *
 * Only the application range counts. Anything else — a dropped client, the
 * stream ending — is a connection problem, and those are worth reconnecting
 * to; a refusal is a verdict that the next attempt would get again.
 */
export function refusalFromClose(code: number, reason: string): TerminalRefusal | null {
  if (!(code >= 4000 && code <= 4999)) return null;
  const text = reason || '';
  const colon = text.indexOf(':');
  const key = (colon < 0 ? text : text.slice(0, colon)).trim();
  if (key in messageKeys) {
    return {
      reason: key as TerminalRefusalReason,
      detail: colon < 0 ? '' : text.slice(colon + 1).trim(),
    };
  }
  // A reason this build does not know, from a newer backend say: still a
  // refusal, and its text is the best explanation there is.
  return { reason: 'attach-failed', detail: text.trim() };
}

/** The translation key for the sentence the pane shows. */
export function refusalMessageKey(refusal: TerminalRefusal): string {
  return messageKeys[refusal.reason];
}

/** Every translation key a refusal can show, for checking the locales. */
export const refusalMessageKeys: readonly string[] = Object.values(messageKeys);
