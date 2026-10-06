import { writable } from 'svelte/store';
import * as DictationService from '../../../wailsjs/go/main/DictationService';

// Where dictated text goes: the terminal, or a text field — a dialog's, or the
// notes'.
//
// The backend routes the text; the frontend has to know too. The dictation
// panel over the terminal opened for every dictation and took the keyboard,
// so dictating into a task's title put the panel over the dialog, pulled the
// caret out of the field, and emptied itself as each sentence went where it
// was meant to go — which looked like the words vanishing.

export type DictationTarget = 'terminal' | 'field';

export const dictationTarget = writable<DictationTarget>('terminal');

/**
 * The dictation panel is open for a field: with the buffer on, a field's words
 * gather there to be corrected, and sending puts them into the field. The
 * field's own dialog then leaves showing them to the panel.
 */
export const dictationPanelForField = writable(false);

/**
 * The event the panel sends its text into a field with. Handled by the field's
 * dictation controller, which inserts it at the caret.
 */
export const INSERT_INTO_FIELD_EVENT = 'dictation:insertIntoField';

/**
 * Routes dictation, here and in the backend. The store changes first, so a
 * `dictation:state` that arrives before the backend answers already sees it.
 */
export async function setDictationTarget(target: DictationTarget): Promise<void> {
  dictationTarget.set(target);
  await DictationService.SetDictationTarget(target);
}
