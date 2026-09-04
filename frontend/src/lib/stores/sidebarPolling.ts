import { activities } from './activities';
import { statusLines, spinnerTexts, tabStatuses, lastActive } from './statusLines';
import { EventsOn, EventsOff } from '../../../wailsjs/runtime/runtime';
import { get } from 'svelte/store';
import { activeProjectId } from './projects';

let listening = false;
let cancelFn: (() => void) | null = null;

// Cache the last payload so we only notify Svelte stores when something
// actually changed. Without this, every 2s sidebar tick publishes a fresh
// object identity and fans out to every SessionItem subscriber even when
// the contents are byte-identical — wastes a surprising amount of CPU
// on large session lists.
let lastActivitiesJSON = '';
let lastStatusLinesJSON = '';
let lastSpinnerTextsJSON = '';
let lastTabStatusesJSON = '';
let lastActiveJSON = '';

/** Drop project-scoped payload and its equality cache before target changes. */
export function invalidateSidebarProject() {
  lastActivitiesJSON = '';
  lastStatusLinesJSON = '';
  lastSpinnerTextsJSON = '';
  lastTabStatusesJSON = '';
  lastActiveJSON = '';
  activities.set({});
  statusLines.set({});
  spinnerTexts.set({});
  tabStatuses.set({});
  lastActive.set({});
}

function handleUpdate(data: any) {
  if (!data) return;
  // Event delivery is asynchronous. The backend can finish an A-project
  // snapshot immediately before SelectProject switches to B, then that event
  // reaches JavaScript after B's stores have already been cleared. Session
  // IDs are only project-local, so accepting it would paint A's live status on
  // B's same-id cards until the next poll.
  if (data.projectId !== get(activeProjectId)) return;

  const nextActivities = data.activities || {};
  const nextStatusLines = data.statusLines || {};
  const nextSpinnerTexts = data.spinnerTexts || {};
  const nextTabStatuses = data.tabStatuses || {};
  const nextLastActive = data.lastActive || {};

  const a = JSON.stringify(nextActivities);
  if (a !== lastActivitiesJSON) {
    lastActivitiesJSON = a;
    activities.set(nextActivities);
  }
  const s = JSON.stringify(nextStatusLines);
  if (s !== lastStatusLinesJSON) {
    lastStatusLinesJSON = s;
    statusLines.set(nextStatusLines);
  }
  const sp = JSON.stringify(nextSpinnerTexts);
  if (sp !== lastSpinnerTextsJSON) {
    lastSpinnerTextsJSON = sp;
    spinnerTexts.set(nextSpinnerTexts);
  }
  const ts = JSON.stringify(nextTabStatuses);
  if (ts !== lastTabStatusesJSON) {
    lastTabStatusesJSON = ts;
    tabStatuses.set(nextTabStatuses);
  }
  // The activity ordering needs these every tick: the session list itself is
  // reloaded only on events, so without this a session showing a live activity
  // dot was still ordered by the timestamp it was loaded with at startup.
  const la = JSON.stringify(nextLastActive);
  if (la !== lastActiveJSON) {
    lastActiveJSON = la;
    lastActive.set(nextLastActive);
  }
}

export function startSidebarPolling() {
  if (listening) return;
  listening = true;

  cancelFn = EventsOn('sidebar:update', handleUpdate);
}

export function stopSidebarPolling() {
  if (cancelFn) {
    cancelFn();
    cancelFn = null;
  }
  if (listening) {
    EventsOff('sidebar:update');
    listening = false;
  }
}
