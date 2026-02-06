import { writable } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';

// Map of session ID to last output line
export const statusLines = writable<Record<string, string>>({});

let pollInterval: ReturnType<typeof setInterval> | null = null;

export async function loadStatusLines() {
  try {
    const data = await App.GetStatusLines();
    statusLines.set(data as Record<string, string>);
  } catch (e) {
    console.error('Failed to load status lines:', e);
  }
}

export function startStatusLinePolling() {
  if (pollInterval) return;

  // Initial load
  loadStatusLines();

  // Poll every 2 seconds (same as activities)
  pollInterval = setInterval(loadStatusLines, 2000);
}

export function stopStatusLinePolling() {
  if (pollInterval) {
    clearInterval(pollInterval);
    pollInterval = null;
  }
}

export function getStatusLine(sessionId: string, statusLinesMap: Record<string, string>): string {
  return statusLinesMap[sessionId] || '';
}
