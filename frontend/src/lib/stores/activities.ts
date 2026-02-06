import { writable } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';

export type Activity = 'idle' | 'busy' | 'waiting';

// Map of session ID to activity status
export const activities = writable<Record<string, Activity>>({});

let pollInterval: ReturnType<typeof setInterval> | null = null;

export async function loadActivities() {
  try {
    const data = await App.GetActivities();
    activities.set(data as Record<string, Activity>);
  } catch (e) {
    console.error('Failed to load activities:', e);
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
}

export function getActivity(sessionId: string, activitiesMap: Record<string, Activity>): Activity {
  return activitiesMap[sessionId] || 'idle';
}
