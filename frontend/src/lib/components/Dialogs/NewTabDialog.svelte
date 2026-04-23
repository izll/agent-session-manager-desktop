<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { selectedSessionId, loadSessions } from '../../stores/sessions';
  import { agents } from '../../stores/agents';
  import { get } from 'svelte/store';
  import * as App from '../../../../wailsjs/go/main/App';
  import AgentIcon from '../common/AgentIcon.svelte';
  import { t } from '../../i18n';

  export let show = false;

  const dispatch = createEventDispatcher();

  let tabType: 'agent' | 'terminal' = 'agent';
  let selectedAgent = 'claude';
  let name = '';
  let extraArgs = '';
  let isSubmitting = false;
  let error = '';
  let userTouchedName = false;

  // Auto-fill name based on tab type / agent (only if user hasn't edited it)
  $: if (show && !userTouchedName) {
    name = tabType === 'terminal' ? 'Terminal' : `${selectedAgent} tab`;
  }

  function close() {
    show = false;
    resetForm();
    dispatch('close');
  }

  function resetForm() {
    tabType = 'agent';
    selectedAgent = 'claude';
    name = '';
    extraArgs = '';
    error = '';
    userTouchedName = false;
  }

  async function handleSubmit() {
    if (!name.trim()) {
      error = $t('newTab.nameRequired');
      return;
    }

    const sessionId = get(selectedSessionId);
    if (!sessionId) {
      error = $t('newTab.noSession');
      return;
    }

    isSubmitting = true;
    error = '';

    try {
      const isAgent = tabType === 'agent';
      const agent = isAgent ? selectedAgent : 'terminal';
      await App.CreateTab(sessionId, isAgent, agent, name.trim(), extraArgs.trim());
      await loadSessions();
      close();
      dispatch('created', { name: name.trim(), type: tabType, agent });
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

  function selectAgent(agent: string) {
    selectedAgent = agent;
    name = `${agent} tab`;
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
    font-size: 12px;
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
    background: linear-gradient(135deg, rgba(139, 92, 246, 0.2) 0%, rgba(99, 102, 241, 0.15) 100%);
    border-color: rgba(139, 92, 246, 0.5);
    box-shadow: 0 0 20px rgba(139, 92, 246, 0.15);
    color: #a78bfa;
  }

  .type-title {
    font-size: 13px;
    font-weight: 600;
  }

  .type-desc {
    font-size: 11px;
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
    font-size: 11px;
  }

  .agent-btn:hover {
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(255, 255, 255, 0.15);
  }

  .agent-btn.selected {
    background: rgba(139, 92, 246, 0.15);
    border-color: rgba(139, 92, 246, 0.4);
    color: #a78bfa;
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

  /* Component-specific: primary button with icon and flex layout */
  .btn-primary {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 24px;
    box-shadow: 0 4px 15px rgba(139, 92, 246, 0.3);
  }

  .btn-primary:hover:not(:disabled) {
    box-shadow: 0 6px 20px rgba(139, 92, 246, 0.4);
  }

  .spinner {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }
</style>
