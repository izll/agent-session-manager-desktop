<script lang="ts">
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { createEventDispatcher } from 'svelte';
  import { selectedSessionId, loadSessions, selectWindow, sessions } from '../../stores/sessions';
  import { agents } from '../../stores/agents';
  import { get } from 'svelte/store';
  import * as App from '../../../../wailsjs/go/main/App';
  import AgentIcon from '../common/AgentIcon.svelte';
  import { t } from '../../i18n';
  import { activeProjectId } from '../../stores/projects';

  export let show = false;
  export let sessionId = '';

  const dispatch = createEventDispatcher();

  let tabType: 'agent' | 'terminal' = 'agent';
  let selectedAgent = 'claude';
  let name = '';
  let extraArgs = '';
  let workDir = '';
  let isSubmitting = false;
  let error = '';
  let userTouchedName = false;
  let operationGeneration = 0;
  let lastTargetKey = '';
  let targetProjectId = '';

  $: {
    const key = show ? sessionId : '';
    if (key !== lastTargetKey) {
      operationGeneration++;
      lastTargetKey = key;
      if (show) isSubmitting = false;
      if (show) targetProjectId = get(activeProjectId);
    }
  }

  $: if (show && targetProjectId && $activeProjectId !== targetProjectId) close();

  $: sessionPath = $sessions.find(s => s.id === sessionId)?.path || '';

  // Auto-fill name based on tab type / agent (only if user hasn't edited it)
  $: if (show && !userTouchedName) {
    name = tabType === 'terminal' ? 'Terminal' : `${selectedAgent} tab`;
  }

  async function browseWorkDir() {
    const generation = operationGeneration;
    const targetSessionId = sessionId;
    const initialWorkDir = workDir;
    try {
      const dir = await App.BrowseDirectory(initialWorkDir || sessionPath);
      if (dir && show && generation === operationGeneration && sessionId === targetSessionId &&
          workDir === initialWorkDir) workDir = dir;
    } catch { /* cancelled */ }
  }

  function close() {
    operationGeneration++;
    isSubmitting = false;
    show = false;
    resetForm();
    dispatch('close');
  }

  function resetForm() {
    tabType = 'agent';
    selectedAgent = 'claude';
    name = '';
    extraArgs = '';
    workDir = '';
    error = '';
    userTouchedName = false;
  }

  async function handleSubmit() {
    if (isSubmitting) return;
    if (!name.trim()) {
      error = $t('newTab.nameRequired');
      return;
    }

    const targetSessionId = sessionId;
    if (!targetSessionId) {
      error = $t('newTab.noSession');
      return;
    }

    const generation = operationGeneration;
    const submitted = {
      isAgent: tabType === 'agent',
      agent: tabType === 'agent' ? selectedAgent : 'terminal',
      name: name.trim(),
      extraArgs: extraArgs.trim(),
      workDir: workDir.trim(),
      type: tabType,
    };
    isSubmitting = true;
    error = '';

    try {
      const newIdx = await App.CreateTab(targetSessionId, submitted.isAgent, submitted.agent,
        submitted.name, submitted.extraArgs, submitted.workDir, targetProjectId);
      await loadSessions();
      if (!show || generation !== operationGeneration || sessionId !== targetSessionId ||
          targetProjectId !== get(activeProjectId)) return;
      close();
      // Switch to the freshly created tab and put the keyboard focus into
      // its terminal (the window-change triggers the pool to attach; the
      // focus event lands after the dialog teardown released focus).
      if (get(selectedSessionId) === targetSessionId && typeof newIdx === 'number' && newIdx >= 0) {
        selectWindow(newIdx);
        requestAnimationFrame(() =>
          window.dispatchEvent(new CustomEvent('terminal:focus')));
      }
      dispatch('created', { name: submitted.name, type: submitted.type, agent: submitted.agent });
    } catch (e) {
      if (!show || generation !== operationGeneration || sessionId !== targetSessionId ||
          targetProjectId !== get(activeProjectId)) return;
      error = String(e);
    } finally {
      if (generation === operationGeneration) isSubmitting = false;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      close();
    }
  }

  function selectAgent(agent: string) {
    selectedAgent = agent;
    name = `${agent} tab`;
  }
</script>

