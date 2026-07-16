<script lang="ts">
  import { projects, activeProjectId, selectProject, createProject } from '../../stores/projects';
  import { showDashboard } from '../../stores/navigation';
  import { t } from '../../i18n';

  let isOpen = false;
  let isCreating = false;
  let isSelecting = false;
  let newProjectName = '';

  $: currentProject = $projects.find(p => p.id === $activeProjectId);

  function toggle() {
    isOpen = !isOpen;
    if (!isOpen) {
      isCreating = false;
      newProjectName = '';
    }
  }

  async function handleSelect(id: string) {
    if (isSelecting) return;
    isSelecting = true;
    try {
      await selectProject(id);
      showDashboard();
      isOpen = false;
    } finally {
      isSelecting = false;
    }
  }

  function handleOpenDashboard() {
    isOpen = false;
    isCreating = false;
    newProjectName = '';
    showDashboard();
  }

  async function handleCreate() {
    if (!newProjectName.trim() || isSelecting) return;

    isSelecting = true;
    try {
      const project = await createProject(newProjectName.trim());
      if (project) {
        await selectProject(project.id);
        showDashboard();
      }
      newProjectName = '';
      isCreating = false;
      isOpen = false;
    } catch (e) {
      console.error('Failed to create project:', e);
    } finally {
      isSelecting = false;
    }
  }
</script>

<div class="project-selector">
  <button class="dashboard-button" on:click={handleOpenDashboard} title={$t('dashboard.open')}>
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
      <rect x="3" y="3" width="7" height="7" rx="1"/>
      <rect x="14" y="3" width="7" height="7" rx="1"/>
      <rect x="3" y="14" width="7" height="7" rx="1"/>
      <rect x="14" y="14" width="7" height="7" rx="1"/>
    </svg>
  </button>
  <button class="selector-button" on:click={toggle}>
    <div class="project-info">
      <svg class="project-icon" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M3 3h7l2 2h9a1 1 0 011 1v13a1 1 0 01-1 1H3a1 1 0 01-1-1V4a1 1 0 011-1z"/>
      </svg>
      <span class="project-name">{currentProject?.name || $t('project.default')}</span>
    </div>
    <svg class="chevron" class:open={isOpen} width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
      <polyline points="6 9 12 15 18 9"/>
    </svg>
  </button>

  {#if isOpen}
    <div class="dropdown">
      <!-- Default project -->
      <button
        class="dropdown-item"
        class:active={$activeProjectId === ''}
        disabled={isSelecting}
        on:click={() => handleSelect('')}
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M3 3h7l2 2h9a1 1 0 011 1v13a1 1 0 01-1 1H3a1 1 0 01-1-1V4a1 1 0 011-1z"/>
        </svg>
        <span>{$t('project.default')}</span>
      </button>

      <!-- Other projects -->
      {#each $projects as project (project.id)}
        <button
          class="dropdown-item"
          class:active={$activeProjectId === project.id}
          disabled={isSelecting}
          on:click={() => handleSelect(project.id)}
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M3 3h7l2 2h9a1 1 0 011 1v13a1 1 0 01-1 1H3a1 1 0 01-1-1V4a1 1 0 011-1z"/>
          </svg>
          <span>{project.name}</span>
          {#if project.isLocked}
            <span class="lock-icon" title={$t('project.inUse')}>
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <rect x="3" y="11" width="18" height="11" rx="2" ry="2"/>
                <path d="M7 11V7a5 5 0 0110 0v4"/>
              </svg>
            </span>
          {/if}
        </button>
      {/each}

      <div class="divider"></div>

      {#if isCreating}
        <div class="create-form">
          <input
            type="text"
            bind:value={newProjectName}
            placeholder={$t('project.namePlaceholder')}
            class="create-input"
            on:keydown={(e) => e.key === 'Enter' && handleCreate()}
          />
          <div class="create-actions">
            <button class="btn-primary" on:click={handleCreate}>
              {$t('project.create')}
            </button>
            <button class="btn-cancel" on:click={() => isCreating = false}>
              {$t('project.cancel')}
            </button>
          </div>
        </div>
      {:else}
        <button class="dropdown-item new-project" on:click={() => isCreating = true}>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="12" y1="5" x2="12" y2="19"/>
            <line x1="5" y1="12" x2="19" y2="12"/>
          </svg>
          <span>{$t('project.new')}</span>
        </button>
      {/if}
    </div>
  {/if}
</div>

<style>
  .project-selector {
    position: relative;
    display: flex;
    gap: 6px;
  }

  .dashboard-button {
    width: 38px;
    flex: 0 0 38px;
    display: flex;
    align-items: center;
    justify-content: center;
    color: #a78bfa;
    background: rgba(139, 92, 246, 0.08);
    border: 1px solid rgba(139, 92, 246, 0.18);
    border-radius: 8px;
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .dashboard-button:hover {
    color: #c4b5fd;
    background: rgba(139, 92, 246, 0.18);
    border-color: rgba(139, 92, 246, 0.4);
  }

  .selector-button {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 12px;
    background: rgba(0, 0, 0, 0.3);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 8px;
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .selector-button:hover {
    background: rgba(139, 92, 246, 0.1);
    border-color: rgba(139, 92, 246, 0.2);
  }

  .project-info {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .project-icon {
    color: #a78bfa;
  }

  .project-name {
    font-size: 13px;
    font-weight: 500;
    color: #e4e4e7;
  }

  .chevron {
    color: #6b7280;
    transition: transform 0.2s ease;
  }

  .chevron.open {
    transform: rotate(180deg);
  }

  .dropdown {
    position: absolute;
    top: calc(100% + 4px);
    left: 44px;
    right: 0;
    background: #1a1a2e;
    border: 1px solid rgba(139, 92, 246, 0.2);
    border-radius: 8px;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
    z-index: 50;
    overflow: hidden;
  }

  .dropdown-item {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 12px;
    font-size: 13px;
    color: #e4e4e7;
    background: transparent;
    border: none;
    cursor: pointer;
    transition: all 0.15s ease;
    text-align: left;
  }

  .dropdown-item:hover {
    background: rgba(139, 92, 246, 0.1);
  }

  .dropdown-item:disabled {
    opacity: 0.55;
    cursor: wait;
  }

  .dropdown-item.active {
    background: rgba(139, 92, 246, 0.2);
    color: #a78bfa;
  }

  .dropdown-item svg {
    color: #6b7280;
  }

  .dropdown-item.active svg {
    color: #a78bfa;
  }

  .dropdown-item.new-project {
    color: #a78bfa;
  }

  .dropdown-item.new-project svg {
    color: #a78bfa;
  }

  .lock-icon {
    margin-left: auto;
    color: #fbbf24;
  }

  .divider {
    height: 1px;
    background: rgba(255, 255, 255, 0.08);
    margin: 4px 0;
  }

  .create-form {
    padding: 12px;
  }

  .create-input {
    width: 100%;
    padding: 8px 10px;
    background: rgba(0, 0, 0, 0.3);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 6px;
    font-size: 13px;
    color: #e4e4e7;
    outline: none;
    transition: all 0.15s ease;
  }

  .create-input::placeholder {
    color: #6b7280;
  }

  .create-input:focus {
    border-color: rgba(139, 92, 246, 0.5);
    box-shadow: 0 0 0 2px rgba(139, 92, 246, 0.1);
  }

  .create-actions {
    display: flex;
    gap: 8px;
    margin-top: 8px;
  }

</style>
