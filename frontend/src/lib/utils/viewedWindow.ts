import { get } from 'svelte/store';
import { selectedSessionId, selectedWindowIdx } from '../stores/sessions';

/**
 * The tab on screen, if it is one of `sessionId`'s; -1 otherwise.
 *
 * A task assigned to no tab goes there. Without it the backend fell back to
 * the multiplexer's active window, which is not the tab on screen — each tab is
 * shown through a view of its own — so a task sent while looking at a Codex
 * tab went to the main window's Claude.
 */
export function viewedWindowOf(sessionId: string): number {
  if (get(selectedSessionId) !== sessionId) return -1;
  return get(selectedWindowIdx) ?? -1;
}
