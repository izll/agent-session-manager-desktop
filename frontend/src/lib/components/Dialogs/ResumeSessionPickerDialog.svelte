<script lang="ts">
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { createEventDispatcher } from 'svelte';
  import type { Session } from '../../stores/sessions';
  import * as App from '../../../../wailsjs/go/main/App';
  import { t } from '../../i18n';

  export let show = false;
  export let session: Session | null = null;
  // Optional overrides used when resuming a specific tab whose agent
  // differs from the parent session's main agent (e.g. a Codex tab inside
  // a Claude-led session). Defaults to the session's own values.
  export let agentOverride: string | null = null;
  export let pathOverride: string | null = null;
  // The machine the conversations live on, for a tab that will run on a
  // server. Empty is this computer, which is where a session's own
  // conversations are.
  export let serverId = '';
  // A name to show instead of the session's, for a tab that does not exist
  // yet and so has no session of its own to name.
  export let subjectName = '';

  // The label travels with the id: a caller that shows what was chosen —
  // the new-tab dialog does — would otherwise have to look it up again in a
  // list it does not hold.
  const dispatch = createEventDispatcher<{
    select: { resumeId: string; displayName: string };
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
  let lastLoadKey = '';
  let loadGeneration = 0;

  // Load sessions once per dialog open for a given (session, agent override)
  // pair. The override is part of the key so a Codex tab and a Claude main
  // session don't collide in the cache.
  $: {
    const key = show && (session || pathOverride)
      ? `${session?.id ?? ''}|${agentOverride ?? ''}|${pathOverride ?? ''}|${serverId}`
      : '';
    if (key && key !== lastLoadKey) {
      lastLoadKey = key;
      loadSessions(key);
    } else if (!show) {
      lastLoadKey = '';
      loadGeneration++;
      isLoadingSessions = false;
    }
  }

  async function loadSessions(key: string) {
    const agent = agentOverride || session?.agent || '';
    const path = pathOverride || session?.path || '';
    if (!agent || !path) return;

    const generation = ++loadGeneration;

    isLoadingSessions = true;
    error = '';
    try {
      // Asked of the machine the conversations are on: a tab bound for a
      // server resumes what that server holds, not this computer's.
      const result = await App.GetResumeSessionsOn(
        session?.id ?? '', serverId, agent, path);
      if (!show || generation !== loadGeneration || key !== lastLoadKey) return;
      availableSessions = result || [];
      cursor = 0;
    } catch (e) {
      if (!show || generation !== loadGeneration || key !== lastLoadKey) return;
      error = String(e);
      availableSessions = [];
    } finally {
      if (generation === loadGeneration) isLoadingSessions = false;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      claimKeyForDialog();
      e.stopPropagation();
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
      dispatch('select', { resumeId: '', displayName: '' });
    } else if (cursor > 0 && cursor <= availableSessions.length) {
      // Existing session
      const chosen = availableSessions[cursor - 1];
      show = false;
      dispatch('select', { resumeId: chosen.id, displayName: chosen.displayName });
    }
  }

  function handleCancel() {
    show = false;
    dispatch('cancel');
  }
</script>

{#if show}
  <div
    class="dialog-overlay" use:autoFocusDialog
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
    tabindex="0"
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2>{$t('resumePicker.title')}</h2>
        <button class="close-btn" on:click={handleCancel}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/>
            <line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>

      <div class="dialog-body">
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
          <span class="label">{$t('bgAgents.session')}</span>
          <span class="value">{subjectName || session?.name || ''}</span>
        </div>

        <div class="session-list-container">
          {#if isLoadingSessions}
            <div class="loading-sessions">
              <svg class="spinner" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83"/>
              </svg>
              {$t('resumePicker.loading')}
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
                  <span class="session-name">{$t('resumePicker.startFresh')}</span>
                  <span class="session-desc">{$t('resumePicker.newConversation')}</span>
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
      </div>

      <div class="dialog-footer">
        {$t('resumePicker.navHint')}
      </div>
    </div>
  </div>
{/if}

<style>
  /* Header, close button and footer come from the global sheet, which is
     what gives every dialog the same accent header band and footer strip.
     Only the parts the list needs are set here.

     The height is a maximum rather than a fixed one: the list is as long as
     the agent's history, and a session with two conversations should not be
     shown in a dialog sized for twenty. */
  .dialog-content {
    max-height: 80vh;
    display: flex;
    flex-direction: column;
  }

  .dialog-body {
    padding: 20px 24px;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }

  /* A hint, not a row of buttons — centred, and in the footer's muted type. */
  .dialog-footer {
    justify-content: center;
    font-size: 13px;
    color: #6b7280;
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

  /* Scrolls inside the body rather than against a fixed height, so the
     dialog shrinks to a short history and the footer stays put on a long
     one. The 2px of padding leaves room for the focus ring on the first and
     last item, which a flush edge would clip. */
  .session-list {
    overflow-y: auto;
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 2px;
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
    font-size: 13px;
    color: #9ca3af;
  }
</style>
