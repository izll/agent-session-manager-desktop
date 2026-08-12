import { get, writable } from 'svelte/store';

export type AppView = 'dashboard' | 'session' | 'tasks';

// Keep navigation separate from session selection. A selected session may stay
// alive while the dashboard is open, so its TerminalPool and WebSocket are not
// torn down merely because the user wants a project overview.
export const appView = writable<AppView>('dashboard');

/**
 * The view to return to, for the back button in the task list.
 *
 * One step, not a stack: the task view is somewhere you drop into and leave
 * again, and a full history would let someone walk backwards through a chain
 * of their own switching, which is not what a single back button promises.
 *
 * Never set to 'tasks' — going back from the task view to the task view is a
 * button that appears to do nothing.
 */
export const previousView = writable<AppView>('dashboard');

function goTo(view: AppView) {
  const current = get(appView);
  if (current !== view && current !== 'tasks') {
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

/** Return to whichever view the task list was opened from. */
export function goBack() {
  appView.set(get(previousView));
}
