<script lang="ts">
  import { createEventDispatcher, onMount, onDestroy, tick } from 'svelte';
  import SessionItem from './SessionItem.svelte';
  import type { Group, Session } from '../../stores/sessions';
  import { toggleGroupCollapse, renameGroup, deleteGroup, moveGroup, sessions as allSessions, assignToGroup, startSession, stopSession } from '../../stores/sessions';
  import { activities, getActivity } from '../../stores/activities';
  import { statusLines, spinnerTexts, tabStatuses, getStatusLine } from '../../stores/statusLines';
  import { settings } from '../../stores/settings';
  import { t } from '../../i18n';
  import { portal } from '../../utils/portal';
  import { menuPosition } from '../../utils/menuPosition';
  import { claimMenu, releaseMenu } from '../../utils/openMenu';
  import SessionColorDialog from '../Dialogs/SessionColorDialog.svelte';
  import {
    getGradientCSS,
    getNameStyle,
    getRowBackgroundStyle,
    isGradient as isGradientColor,
  } from '../../utils/rowColors';

  export let group: Group;
  export let sessions: Session[] = [];
  export let index: number = 0;
  export let groupCount: number = 0;

  const dispatch = createEventDispatcher();

  // Groups are dragged with their own MIME type so a group drop can be told
  // apart from a session drop on the very same header. dataTransfer payloads
  // are unreadable during dragover (protected mode), so presence of the type
  // in `types` is what we branch on there.
  const GROUP_MIME = 'application/x-asmgr-group';

  let isDragOver = false;
  let isGroupDragOver = false;
  let isDragging = false;

  $: isFirst = index <= 0;
  $: isLast = index >= groupCount - 1;

  // Group colours are rendered exactly like a session row's, via the shared helpers.
  $: isGradient = isGradientColor(group.color);
  $: displayColor = getGradientCSS(group.color);
  $: headerStyle = getRowBackgroundStyle(group.bgColor, group.fullRowColor);
  $: nameStyle = getNameStyle(group.color, group.bgColor, group.fullRowColor);

  // Colour dialog state
  let showColorDialog = false;

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

  // Bulk ops: sequential so tmux/session startup doesn't stampede; errors on
  // one session don't stop the rest. Start resumes each session's saved
  // conversation (startSession(id) alone would wipe the resume ID).
  async function handleStartAll() {
    showContextMenu = false;
    for (const s of sessions) {
      if (s.status === 'running') continue;
      try { await startSession(s.id, s.resumeSessionId || undefined); } catch { /* keep going */ }
    }
  }

  async function handleStopAll() {
    showContextMenu = false;
    const running = sessions.filter(s => s.status === 'running');
    if (running.length === 0) return;
    if (!window.confirm($t('group.stopAllConfirm', { n: running.length, name: group.name }))) return;
    for (const s of running) {
      try { await stopSession(s.id); } catch { /* keep going */ }
    }
  }

  function handleContextMenu(e: MouseEvent) {
    e.preventDefault();
    e.stopPropagation();
    contextMenuX = e.clientX;
    contextMenuY = e.clientY;
    showContextMenu = true;
    // See SessionItem: contextmenu doesn't fire the click that closes menus.
    claimMenu(closeContextMenu);
  }

  function closeContextMenu() {
    showContextMenu = false;
    releaseMenu(closeContextMenu);
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

  function openColorDialog() {
    closeContextMenu();
    showColorDialog = true;
  }

  async function handleMoveUp() {
    closeContextMenu();
    if (isFirst) return;
    await moveGroup(group.id, index - 1);
  }

  async function handleMoveDown() {
    closeContextMenu();
    if (isLast) return;
    await moveGroup(group.id, index + 1);
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

  function handleGroupDragStart(e: DragEvent) {
    if (!e.dataTransfer) return;
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData(GROUP_MIME, JSON.stringify({ id: group.id, index }));
    isDragging = true;
  }

  function handleGroupDragEnd() {
    isDragging = false;
    isGroupDragOver = false;
  }

  // The header accepts both a dropped session (assign to this group) and a
  // dropped group (reorder), so every handler here branches on the drag type.
  function handleHeaderDragOver(e: DragEvent) {
    e.preventDefault();
    if (e.dataTransfer) {
      e.dataTransfer.dropEffect = 'move';
    }
    if (e.dataTransfer?.types.includes(GROUP_MIME)) {
      // Dropping a group on itself is a no-op; don't advertise it as a target.
      if (!isDragging) isGroupDragOver = true;
    } else {
      isDragOver = true;
    }
  }

  function handleHeaderDragLeave() {
    isDragOver = false;
    isGroupDragOver = false;
  }

  async function handleHeaderDrop(e: DragEvent) {
    e.preventDefault();
    e.stopPropagation();
    isDragOver = false;
    isGroupDragOver = false;
    if (!e.dataTransfer) return;

    const groupPayload = e.dataTransfer.getData(GROUP_MIME);
    if (groupPayload) {
      try {
        const data = JSON.parse(groupPayload);
        if (data.id && data.id !== group.id) {
          await moveGroup(data.id, index);
        }
      } catch {
        // Invalid drop data
      }
      return;
    }

    await handleDrop(e);
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
    class:group-drag-over={isGroupDragOver}
    class:dragging={isDragging}
    style={headerStyle}
    draggable="true"
    on:click={handleToggle}
    on:contextmenu={handleContextMenu}
    on:dragstart={handleGroupDragStart}
    on:dragend={handleGroupDragEnd}
    on:dragover={handleHeaderDragOver}
    on:dragleave={handleHeaderDragLeave}
    on:drop={handleHeaderDrop}
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
        <!-- lucide folder-open: the previous hand-made path ended mid-edge
             ("…2 2v1"), which rendered as a folder with its right side cut
             off. This is a complete open-folder outline. -->
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="m6 14 1.45-2.9A2 2 0 0 1 9.24 10H20a2 2 0 0 1 1.94 2.5l-1.55 6a2 2 0 0 1-1.94 1.5H4a2 2 0 0 1-2-2V5c0-1.1.9-2 2-2h3.93a2 2 0 0 1 1.66.9l.82 1.2a2 2 0 0 0 1.66.9H18a2 2 0 0 1 2 2v2"/>
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
      <span class="group-name">
        {#if isGradient}
          <span style="background: {displayColor}; -webkit-background-clip: text; -webkit-text-fill-color: transparent; background-clip: text;">{group.name}</span>
        {:else}
          <span style={nameStyle}>{group.name}</span>
        {/if}
      </span>
    {/if}

    <span class="session-count">
      {sessions.length}
    </span>
  </button>

  {#if showContextMenu}
    <div
      class="context-menu"
      use:portal
      use:menuPosition={{ x: contextMenuX, y: contextMenuY }}
      on:click|stopPropagation
    >
      <button class="context-menu-item" on:click={handleStartAll}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <polygon points="5 3 19 12 5 21 5 3"/>
        </svg>
        {$t('group.startAll')}
      </button>
      <button class="context-menu-item" on:click={handleStopAll}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <rect x="6" y="6" width="12" height="12" rx="1"/>
        </svg>
        {$t('group.stopAll')}
      </button>
      <button
        class="context-menu-item"
        class:disabled={isFirst}
        disabled={isFirst}
        on:click={handleMoveUp}
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <line x1="12" y1="19" x2="12" y2="5"/>
          <polyline points="5 12 12 5 19 12"/>
        </svg>
        {$t('group.moveUp')}
      </button>
      <button
        class="context-menu-item"
        class:disabled={isLast}
        disabled={isLast}
        on:click={handleMoveDown}
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <line x1="12" y1="5" x2="12" y2="19"/>
          <polyline points="19 12 12 19 5 12"/>
        </svg>
        {$t('group.moveDown')}
      </button>
      <button class="context-menu-item" on:click={startRename}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/>
          <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
        </svg>
        {$t('group.rename')}
      </button>
      <button class="context-menu-item" on:click={openColorDialog}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="13.5" cy="6.5" r=".5"/>
          <circle cx="17.5" cy="10.5" r=".5"/>
          <circle cx="8.5" cy="7.5" r=".5"/>
          <circle cx="6.5" cy="12.5" r=".5"/>
          <path d="M12 2C6.5 2 2 6.5 2 12s4.5 10 10 10c.926 0 1.648-.746 1.648-1.688 0-.437-.18-.835-.437-1.125-.29-.289-.438-.652-.438-1.125a1.64 1.64 0 0 1 1.668-1.668h1.996c3.051 0 5.555-2.503 5.555-5.554C21.965 6.012 17.461 2 12 2z"/>
        </svg>
        {$t('group.color')}
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

<SessionColorDialog bind:show={showColorDialog} {group} />

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
    background: rgba(var(--accent-rgb), 0.08);
    border-color: rgba(var(--accent-rgb), 0.15);
  }

  .group-header.drag-over {
    background: rgba(var(--accent-rgb), 0.2);
    border-color: rgba(var(--accent-rgb), 0.5);
    box-shadow: 0 0 0 2px rgba(var(--accent-rgb), 0.2), inset 0 0 20px rgba(var(--accent-rgb), 0.1);
  }

  .group-header.dragging {
    opacity: 0.5;
    transform: scale(0.98);
  }

  /* Reorder target: a solid bar on the edge the group will land on, kept
     visually distinct from the "drop a session in here" fill above. */
  .group-header.group-drag-over {
    position: relative;
    border-color: rgba(var(--accent-rgb), 0.5);
  }

  .group-header.group-drag-over::before {
    content: '';
    position: absolute;
    top: -2px;
    left: 0;
    right: 0;
    height: 3px;
    background: var(--accent);
    border-radius: 2px;
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
    background: rgba(var(--accent-rgb), 0.15);
    border: 1px solid rgba(var(--accent-rgb), 0.4);
    border-radius: 4px;
    padding: 2px 6px;
    outline: none;
    min-width: 0;
  }

  .rename-input:focus {
    border-color: rgba(var(--accent-rgb), 0.7);
    box-shadow: 0 0 0 2px rgba(var(--accent-rgb), 0.2);
  }

  .session-count {
    font-size: 12px;
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
    border-left: 1px solid rgba(var(--accent-rgb), 0.2);
    transition: all 0.15s ease;
  }

  .group-content.drag-over {
    background: rgba(var(--accent-rgb), 0.1);
    border-left-color: rgba(var(--accent-rgb), 0.5);
    border-radius: 0 8px 8px 0;
  }

  .empty-group {
    padding: 12px 16px;
    font-size: 13px;
    color: #6b7280;
    font-style: italic;
  }

  /* Context menu */
  .context-menu {
    position: fixed;
    z-index: 1000;
    min-width: 160px;
    background: #1a1a2e;
    border: 1px solid rgba(var(--accent-rgb), 0.3);
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
    background: rgba(var(--accent-rgb), 0.15);
  }

  /* Shown but inert at the ends of the list — hiding the entry made it hard
     to find when it mattered. */
  .context-menu-item.disabled {
    opacity: 0.4;
    cursor: default;
  }

  .context-menu-item.disabled:hover {
    background: none;
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
    font-size: 13px;
  }

  .group-container.compact .session-count {
    font-size: 11px;
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
    font-size: 12px;
  }
</style>
