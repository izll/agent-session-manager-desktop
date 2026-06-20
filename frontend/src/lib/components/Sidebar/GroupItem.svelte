<script lang="ts">
  import { createEventDispatcher, onMount, onDestroy, tick } from 'svelte';
  import SessionItem from './SessionItem.svelte';
  import type { Group, Session } from '../../stores/sessions';
  import { toggleGroupCollapse, renameGroup, deleteGroup, sessions as allSessions, assignToGroup } from '../../stores/sessions';
  import { activities, getActivity } from '../../stores/activities';
  import { statusLines, spinnerTexts, tabStatuses, getStatusLine } from '../../stores/statusLines';
  import { settings } from '../../stores/settings';
  import { t } from '../../i18n';

  export let group: Group;
  export let sessions: Session[] = [];

  const dispatch = createEventDispatcher();

  let isDragOver = false;

  // Context menu state
  let showContextMenu = false;
  let contextMenuX = 0;
  let contextMenuY = 0;

  // Inline rename state
  let isRenaming = false;
  let renameValue = '';
  let renameInput: HTMLInputElement;

  function handleToggle() {
    toggleGroupCollapse(group.id);
  }

  function handleContextMenu(e: MouseEvent) {
    e.preventDefault();
    e.stopPropagation();
    contextMenuX = e.clientX;
    contextMenuY = e.clientY;
    showContextMenu = true;
  }

  function closeContextMenu() {
    showContextMenu = false;
  }

  function handleWindowClick() {
    if (showContextMenu) {
      closeContextMenu();
    }
  }

  async function startRename() {
    closeContextMenu();
    renameValue = group.name;
    isRenaming = true;
    await tick();
    renameInput?.focus();
    renameInput?.select();
  }

  async function confirmRename() {
    const trimmed = renameValue.trim();
    if (trimmed && trimmed !== group.name) {
      await renameGroup(group.id, trimmed);
    }
    isRenaming = false;
  }

  function cancelRename() {
    isRenaming = false;
  }

  function handleRenameKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault();
      confirmRename();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      cancelRename();
    }
  }

  async function handleDeleteGroup() {
    closeContextMenu();
    await deleteGroup(group.id);
  }

  async function handleSessionDrop(e: CustomEvent<{ sourceId: string; targetIndex: number }>) {
    const { sourceId } = e.detail;
    const session = $allSessions.find(s => s.id === sourceId);

    // If session is from a different group, assign to this group
    if (session && session.groupId !== group.id) {
      await assignToGroup(sourceId, group.id);
    } else {
      // Same group - just reorder
      dispatch('sessionDrop', e.detail);
    }
  }

  function handleDragOver(e: DragEvent) {
    e.preventDefault();
    if (e.dataTransfer) {
      e.dataTransfer.dropEffect = 'move';
    }
    isDragOver = true;
  }

  function handleDragLeave() {
    isDragOver = false;
  }

  async function handleDrop(e: DragEvent) {
    e.preventDefault();
    e.stopPropagation();
    isDragOver = false;
    if (!e.dataTransfer) return;

    try {
      const data = JSON.parse(e.dataTransfer.getData('text/plain'));
      if (data.id) {
        const session = $allSessions.find(s => s.id === data.id);
        if (session && session.groupId !== group.id) {
          await assignToGroup(data.id, group.id);
        }
      }
    } catch {
      // Invalid drop data
    }
  }

  onMount(() => {
    window.addEventListener('click', handleWindowClick);
  });

  onDestroy(() => {
    window.removeEventListener('click', handleWindowClick);
  });
</script>

