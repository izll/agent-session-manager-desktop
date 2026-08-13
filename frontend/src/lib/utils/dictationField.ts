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
  /** Set up listeners for externally started dictation (e.g. hotkey) without toggling */
  startExternally: () => void;
  /** Clean up listeners for externally stopped dictation without toggling */
  stopExternally: () => void;
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
  let externallyManaged = false;

  /**
   * Insert dictated text at the cursor, keeping undo working.
   *
   * Assigning to el.value clears the browser's own undo history for the field,
   * so after a dictation Ctrl+Z had nothing left to go back to — including the
   * text typed by hand before it. execCommand('insertText') is deprecated but
   * is the only way to make an edit the browser records as an undoable step,
   * and every engine this ships on still implements it. The assignment stays
   * as a fallback for the case where it is refused.
   */
  function insertAtCursor(el: HTMLTextAreaElement | HTMLInputElement, text: string) {
    const start = el.selectionStart ?? el.value.length;
    const end = el.selectionEnd ?? el.value.length;

    el.focus();
    el.setSelectionRange(start, end);
    let inserted = false;
    try {
      inserted = document.execCommand('insertText', false, text);
    } catch {
      inserted = false;
    }

    if (!inserted) {
      el.value = el.value.substring(0, start) + text + el.value.substring(end);
      el.selectionStart = el.selectionEnd = start + text.length;
    }
    // Trigger Svelte binding update. execCommand fires its own input event, so
    // this would be a second one — harmless, since the handler only schedules a
    // save, but skipped when the browser already did it.
    if (!inserted) el.dispatchEvent(new Event('input', { bubbles: true }));
  }

  /** Same reasoning as insertAtCursor: deleting has to stay undoable too. */
  function deleteBeforeCursor(el: HTMLTextAreaElement | HTMLInputElement, count: number) {
    const start = el.selectionStart ?? el.value.length;
    const deleteFrom = Math.max(0, start - count);

    el.focus();
    el.setSelectionRange(deleteFrom, start);
    let deleted = false;
    try {
      // An empty insertText over a selection is a delete the browser records.
      deleted = document.execCommand('insertText', false, '');
    } catch {
      deleted = false;
    }

    if (!deleted) {
      el.value = el.value.substring(0, deleteFrom) + el.value.substring(start);
      el.selectionStart = el.selectionEnd = deleteFrom;
      el.dispatchEvent(new Event('input', { bubbles: true }));
    }
  }

  function setupListeners() {
    if (unsubFieldText) return; // Already set up

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
        // Only restore terminal target if not externally managed (modal manages its own target)
        if (!externallyManaged) {
          DictationService.SetDictationTarget('terminal').catch(() => {});
        }
        externallyManaged = false;
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

  /** Set up listeners for externally started dictation (hotkey) without toggling */
  function startExternally() {
    if (!get(listening)) {
      externallyManaged = true;
      setupListeners();
      listening.set(true);
    }
  }

  /** Clean up listeners for externally stopped dictation without toggling */
  function stopExternally() {
    externallyManaged = false;
    listening.set(false);
    cleanup();
  }

  function destroy() {
    if (get(listening)) {
      // Fire-and-forget stop
      DictationService.ToggleDictation().catch(() => {});
      if (!externallyManaged) {
        DictationService.SetDictationTarget('terminal').catch(() => {});
      }
    }
    externallyManaged = false;
    listening.set(false);
    cleanup();
  }

  return { toggle, stop, startExternally, stopExternally, destroy, listening };
}
