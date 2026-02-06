<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import type { Session } from '../../stores/sessions';
  import * as App from '../../../../wailsjs/go/main/App';

  export let show = false;
  export let session: Session | null = null;

  const dispatch = createEventDispatcher<{
    select: { resumeId: string };
    cancel: void;
  }>();

  interface ResumeSession {
    id: string;
    displayName: string;
    timestamp: string;
  }

  let availableSessions: ResumeSession[] = [];
  let isLoadingSessions = false;
  let cursor = 0; // 0 = new session, 1+ = existing sessions
  let error = '';

  $: if (show && session) {
    loadSessions();
  }

  async function loadSessions() {
    if (!session) return;

    isLoadingSessions = true;
    error = '';
    try {
      const result = await App.GetResumeSessions(session.agent, session.path);
      availableSessions = result || [];
      cursor = 0;
    } catch (e) {
      error = String(e);
      availableSessions = [];
    } finally {
      isLoadingSessions = false;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      handleCancel();
    } else if (e.key === 'ArrowUp' || e.key === 'k') {
      e.preventDefault();
      cursor = Math.max(0, cursor - 1);
    } else if (e.key === 'ArrowDown' || e.key === 'j') {
      e.preventDefault();
      cursor = Math.min(availableSessions.length, cursor + 1);
    } else if (e.key === 'Home') {
      e.preventDefault();
      cursor = 0;
    } else if (e.key === 'End') {
      e.preventDefault();
      cursor = availableSessions.length;
    } else if (e.key === 'Enter') {
      e.preventDefault();
      handleSelect();
    }
  }

  function handleSelect() {
    if (cursor === 0) {
      // New session
      show = false;
      dispatch('select', { resumeId: '' });
    } else if (cursor > 0 && cursor <= availableSessions.length) {
      // Existing session
      const resumeId = availableSessions[cursor - 1].id;
      show = false;
      dispatch('select', { resumeId });
    }
  }

  function handleCancel() {
    show = false;
    dispatch('cancel');
  }
</script>

{#if show}
  <div
    class="dialog-overlay"
    on:click|self={handleCancel}
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
    tabindex="0"
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2>Resume Session</h2>
        <button class="close-btn" on:click={handleCancel}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/>
            <line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
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

      <div class="session-info">
        <span class="label">Session:</span>
        <span class="value">{session?.name || ''}</span>
      </div>

      <div class="session-list-container">
        {#if isLoadingSessions}
          <div class="loading-sessions">
            <svg class="spinner" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83"/>
            </svg>
            Loading sessions...
          </div>
        {:else}
          <div class="session-list">
            <button
              type="button"
              class="session-item {cursor === 0 ? 'active' : ''}"
              on:click={() => { cursor = 0; handleSelect(); }}
              on:mouseenter={() => cursor = 0}
            >
              <span class="session-icon new">+</span>
              <span class="session-info-inner">
                <span class="session-name">Start fresh</span>
                <span class="session-desc">New conversation</span>
              </span>
            </button>
            {#each availableSessions as sess, i (sess.id)}
              <button
                type="button"
                class="session-item {cursor === i + 1 ? 'active' : ''}"
                on:click={() => { cursor = i + 1; handleSelect(); }}
                on:mouseenter={() => cursor = i + 1}
              >
                <span class="session-icon resume">↻</span>
                <span class="session-info-inner">
                  <span class="session-name">{sess.displayName}</span>
                  <span class="session-desc">{sess.timestamp}</span>
                </span>
              </button>
            {/each}
          </div>
        {/if}
      </div>

      <div class="dialog-hint">
        Use ↑↓ to navigate, Enter to select, Esc to cancel
      </div>
    </div>
  </div>
{/if}

<style>
  .dialog-content {
    padding: 24px;
    max-width: 500px;
    max-height: 80vh;
    display: flex;
    flex-direction: column;
  }

  .dialog-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 20px;
  }

  .dialog-header h2 {
    font-size: 18px;
    font-weight: 600;
    color: #e4e4e7;
    margin: 0;
  }

  .close-btn {
    background: transparent;
    border: none;
    color: #9ca3af;
    cursor: pointer;
    padding: 4px;
    border-radius: 6px;
    transition: all 0.15s ease;
  }

  .close-btn:hover {
    background: rgba(255, 255, 255, 0.05);
    color: white;
  }

  .error-message {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.3);
    border-radius: 8px;
    color: #fca5a5;
    font-size: 13px;
    margin-bottom: 16px;
  }

  .session-info {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px;
    background: rgba(255, 255, 255, 0.03);
    border-radius: 8px;
    margin-bottom: 16px;
  }

  .session-info .label {
    font-size: 13px;
    color: #9ca3af;
  }

  .session-info .value {
    font-size: 13px;
    font-weight: 600;
    color: #e4e4e7;
  }

  .session-list-container {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .loading-sessions {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 12px;
    padding: 40px;
    color: #9ca3af;
    font-size: 14px;
  }

  .spinner {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }

  .session-list {
    overflow-y: auto;
    max-height: 400px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .session-item {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 16px;
    background: rgba(255, 255, 255, 0.03);
    border: 2px solid transparent;
    border-radius: 10px;
    cursor: pointer;
    transition: all 0.15s ease;
    text-align: left;
  }

  .session-item:hover {
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(255, 255, 255, 0.1);
  }

  .session-item.active {
    background: rgba(59, 130, 246, 0.1);
    border-color: #3b82f6;
    box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.1);
  }

  .session-icon {
    flex-shrink: 0;
    width: 32px;
    height: 32px;
    border-radius: 8px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 16px;
    font-weight: 600;
  }

  .session-icon.new {
    background: rgba(34, 197, 94, 0.15);
    color: #22c55e;
  }

  .session-icon.resume {
    background: rgba(59, 130, 246, 0.15);
    color: #3b82f6;
  }

  .session-info-inner {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .session-name {
    font-size: 14px;
    font-weight: 600;
    color: #e4e4e7;
  }

  .session-desc {
    font-size: 12px;
    color: #9ca3af;
  }

  .dialog-hint {
    margin-top: 16px;
    padding-top: 16px;
    border-top: 1px solid rgba(255, 255, 255, 0.1);
    font-size: 12px;
    color: #6b7280;
    text-align: center;
  }
</style>
