import { get, writable } from 'svelte/store';

export type AppView = 'dashboard' | 'session' | 'tasks' | 'usage';

// Keep navigation separate from session selection. A selected session may stay
// alive while the dashboard is open, so its TerminalPool and WebSocket are not
// torn down merely because the user wants a project overview.
export const appView = writable<AppView>('dashboard');

/**
 * The view to return to, for the back button in the task and usage views.
 *
 * One step, not a stack: those views are somewhere you drop into and leave
 * again, and a full history would let someone walk backwards through a chain
 * of their own switching, which is not what a single back button promises.
 *
 * Never set to one of them — going back from the task view to the task view is
 * a button that appears to do nothing.
 */
export const previousView = writable<AppView>('dashboard');

function goTo(view: AppView) {
  const current = get(appView);
  if (current !== view && current !== 'tasks' && current !== 'usage') {
    previousView.set(current);
  }
  appView.set(view);
}

export function showDashboard() {
  goTo('dashboard');
}

export function showSessionView() {
  goTo('session');
}

// The all-projects task view. Deliberately its own view rather than a dialog:
// it is where deadlines across every project are read, which is a place to
// work from, not something to dismiss.
export function showTasksView() {
  goTo('tasks');
}

// Tokens per session across every project and machine. Its own view for the
// same reason: it is read and compared, not glanced at and dismissed.
export function showUsageView() {
  goTo('usage');
}

/** Return to whichever view the task or usage list was opened from. */
export function goBack() {
  appView.set(get(previousView));
}
