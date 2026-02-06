import { activities } from './activities';
import { statusLines } from './statusLines';
import * as App from '../../../wailsjs/go/main/App';

let pollInterval: ReturnType<typeof setInterval> | null = null;
let isVisible = true;

async function loadSidebarUpdates() {
  if (!isVisible) return;
  try {
    const data = await (App as any).GetSidebarUpdates();
    if (data) {
      activities.set(data.activities || {});
      statusLines.set(data.statusLines || {});
    }
  } catch (e) {
    // Fallback to separate calls if combined endpoint not available yet
    try {
      const [acts, lines] = await Promise.all([
        App.GetActivities(),
        App.GetStatusLines()
      ]);
      activities.set(acts as Record<string, string>);
      statusLines.set(lines as Record<string, string>);
    } catch (e2) {
      console.error('Failed to load sidebar updates:', e2);
    }
  }
}

function handleVisibilityChange() {
  isVisible = !document.hidden;
  if (isVisible) {
    loadSidebarUpdates();
  }
}

export function startSidebarPolling() {
  if (pollInterval) return;

  document.addEventListener('visibilitychange', handleVisibilityChange);

  // Initial load
  loadSidebarUpdates();

  // Single poll for both activities and status lines
  pollInterval = setInterval(loadSidebarUpdates, 2000);
}

export function stopSidebarPolling() {
  if (pollInterval) {
    clearInterval(pollInterval);
    pollInterval = null;
  }
  document.removeEventListener('visibilitychange', handleVisibilityChange);
}
