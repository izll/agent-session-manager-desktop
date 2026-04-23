import { activities } from './activities';
import { statusLines, spinnerTexts, tabStatuses } from './statusLines';
import { EventsOn, EventsOff } from '../../../wailsjs/runtime/runtime';

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

function handleUpdate(data: any) {
  if (!data) return;

  const nextActivities = data.activities || {};
  const nextStatusLines = data.statusLines || {};
  const nextSpinnerTexts = data.spinnerTexts || {};
  const nextTabStatuses = data.tabStatuses || {};

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
