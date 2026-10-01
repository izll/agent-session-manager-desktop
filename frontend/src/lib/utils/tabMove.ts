import { get, writable } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';
import { t } from '../i18n';
import { describeBackendError } from './backendError';
import { reportError, reportNotice } from '../stores/appErrors';
import { mergeSessionInto, moveTabToNewSession, moveTabToSession, sessions } from '../stores/sessions';
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
  tabDrag.set({ ...payload, refusals: null, newSessionRefusal: null });
  App.TabMoveRefusals(payload.sessionId, payload.windowIdx).then((refusals) => {
    if (generation !== dragGeneration) return;
    tabDrag.update(drag => (drag ? { ...drag, refusals: refusals ?? {} } : drag));
  }).catch(() => {
    // Unanswered, every session accepts the drop and the move itself says no.
  });
  App.TabSplitRefusal(payload.sessionId, payload.windowIdx).then((refusal) => {
    if (generation !== dragGeneration) return;
    tabDrag.update(drag => (drag ? { ...drag, newSessionRefusal: refusal ?? '' } : drag));
  }).catch(() => {
    // Likewise: the "new session" target accepts, and the split says no.
  });
}

export function endTabDrag() {
  dragGeneration++;
  tabDrag.set(null);
}

/**
 * A question to ask before a tab moves: the name of the session it becomes,
 * or — for the only tab of a session — whether to end the session by moving
 * it. Asked by TabMovePrompts, wherever the move came from: a drop on a row
 * or on the "new session" target, or the tab's menu.
 */
export type TabMovePrompt =
  | { kind: 'newSession'; sessionId: string; windowIdx: number; tabName: string }
  | { kind: 'onlyTab'; sessionId: string; sessionName: string; tabName: string; targetId: string };

export const tabMovePrompt = writable<TabMovePrompt | null>(null);

/** Asks for the name of the session a tab is to become, then makes it one. */
export function askNewSessionFor(sessionId: string, windowIdx: number, tabName: string) {
  tabMovePrompt.set({ kind: 'newSession', sessionId, windowIdx, tabName });
}

/**
 * Moves a tab into another session — after asking, when it is the only tab of
 * its session, since the session ends with it.
 */
export function requestTabMove(sourceId: string, windowIdx: number, targetId: string, tabName: string,
  onlyTab: boolean) {
  if (!onlyTab) {
    void moveTabWithNotice(sourceId, windowIdx, targetId, tabName);
    return;
  }
  const source = get(sessions).find(s => s.id === sourceId);
  tabMovePrompt.set({ kind: 'onlyTab', sessionId: sourceId, sessionName: source?.name ?? tabName,
    tabName, targetId });
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

/**
 * Moves the only tab of a session into another: the session is merged into
 * the target and ends. Said as the move of the tab it was to the user.
 */
export async function moveOnlyTabWithNotice(sourceId: string, targetId: string, tabName: string) {
  const translate = get(t);
  try {
    const result = await mergeSessionInto(sourceId, targetId);
    if (result) reportNotice(translate('tabMove.movedTo', { tab: tabName, session: result.sessionName }));
    return true;
  } catch (e) {
    reportError(translate('tabMove.failed', { error: describeBackendError(e) }));
    return false;
  }
}

/** Makes a tab a session of its own, named name, and says so. */
export async function splitTabWithNotice(sourceId: string, windowIdx: number, tabName: string, name: string) {
  const translate = get(t);
  try {
    const result = await moveTabToNewSession(sourceId, windowIdx, name);
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
