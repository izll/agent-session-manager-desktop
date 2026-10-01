import { writable } from 'svelte/store';

/**
 * Failures worth telling the user about, from anywhere.
 *
 * Its own module rather than a field on an existing store: sessions.ts already
 * imports settings.ts, so putting this in either would make anything that
 * reports an error depend on the store it happens to live next to — and one of
 * those pairs would be a cycle.
 *
 * Set a message to show it; the reader clears it, so the same failure repeating
 * is reported each time rather than once.
 */
export const appError = writable<string | null>(null);

export function reportError(message: string): void {
  appError.set(message);
}

/**
 * Outcomes worth a word, from anywhere — the success counterpart of appError,
 * for an action whose result is not otherwise visible where it was started
 * (a tab dropped on another session moves the view along with it, and the
 * user should be told where it went).
 */
export const appNotice = writable<string | null>(null);

export function reportNotice(message: string): void {
  appNotice.set(message);
}
