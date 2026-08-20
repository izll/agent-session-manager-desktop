<script lang="ts">
  import { onMount, onDestroy, createEventDispatcher, tick } from 'svelte';
  import { selectedSessionId, selectedWindowIdx } from '../../stores/sessions';
  import { get } from 'svelte/store';
  import * as App from '../../../../wailsjs/go/main/App';
  import { createFieldDictation } from '../../utils/dictationField';
  import { t } from '../../i18n';
  import { registerUnsavedGuard } from '../../stores/unsavedChanges';
  import ConfirmDialog from '../Dialogs/ConfirmDialog.svelte';

  export let active = false;

  const dispatch = createEventDispatcher();

  let notes = '';
  let lastSessionId: string | null = null;
  let lastWindowIdx: number = 0;
  let saveTimeout: ReturnType<typeof setTimeout> | null = null;
  let loadGeneration = 0;
  let saving = false;
  let lastSaved = '';
  let textareaEl: HTMLTextAreaElement;
  const saveQueues = new Map<string, Promise<void>>();
  type NoteDraft = { text: string; saved: string; saveError: string; loadError: string };
  const draftsByTarget = new Map<string, NoteDraft>();
  let savesInFlight = 0;
  let activationGeneration = 0;
  let loadingNotes = false;
  let saveError = '';
  let loadError = '';
  let unregisterUnsavedGuard: (() => void) | null = null;
  let pendingDiscard: (() => void) | null = null;

  function hasUnsavedDrafts(): boolean {
    return [...draftsByTarget.values()].some((draft) =>
      draft.text !== draft.saved || !!draft.saveError
    ) || notes !== lastSaved || !!saveError;
  }

  function confirmDiscardNotes() {
    const continuation = pendingDiscard;
    pendingDiscard = null;
    if (continuation) continuation();
  }

  function cancelDiscardNotes() {
    pendingDiscard = null;
  }

  function noteKey(sessionId: string, windowIdx: number): string {
    return `${sessionId}:${windowIdx}`;
  }

  function rememberCurrentDraft() {
    if (!lastSessionId) return;
    draftsByTarget.set(noteKey(lastSessionId, lastWindowIdx), {
      text: notes,
      saved: lastSaved,
      saveError,
      loadError,
    });
  }

  function showDraft(draft: NoteDraft) {
    notes = draft.text;
    lastSaved = draft.saved;
    saveError = draft.saveError;
    loadError = draft.loadError;
  }

  /**
   * Find within the note.
   *
   * A textarea cannot highlight a range of its own text — the browser gives no
   * way to paint inside it — so a match is shown by selecting it and scrolling
   * it into view. That is what every plain-text find does here, and it has the
   * advantage that the match is then ready to be typed over.
   */
  let showFind = false;
  let findQuery = '';
  let findInputEl: HTMLInputElement | undefined;

  // Positions of every match, recomputed as the query or the text changes so a
  // count never describes a note that has since been edited.
  $: matches = (() => {
    if (!showFind || !findQuery) return [] as number[];
    const haystack = notes.toLowerCase();
    const needle = findQuery.toLowerCase();
    const found: number[] = [];
    let at = haystack.indexOf(needle);
    while (at !== -1) {
      found.push(at);
      at = haystack.indexOf(needle, at + needle.length);
    }
    return found;
  })();

  let matchIndex = 0;
  // A shorter list must not leave the cursor pointing past its end.
  $: if (matchIndex >= matches.length) matchIndex = 0;

  function goToMatch(index: number) {
    if (!matches.length || !textareaEl) return;
    matchIndex = (index + matches.length) % matches.length;
    const start = matches[matchIndex];

    textareaEl.focus();
    textareaEl.setSelectionRange(start, start + findQuery.length);

    // Scrolling is by line, because a textarea has no way to ask where a
    // character sits. Close enough to put the match on screen, which is all
    // that is needed.
    const lineHeight = parseFloat(getComputedStyle(textareaEl).lineHeight) || 20;
    const line = notes.slice(0, start).split('\n').length - 1;
    textareaEl.scrollTop = Math.max(0, (line * lineHeight) - (textareaEl.clientHeight / 2));
  }

  function openFind() {
    showFind = true;
    // Pre-fill from the selection, as editors do: having just highlighted the
    // word you want to find, retyping it is pure ceremony.
    const selected = textareaEl?.value.slice(textareaEl.selectionStart, textareaEl.selectionEnd);
    if (selected && !selected.includes('\n')) findQuery = selected;
    tick().then(() => { findInputEl?.focus(); findInputEl?.select(); });
  }

  function closeFind() {
    showFind = false;
    findQuery = '';
    textareaEl?.focus();
  }

  function handleFindKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.preventDefault();
      closeFind();
      return;
    }
    // Enter, F3 and Ctrl+G all step, so whichever one the user reaches for
    // works from the box as well as from the note.
    if (event.key === 'Enter' || event.key === 'F3' ||
        ((event.ctrlKey || event.metaKey) && event.key === 'g')) {
      event.preventDefault();
      goToMatch(event.shiftKey ? matchIndex - 1 : matchIndex + 1);
    }
  }

  /**
   * Undo history for the note.
   *
   * The textarea has its own, but it is emptied whenever the value is assigned
   * from code — which happens on every tab switch, and happened on every
   * dictated word until that was fixed. Keeping a history here means Ctrl+Z
   * still reaches text typed before the last such assignment, and gives Ctrl+Y
   * a redo, which a textarea only offers as Ctrl+Shift+Z.
   *
   * Entries are whole snapshots. A note is a few kilobytes at most, and diffing
   * to save memory would cost more code than the memory is worth.
   */
  type Snapshot = { text: string; caret: number };
  let history: Snapshot[] = [];
  let historyAt = -1;
  /** Set while undo/redo is writing, so the change is not recorded as an edit. */
  let restoring = false;

  const HISTORY_LIMIT = 200;
  /** Typing is grouped into one entry until this much time passes. */
  const COALESCE_MS = 600;
  let lastRecordedAt = 0;

  /**
   * Start the history over for a freshly loaded note.
   *
   * Without this, undo would walk back into the previous tab's text and write
   * it into this one — the worst possible outcome for a key people press
   * without looking.
   */
  function resetHistory() {
    history = [{ text: notes, caret: 0 }];
    historyAt = 0;
    lastRecordedAt = 0;
  }

  function recordHistory(force = false) {
    if (restoring) return;

    const snapshot: Snapshot = { text: notes, caret: textareaEl?.selectionStart ?? notes.length };
    const current = history[historyAt];
    if (current && current.text === snapshot.text) return;

    const now = Date.now();
    // Successive keystrokes replace the last entry rather than adding one, so
    // Ctrl+Z steps back by a word or a pause, not by a character.
    const coalesce = !force && historyAt >= 0 && now - lastRecordedAt < COALESCE_MS;
    lastRecordedAt = now;

    if (coalesce) {
      history[historyAt] = snapshot;
      return;
    }

    // A new edit after undoing discards what was undone, as every editor does.
    history = history.slice(0, historyAt + 1);
    history.push(snapshot);
    if (history.length > HISTORY_LIMIT) history.shift();
    historyAt = history.length - 1;
  }

  function applySnapshot(snapshot: Snapshot) {
    restoring = true;
    notes = snapshot.text;
    tick().then(() => {
      restoring = false;
      if (!textareaEl) return;
      textareaEl.focus();
      const at = Math.min(snapshot.caret, snapshot.text.length);
      textareaEl.setSelectionRange(at, at);
    });
    handleInput(); // the restored text still has to be saved
  }

  function undoNote() {
    if (historyAt <= 0) return;
    historyAt -= 1;
    applySnapshot(history[historyAt]);
  }

  function redoNote() {
    if (historyAt >= history.length - 1) return;
    historyAt += 1;
    applySnapshot(history[historyAt]);
  }

  /** Ctrl+F anywhere in the note opens the bar. */
  function handleContainerKeydown(event: KeyboardEvent) {
    // F3 takes no modifier, so it is checked before the others.
    if (event.key === 'F3' && showFind) {
      event.preventDefault();
      goToMatch(event.shiftKey ? matchIndex - 1 : matchIndex + 1);
      return;
    }

    const mod = event.ctrlKey || event.metaKey;
    if (!mod) return;

    if (event.key === 'f') {
      event.preventDefault();
      openFind();
      return;
    }
    // Step through matches from the note itself, not only from the find box.
    // goToMatch puts the focus back in the textarea so the match shows as a
    // selection, which means Enter there types a newline rather than advancing
    // — without these, stepping meant clicking back into the box every time.
    if (event.key === 'g' && showFind) {
      event.preventDefault();
      goToMatch(event.shiftKey ? matchIndex - 1 : matchIndex + 1);
      return;
    }

    // Handled here rather than left to the browser: its own history is empty
    // after a tab switch, so Ctrl+Z would appear to do nothing at all.
    if (event.key === 'z' && !event.shiftKey) {
      event.preventDefault();
      undoNote();
      return;
    }
    // Both spellings of redo, since the note takes Ctrl+Y as well.
    if (event.key === 'y' || (event.key === 'z' && event.shiftKey)) {
      event.preventDefault();
      redoNote();
    }
  }

  // Dictation support
  const dictation = createFieldDictation(
    () => textareaEl,
    () => handleInput() // trigger autosave after dictation inserts text
  );
  const dictationListening = dictation.listening;

  onMount(() => {
    unregisterUnsavedGuard = registerUnsavedGuard({
      isDirty: hasUnsavedDrafts,
      requestDiscard: (continueAfterDiscard) => { pendingDiscard = continueAfterDiscard; },
    });
    loadNotes();
  });

  onDestroy(() => {
    unregisterUnsavedGuard?.();
    unregisterUnsavedGuard = null;
    pendingDiscard = null;
    rememberCurrentDraft();
    // Save any pending changes
    if (saveTimeout) {
      clearTimeout(saveTimeout);
      void saveNow(lastSessionId, lastWindowIdx, notes);
    }
    dictation.destroy();
  });

  // Load notes when session or window changes
  async function loadNotes(force = false) {
    const sessionId = get(selectedSessionId);
    const windowIdx = get(selectedWindowIdx);

    if (!sessionId) {
      loadGeneration++;
      loadingNotes = false;
      lastSessionId = null;
      notes = '';
      lastSaved = '';
      saveError = '';
      loadError = '';
      resetHistory();
      return;
    }

    // Only reload if session or window changed (unless forced)
    if (!force && sessionId === lastSessionId && windowIdx === lastWindowIdx) {
      return;
    }

    lastSessionId = sessionId;
    lastWindowIdx = windowIdx;
    const generation = ++loadGeneration;
    const targetKey = noteKey(sessionId, windowIdx);
    const remembered = draftsByTarget.get(targetKey) ?? {
      text: '', saved: '', saveError: '', loadError: '',
    };
    showDraft({ ...remembered, loadError: '' });
    loadingNotes = true;

    try {
      // A previous visit to this tab may still be flushing its last edit.
      // Read only after that tab's queue has drained; otherwise a fast
      // A → B → A switch can fetch the pre-save value and put stale text
      // back into the editor even though the later write succeeds.
      const pendingSave = saveQueues.get(targetKey);
      if (pendingSave) await pendingSave;
      if (generation !== loadGeneration || sessionId !== lastSessionId || windowIdx !== lastWindowIdx) return;
      // A failed or not-yet-saved draft is the newest copy. Reading the backend
      // here would turn a failed save into apparent success and discard the only
      // copy of the user's text on a fast A -> B -> A switch.
      const latestDraft = draftsByTarget.get(targetKey);
      if (latestDraft && (latestDraft.saveError || latestDraft.text !== latestDraft.saved)) {
        showDraft(latestDraft);
        resetHistory();
        return;
      }
      const content = await App.GetTabNotes(sessionId, windowIdx);
      if (generation !== loadGeneration || sessionId !== lastSessionId || windowIdx !== lastWindowIdx) return;
      notes = content || '';
      lastSaved = notes;
      saveError = '';
      loadError = '';
      draftsByTarget.set(targetKey, { text: notes, saved: notes, saveError: '', loadError: '' });
      resetHistory();
    } catch (e) {
      if (generation !== loadGeneration || sessionId !== lastSessionId || windowIdx !== lastWindowIdx) return;
      console.error('Failed to load notes:', e);
      // Keep the per-target draft (including a clean cached copy) and make the
      // failed load explicit. An empty editable textarea is indistinguishable
      // from a genuinely empty note and lets the next keystroke overwrite it.
      const draft = draftsByTarget.get(targetKey) ?? remembered;
      showDraft({ ...draft, loadError: String(e) });
      draftsByTarget.set(targetKey, { ...draft, loadError: String(e) });
      resetHistory();
    } finally {
      // A stale request must not re-enable the textarea while the replacement
      // target is still loading. Keeping the old note read-only in this short
      // interval also prevents keystrokes from being saved under the new tab.
      if (generation === loadGeneration && sessionId === lastSessionId && windowIdx === lastWindowIdx) {
        loadingNotes = false;
      }
    }
  }

  // Reload when tab becomes active
  let wasActive = false;
  $: if (active && !wasActive) {
    wasActive = true;
    void activateNotes();
  } else if (!active) {
    wasActive = false;
    activationGeneration++;
  }

  async function activateNotes() {
    const generation = ++activationGeneration;
    await flushPendingSave();
    if (generation !== activationGeneration || !active) return;
    // A keystroke made while the flush was in flight owns the textarea. A
    // forced read here would replace it with the previous disk snapshot.
    if (saveTimeout || notes !== lastSaved) return;
    await loadNotes(true);
  }

  async function flushPendingSave() {
    if (saveTimeout) {
      clearTimeout(saveTimeout);
      saveTimeout = null;
    }
    if (!lastSessionId || notes === lastSaved) return;
    await saveNow(lastSessionId, lastWindowIdx, notes);
  }

  // Watch for session/window changes
  $: if ($selectedSessionId !== lastSessionId || $selectedWindowIdx !== lastWindowIdx) {
    rememberCurrentDraft();
    // Save current notes before loading new ones
    if (saveTimeout) {
      clearTimeout(saveTimeout);
      saveTimeout = null;
      void saveNow(lastSessionId, lastWindowIdx, notes);
    }
    void loadNotes();
  }

  // Debounced save
  function handleInput() {
    if (loadingNotes || loadError) return;
    recordHistory();
    if (saveTimeout) {
      clearTimeout(saveTimeout);
    }
    const sessionId = lastSessionId;
    const windowIdx = lastWindowIdx;
    const snapshot = notes;
    if (sessionId) {
      draftsByTarget.set(noteKey(sessionId, windowIdx), {
        text: snapshot,
        saved: lastSaved,
        saveError: '',
        loadError: '',
      });
      saveError = '';
    }
    const timeout = setTimeout(() => {
      if (saveTimeout === timeout) saveTimeout = null;
      void saveNow(sessionId, windowIdx, snapshot);
    }, 500);
    saveTimeout = timeout;
  }

  async function saveNow(sessionId: string | null, windowIdx: number, snapshot: string) {
    if (!sessionId || (sessionId === lastSessionId && windowIdx === lastWindowIdx && snapshot === lastSaved)) return;

    const key = noteKey(sessionId, windowIdx);
    const previous = saveQueues.get(key) ?? Promise.resolve();
    const queued = previous.catch(() => undefined).then(async () => {
      savesInFlight++;
      saving = true;
      try {
        await App.SetTabNotes(sessionId, windowIdx, snapshot);
        const draft = draftsByTarget.get(key) ?? { text: snapshot, saved: lastSaved, saveError: '', loadError: '' };
        draftsByTarget.set(key, { ...draft, saved: snapshot, saveError: '', loadError: '' });
        if (sessionId === lastSessionId && windowIdx === lastWindowIdx && notes === snapshot) {
          lastSaved = snapshot;
          saveError = '';
          loadError = '';
        }
        // Notify parent to update status bar preview
        dispatch('notesChange', { sessionId, windowIdx, notes: snapshot });
      } catch (e) {
        console.error('Failed to save notes:', e);
        const message = String(e);
        const draft = draftsByTarget.get(key) ?? { text: snapshot, saved: '', saveError: '', loadError: '' };
        draftsByTarget.set(key, { ...draft, saveError: message });
        if (sessionId === lastSessionId && windowIdx === lastWindowIdx) saveError = message;
      } finally {
        savesInFlight--;
        saving = savesInFlight > 0;
      }
    });
    saveQueues.set(key, queued);
    await queued;
    if (saveQueues.get(key) === queued) saveQueues.delete(key);
  }

  async function retryNotes() {
    if (!lastSessionId) return;
    if (notes !== lastSaved || saveError) {
      await saveNow(lastSessionId, lastWindowIdx, notes);
      if (saveError) return;
    }
    await loadNotes(true);
  }
