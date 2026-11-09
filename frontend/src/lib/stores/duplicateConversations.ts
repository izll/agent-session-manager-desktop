import { writable } from 'svelte/store';
import type { DuplicateConversation } from '../utils/duplicateConversations';

/**
 * The tabs whose Claude conversation is open somewhere else too, as "session:window",
 * each with the other places it is open in.
 */
export const duplicateTabs = writable<Map<string, string[]>>(new Map());

export const duplicateTabKey = (sessionId: string, windowIdx: number) => `${sessionId}:${windowIdx}`;

export function setDuplicateConversations(found: DuplicateConversation[] | null | undefined): void {
  const tabs = new Map<string, string[]>();
  for (const duplicate of found || []) {
    for (const tab of duplicate.tabs || []) {
      tabs.set(duplicateTabKey(tab.sessionId, tab.windowIdx),
        duplicate.places.filter((_, index) => index !== tab.place));
    }
  }
  duplicateTabs.set(tabs);
}
