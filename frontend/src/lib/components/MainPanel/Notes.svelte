<script lang="ts">
  import { onMount, onDestroy, createEventDispatcher } from 'svelte';
  import { selectedSessionId, selectedWindowIdx } from '../../stores/sessions';
  import { get } from 'svelte/store';
  import * as App from '../../../../wailsjs/go/main/App';
  import { createFieldDictation } from '../../utils/dictationField';
  import { t } from '../../i18n';

  export let active = false;

  const dispatch = createEventDispatcher();

  let notes = '';
  let lastSessionId: string | null = null;
  let lastWindowIdx: number = 0;
  let saveTimeout: ReturnType<typeof setTimeout> | null = null;
  let saving = false;
  let lastSaved = '';
  let textareaEl: HTMLTextAreaElement;

  // Dictation support
  const dictation = createFieldDictation(
    () => textareaEl,
    () => handleInput() // trigger autosave after dictation inserts text
  );
  const dictationListening = dictation.listening;

  onMount(() => {
    loadNotes();
  });

  onDestroy(() => {
    // Save any pending changes
    if (saveTimeout) {
      clearTimeout(saveTimeout);
      saveNow();
    }
    dictation.destroy();
  });

  // Load notes when session or window changes
  async function loadNotes(force = false) {
    const sessionId = get(selectedSessionId);
    const windowIdx = get(selectedWindowIdx);

    if (!sessionId) {
      notes = '';
      lastSaved = '';
      return;
    }

    // Only reload if session or window changed (unless forced)
    if (!force && sessionId === lastSessionId && windowIdx === lastWindowIdx) {
      return;
    }

    lastSessionId = sessionId;
    lastWindowIdx = windowIdx;

    try {
      const content = await App.GetTabNotes(sessionId, windowIdx);
      notes = content || '';
      lastSaved = notes;
    } catch (e) {
      console.error('Failed to load notes:', e);
      notes = '';
      lastSaved = '';
    }
  }

  // Reload when tab becomes active
  let wasActive = false;
  $: if (active && !wasActive) {
    wasActive = true;
    loadNotes(true);
  } else if (!active) {
    wasActive = false;
  }

  // Watch for session/window changes
  $: if ($selectedSessionId !== lastSessionId || $selectedWindowIdx !== lastWindowIdx) {
    // Save current notes before loading new ones
    if (saveTimeout) {
      clearTimeout(saveTimeout);
      saveNow();
    }
    loadNotes();
  }

  // Debounced save
  function handleInput() {
    if (saveTimeout) {
      clearTimeout(saveTimeout);
    }
    saveTimeout = setTimeout(saveNow, 500);
  }

  async function saveNow() {
    const sessionId = get(selectedSessionId);
    const windowIdx = get(selectedWindowIdx);

    if (!sessionId || notes === lastSaved) return;

    saving = true;
    try {
      await App.SetTabNotes(sessionId, windowIdx, notes);
      lastSaved = notes;
      // Notify parent to update status bar preview
      dispatch('notesChange', { sessionId, windowIdx, notes });
    } catch (e) {
      console.error('Failed to save notes:', e);
    }
    saving = false;
    saveTimeout = null;
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
      <button
        class="mic-btn"
        class:active={$dictationListening}
        on:click={() => dictation.toggle()}
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
  <div class="notes-content">
    <textarea
      class="notes-textarea"
      class:dictating={$dictationListening}
      placeholder={$t('notes.placeholder')}
      bind:value={notes}
      bind:this={textareaEl}
      on:input={handleInput}
    ></textarea>
  </div>
</div>

<style>
  .notes-container {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: #0a0a0f;
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
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #6b7280;
  }

  .save-indicator {
    font-size: 11px;
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
    border-color: rgba(139, 92, 246, 0.4);
    box-shadow: 0 0 0 3px rgba(139, 92, 246, 0.1);
  }

  .notes-textarea::placeholder {
    color: #4b5563;
  }

  .notes-textarea::-webkit-scrollbar {
    width: 6px;
  }

  .notes-textarea::-webkit-scrollbar-track {
    background: transparent;
  }

  .notes-textarea::-webkit-scrollbar-thumb {
    background: rgba(139, 92, 246, 0.3);
    border-radius: 3px;
  }

  .notes-textarea.dictating {
    border-color: rgba(139, 92, 246, 0.5);
    box-shadow: 0 0 0 3px rgba(139, 92, 246, 0.15);
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
    color: #8b5cf6;
    animation: mic-pulse 1.5s ease-in-out infinite;
  }

  @keyframes mic-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.5; }
  }
</style>
