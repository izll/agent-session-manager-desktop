<script lang="ts">
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { createEventDispatcher } from 'svelte';
  import { sessions, groups, selectedSessionId, selectedWindowIdx, selectSession, selectWindow, loadSessions, assignToGroup } from '../../stores/sessions';
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
  /** Which group a forked SESSION lands in. A forked tab has no say: it stays
   *  inside the session it was branched from. */
  let selectedGroupId = '';

  // Generate default name only when dialog transitions from hidden to shown
  // Assign lastShow inside the same block: a separate `$: lastShow = show`
  // is ordered BEFORE this guard, so the "just opened" test never passes.
  $: {
    if (show && !lastShow) {
      name = `Fork ${new Date().toLocaleTimeString()}`;
      // Defaults to where the original lives, which is what a fork inherited
      // silently before. Offered rather than assumed: a branch is often an
      // experiment, and experiments belong somewhere else.
      selectedGroupId = get(sessions).find(s => s.id === get(selectedSessionId))?.groupId || '';
    }
    lastShow = show;
  }

  function close() {
    show = false;
    resetForm();
    dispatch('close');
  }

  function resetForm() {
    name = '';
    error = '';
    forkMode = 'tab';
    selectedGroupId = '';
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
      // Branch the conversation in THIS tab. Without the window index the
      // backend read the session's main window, so forking from a second Claude
      // tab produced a branch of a different conversation.
      const result = await App.ForkSession(sessionId, $selectedWindowIdx ?? 0);
      if (!result || !result.sessionId) {
        throw new Error('Fork failed - no session ID returned');
      }

      if (forkMode === 'tab') {
        const newIdx = await App.ForkToNewTab(sessionId, name.trim(), result.sessionId);
        await loadSessions();
        // Switch to the branch. It was created and then left for the user to
        // find, which is half a feature.
        selectWindow(newIdx);
        close();
        dispatch('forked', { sessionId, windowIdx: newIdx, name: name.trim() });
      } else {
        const newSession = await App.ForkToNewSession(sessionId, name.trim(), result.sessionId);
        if (!newSession) {
          // Closing silently here would look like it had worked.
          throw new Error('Fork failed - no session was created');
        }
        // The backend gives the branch the original's group. Applied here only
        // where the user chose otherwise, so the common case makes no extra
        // call — and "no group" is a real choice, which is why this compares
        // against the original rather than testing for a non-empty value.
        const inheritedGroup = get(sessions).find(s => s.id === sessionId)?.groupId || '';
        if (selectedGroupId !== inheritedGroup) {
          await assignToGroup(newSession.id, selectedGroupId);
        }

        await loadSessions();
        // Switch to the branch, as the new-tab case does. Created and then left
        // for the user to find, a fork to a new session looked like nothing had
        // happened at all: the dialog closed, the view stayed where it was, and
        // the new session was somewhere down the sidebar under a name only the
        // clock knew.
        selectSession(newSession.id);
        selectWindow(0);
        close();
        // One event per fork, and its sessionId is always this app's session
        // id. It used to fire twice for a new session, the second time carrying
        // the Claude conversation id under the same field name.
        dispatch('forked', { sessionId: newSession.id, name: name.trim(), isNewSession: true });
      }
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
    class="dialog-overlay" use:autoFocusDialog
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

        <!-- Group, for a forked SESSION only.
             A forked tab stays inside the session it came from, so there is
             nothing to choose. Shown only when groups exist, as the new-session
             dialog does — an empty picker is a question with one answer. -->
        {#if forkMode === 'session' && $groups.length > 0}
          <div class="form-group">
            <label class="form-label" for="fork-group">{$t('fork.group')}</label>
            <select id="fork-group" bind:value={selectedGroupId} class="form-input form-select">
              <option value="">{$t('fork.noGroup')}</option>
              {#each $groups as group (group.id)}
                <option value={group.id}>{group.name}</option>
              {/each}
            </select>
          </div>
        {/if}

        <!-- What a fork is until it is written to.
             The agent loads the conversation and waits: the branch has no
             transcript of its own until the first message, so a fork left
             untouched leaves nothing behind. Said here because there is no
             way to tell by looking — the tab opens with the full history
             visible, which is exactly what a finished fork looks like. -->
        <p class="fork-note">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="12" cy="12" r="10"/>
            <line x1="12" y1="16" x2="12" y2="12"/>
            <line x1="12" y1="8" x2="12.01" y2="8"/>
          </svg>
          <span>{$t('fork.firstMessageNote')}</span>
        </p>

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
    background: rgba(var(--accent-rgb), 0.1);
    border: 1px solid rgba(var(--accent-rgb), 0.2);
    border-radius: 10px;
    font-size: 13px;
    color: var(--accent-light);
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
    font-size: 13px;
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
    background: linear-gradient(135deg, rgba(var(--accent-rgb), 0.2) 0%, rgba(99, 102, 241, 0.15) 100%);
    border-color: rgba(var(--accent-rgb), 0.5);
    box-shadow: 0 0 20px rgba(var(--accent-rgb), 0.15);
    color: var(--accent-light);
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
    font-size: 12px;
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
    border-color: rgba(var(--accent-rgb), 0.5);
    box-shadow: 0 0 0 3px rgba(var(--accent-rgb), 0.1);
  }

  /* The same chevron the new-session dialog's group picker uses, so the two
     read as the same control. */
  .form-select {
    appearance: none;
    background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 24 24' fill='none' stroke='%239ca3af' stroke-width='2'%3E%3Cpolyline points='6 9 12 15 18 9'%3E%3C/polyline%3E%3C/svg%3E");
    background-repeat: no-repeat;
    background-position: right 12px center;
    padding-right: 36px;
    cursor: pointer;
  }

  /* Amber, like the fork warning in the new-session dialog, but lighter: this
     is telling you how forking works rather than warning you off it. */
  .fork-note {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    margin: 0 0 20px;
    padding: 10px 12px;
    background: rgba(251, 191, 36, 0.07);
    border: 1px solid rgba(251, 191, 36, 0.2);
    border-radius: 8px;
    font-size: 12px;
    line-height: 1.5;
    color: #d4b878;
  }
  .fork-note svg {
    flex-shrink: 0;
    margin-top: 1px;
    opacity: 0.8;
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
