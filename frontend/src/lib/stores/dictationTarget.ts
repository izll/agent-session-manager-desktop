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
 * Routes dictation, here and in the backend. The store changes first, so a
 * `dictation:state` that arrives before the backend answers already sees it.
 */
export async function setDictationTarget(target: DictationTarget): Promise<void> {
  dictationTarget.set(target);
  await DictationService.SetDictationTarget(target);
}
