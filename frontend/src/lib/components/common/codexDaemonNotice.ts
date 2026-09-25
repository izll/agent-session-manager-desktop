/** What the backend sends when a Codex conversation was continued through the background server. */
export type CodexDaemonHeldNotice = {
  sessionId: string;
  sessionName: string;
  serverId: string;
  conversationId: string;
  /** The window the conversation was started in: the tab "Restart tab" restarts. */
  windowIdx: number;
  /** The project the session belongs to; session ids are only unique inside one. */
  projectId: string;
};

/**
 * held: just continued through the server. stopped / failed: after "Stop
 * background server". restarted / restartFailed: after "Restart tab".
 */
export type CodexDaemonNoticeState = 'held' | 'stopped' | 'failed' | 'restarted' | 'restartFailed';

type Translate = (key: string, params?: Record<string, string | number>) => string;

/** The parts of a session the notice needs to find its tab. */
type SessionLike = {
  id: string;
  agent: string;
  status: string;
  mainWindowIndex: number;
  followedWindows?: { index: number; agent: string }[] | null;
};

/**
 * The window "Restart tab" restarts, or null when there is none to offer.
 *
 * The notice names the window the conversation was started in. It is offered
 * only while that window still belongs to the same running session, in the
 * project in front of the user, and still runs Codex: a session restarted,
 * or a tab deleted, since the notice came leaves nothing safe to restart, and
 * the text then tells the user to do it by hand.
 */
export function codexDaemonNoticeWindow(
  notice: CodexDaemonHeldNotice | null,
  sessions: SessionLike[],
  activeProjectId: string,
): number | null {
  if (!notice || notice.projectId !== activeProjectId) return null;
  const session = sessions.find((s) => s.id === notice.sessionId);
  if (!session || session.status !== 'running') return null;
  const agent = notice.windowIdx === session.mainWindowIndex
    ? session.agent
    : session.followedWindows?.find((w) => w.index === notice.windowIdx)?.agent;
  return agent === 'codex' ? notice.windowIdx : null;
}

/**
 * The toast's text, colour and button for each state.
 *
 * One button at a time: first to stop the server, then — once it is stopped
 * and the tab can be found — to restart the tab so it comes back without the
 * server. After a failure the error is what matters, so no button.
 */
export function codexDaemonNoticeText(
  t: Translate,
  state: CodexDaemonNoticeState,
  notice: CodexDaemonHeldNotice | null,
  failure: string,
  canRestart = false,
): { message: string; variant: 'warning' | 'success' | 'error'; action: string } {
  switch (state) {
    case 'stopped':
      return {
        message: t('codexDaemon.stopped'),
        variant: 'success',
        action: canRestart ? t('codexDaemon.restart') : '',
      };
    case 'failed':
      return { message: t('codexDaemon.stopFailed', { error: failure }), variant: 'error', action: '' };
    case 'restarted':
      return { message: t('codexDaemon.restarted'), variant: 'success', action: '' };
    case 'restartFailed':
      return { message: t('codexDaemon.restartFailed', { error: failure }), variant: 'error', action: '' };
    default:
      return {
        message: t('codexDaemon.held', { session: notice?.sessionName ?? '' }),
        variant: 'warning',
        action: t('codexDaemon.stop'),
      };
  }
}
