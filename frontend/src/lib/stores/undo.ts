import { writable } from 'svelte/store';

/**
 * A short window in which the last action can be taken back.
 *
 * The alternative is a confirmation dialog, which asks before every action —
 * including the overwhelming majority that were meant. This asks after, and
 * only costs anything when the action was a mistake.
 *
 * One pending action at a time, not a stack. A second action replaces the
 * first: undo here means "that last thing", and a queue of half-expired
 * offers would leave the user guessing which one the button belongs to.
 */
export type UndoAction = {
  /** What was done, shown in the toast. Already translated by the caller. */
  message: string;
  /** Puts it back. Awaited, so a failure can be reported rather than assumed. */
  undo: () => Promise<void>;
};

type UndoState = {
  action: UndoAction | null;
  /** Whole seconds left, for the countdown. */
  remaining: number;
};

export const undoState = writable<UndoState>({ action: null, remaining: 0 });

/** How long an action stays undoable. */
const WINDOW_SECONDS = 10;

let timer: ReturnType<typeof setInterval> | null = null;

function stopTimer() {
  if (timer !== null) {
    clearInterval(timer);
    timer = null;
  }
}

/**
 * Offer to undo an action that has already happened.
 *
 * The action is performed by the caller first, not deferred until the window
 * expires: deferring means the interface shows a state the storage does not
 * have yet, and anything that reads it in between — another view, a restart —
 * sees the old value.
 */
export function offerUndo(action: UndoAction): void {
  stopTimer();
  undoState.set({ action, remaining: WINDOW_SECONDS });

  timer = setInterval(() => {
    undoState.update((state) => {
      const remaining = state.remaining - 1;
      if (remaining <= 0) {
        stopTimer();
        return { action: null, remaining: 0 };
      }
      return { ...state, remaining };
    });
  }, 1000);
}

/** Dismiss the offer without undoing anything. */
export function dismissUndo(): void {
  stopTimer();
  undoState.set({ action: null, remaining: 0 });
}

/**
 * Take the action back.
 *
 * The offer is cleared first, so a second click cannot run the undo twice
 * while the first is still in flight.
 */
export async function runUndo(): Promise<void> {
  let pending: UndoAction | null = null;
  undoState.update((state) => {
    pending = state.action;
    return { action: null, remaining: 0 };
  });
  stopTimer();

  if (pending) {
    await (pending as UndoAction).undo();
  }
}
