import { activities } from './activities';
import { statusLines, spinnerTexts, tabStatuses } from './statusLines';
import { EventsOn, EventsOff } from '../../../wailsjs/runtime/runtime';

let listening = false;
let cancelFn: (() => void) | null = null;

function handleUpdate(data: any) {
  if (data) {
    activities.set(data.activities || {});
    statusLines.set(data.statusLines || {});
    spinnerTexts.set(data.spinnerTexts || {});
    tabStatuses.set(data.tabStatuses || {});
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