</script>

<div class="notes-container">
  <div class="notes-header">
    <span class="notes-title">{$t('notes.title')}</span>
    <div class="header-actions">
      {#if saving}
        <span class="save-indicator">{$t('notes.saving')}</span>
      {:else if notes !== lastSaved}
        <span class="save-indicator unsaved">{$t('notes.unsaved')}</span>
      {/if}
      <!-- Ctrl+F opens the same bar; the button is here for the people who
           never learn the shortcut, which is most of them. -->
      <button
        class="mic-btn find-toggle"
        class:active={showFind}
        on:click={() => (showFind ? closeFind() : openFind())}
        title="{$t('notes.findPlaceholder')} (Ctrl+F)"
        aria-label={$t('notes.findPlaceholder')}
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="11" cy="11" r="7"/>
          <path d="M21 21l-4.35-4.35"/>
        </svg>
      </button>
      <button
        class="mic-btn"
        class:active={$dictationListening}
        on:click={() => dictation.toggle()}
        disabled={loadingNotes}
        title={$dictationListening ? $t('tabBar.stopDictation') : $t('tabBar.startDictation')}
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z"/>
          <path d="M19 10v2a7 7 0 0 1-14 0v-2"/>
          <line x1="12" y1="19" x2="12" y2="23"/>
          <line x1="8" y1="23" x2="16" y2="23"/>
        </svg>
      </button>
    </div>
  </div>
  {#if showFind}
    <div class="find-bar">
      <input
        type="text"
        bind:this={findInputEl}
        bind:value={findQuery}
        on:keydown={handleFindKeydown}
        placeholder={$t('notes.findPlaceholder')}
        title="{$t('notes.nextMatch')}: Enter · F3 · Ctrl+G — {$t('notes.previousMatch')}: Shift+Enter"
      />
      <span class="find-count">
        {matches.length ? `${matchIndex + 1}/${matches.length}` : (findQuery ? $t('notes.noMatches') : '')}
      </span>
      <!-- The shortcut is named in the tooltip, not only bound: a key nobody
           is told about is a key nobody presses. -->
      <button
        on:click={() => goToMatch(matchIndex - 1)}
        disabled={!matches.length}
        title="{$t('notes.previousMatch')} (Shift+Enter · Shift+F3)"
        aria-label={$t('notes.previousMatch')}
      >↑</button>
      <button
        on:click={() => goToMatch(matchIndex + 1)}
        disabled={!matches.length}
        title="{$t('notes.nextMatch')} (Enter · F3 · Ctrl+G)"
        aria-label={$t('notes.nextMatch')}
      >↓</button>
      <button
        on:click={closeFind}
        title="{$t('common.close')} (Esc)"
        aria-label={$t('common.close')}
      >×</button>
    </div>
  {/if}
  {#if saveError || loadError}
    <div class="notes-error" role="alert">
      <span>{saveError || loadError}</span>
      <button on:click={retryNotes} disabled={loadingNotes || saving}>{$t('common.refresh')}</button>
    </div>
  {/if}
  <div class="notes-content">
    <textarea
      class="notes-textarea"
      class:dictating={$dictationListening}
      placeholder={$t('notes.placeholder')}
      bind:value={notes}
      bind:this={textareaEl}
      on:input={handleInput}
      on:keydown={handleContainerKeydown}
      disabled={loadingNotes || !!loadError}
    ></textarea>
  </div>
</div>

{#if pendingDiscard}
  <ConfirmDialog
    show={true}
    variant="warning"
    title={$t('notes.unsavedQuitTitle')}
    message={$t('notes.unsavedQuitMessage')}
    confirmText={$t('browser.discardChanges')}
    cancelText={$t('browser.keepEditing')}
    on:confirm={confirmDiscardNotes}
    on:cancel={cancelDiscardNotes}
  />
{/if}

<style>
  .find-bar {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 10px;
    background: #14141f;
    border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  }

  .find-bar input {
    flex: 1;
    min-width: 0;
    padding: 5px 9px;
    background: rgba(0, 0, 0, 0.3);
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 6px;
    color: #e5e7eb;
    font-size: 13px;
    font-family: inherit;
  }

  .find-bar input:focus {
    outline: none;
    border-color: rgba(var(--accent-rgb), 0.6);
  }

  .find-count {
    font-size: 12px;
    color: #6b7280;
    font-variant-numeric: tabular-nums;
    /* Fixed width so the buttons do not shift as the count changes. */
    min-width: 52px;
    text-align: center;
  }

  .find-bar button {
    padding: 4px 9px;
    background: transparent;
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 5px;
    color: #9ca3af;
    font-size: 13px;
    line-height: 1;
    cursor: pointer;
  }

  .find-bar button:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.07);
    color: #e5e7eb;
  }

  .find-bar button:disabled {
    opacity: 0.4;
    cursor: default;
  }

  .notes-container {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: #0a0a0f;
  }

  .notes-error {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 7px 12px;
    border-bottom: 1px solid rgba(248, 113, 113, 0.25);
    background: rgba(127, 29, 29, 0.25);
    color: #fca5a5;
    font-size: 12px;
  }

  .notes-error span {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .notes-error button {
    flex: 0 0 auto;
    padding: 3px 8px;
    border: 1px solid rgba(248, 113, 113, 0.35);
    border-radius: 5px;
    background: rgba(255, 255, 255, 0.05);
    color: #fecaca;
    cursor: pointer;
  }

  .notes-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 16px;
    background: rgba(0, 0, 0, 0.3);
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  }

  .header-actions {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .notes-title {
    font-size: 13px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #6b7280;
  }

  .save-indicator {
    font-size: 12px;
    color: #4ade80;
  }

  .save-indicator.unsaved {
    color: #fbbf24;
  }

  .notes-content {
    flex: 1;
    padding: 12px;
    overflow: hidden;
  }

  .notes-textarea {
    width: 100%;
    height: 100%;
    background: rgba(0, 0, 0, 0.2);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 12px;
    padding: 16px;
    font-size: 14px;
    font-family: inherit;
    color: white;
    resize: none;
    transition: all 0.2s ease;
    line-height: 1.6;
  }

  .notes-textarea:focus {
    outline: none;
    border-color: rgba(var(--accent-rgb), 0.4);
    box-shadow: 0 0 0 3px rgba(var(--accent-rgb), 0.1);
  }

  .notes-textarea::placeholder {
    color: #4b5563;
  }




  .notes-textarea.dictating {
    border-color: rgba(var(--accent-rgb), 0.5);
    box-shadow: 0 0 0 3px rgba(var(--accent-rgb), 0.15);
  }

  .mic-btn {
    background: none;
    border: none;
    cursor: pointer;
    color: #6b7280;
    padding: 4px;
    border-radius: 4px;
    display: flex;
    align-items: center;
    transition: color 0.2s;
  }

  .mic-btn:hover {
    color: #9ca3af;
  }

  .mic-btn.active {
    color: var(--accent);
    animation: mic-pulse 1.5s ease-in-out infinite;
  }

  @keyframes mic-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.5; }
  }
</style>
