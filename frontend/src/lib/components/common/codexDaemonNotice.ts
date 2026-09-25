/** What the backend sends when a Codex conversation was continued through the background server. */
export type CodexDaemonHeldNotice = {
  sessionId: string;
  sessionName: string;
  serverId: string;
  conversationId: string;
};

/** held: just continued through the server; stopped / failed: after the button. */
export type CodexDaemonNoticeState = 'held' | 'stopped' | 'failed';

type Translate = (key: string, params?: Record<string, string | number>) => string;

/**
 * The toast's text, colour and button for each state.
 *
 * The button is offered only while there is something to stop: after a stop
 * the server is gone, and after a failure the error is what matters.
 */
export function codexDaemonNoticeText(
  t: Translate,
  state: CodexDaemonNoticeState,
  notice: CodexDaemonHeldNotice | null,
  failure: string,
): { message: string; variant: 'warning' | 'success' | 'error'; action: string } {
  switch (state) {
    case 'stopped':
      return { message: t('codexDaemon.stopped'), variant: 'success', action: '' };
    case 'failed':
      return { message: t('codexDaemon.stopFailed', { error: failure }), variant: 'error', action: '' };
    default:
      return {
        message: t('codexDaemon.held', { session: notice?.sessionName ?? '' }),
        variant: 'warning',
        action: t('codexDaemon.stop'),
      };
  }
}
