<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { selectedSessionId, selectedWindowIdx, selectWindow, loadSessions } from '../../stores/sessions';
  import { get } from 'svelte/store';
  import * as App from '../../../../wailsjs/go/main/App';
  import { t } from '../../i18n';

  export let show = false;

  const dispatch = createEventDispatcher();

  let name = '';
  let isSubmitting = false;
  let error = '';
  let forkMode: 'tab' | 'session' = 'tab';
  let lastShow = false;

  // Generate default name only when dialog transitions from hidden to shown
  $: if (show && !lastShow) {
    name = `Fork ${new Date().toLocaleTimeString()}`;
  }
  $: lastShow = show;

  function close() {
    show = false;
    resetForm();
    dispatch('close');
  }

  function resetForm() {
    name = '';
    error = '';
    forkMode = 'tab';
  }

  async function handleSubmit() {
    if (!name.trim()) {
      error = $t('fork.nameRequired');
      return;
    }

    const sessionId = get(selectedSessionId);
    if (!sessionId) {
      error = $t('fork.noSession');
      return;
    }

    isSubmitting = true;
    error = '';

    try {
      // First, create the fork to get new session ID
      const result = await App.ForkSession(sessionId);
      if (!result || !result.sessionId) {
        throw new Error('Fork failed - no session ID returned');
      }

      if (forkMode === 'tab') {
        // Create new tab with forked session
        await App.ForkToNewTab(sessionId, name.trim(), result.sessionId);
        // Refresh session list to get new windows
        await loadSessions();
      } else {
        // Create entirely new session with forked Claude session
        const newSession = await App.ForkToNewSession(sessionId, name.trim(), result.sessionId);
        if (newSession) {
          // Refresh and select the new session
          await loadSessions();
          dispatch('forked', { sessionId: newSession.id, name: name.trim(), isNewSession: true });
        }
      }

      close();
      dispatch('forked', { sessionId: result.sessionId, name: name.trim() });
    } catch (e) {
      error = String(e);
    } finally {
      isSubmitting = false;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      close();
    } else if (e.key === 'Enter' && !e.shiftKey) {
      handleSubmit();
    }
  }
</script>

{#if show}
  <div
    class="dialog-overlay"
    on:click|self={close}
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2>{$t('fork.title')}</h2>
        <button class="close-btn" on:click={close}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/>
            <line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>

      <div class="dialog-info">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="10"/>
          <path d="M12 16v-4"/>
          <path d="M12 8h.01"/>
        </svg>
        <span>{$t('fork.info')}</span>
      </div>

      {#if error}
        <div class="error-message">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="12" cy="12" r="10"/>
            <line x1="12" y1="8" x2="12" y2="12"/>
            <line x1="12" y1="16" x2="12.01" y2="16"/>
          </svg>
          {error}
        </div>
      {/if}

      <form on:submit|preventDefault={handleSubmit}>
        <!-- Fork Mode -->
        <div class="form-group">
          <span class="form-label">{$t('fork.forkTo')}</span>
          <div class="mode-grid">
            <button
              type="button"
              class="mode-btn {forkMode === 'tab' ? 'selected' : ''}"
              on:click={() => forkMode = 'tab'}
            >
              <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <rect x="3" y="3" width="18" height="18" rx="2"/>
                <line x1="9" y1="3" x2="9" y2="21"/>
              </svg>
              <span class="mode-title">{$t('fork.newTab')}</span>
              <span class="mode-desc">{$t('fork.newTabDesc')}</span>
            </button>
            <button
              type="button"
              class="mode-btn {forkMode === 'session' ? 'selected' : ''}"
              on:click={() => forkMode = 'session'}
            >
              <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <rect x="3" y="3" width="18" height="18" rx="2"/>
                <path d="M3 9h18"/>
              </svg>
              <span class="mode-title">{$t('fork.newSession')}</span>
              <span class="mode-desc">{$t('fork.newSessionDesc')}</span>
            </button>
          </div>
        </div>

        <!-- Name -->
        <div class="form-group">
          <label class="form-label" for="fork-name">{$t('fork.forkName')}</label>
          <input
            id="fork-name"
            type="text"
            bind:value={name}
            placeholder={$t('fork.namePlaceholder')}
            class="form-input"
          />
        </div>

        <!-- Actions -->
        <div class="dialog-actions">
          <button type="button" class="btn-cancel" on:click={close}>
            {$t('fork.cancel')}
          </button>
          <button type="submit" class="btn-primary" disabled={isSubmitting}>
            {#if isSubmitting}
              <svg class="spinner" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83"/>
              </svg>
              {$t('fork.forking')}
            {:else}
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <circle cx="12" cy="18" r="3"/>
                <circle cx="6" cy="6" r="3"/>
                <circle cx="18" cy="6" r="3"/>
                <path d="M6 9v3a3 3 0 003 3h6a3 3 0 003-3V9"/>
                <path d="M12 12v3"/>
              </svg>
              {$t('fork.forkBtn')}
            {/if}
          </button>
        </div>
      </form>
    </div>
  </div>
{/if}

<style>
  /* Component-specific: wider dialog */
  .dialog-content {
    max-width: 420px;
  }

  .dialog-info {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    margin: 16px 24px;
    padding: 12px 16px;
    background: rgba(139, 92, 246, 0.1);
    border: 1px solid rgba(139, 92, 246, 0.2);
    border-radius: 10px;
    font-size: 12px;
    color: #a78bfa;
    line-height: 1.5;
  }

  .dialog-info svg {
    flex-shrink: 0;
    margin-top: 2px;
  }

  /* Component-specific: error message with icon and margin */
  .error-message {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 16px 24px;
  }

  form {
    padding: 24px;
    padding-top: 0;
  }

  /* Component-specific: larger margin for form groups */
  .form-group {
    margin-bottom: 20px;
  }

  .form-label {
    display: block;
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #9ca3af;
    margin-bottom: 10px;
  }

  .mode-grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 12px;
  }

  .mode-btn {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    padding: 16px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 12px;
    cursor: pointer;
    transition: all 0.2s ease;
    color: #9ca3af;
  }

  .mode-btn:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(255, 255, 255, 0.15);
  }

  .mode-btn.selected {
    background: linear-gradient(135deg, rgba(139, 92, 246, 0.2) 0%, rgba(99, 102, 241, 0.15) 100%);
    border-color: rgba(139, 92, 246, 0.5);
    box-shadow: 0 0 20px rgba(139, 92, 246, 0.15);
    color: #a78bfa;
  }

  .mode-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .mode-title {
    font-size: 13px;
    font-weight: 600;
  }

  .mode-desc {
    font-size: 11px;
    opacity: 0.7;
  }

  .form-input {
    width: 100%;
    padding: 12px 16px;
    background: rgba(0, 0, 0, 0.2);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 10px;
    font-size: 14px;
    color: white;
    transition: all 0.2s ease;
  }

  .form-input::placeholder {
    color: #4b5563;
  }

  .form-input:focus {
    outline: none;
    border-color: rgba(139, 92, 246, 0.5);
    box-shadow: 0 0 0 3px rgba(139, 92, 246, 0.1);
  }

  .dialog-actions {
    display: flex;
    justify-content: flex-end;
    gap: 12px;
    padding-top: 16px;
    border-top: 1px solid rgba(255, 255, 255, 0.05);
  }

  /* Component-specific: primary button with icon support */
  .btn-primary {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 24px;
  }

  .spinner {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }
</style>
