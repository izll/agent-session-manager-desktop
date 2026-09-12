/**
 * Whether a dialog has just consumed a keystroke.
 *
 * The terminal has to refuse keys that belong to a dialog, and it cannot do so
 * by looking for `.dialog-overlay` in the DOM. xterm listens on its own
 * textarea, deeper in the tree than anything a dialog attaches to, so it sees
 * the key first — and by the time it looks, Escape has already closed the
 * dialog and Svelte has removed the overlay. The check finds nothing and the
 * key is typed into the pane behind. That is the "Escape leaks into the
 * terminal" report.
 *
 * A short-lived flag closes that window. A dialog sets it as it handles a key;
 * the terminal declines anything arriving while it is set, whatever the DOM
 * looks like by then.
 *
 * The flag is cleared on a timer rather than at the end of the current task:
 * the key travels through several turns of the event loop — the dialog's
 * handler, Svelte's re-render, the terminal's handler — and a microtask would
 * be gone before the last of them.
 */
let releaseAt = 0;

/**
 * How long a dialog's keystroke keeps the terminal out.
 *
 * Long enough to outlast the render and the handlers that follow it, short
 * enough that the next deliberate keypress is never caught by it — nobody
 * types Escape and then another key inside 150ms by accident.
 */
const CLAIM_MS = 150;

/** Record that a dialog is handling a key right now. */
export function claimKeyForDialog(): void {
  releaseAt = Date.now() + CLAIM_MS;
}

/** True while a dialog's keystroke is still travelling. */
export function keyClaimedByDialog(): boolean {
  return Date.now() < releaseAt;
}

/** Test seam: forget any outstanding claim. */
export function resetDialogKeyClaim(): void {
  releaseAt = 0;
}
