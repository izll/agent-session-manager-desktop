import { writable } from 'svelte/store';
import type { NoteResultScope } from '../utils/noteSearchResult';

/**
 * A request to show a particular note — the tab's or the session's — with a
 * text to find in it. Made by the global search when a note result is opened.
 *
 * Passed through a store for the same reason as a file jump (see fileJump.ts):
 * the search dialog, the panel that owns view switching and the notes view are
 * not neighbours, and which note is open is the notes view's own state.
 *
 * Cleared by the notes view once it has acted, so a later visit opens on the
 * note the user chose, not on the one the search once pointed at.
 */
export type NoteJump = {
  /** The project the request was made in; ignored after a switch. */
  projectId: string;
  sessionId: string;
  scope: NoteResultScope;
  /** Pre-filled into the notes view's find bar; empty for none. */
  query: string;
};

export const pendingNoteJump = writable<NoteJump | null>(null);

/**
 * Whether something has asked to switch to the notes view. Separate from the
 * jump itself for the reason browserViewRequested is: the panel switches
 * views, the notes view picks the note.
 */
export const notesViewRequested = writable(false);

export function requestNoteJump(jump: NoteJump): void {
  pendingNoteJump.set(jump);
  notesViewRequested.set(true);
}

/** Called by the panel once it has switched views. */
export function clearNotesViewRequest(): void {
  notesViewRequested.set(false);
}

/** Called by the notes view once the request has been honoured. */
export function clearNoteJump(): void {
  pendingNoteJump.set(null);
}
