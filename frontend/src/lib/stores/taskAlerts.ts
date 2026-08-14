import { writable } from 'svelte/store';
import { tasks } from './tasks';
import { GetAllTasks } from '../../../wailsjs/go/main/App';

/**
 * How many tasks are still open, across every project.
 *
 * Shown as a badge on the task button, so the amount of outstanding work is
 * visible without opening the view.
 *
 * Open, not overdue: most tasks never get a deadline at all — measured, 18 of
 * 18 here — so a badge counting missed deadlines would sit at zero while there
 * was plenty left to do, and say nothing.
 */
export const openTaskCount = writable(0);

/**
 * Recount the open tasks.
 *
 * Failure leaves the previous count alone rather than zeroing it: a transient
 * read error should not quietly report that there is nothing left to do.
 */
export async function refreshOpenCount(): Promise<void> {
  try {
    const tasks = (await GetAllTasks()) || [];
    openTaskCount.set(tasks.filter((task: { status?: string }) => task.status !== 'done').length);
  } catch {
    // Left as-is on purpose; see above.
  }
}

/** How often the count is refreshed while the app is open. */
const REFRESH_MS = 5 * 60 * 1000;

/**
 * Keep the count current, and stop when the caller is done.
 *
 * Two things drive it. The poll is a backstop for tasks changed outside this
 * window — by an agent writing the file, or another copy of the app — and is
 * five minutes because answering means reading every project's task file.
 *
 * The immediate half is the subscription: any edit made here goes through the
 * tasks store, so recounting when that changes means a tick is reflected at
 * once. Polling alone left the badge showing the old number for up to five
 * minutes after a task was ticked off, which reads as the badge being broken.
 */
export function watchOpenCount(): () => void {
  void refreshOpenCount();
  const timer = setInterval(() => void refreshOpenCount(), REFRESH_MS);

  // Skips the first call: subscribing fires immediately with the current value,
  // and the refresh above has already covered that.
  //
  // Coalesced, because the store is written more than once per change — a tick
  // updates it and the reload that follows updates it again — and answering
  // means reading every project's task file. A short delay turns a burst into
  // one read while still being immediate to the eye.
  let first = true;
  let pending: ReturnType<typeof setTimeout> | null = null;

  const unsubscribe = tasks.subscribe(() => {
    if (first) {
      first = false;
      return;
    }
    if (pending) clearTimeout(pending);
    pending = setTimeout(() => {
      pending = null;
      void refreshOpenCount();
    }, 150);
  });

  return () => {
    clearInterval(timer);
    if (pending) clearTimeout(pending);
    unsubscribe();
  };
}
