import { writable } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';

export type Activity = 'idle' | 'busy' | 'waiting';

// Map of session ID to activity status
export const activities = writable<Record<string, Activity>>({});

let pollInterval: ReturnType<typeof setInterval> | null = null;

/**
 * Fires when a session's activity changes, with the session id and both states.
 *
 * The moment an agent stops working is exactly when its pane stops producing
 * output — and a pane that has gone quiet is one that can sit there showing
 * something out of date. The terminal listens for this so it can ask for a
 * repaint at the one moment it is most likely to be needed, rather than only
 * on the idle timer.
 */
export const ACTIVITY_CHANGED_EVENT = 'session:activity-changed';

export interface ActivityChangedDetail {
  sessionId: string;
  from: Activity;
  to: Activity;
}

let lastActivities: Record<string, Activity> = {};

export async function loadActivities() {
  try {
    const data = await App.GetActivities();
    const next = data as Record<string, Activity>;
    announceActivityChanges(lastActivities, next);
    lastActivities = next;
    activities.set(next);
  } catch (e) {
    console.error('Failed to load activities:', e);
  }
}

function announceActivityChanges(
  previous: Record<string, Activity>,
  next: Record<string, Activity>,
): void {
  if (typeof window === 'undefined') return;
  for (const [sessionId, to] of Object.entries(next)) {
    const from = previous[sessionId];
    // A session seen for the first time has not *changed*; announcing it would
    // fire for every session on the first poll after a project loads.
    if (from === undefined || from === to) continue;
    window.dispatchEvent(new CustomEvent<ActivityChangedDetail>(ACTIVITY_CHANGED_EVENT, {
      detail: { sessionId, from, to },
    }));
  }
}

export function startActivityPolling() {
  if (pollInterval) return;

  // Initial load
  loadActivities();

  // Poll every 2 seconds
  pollInterval = setInterval(loadActivities, 2000);
}

export function stopActivityPolling() {
  if (pollInterval) {
    clearInterval(pollInterval);
    pollInterval = null;
  }
  // Forget what was seen: the next run belongs to whatever project starts it,
  // and comparing across that boundary would report changes that never happened.
  lastActivities = {};
}

export function getActivity(sessionId: string, activitiesMap: Record<string, Activity>): Activity {
  return activitiesMap[sessionId] || 'idle';
}
