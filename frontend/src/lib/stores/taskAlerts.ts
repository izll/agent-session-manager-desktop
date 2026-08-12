import { writable } from 'svelte/store';
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
 * Five minutes, not five seconds: this walks every project's task file to
 * answer, and the number does not change on its own between edits. It also
 * refreshes when the task view is left, which is when a task was most likely
 * just completed.
 */
export function watchOpenCount(): () => void {
  void refreshOpenCount();
  const timer = setInterval(() => void refreshOpenCount(), REFRESH_MS);
  return () => clearInterval(timer);
}
