<script lang="ts">
  import { onMount } from 'svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import { loadSessions } from '../../stores/sessions';
  import AgentIcon from '../common/AgentIcon.svelte';

  export let show = false;

  interface Project {
    id: string;
    name: string;
    isLocked: boolean;
  }

  interface SessionInfo {
    id: string;
    name: string;
    path: string;
    status: string;
    agent: string;
    color: string;
    favorite: boolean;
  }

  let projects: Project[] = [];
  let sessions: SessionInfo[] = [];
  let selectedProjectId = '';
  let selectedSessionIds: Set<string> = new Set();
  let currentProjectId = '';
  let isLoading = false;
  let isImporting = false;
  let error = '';
  let successMessage = '';

  $: if (show) {
    loadProjects();
  }

  async function loadProjects() {
    isLoading = true;
    error = '';
    successMessage = '';
    selectedSessionIds = new Set();
    sessions = [];
    selectedProjectId = '';

    try {
      const [projectList, currentId] = await Promise.all([
        App.GetProjects(),
        App.GetActiveProjectID()
      ]);
      currentProjectId = currentId;
      // Filter out current project
      projects = (projectList as Project[]).filter(p => p.id !== currentId);
    } catch (e) {
      error = String(e);
    } finally {
      isLoading = false;
    }
  }

  async function loadProjectSessions(projectId: string) {
    if (!projectId) {
      sessions = [];
      return;
    }

    isLoading = true;
    error = '';
    selectedSessionIds = new Set();

    try {
      sessions = await App.GetProjectSessions(projectId) as SessionInfo[];
    } catch (e) {
      error = String(e);
      sessions = [];
    } finally {
      isLoading = false;
    }
  }

  function handleProjectChange() {
    loadProjectSessions(selectedProjectId);
  }

  function toggleSession(id: string) {
    if (selectedSessionIds.has(id)) {
      selectedSessionIds.delete(id);
    } else {
      selectedSessionIds.add(id);
    }
    selectedSessionIds = selectedSessionIds; // trigger reactivity
  }

  function selectAll() {
    sessions.forEach(s => selectedSessionIds.add(s.id));
    selectedSessionIds = selectedSessionIds;
  }

  function selectNone() {
    selectedSessionIds.clear();
    selectedSessionIds = selectedSessionIds;
  }

  async function handleImport() {
    if (selectedSessionIds.size === 0) return;

    isImporting = true;
    error = '';

    try {
      const count = await App.ImportSessions(selectedProjectId, Array.from(selectedSessionIds));
      successMessage = `Successfully imported ${count} session${count !== 1 ? 's' : ''}!`;
      await loadSessions(); // Refresh session list
      selectedSessionIds = new Set();
    } catch (e) {
      error = String(e);
    } finally {
      isImporting = false;
    }
  }

  function close() {
    show = false;
    error = '';
    successMessage = '';
    selectedProjectId = '';
    sessions = [];
    selectedSessionIds = new Set();
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      close();
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
        <h2>Import Sessions</h2>
        <button class="close-btn" on:click={close}>
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
              <line x1="15" y1="9" x2="9" y2="15"/>
              <line x1="9" y1="9" x2="15" y2="15"/>
            </svg>
            <span>{error}</span>
          </div>
        {/if}

        {#if successMessage}
          <div class="success-message">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/>
              <polyline points="22 4 12 14.01 9 11.01"/>
            </svg>
            <span>{successMessage}</span>
          </div>
        {/if}

        <!-- Project Selection -->
        <div class="form-group">
          <label for="project-select">Source Project</label>
          <select
            id="project-select"
            bind:value={selectedProjectId}
            on:change={handleProjectChange}
            disabled={isLoading || isImporting}
          >
            <option value="">Select a project...</option>
            {#each projects as project}
              <option value={project.id}>
                {project.name} {project.isLocked ? '(in use)' : ''}
              </option>
            {/each}
          </select>
        </div>

        <!-- Sessions List -->
        {#if selectedProjectId}
          <div class="sessions-section">
            <div class="sessions-header">
              <span class="sessions-label">Sessions ({sessions.length})</span>
              <div class="sessions-actions">
                <button class="link-btn" on:click={selectAll} disabled={sessions.length === 0}>
                  Select All
                </button>
                <span class="separator">|</span>
                <button class="link-btn" on:click={selectNone} disabled={selectedSessionIds.size === 0}>
                  Select None
                </button>
              </div>
            </div>

            {#if isLoading}
              <div class="loading">
                <div class="spinner"></div>
                <span>Loading sessions...</span>
              </div>
            {:else if sessions.length === 0}
              <div class="empty-state">
                No sessions in this project
              </div>
            {:else}
              <div class="sessions-list">
                {#each sessions as session}
                  <label class="session-item" class:selected={selectedSessionIds.has(session.id)}>
                    <input
                      type="checkbox"
                      checked={selectedSessionIds.has(session.id)}
                      on:change={() => toggleSession(session.id)}
                    />
                    <AgentIcon agent={session.agent} size="sm" />
                    <span class="session-name" style={session.color ? `color: ${session.color}` : ''}>
                      {session.name}
                    </span>
                    {#if session.favorite}
                      <span class="favorite">★</span>
                    {/if}
                    <span class="session-path">{session.path}</span>
                  </label>
                {/each}
              </div>
            {/if}
          </div>
        {:else if projects.length === 0 && !isLoading}
          <div class="empty-state large">
            <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
              <path d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2z"/>
            </svg>
            <p>No other projects available</p>
            <span class="hint">Create more projects to import sessions between them</span>
          </div>
        {/if}
      </div>

      <div class="dialog-footer">
        <span class="selected-count">
          {selectedSessionIds.size} session{selectedSessionIds.size !== 1 ? 's' : ''} selected
        </span>
        <div class="footer-buttons">
          <button
            class="btn btn-primary"
            on:click={handleImport}
            disabled={selectedSessionIds.size === 0 || isImporting}
          >
            {#if isImporting}
              <div class="spinner small"></div>
              Importing...
            {:else}
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
                <polyline points="17 8 12 3 7 8"/>
                <line x1="12" y1="3" x2="12" y2="15"/>
              </svg>
              Import Selected
            {/if}
          </button>
          <button class="btn-cancel" on:click={close}>
            Cancel
          </button>
        </div>
      </div>
    </div>
  </div>
{/if}

<style>
  /* Component-specific overrides */
  .dialog-content {
    width: 550px;
    max-width: 90vw;
    max-height: 80vh;
    display: flex;
    flex-direction: column;
  }

  .dialog-header {
    flex-shrink: 0;
  }

  .dialog-body {
    flex: 1;
    overflow-y: auto;
  }

  .dialog-footer {
    justify-content: space-between;
    align-items: center;
    flex-shrink: 0;
  }

  /* Success message (component-specific) */
  .success-message {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px 16px;
    border-radius: 8px;
    margin-bottom: 16px;
    font-size: 14px;
    background: rgba(34, 197, 94, 0.1);
    border: 1px solid rgba(34, 197, 94, 0.2);
    color: #4ade80;
  }

  /* Error message override for margin */
  .error-message {
    margin-bottom: 16px;
  }

  /* Select styling */
  select {
    width: 100%;
    padding: 12px 16px;
    background: rgba(0, 0, 0, 0.3);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 8px;
    font-size: 14px;
    color: #e4e4e7;
    cursor: pointer;
    outline: none;
    transition: all 0.2s ease;
  }

  select:focus {
    border-color: rgba(139, 92, 246, 0.5);
    box-shadow: 0 0 0 2px rgba(139, 92, 246, 0.1);
  }

  select:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  /* Sessions section */
  .sessions-section {
    margin-top: 8px;
  }

  .sessions-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 12px;
  }

  .sessions-label {
    font-size: 13px;
    font-weight: 500;
    color: #9ca3af;
  }

  .sessions-actions {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .link-btn {
    background: none;
    border: none;
    color: #a78bfa;
    font-size: 12px;
    cursor: pointer;
    padding: 0;
  }

  .link-btn:hover:not(:disabled) {
    text-decoration: underline;
  }

  .link-btn:disabled {
    color: #6b7280;
    cursor: not-allowed;
  }

  .separator {
    color: #4b5563;
  }

  .sessions-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
    max-height: 300px;
    overflow-y: auto;
  }

  .session-item {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    background: rgba(255, 255, 255, 0.02);
    border: 1px solid rgba(255, 255, 255, 0.05);
    border-radius: 8px;
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .session-item:hover {
    background: rgba(255, 255, 255, 0.05);
    border-color: rgba(255, 255, 255, 0.1);
  }

  .session-item.selected {
    background: rgba(139, 92, 246, 0.1);
    border-color: rgba(139, 92, 246, 0.3);
  }

  .session-item input[type="checkbox"] {
    width: 16px;
    height: 16px;
    accent-color: #8b5cf6;
  }

  .session-name {
    font-size: 13px;
    font-weight: 500;
    color: #e4e4e7;
    flex: 1;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .favorite {
    color: #fbbf24;
    font-size: 12px;
  }

  .session-path {
    font-size: 11px;
    color: #6b7280;
    max-width: 150px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .loading, .empty-state {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 12px;
    padding: 40px 20px;
    color: #6b7280;
    font-size: 14px;
  }

  .empty-state.large {
    flex-direction: column;
    gap: 16px;
    text-align: center;
  }

  .empty-state.large svg {
    opacity: 0.4;
  }

  .empty-state.large p {
    margin: 0;
    font-size: 16px;
    color: #9ca3af;
  }

  .empty-state .hint {
    font-size: 13px;
    color: #6b7280;
  }

  .selected-count {
    font-size: 13px;
    color: #9ca3af;
  }

  .footer-buttons {
    display: flex;
    gap: 12px;
  }

  /* Button styling for inline-flex with icons */
  .btn-primary {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }

  .spinner {
    width: 20px;
    height: 20px;
    border: 2px solid rgba(139, 92, 246, 0.2);
    border-top-color: #8b5cf6;
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  .spinner.small {
    width: 16px;
    height: 16px;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }
</style>
