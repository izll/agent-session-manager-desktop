/**
 * One open context menu at a time, across the whole app.
 *
 * Each menu lives in its own component with its own boolean, and they close on
 * `click` — but a right-click fires `contextmenu`, not `click`, so opening one
 * menu never closed another. Right-clicking a session and then a tab left both
 * on screen.
 *
 * Rather than have every component watch every other, each one registers a
 * close callback here when it opens; registering evicts whoever held the slot.
 */

type CloseFn = () => void;

let current: CloseFn | null = null;

/**
 * Claim the single menu slot, closing any menu already open.
 *
 * Call this as the menu opens, passing its own close function. Safe to call
 * when this menu is already the open one — it will not close itself.
 */
export function claimMenu(close: CloseFn): void {
  if (current && current !== close) current();
  current = close;
}

/**
 * Give up the slot. Call it when the menu closes, whatever closed it.
 *
 * Only clears the slot if this menu still holds it, so a late close from a
 * menu that was already evicted cannot wipe the newer one's registration.
 */
export function releaseMenu(close: CloseFn): void {
  if (current === close) current = null;
}