<div class="group-container" class:compact={$settings?.compactList}>
  <button
    class="group-header"
    class:drag-over={isDragOver}
    on:click={handleToggle}
    on:contextmenu={handleContextMenu}
    on:dragover={handleDragOver}
    on:dragleave={handleDragLeave}
    on:drop={handleDrop}
  >
    <span class="chevron" class:expanded={!group.collapsed}>
      <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor">
        <path d="M9 18l6-6-6-6"/>
      </svg>
    </span>

    <span class="folder-icon">
      {#if group.collapsed}
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2z"/>
        </svg>
      {:else}
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2v1"/>
          <path d="M2 10h20"/>
        </svg>
      {/if}
    </span>

    {#if isRenaming}
      <!-- svelte-ignore a11y-autofocus -->
      <input
        class="rename-input"
        type="text"
        bind:this={renameInput}
        bind:value={renameValue}
        on:keydown={handleRenameKeydown}
        on:blur={confirmRename}
        on:click|stopPropagation
      />
    {:else}
      <span class="group-name" style={group.color ? `color: ${group.color}` : ''}>
        {group.name}
      </span>
    {/if}

    <span class="session-count">
      {sessions.length}
    </span>
  </button>

  {#if showContextMenu}
    <div
      class="context-menu"
      style="left: {contextMenuX}px; top: {contextMenuY}px"
      on:click|stopPropagation
    >
      <button class="context-menu-item" on:click={startRename}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/>
          <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
        </svg>
        {$t('group.rename')}
      </button>
      <button class="context-menu-item danger" on:click={handleDeleteGroup}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <polyline points="3 6 5 6 21 6"/>
          <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
        </svg>
        {$t('group.delete')}
      </button>
    </div>
  {/if}

  {#if !group.collapsed}
    <div
      class="group-content"
      class:drag-over={isDragOver}
      on:dragover={handleDragOver}
      on:dragleave={handleDragLeave}
      on:drop={handleDrop}
    >
      {#each sessions as session (session.id)}
        <SessionItem {session} index={$allSessions.findIndex(s => s.id === session.id)} activity={getActivity(session.id, $activities)} statusLine={getStatusLine(session.id, $statusLines)} spinnerText={$spinnerTexts[session.id] || ''} tabStatuses={$tabStatuses[session.id] || []} on:drop={handleSessionDrop} />
      {/each}

      {#if sessions.length === 0}
        <div class="empty-group">
          {$t('group.noSessions')}
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .group-container {
    margin-bottom: 8px;
  }

  .group-header {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    background: rgba(255, 255, 255, 0.02);
    border: 1px solid rgba(255, 255, 255, 0.05);
    border-radius: 8px;
    cursor: pointer;
    transition: all 0.15s ease;
    text-align: left;
  }

  .group-header:hover {
    background: rgba(139, 92, 246, 0.08);
    border-color: rgba(139, 92, 246, 0.15);
  }

  .group-header.drag-over {
    background: rgba(139, 92, 246, 0.2);
    border-color: rgba(139, 92, 246, 0.5);
    box-shadow: 0 0 0 2px rgba(139, 92, 246, 0.2), inset 0 0 20px rgba(139, 92, 246, 0.1);
  }

  .chevron {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    color: #a1a1aa;
    transition: transform 0.2s ease;
  }

  .chevron.expanded {
    transform: rotate(90deg);
  }

  .folder-icon {
    display: flex;
    align-items: center;
    color: #fbbf24;
    filter: drop-shadow(0 0 4px rgba(251, 191, 36, 0.3));
  }

  .group-name {
    flex: 1;
    font-size: 13px;
    font-weight: 600;
    color: #e4e4e7;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .rename-input {
    flex: 1;
    font-size: 13px;
    font-weight: 600;
    color: #e4e4e7;
    background: rgba(139, 92, 246, 0.15);
    border: 1px solid rgba(139, 92, 246, 0.4);
    border-radius: 4px;
    padding: 2px 6px;
    outline: none;
    min-width: 0;
  }

  .rename-input:focus {
    border-color: rgba(139, 92, 246, 0.7);
    box-shadow: 0 0 0 2px rgba(139, 92, 246, 0.2);
  }

  .session-count {
    font-size: 11px;
    font-weight: 500;
    color: #6b7280;
    background: rgba(107, 114, 128, 0.2);
    padding: 2px 8px;
    border-radius: 10px;
  }

  .group-content {
    margin-top: 4px;
    margin-left: 12px;
    padding-left: 12px;
    border-left: 1px solid rgba(139, 92, 246, 0.2);
    transition: all 0.15s ease;
  }

  .group-content.drag-over {
    background: rgba(139, 92, 246, 0.1);
    border-left-color: rgba(139, 92, 246, 0.5);
    border-radius: 0 8px 8px 0;
  }

  .empty-group {
    padding: 12px 16px;
    font-size: 12px;
    color: #6b7280;
    font-style: italic;
  }

  /* Context menu */
  .context-menu {
    position: fixed;
    z-index: 1000;
    min-width: 160px;
    background: #1a1a2e;
    border: 1px solid rgba(139, 92, 246, 0.3);
    border-radius: 8px;
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.5);
    padding: 4px;
  }

  .context-menu-item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 8px 12px;
    font-size: 13px;
    color: #e4e4e7;
    background: none;
    border: none;
    border-radius: 6px;
    cursor: pointer;
    transition: all 0.15s ease;
    text-align: left;
  }

  .context-menu-item:hover {
    background: rgba(139, 92, 246, 0.15);
  }

  .context-menu-item.danger {
    color: #f87171;
  }

  .context-menu-item.danger:hover {
    background: rgba(239, 68, 68, 0.15);
  }

  /* Compact mode */
  .group-container.compact .group-header {
    padding: 5px 10px;
    gap: 6px;
  }

  .group-container.compact .group-name {
    font-size: 12px;
  }

  .group-container.compact .session-count {
    font-size: 10px;
    padding: 1px 6px;
  }

  .group-container.compact .chevron {
    width: 18px;
    height: 18px;
  }

  .group-container.compact .chevron svg {
    width: 14px;
    height: 14px;
  }

  .group-container.compact .folder-icon svg {
    width: 13px;
    height: 13px;
  }

  .group-container.compact .group-content {
    margin-left: 8px;
    padding-left: 8px;
  }

  .group-container.compact {
    margin-bottom: 4px;
  }

  .group-container.compact .empty-group {
    padding: 6px 12px;
    font-size: 11px;
  }
</style>