{#if show}
  <div
    class="dialog-overlay" use:autoFocusDialog
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2>{$t('newTab.title')}</h2>
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
        <!-- Tab Type -->
        <div class="form-group">
          <span class="form-label">{$t('newTab.tabType')}</span>
          <div class="type-grid">
            <button
              type="button"
              class="type-btn {tabType === 'agent' ? 'selected' : ''}"
              on:click={() => { tabType = 'agent'; name = `${selectedAgent} tab`; }}
            >
              <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <circle cx="12" cy="8" r="4"/>
                <path d="M20 21a8 8 0 10-16 0"/>
              </svg>
              <span class="type-title">{$t('newTab.agent')}</span>
              <span class="type-desc">{$t('newTab.agentDesc')}</span>
            </button>
            <button
              type="button"
              class="type-btn {tabType === 'terminal' ? 'selected' : ''}"
              on:click={() => { tabType = 'terminal'; name = 'Terminal'; }}
            >
              <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <polyline points="4 17 10 11 4 5"/>
                <line x1="12" y1="19" x2="20" y2="19"/>
              </svg>
              <span class="type-title">{$t('newTab.terminal')}</span>
              <span class="type-desc">{$t('newTab.terminalDesc')}</span>
            </button>
          </div>
        </div>

        <!-- Agent Selection (if agent type) -->
        {#if tabType === 'agent'}
          <div class="form-group">
            <span class="form-label">{$t('newTab.agentLabel')}</span>
            <div class="agent-grid">
              {#each $agents.filter(a => a.type !== 'terminal') as agent}
                <button
                  type="button"
                  class="agent-btn {selectedAgent === agent.type ? 'selected' : ''}"
                  on:click={() => selectAgent(agent.type)}
                >
                  <AgentIcon agent={agent.type} size="md" />
                  <span>{agent.name}</span>
                </button>
              {/each}
            </div>
          </div>
        {/if}

        <!-- Extra CLI Arguments (agent tab, not custom/terminal) -->
        {#if tabType === 'agent' && selectedAgent !== 'custom'}
          <div class="form-group">
            <label class="form-label" for="tab-extra-args">{$t('newTab.extraArgs')}</label>
            <input
              id="tab-extra-args"
              type="text"
              bind:value={extraArgs}
              placeholder={$t('newTab.extraArgsPlaceholder')}
              class="form-input"
            />
          </div>
        {/if}

        <!-- Working directory (optional; defaults to the session's path) -->
        <div class="form-group">
          <label class="form-label" for="tab-workdir">{$t('newTab.workDir')}</label>
          <div class="workdir-row">
            <input
              id="tab-workdir"
              type="text"
              bind:value={workDir}
              placeholder={sessionPath || $t('newTab.workDirPlaceholder')}
              class="form-input"
            />
            <button type="button" class="browse-btn" on:click={browseWorkDir}>{$t('newTab.browse')}</button>
          </div>
        </div>

        <!-- Name -->
        <div class="form-group">
          <label class="form-label" for="tab-name">{$t('newTab.tabName')}</label>
          <input
            id="tab-name"
            type="text"
            bind:value={name}
            on:input={() => userTouchedName = true}
            placeholder={$t('newTab.tabNamePlaceholder')}
            class="form-input"
          />
        </div>

        <!-- Actions -->
        <div class="dialog-actions">
          <button type="button" class="btn-cancel" on:click={close}>
            {$t('common.cancel')}
          </button>
          <button type="submit" class="btn-primary" disabled={isSubmitting}>
            {#if isSubmitting}
              <svg class="spinner" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83"/>
              </svg>
              {$t('newTab.creating')}
            {:else}
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <line x1="12" y1="5" x2="12" y2="19"/>
                <line x1="5" y1="12" x2="19" y2="12"/>
              </svg>
              {$t('newTab.create')}
            {/if}
          </button>
        </div>
      </form>
    </div>
  </div>
{/if}

<style>
  /* Component-specific: wider dialog for this component */
  .dialog-content {
    max-width: 480px;
  }

  /* Component-specific: error message with icon layout */
  .error-message {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 16px 24px;
  }

  form {
    padding: 24px;
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

  .type-grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 12px;
  }

  .type-btn {
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

  .type-btn:hover {
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(255, 255, 255, 0.15);
  }

  .type-btn.selected {
    background: linear-gradient(135deg, rgba(var(--accent-rgb), 0.2) 0%, rgba(99, 102, 241, 0.15) 100%);
    border-color: rgba(var(--accent-rgb), 0.5);
    box-shadow: 0 0 20px rgba(var(--accent-rgb), 0.15);
    color: var(--accent-light);
  }

  .type-title {
    font-size: 13px;
    font-weight: 600;
  }

  .type-desc {
    font-size: 12px;
    opacity: 0.7;
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
    border-radius: 10px;
    cursor: pointer;
    transition: all 0.2s ease;
    color: #9ca3af;
    font-size: 12px;
  }

  .agent-btn:hover {
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(255, 255, 255, 0.15);
  }

  .agent-btn.selected {
    background: rgba(var(--accent-rgb), 0.15);
    border-color: rgba(var(--accent-rgb), 0.4);
    color: var(--accent-light);
  }

  .workdir-row { display: flex; gap: 8px; }
  .workdir-row .form-input { flex: 1; min-width: 0; }
  .browse-btn {
    flex-shrink: 0;
    padding: 0 14px;
    border-radius: 8px;
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(255, 255, 255, 0.05);
    color: #d4d4d8;
    font-size: 13px;
    cursor: pointer;
  }
  .browse-btn:hover { border-color: rgba(var(--accent-rgb), 0.5); color: var(--accent-pale); }

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

  .dialog-actions {
    display: flex;
    justify-content: flex-end;
    gap: 12px;
    padding-top: 16px;
    border-top: 1px solid rgba(255, 255, 255, 0.05);
  }

  /* Component-specific: primary button with icon and flex layout */
  .btn-primary {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 24px;
    box-shadow: 0 4px 15px rgba(var(--accent-rgb), 0.3);
  }

  .btn-primary:hover:not(:disabled) {
    box-shadow: 0 6px 20px rgba(var(--accent-rgb), 0.4);
  }

  .spinner {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }
</style>
