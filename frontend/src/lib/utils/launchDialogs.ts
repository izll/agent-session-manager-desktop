/**
 * Dialogs that open by themselves as the app starts — the release notes after
 * an update, and anything else that wants the user's attention at launch —
 * take turns rather than stacking on top of each other.
 *
 * Each asks with `request`; the queue opens one at a time, and only while no
 * other dialog is on screen (`setBlocked`, fed from the app's "any dialog
 * open" flag). The dialog calls `done` when it closes, and the next one opens.
 * A lower `order` goes first when several are waiting.
 */
export interface LaunchDialogQueue {
  request(id: string, open: () => void, order?: number): void;
  done(id: string): void;
  setBlocked(blocked: boolean): void;
  /** The dialog on screen now, for tests and diagnostics. */
  current(): string | null;
}

export function createLaunchDialogQueue(): LaunchDialogQueue {
  let waiting: { id: string; open: () => void; order: number; seq: number }[] = [];
  let showing: string | null = null;
  let blocked = false;
  let seq = 0;

  function pump() {
    if (showing || blocked || waiting.length === 0) return;
    waiting.sort((a, b) => a.order - b.order || a.seq - b.seq);
    const next = waiting.shift()!;
    showing = next.id;
    next.open();
  }

  return {
    request(id, open, order = 0) {
      if (showing === id || waiting.some((w) => w.id === id)) return;
      waiting.push({ id, open, order, seq: seq++ });
      pump();
    },
    done(id) {
      waiting = waiting.filter((w) => w.id !== id);
      if (showing === id) showing = null;
      pump();
    },
    setBlocked(value) {
      blocked = value;
      pump();
    },
    current: () => showing,
  };
}

/** The app's queue. */
export const launchDialogs = createLaunchDialogQueue();
