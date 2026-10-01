import { get, writable } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';
import { t } from '../i18n';
import { describeBackendError } from './backendError';
import { reportError, reportNotice } from '../stores/appErrors';
import { mergeSessionInto, moveTabToNewSession, moveTabToSession } from '../stores/sessions';
import { TAB_DRAG_MIME, encodeTabDrag, type TabDrag, type TabDragPayload } from './tabMoveRules';

/**
 * Moving tabs between sessions, as the UI does it: the drag of a tab out of
 * the tab bar, and the three actions with the word they end with.
 */

/**
 * The tab being dragged, for the session rows to answer while it is over them.
 *
 * A store rather than the drag's own data: a drop target may look at the drag
 * types during dragover, but not at the data (browsers withhold it until the
 * drop), so whether a row would accept the tab has to be known some other way.
 */
export const tabDrag = writable<TabDrag | null>(null);

let dragGeneration = 0;

/**
 * Marks a tab drag as one that can be dropped on another session, and asks the
 * backend once which sessions would refuse it.
 */
export function beginTabDrag(e: DragEvent, payload: TabDragPayload) {
  e.dataTransfer?.setData(TAB_DRAG_MIME, encodeTabDrag(payload));
  const generation = ++dragGeneration;
  tabDrag.set({ ...payload, refusals: null });
  App.TabMoveRefusals(payload.sessionId, payload.windowIdx).then((refusals) => {
    if (generation !== dragGeneration) return;
    tabDrag.update(drag => (drag ? { ...drag, refusals: refusals ?? {} } : drag));
  }).catch(() => {
    // Unanswered, every session accepts the drop and the move itself says no.
  });
}

export function endTabDrag() {
  dragGeneration++;
  tabDrag.set(null);
}

/** Moves a tab into another session and says where it went. */
export async function moveTabWithNotice(sourceId: string, windowIdx: number, targetId: string, tabName: string) {
  const translate = get(t);
  try {
    const result = await moveTabToSession(sourceId, windowIdx, targetId);
    if (result) reportNotice(translate('tabMove.movedTo', { tab: tabName, session: result.sessionName }));
    return true;
  } catch (e) {
    reportError(translate('tabMove.failed', { error: describeBackendError(e) }));
    return false;
  }
}

/** Makes a tab a session of its own and says so. */
export async function splitTabWithNotice(sourceId: string, windowIdx: number, tabName: string) {
  const translate = get(t);
  try {
    const result = await moveTabToNewSession(sourceId, windowIdx, '');
    if (result) reportNotice(translate('tabMove.movedToNew', { tab: tabName, session: result.sessionName }));
    return true;
  } catch (e) {
    reportError(translate('tabMove.failed', { error: describeBackendError(e) }));
    return false;
  }
}

/** Merges a session into another and says how many tabs went. */
export async function mergeSessionWithNotice(sourceId: string, targetId: string, sourceName: string) {
  const translate = get(t);
  try {
    const result = await mergeSessionInto(sourceId, targetId);
    if (result) {
      reportNotice(translate('tabMove.merged', {
        source: sourceName, session: result.sessionName, count: result.tabsMoved,
      }));
    }
    return true;
  } catch (e) {
    reportError(translate('tabMove.mergeFailed', { error: describeBackendError(e) }));
    return false;
  }
}
