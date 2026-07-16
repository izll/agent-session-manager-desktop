import { writable } from 'svelte/store';

export type AppView = 'dashboard' | 'session';

// Keep navigation separate from session selection. A selected session may stay
// alive while the dashboard is open, so its TerminalPool and WebSocket are not
// torn down merely because the user wants a project overview.
export const appView = writable<AppView>('dashboard');

export function showDashboard() {
  appView.set('dashboard');
}

export function showSessionView() {
  appView.set('session');
}
