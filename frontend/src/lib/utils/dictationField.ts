/**
 * Reusable dictation utility for textarea/input fields.
 * Listens for dictation:fieldText and dictation:fieldDelete events
 * and inserts/deletes text at the cursor position in the target element.
 */
import { writable, get } from 'svelte/store';
import { EventsOn, EventsOff } from '../../../wailsjs/runtime/runtime';
import * as DictationService from '../../../wailsjs/go/main/DictationService';

export interface FieldDictation {
  /** Toggle dictation on/off for this field */
  toggle: () => Promise<void>;
  /** Stop dictation if running */
  stop: () => Promise<void>;
  /** Cleanup all listeners - call on component destroy */
  destroy: () => void;
  /** Whether dictation is currently active for this field */
  listening: import('svelte/store').Writable<boolean>;
}

/**
 * Create a field dictation controller for a textarea or input element.
 * @param getElement - Function returning the target element (allows lazy binding)
 * @param onTextInserted - Optional callback after text is inserted (e.g. trigger save)
 */
export function createFieldDictation(
  getElement: () => HTMLTextAreaElement | HTMLInputElement | null,
  onTextInserted?: () => void
): FieldDictation {
  const listening = writable(false);
  let unsubFieldText: (() => void) | null = null;
  let unsubFieldDelete: (() => void) | null = null;
  let unsubState: (() => void) | null = null;

  function insertAtCursor(el: HTMLTextAreaElement | HTMLInputElement, text: string) {
    const start = el.selectionStart ?? el.value.length;
    const end = el.selectionEnd ?? el.value.length;
    el.value = el.value.substring(0, start) + text + el.value.substring(end);
    el.selectionStart = el.selectionEnd = start + text.length;
    // Trigger Svelte binding update
    el.dispatchEvent(new Event('input', { bubbles: true }));
  }

  function deleteBeforeCursor(el: HTMLTextAreaElement | HTMLInputElement, count: number) {
    const start = el.selectionStart ?? el.value.length;
    const deleteFrom = Math.max(0, start - count);
    el.value = el.value.substring(0, deleteFrom) + el.value.substring(start);
    el.selectionStart = el.selectionEnd = deleteFrom;
    el.dispatchEvent(new Event('input', { bubbles: true }));
  }

  function setupListeners() {
    unsubFieldText = EventsOn('dictation:fieldText', (text: string) => {
      const el = getElement();
      if (el) {
        insertAtCursor(el, text);
        onTextInserted?.();
      }
    });

    unsubFieldDelete = EventsOn('dictation:fieldDelete', (count: number) => {
      const el = getElement();
      if (el && count > 0) {
        deleteBeforeCursor(el, count);
        onTextInserted?.();
      }
    });

    // Listen for dictation state changes (e.g. auto-stop on silence)
    unsubState = EventsOn('dictation:state', (isListening: boolean) => {
      if (!isListening && get(listening)) {
        listening.set(false);
        cleanup();
        // Restore terminal target
        DictationService.SetDictationTarget('terminal').catch(() => {});
      }
    });
  }

  function cleanup() {
    if (unsubFieldText) { unsubFieldText(); unsubFieldText = null; }
    if (unsubFieldDelete) { unsubFieldDelete(); unsubFieldDelete = null; }
    if (unsubState) { unsubState(); unsubState = null; }
  }

  async function toggle() {
    if (get(listening)) {
      await stop();
    } else {
      // Set target to field before starting
      await DictationService.SetDictationTarget('field');
      setupListeners();
      try {
        await DictationService.ToggleDictation();
        listening.set(true);
      } catch (e) {
        cleanup();
        await DictationService.SetDictationTarget('terminal');
        throw e;
      }
    }
  }

  async function stop() {
    if (get(listening)) {
      try {
        await DictationService.ToggleDictation();
      } catch (_) {}
      listening.set(false);
      cleanup();
      await DictationService.SetDictationTarget('terminal').catch(() => {});
    }
  }

  function destroy() {
    if (get(listening)) {
      // Fire-and-forget stop
      DictationService.ToggleDictation().catch(() => {});
      DictationService.SetDictationTarget('terminal').catch(() => {});
    }
    listening.set(false);
    cleanup();
  }

  return { toggle, stop, destroy, listening };
}
