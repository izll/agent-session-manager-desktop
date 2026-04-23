<script lang="ts">
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { createEventDispatcher } from 'svelte';
  import { agents, loadAgents } from '../../stores/agents';
  import { sessions, groups, selectedSession, createSession, startSession, assignToGroup } from '../../stores/sessions';
  import { get } from 'svelte/store';
  import AgentIcon from '../common/AgentIcon.svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import { t } from '../../i18n';

  export let show = false;

  const dispatch = createEventDispatcher();

  let name = '';
  let path = '';
  let selectedAgent = 'claude';
  let autoYes = false;
  let autoStart = true;
  let isSubmitting = false;
  let error = '';
  let selectedGroupId = '';
  let groupInitialized = false;
  let extraArgs = '';

  // Resume session selection
  interface ResumeSession {
    id: string;
    displayName: string;
    timestamp: string;
  }
  let availableSessions: ResumeSession[] = [];
  let selectedResumeId: string = '';
  let isLoadingSessions = false;
  let showForkWarning = false;
  let conflictingSessionName = '';

  $: if (show && $agents.length === 0) {
    loadAgents();
  }

  // Set default group from selected session ONLY when dialog first opens
  $: if (show && !groupInitialized) {
    selectedGroupId = $selectedSession?.groupId || '';
    groupInitialized = true;
  }
  $: if (!show) {
    groupInitialized = false;
  }

  // Auto-fill name from path (only if user hasn't manually edited the name field)
  let userTouchedName = false;
  $: if (path && !userTouchedName) {
    const parts = path.replace(/\/+$/, '').split('/');
    const folderName = parts[parts.length - 1] || '';
    if (folderName) {
      name = folderName;
    }
  }

  function handleNameInput() {
    userTouchedName = true;
  }

  // Debounced path change handler
  let pathDebounceTimer: ReturnType<typeof setTimeout>;
  $: if (path && selectedAgent) {
    clearTimeout(pathDebounceTimer);
    pathDebounceTimer = setTimeout(() => loadAvailableSessions(path, selectedAgent), 500);
  } else {
    availableSessions = [];
    selectedResumeId = '';
  }

  async function loadAvailableSessions(searchPath: string, agent: string) {
    if (!searchPath.trim()) {
      availableSessions = [];
      return;
    }

    // Check if agent supports resume
    const agentConfig = $agents.find(a => a.type === agent);
    if (!agentConfig?.supportsResume) {
      availableSessions = [];
      return;
    }

    isLoadingSessions = true;
    try {
      const result = await App.GetResumeSessions(agent, searchPath.trim());
      availableSessions = result || [];
    } catch (e) {
      console.error('Failed to load sessions:', e);
      availableSessions = [];
    } finally {
      isLoadingSessions = false;
    }
  }

  function checkSessionConflict(resumeId: string): string | null {
    if (!resumeId) return null;

    const existingSessions = get(sessions);
    for (const sess of existingSessions) {
      // Check main session
      if (sess.resumeSessionId === resumeId) {
        return sess.name;
      }
      // Check followed windows (tabs)
      if (sess.followedWindows) {
        for (const fw of sess.followedWindows) {
          if (fw.resumeSessionId === resumeId) {
            return `${sess.name} (tab)`;
          }
        }
      }
    }
    return null;
  }

  function handleResumeSelect(resumeId: string) {
    const conflict = checkSessionConflict(resumeId);
    if (conflict) {
      conflictingSessionName = conflict;
      showForkWarning = true;
      selectedResumeId = resumeId;
    } else {
      selectedResumeId = resumeId;
      showForkWarning = false;
    }
  }

  function close() {
    show = false;
    resetForm();
    dispatch('close');
  }

  function resetForm() {
    name = '';
    path = '';
    selectedAgent = 'claude';
    autoYes = false;
    autoStart = true;
    error = '';
    userTouchedName = false;
    selectedGroupId = '';
    extraArgs = '';
    availableSessions = [];
    selectedResumeId = '';
    showForkWarning = false;
    conflictingSessionName = '';
  }

  async function handleSubmit(fork: boolean = false) {
    if (!name.trim() || !path.trim()) {
      error = $t('newSession.nameRequired');
      return;
    }

    // If there's a conflict and user hasn't confirmed fork, show warning
    if (selectedResumeId && !fork) {
      const conflict = checkSessionConflict(selectedResumeId);
      if (conflict) {
        conflictingSessionName = conflict;
        showForkWarning = true;
        return;
      }
    }

    isSubmitting = true;
    error = '';

    // Save group selection before createSession changes selectedSessionId,
    // which triggers the reactive statement and resets selectedGroupId to ''
    const groupId = selectedGroupId;

    try {
      const session = await createSession(name.trim(), path.trim(), selectedAgent, autoYes, extraArgs.trim());
      if (session) {
        if (groupId) {
          await assignToGroup(session.id, groupId);
        }
        // If resuming, start with resume ID
        if (selectedResumeId && autoStart) {
          await App.StartSessionWithResume(session.id, selectedResumeId);
          // Reload sessions to update store
          const { loadSessions } = await import('../../stores/sessions');
          await loadSessions();
        } else if (autoStart) {
          await startSession(session.id);
        }
      }
      close();
    } catch (e) {
      error = String(e);
    } finally {
      isSubmitting = false;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      if (showForkWarning) {
        showForkWarning = false;
      } else {
        close();
      }
    } else if (e.key === 'Enter' && !e.shiftKey && !showForkWarning) {
      handleSubmit();
    }
  }

  async function browsePath() {
    try {
      const selectedPath = await App.BrowseDirectory(path || '');
      if (selectedPath) {
        path = selectedPath;
      }
    } catch (e) {
      console.error('Browse failed:', e);
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
        <h2>{$t('newSession.title')}</h2>
        <button class="close-btn" on:click={close}>
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

      <form on:submit|preventDefault={handleSubmit}>
        <!-- Agent Type -->
        <div class="form-group">
          <span class="form-label">{$t('newSession.agentType')}</span>
          <div class="agent-grid">
            {#each $agents.filter(a => a.type !== 'terminal') as agent (agent.type)}
              <button
                type="button"
                class="agent-btn {selectedAgent === agent.type ? 'selected' : ''}"
                on:click={() => selectedAgent = agent.type}
              >
                <span class="agent-icon-wrapper">
                  <AgentIcon agent={agent.type} size="md" />
                </span>
                <span class="agent-name">{agent.name}</span>
              </button>
            {/each}
          </div>
        </div>

        <!-- Path -->
        <div class="form-group">
          <label class="form-label" for="path">{$t('newSession.projectPath')}</label>
          <div class="path-input-group">
            <input
              id="path"
              type="text"
              bind:value={path}
              placeholder="/home/user/project"
              class="form-input"
            />
            <button type="button" class="browse-btn" on:click={browsePath} title={$t('newSession.browse')}>
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>
              </svg>
            </button>
          </div>
        </div>

        <!-- Name -->
        <div class="form-group">
          <label class="form-label" for="name">{$t('newSession.sessionName')}</label>
          <input
            id="name"
            type="text"
            bind:value={name}
            on:input={handleNameInput}
            placeholder="my-project"
            class="form-input"
          />
        </div>

        <!-- Group -->
        {#if $groups.length > 0}
          <div class="form-group">
            <label class="form-label" for="group">{$t('newSession.group')}</label>
            <select id="group" bind:value={selectedGroupId} class="form-input form-select">
              <option value="">{$t('newSession.noGroup')}</option>
              {#each $groups as group (group.id)}
                <option value={group.id}>{group.name}</option>
              {/each}
            </select>
          </div>
        {/if}

        <!-- Extra CLI Arguments (hidden for custom agent) -->
        {#if selectedAgent !== 'custom' && selectedAgent !== 'terminal'}
          <div class="form-group">
            <label class="form-label" for="extra-args">{$t('newSession.extraArgs')}</label>
            <input
              id="extra-args"
              type="text"
              bind:value={extraArgs}
              placeholder={$t('newSession.extraArgsPlaceholder')}
              class="form-input"
            />
            <span class="form-hint">{$t('newSession.extraArgsHint')}</span>
          </div>
        {/if}

        <!-- Available Sessions to Resume -->
        {#if availableSessions.length > 0 || isLoadingSessions}
          <div class="form-group">
            <span class="form-label">{$t('newSession.resumePrevious')}</span>
            {#if isLoadingSessions}
              <div class="loading-sessions">
                <svg class="spinner" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83"/>
                </svg>
                {$t('newSession.loadingSessions')}
              </div>
            {:else}
              <div class="session-list">
                <button
                  type="button"
                  class="session-item {selectedResumeId === '' ? 'selected' : ''}"
                  on:click={() => { selectedResumeId = ''; showForkWarning = false; }}
                >
                  <span class="session-icon new">+</span>
                  <span class="session-info">
                    <span class="session-name">{$t('newSession.startFresh')}</span>
                    <span class="session-desc">{$t('newSession.newConversation')}</span>
                  </span>
                </button>
                {#each availableSessions as sess (sess.id)}
                  {@const isConflict = checkSessionConflict(sess.id)}
                  <button
                    type="button"
                    class="session-item {selectedResumeId === sess.id ? 'selected' : ''} {isConflict ? 'conflict' : ''}"
                    on:click={() => handleResumeSelect(sess.id)}
                  >
                    <span class="session-icon resume">↻</span>
                    <span class="session-info">
                      <span class="session-name">{sess.displayName}</span>
                      <span class="session-desc">
                        {sess.timestamp}
                        {#if isConflict}
                          <span class="conflict-badge">{$t('newSession.inUse', { name: isConflict })}</span>
                        {/if}
                      </span>
                    </span>
                  </button>
                {/each}
              </div>
            {/if}
          </div>
        {/if}

        <!-- Fork Warning -->
        {#if showForkWarning}
          <div class="fork-warning">
            <div class="warning-icon">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
                <line x1="12" y1="9" x2="12" y2="13"/>
                <line x1="12" y1="17" x2="12.01" y2="17"/>
              </svg>
            </div>
            <div class="warning-content">
              <div class="warning-title">{$t('newSession.sessionInUse')}</div>
              <div class="warning-text">
                {$t('newSession.forkWarning', { name: conflictingSessionName })}
              </div>
            </div>
            <div class="warning-actions">
              <button type="button" class="btn-warning-cancel" on:click={() => { showForkWarning = false; selectedResumeId = ''; }}>
                {$t('newSession.forkCancel')}
              </button>
              <button type="button" class="btn-warning-fork" on:click={() => handleSubmit(true)}>
                {$t('newSession.forkCreate')}
              </button>
            </div>
          </div>
        {/if}

        <!-- Options -->
        <div class="form-options">
          <label class="checkbox-label">
            <input type="checkbox" bind:checked={autoYes} class="checkbox-input" />
            <span class="checkbox-custom"></span>
            <span class="checkbox-text">{$t('newSession.autoApprove')}</span>
          </label>

          <label class="checkbox-label">
            <input type="checkbox" bind:checked={autoStart} class="checkbox-input" />
            <span class="checkbox-custom"></span>
            <span class="checkbox-text">{$t('newSession.startImmediately')}</span>
          </label>
        </div>

        <!-- Actions -->
        <div class="dialog-actions">
          <button type="button" class="btn-cancel" on:click={close}>
            {$t('newSession.cancel')}
          </button>
          <button type="submit" class="btn-primary" disabled={isSubmitting}>
            {#if isSubmitting}
              <svg class="spinner" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83"/>
              </svg>
              {$t('newSession.creating')}
            {:else}
              {$t('newSession.createSession')}
            {/if}
          </button>
        </div>
      </form>
    </div>
  </div>
{/if}

<style>
  /* Component-specific: wider dialog for agent grid */
  .dialog-content {
    max-width: 480px;
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
  }

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

  .agent-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 8px;
  }

  .agent-btn {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 12px 8px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 12px;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .agent-btn:hover {
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(255, 255, 255, 0.15);
  }

  .agent-btn.selected {
    background: linear-gradient(135deg, rgba(139, 92, 246, 0.2) 0%, rgba(99, 102, 241, 0.15) 100%);
    border-color: rgba(139, 92, 246, 0.5);
    box-shadow: 0 0 20px rgba(139, 92, 246, 0.15);
  }

  .agent-icon-wrapper {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
  }

  .agent-name {
    font-size: 11px;
    color: #9ca3af;
  }

  .agent-btn.selected .agent-name {
    color: #a78bfa;
  }

  .path-input-group {
    display: flex;
    gap: 8px;
  }

  .path-input-group .form-input {
    flex: 1;
  }

  .browse-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 0 14px;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 10px;
    color: #9ca3af;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .browse-btn:hover {
    background: rgba(255, 255, 255, 0.1);
    border-color: rgba(255, 255, 255, 0.15);
    color: white;
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

  .form-hint {
    display: block;
    font-size: 11px;
    color: #6b7280;
    margin-top: 6px;
  }

  .form-input::placeholder {
    color: #4b5563;
  }

  .form-input:focus {
    outline: none;
    border-color: rgba(139, 92, 246, 0.5);
    box-shadow: 0 0 0 3px rgba(139, 92, 246, 0.1);
  }

  .form-select {
    appearance: none;
    background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 24 24' fill='none' stroke='%239ca3af' stroke-width='2'%3E%3Cpolyline points='6 9 12 15 18 9'%3E%3C/polyline%3E%3C/svg%3E");
    background-repeat: no-repeat;
    background-position: right 12px center;
    padding-right: 36px;
    cursor: pointer;
  }

  .form-select option {
    background: #1f2937;
    color: white;
  }

  .form-options {
    display: flex;
    gap: 24px;
    margin-bottom: 24px;
  }

  .checkbox-label {
    display: flex;
    align-items: center;
    gap: 10px;
    cursor: pointer;
    user-select: none;
  }

  .checkbox-input {
    display: none;
  }

  .checkbox-custom {
    width: 18px;
    height: 18px;
    background: rgba(0, 0, 0, 0.2);
    border: 1px solid rgba(255, 255, 255, 0.15);
    border-radius: 5px;
    position: relative;
    transition: all 0.2s ease;
  }

  .checkbox-input:checked + .checkbox-custom {
    background: linear-gradient(135deg, #8b5cf6 0%, #6366f1 100%);
    border-color: transparent;
  }

  .checkbox-input:checked + .checkbox-custom::after {
    content: '';
    position: absolute;
    left: 6px;
    top: 2px;
    width: 5px;
    height: 10px;
    border: solid white;
    border-width: 0 2px 2px 0;
    transform: rotate(45deg);
  }

  .checkbox-text {
    font-size: 13px;
    color: #9ca3af;
  }

  .dialog-actions {
    display: flex;
    justify-content: flex-end;
    gap: 12px;
    padding-top: 8px;
    border-top: 1px solid rgba(255, 255, 255, 0.05);
  }

  /* Component-specific: primary button with flex for spinner */
  .btn-primary {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .spinner {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }

  /* Session list styles */
  .loading-sessions {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px;
    color: #9ca3af;
    font-size: 13px;
  }

  .session-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
    max-height: 200px;
    overflow-y: auto;
    padding: 4px;
    background: rgba(0, 0, 0, 0.15);
    border-radius: 10px;
  }

  .session-item {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 12px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.05);
    border-radius: 8px;
    cursor: pointer;
    transition: all 0.2s ease;
    text-align: left;
  }

  .session-item:hover {
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(255, 255, 255, 0.1);
  }

  .session-item.selected {
    background: linear-gradient(135deg, rgba(139, 92, 246, 0.15) 0%, rgba(99, 102, 241, 0.1) 100%);
    border-color: rgba(139, 92, 246, 0.4);
  }

  .session-item.conflict {
    border-color: rgba(251, 191, 36, 0.3);
  }

  .session-item.conflict.selected {
    border-color: rgba(251, 191, 36, 0.5);
  }

  .session-icon {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    border-radius: 6px;
    font-size: 14px;
    font-weight: 600;
  }

  .session-icon.new {
    background: rgba(34, 197, 94, 0.15);
    color: #4ade80;
  }

  .session-icon.resume {
    background: rgba(139, 92, 246, 0.15);
    color: #a78bfa;
  }

  .session-info {
    flex: 1;
    min-width: 0;
  }

  .session-name {
    display: block;
    font-size: 13px;
    font-weight: 500;
    color: #e4e4e7;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .session-desc {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 11px;
    color: #6b7280;
    margin-top: 2px;
  }

  .conflict-badge {
    padding: 2px 6px;
    background: rgba(251, 191, 36, 0.15);
    border-radius: 4px;
    font-size: 10px;
    color: #fbbf24;
  }

  /* Fork warning styles */
  .fork-warning {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 16px;
    background: rgba(251, 191, 36, 0.08);
    border: 1px solid rgba(251, 191, 36, 0.25);
    border-radius: 10px;
    margin-bottom: 20px;
  }

  .warning-icon {
    color: #fbbf24;
  }

  .warning-content {
    flex: 1;
  }

  .warning-title {
    font-size: 14px;
    font-weight: 600;
    color: #fbbf24;
    margin-bottom: 4px;
  }

  .warning-text {
    font-size: 13px;
    color: #9ca3af;
    line-height: 1.5;
  }

  .warning-text strong {
    color: #e4e4e7;
  }

  .warning-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 8px;
  }

  .btn-warning-cancel {
    padding: 8px 16px;
    background: transparent;
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 8px;
    font-size: 13px;
    font-weight: 500;
    color: #9ca3af;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .btn-warning-cancel:hover {
    background: rgba(255, 255, 255, 0.05);
    color: white;
  }

  .btn-warning-fork {
    padding: 8px 16px;
    background: linear-gradient(135deg, rgba(251, 191, 36, 0.2) 0%, rgba(245, 158, 11, 0.15) 100%);
    border: 1px solid rgba(251, 191, 36, 0.4);
    border-radius: 8px;
    font-size: 13px;
    font-weight: 600;
    color: #fbbf24;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .btn-warning-fork:hover {
    background: linear-gradient(135deg, rgba(251, 191, 36, 0.3) 0%, rgba(245, 158, 11, 0.25) 100%);
    box-shadow: 0 0 15px rgba(251, 191, 36, 0.2);
  }
</style>
