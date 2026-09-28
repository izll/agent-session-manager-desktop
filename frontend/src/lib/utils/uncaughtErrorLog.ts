/**
 * Uncaught frontend errors, written to the app's log.
 *
 * The webview's console is only visible in a devtools build, so an error that
 * broke something on a user's machine left no trace at all — a command palette
 * that failed while opening looked like Ctrl+K doing nothing, with nothing to
 * go on. Throttled, so a failure repeating in a loop cannot flood the log.
 */

const WINDOW_MS = 60_000;
const MAX_PER_WINDOW = 20;

export function describeUncaught(reason: unknown): string {
  if (reason instanceof Error) return reason.stack || `${reason.name}: ${reason.message}`;
  if (typeof reason === 'string') return reason;
  try {
    return JSON.stringify(reason);
  } catch {
    return String(reason);
  }
}

/** A limiter: true while fewer than max messages were let through in the window. */
export function throttle(max = MAX_PER_WINDOW, windowMs = WINDOW_MS, now: () => number = Date.now) {
  let started = 0;
  let count = 0;
  let dropped = 0;
  return {
    allow(): { ok: boolean; dropped: number } {
      const at = now();
      if (at - started >= windowMs) {
        const wasDropped = dropped;
        started = at;
        count = 0;
        dropped = 0;
        count++;
        return { ok: true, dropped: wasDropped };
      }
      if (count < max) {
        count++;
        return { ok: true, dropped: 0 };
      }
      dropped++;
      return { ok: false, dropped: 0 };
    },
  };
}

/** log is the backend's LogFrontend, passed in so this stays testable. */
export function installUncaughtErrorLog(log: (message: string) => unknown) {
  const limit = throttle();
  const write = (kind: string, detail: string) => {
    const { ok, dropped } = limit.allow();
    if (!ok) return;
    const note = dropped ? ` (${dropped} more dropped in the last minute)` : '';
    try {
      void Promise.resolve(log(`[uncaught] ${kind}${note}: ${detail}`)).catch(() => {});
    } catch {
      // The bridge is not up yet; nothing else to tell.
    }
  };
  window.addEventListener('error', (event) => {
    const where = event.filename ? ` at ${event.filename}:${event.lineno}:${event.colno}` : '';
    write('error', describeUncaught(event.error ?? event.message) + where);
  });
  window.addEventListener('unhandledrejection', (event) => {
    write('rejection', describeUncaught(event.reason));
  });
}
