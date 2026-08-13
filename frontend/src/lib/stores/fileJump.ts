import { writable } from 'svelte/store';

/**
 * A request to open a particular file in the browser view.
 *
 * Passed through a store rather than a prop, because the two components are not
 * neighbours: the diff sits inside MainPanel alongside the browser, and
 * threading a path down would mean MainPanel holding state that belongs to
 * neither of them.
 *
 * Cleared by the browser once it has acted, so re-entering the view later does
 * not silently re-open a file the user has since navigated away from.
 */
export type FileJump = {
  path: string;
  /** 1-based; the browser scrolls here. Absent when only the file matters. */
  line?: number;
};

export const pendingFileJump = writable<FileJump | null>(null);

/**
 * Whether something has asked to switch to the browser view.
 *
 * Separate from the path itself: the panel that owns view switching is not the
 * one that opens files, and having it watch the path would make it re-switch
 * every time a file is opened from within the browser.
 */
export const browserViewRequested = writable(false);

/** Ask the browser view to open a file, optionally at a line. */
export function requestFileJump(path: string, line?: number): void {
  pendingFileJump.set({ path, line });
  browserViewRequested.set(true);
}

/** Called by the panel once it has switched views. */
export function clearBrowserViewRequest(): void {
  browserViewRequested.set(false);
}

/** Called by the browser once the request has been honoured. */
export function clearFileJump(): void {
  pendingFileJump.set(null);
}
