<script lang="ts">
  import { createEventDispatcher, onMount, onDestroy, tick } from 'svelte';
  import StatusIndicator from '../common/StatusIndicator.svelte';
  import AgentIcon from '../common/AgentIcon.svelte';
  import type { Session } from '../../stores/sessions';
  import { selectSession, selectedSessionId, renameSession, deleteSession, toggleFavorite } from '../../stores/sessions';
  import { settings } from '../../stores/settings';
  import { t } from '../../i18n';
  import { focusTerminal } from '../../utils/focus';
  import type { TabStatusInfo } from '../../stores/statusLines';

  export let session: Session;
  export let activity: 'idle' | 'busy' | 'waiting' = 'idle';
  export let statusLine: string = '';
  export let spinnerText: string = '';
  export let tabStatuses: TabStatusInfo[] = [];
  export let index: number = 0;

  const dispatch = createEventDispatcher();

  // Context menu state
  let showContextMenu = false;
  let contextMenuX = 0;
  let contextMenuY = 0;

  // Inline rename state
  let isRenaming = false;
  let renameValue = '';
  let renameInput: HTMLInputElement;

  // Gradient definitions (same as SessionColorDialog)
  const gradients: Record<string, string[]> = {
    'gradient-rainbow':  ['#FF0000', '#FF7F00', '#FFFF00', '#00FF00', '#00FFFF', '#0000FF', '#8B00FF'],
    'gradient-sunset':   ['#FF512F', '#F09819', '#FF8C00', '#DD2476', '#FF416C'],
    'gradient-ocean':    ['#00D2FF', '#3A7BD5', '#00D2D3', '#54A0FF', '#2E86DE'],
    'gradient-forest':   ['#134E5E', '#11998E', '#38EF7D', '#A8E063', '#56AB2F'],
    'gradient-fire':     ['#FF0000', '#FF4500', '#FF6347', '#FF8C00', '#FFD700'],
    'gradient-ice':      ['#E0FFFF', '#B0E0E6', '#87CEEB', '#00CED1', '#4682B4'],
    'gradient-neon':     ['#FF00FF', '#00FFFF', '#39FF14', '#FF6600', '#BF00FF'],
    'gradient-galaxy':   ['#0F0C29', '#302B63', '#8E2DE2', '#4A00E0', '#24243E'],
    'gradient-pastel':   ['#FFB6C1', '#FFDAB9', '#FFFACD', '#98FB98', '#ADD8E6', '#E6E6FA'],
    'gradient-pink':     ['#FF69B4', '#FF1493', '#DB7093', '#FF69B4'],
    'gradient-blue':     ['#00BFFF', '#1E90FF', '#4169E1', '#0000FF', '#4169E1', '#1E90FF'],
    'gradient-green':    ['#00FF00', '#32CD32', '#228B22', '#006400', '#228B22', '#32CD32'],
    'gradient-gold':     ['#FFD700', '#FFA500', '#FF8C00', '#FFA500', '#FFD700'],
    'gradient-purple':   ['#9400D3', '#8A2BE2', '#9932CC', '#BA55D3', '#9932CC', '#8A2BE2'],
    'gradient-cyber':    ['#00FF00', '#00FFFF', '#FF00FF', '#00FFFF', '#00FF00'],
  };

  function getGradientCSS(colorValue: string): string {
    if (colorValue?.startsWith('gradient-')) {
      const colors = gradients[colorValue];
      if (colors) {
        return `linear-gradient(90deg, ${colors.join(', ')})`;
      }
    }
    return colorValue;
  }

  $: isSelected = $selectedSessionId === session.id;
  $: sessionStatus = session.status as 'running' | 'paused' | 'stopped';
  $: isGradient = session.color?.startsWith('gradient-');
  $: displayColor = isGradient ? getGradientCSS(session.color) : session.color;

  // Unique agent types for multi-agent sessions (only when 2+ different agents)
  $: uniqueAgents = (() => {
    if (tabStatuses.length <= 1) return [];
    const agents = new Set<string>();
    for (const tab of tabStatuses) {
      if (tab.agent) agents.add(tab.agent);
    }
    return agents.size > 1 ? Array.from(agents) : [];
  })();

  let isDragging = false;
  let isDragOver = false;

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
    renameValue = session.name;
    isRenaming = true;
    await tick();
    renameInput?.focus();
    renameInput?.select();
  }

  async function confirmRename() {
    const trimmed = renameValue.trim();
    if (trimmed && trimmed !== session.name) {
      await renameSession(session.id, trimmed);
    }
    isRenaming = false;
    focusTerminal();
  }

  function cancelRename() {
    isRenaming = false;
    focusTerminal();
  }

  function handleRenameKeydown(e: KeyboardEvent) {
    e.stopPropagation();
    if (e.key === 'Enter') {
      e.preventDefault();
      confirmRename();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      cancelRename();
    }
  }

  async function handleDelete() {
    closeContextMenu();
    await deleteSession(session.id);
  }

  async function handleToggleFavorite() {
    closeContextMenu();
    await toggleFavorite(session.id);
  }

  function handleDragStart(e: DragEvent) {
    if (!e.dataTransfer) return;
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', JSON.stringify({ id: session.id, index }));
    isDragging = true;
  }

  function handleDragEnd() {
    isDragging = false;
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

  function handleDrop(e: DragEvent) {
    e.preventDefault();
    isDragOver = false;
    if (!e.dataTransfer) return;

    try {
      const data = JSON.parse(e.dataTransfer.getData('text/plain'));
      if (data.id && data.id !== session.id) {
        dispatch('drop', { sourceId: data.id, targetIndex: index });
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

<div
  class="session-item"
  class:selected={isSelected}
  class:running={sessionStatus === 'running'}
  class:dragging={isDragging}
  class:drag-over={isDragOver}
  class:compact={$settings?.compactList}
  on:click={() => selectSession(session.id)}
  on:contextmenu={handleContextMenu}
  on:keydown={(e) => e.key === 'Enter' && selectSession(session.id)}
  draggable="true"
  on:dragstart={handleDragStart}
  on:dragend={handleDragEnd}
  on:dragover={handleDragOver}
  on:dragleave={handleDragLeave}
  on:drop={handleDrop}
  role="button"
  tabindex="0"
>
  <div class="session-main">
    <StatusIndicator
      status={sessionStatus}
      {activity}
      size="sm"
    />

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
      <span class="session-name">
        {#if isGradient}
          <span style="background: {displayColor}; -webkit-background-clip: text; -webkit-text-fill-color: transparent; background-clip: text; font-weight: {isSelected ? 800 : 600};">{session.name}</span>
        {:else}
          <span style={session.color ? `color: ${displayColor}` : ''}>{session.name}</span>
        {/if}
      </span>
    {/if}

    {#if $settings?.showAgentIcons && !(!$settings?.hideStatusLines && sessionStatus === 'running')}
      <div class="multi-agent-icons">
        {#if uniqueAgents.length > 0}
          {#each uniqueAgents as agent}
            <AgentIcon {agent} size="xs" />
          {/each}
        {:else}
          <AgentIcon agent={session.agent} size="xs" />
        {/if}
      </div>
    {/if}

    <div class="badges">
      {#if session.resumeSessionId}
        <span class="badge resume" title={$t('sessionItem.resumed')}>&#8635;</span>
      {/if}
      {#if session.favorite}
        <span class="badge favorite">&#9733;</span>
      {/if}
      {#if session.autoYes}
        <span class="badge yolo">Y</span>
      {/if}
    </div>
  </div>

  {#if !$settings?.hideStatusLines && sessionStatus === 'running'}
    {#if tabStatuses.length > 1}
      <!-- Multi-tab: show per-tab status lines -->
      {#each tabStatuses as tab}
        {#if tab.activity === 'busy'}
          <div class="status-text busy tab-status">
            <span>{tab.spinnerText || tab.statusLine || ''}</span>
            {#if $settings?.showAgentIcons}<AgentIcon agent={tab.agent} size="xs" />{/if}
          </div>
        {:else if tab.activity === 'waiting'}
          <div class="status-text waiting tab-status">
            <span>{$t('sessionItem.waitingInput')}</span>
            {#if $settings?.showAgentIcons}<AgentIcon agent={tab.agent} size="xs" />{/if}
          </div>
        {:else if tab.statusLine}
          <div class="status-text tab-status">
            <span>{tab.statusLine}</span>
            {#if $settings?.showAgentIcons}<AgentIcon agent={tab.agent} size="xs" />{/if}
          </div>
        {/if}
      {/each}
    {:else}
      <!-- Single tab: original behavior -->
      {#if activity === 'busy'}
        <div class="status-text busy tab-status">
          <span>{spinnerText || statusLine || ''}</span>
          {#if $settings?.showAgentIcons}<AgentIcon agent={session.agent} size="xs" />{/if}
        </div>
      {:else if activity === 'waiting'}
        <div class="status-text waiting tab-status">
          <span>{$t('sessionItem.waitingInput')}</span>
          {#if $settings?.showAgentIcons}<AgentIcon agent={session.agent} size="xs" />{/if}
        </div>
      {:else if statusLine}
        <div class="status-text tab-status">
          <span>{statusLine}</span>
          {#if $settings?.showAgentIcons}<AgentIcon agent={session.agent} size="xs" />{/if}
        </div>
      {/if}
    {/if}
  {/if}
</div>

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
      Rename
    </button>
    <button class="context-menu-item" on:click={handleToggleFavorite}>
      <svg width="14" height="14" viewBox="0 0 24 24" fill={session.favorite ? 'currentColor' : 'none'} stroke="currentColor" stroke-width="2">
        <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/>
      </svg>
      {session.favorite ? 'Unfavorite' : 'Favorite'}
    </button>
    <button class="context-menu-item danger" on:click={handleDelete}>
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <polyline points="3 6 5 6 21 6"/>
        <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
      </svg>
      Delete
    </button>
  </div>
{/if}

<style>
  /* ORIGINAL SESSION ITEM STYLES (for restoring later):
  .session-item {
    background: rgba(255, 255, 255, 0.02);
    border: 1px solid rgba(255, 255, 255, 0.04);
  }
  .session-item:hover {
    background: rgba(255, 255, 255, 0.05);
    border-color: rgba(255, 255, 255, 0.08);
  }
  .session-item.selected {
    background: rgba(139, 92, 246, 0.12);
    border-color: rgba(139, 92, 246, 0.3);
    box-shadow: 0 0 0 1px rgba(139, 92, 246, 0.1), 0 4px 12px rgba(0, 0, 0, 0.2);
  }
  .session-item.selected:hover {
    background: rgba(139, 92, 246, 0.15);
  }
  .session-item.running {
    background: rgba(255, 255, 255, 0.03);
  }
  */

  .session-item {
    padding: 10px 12px;
    margin: 4px 0;
    border-radius: 10px;
    cursor: pointer;
    transition: all 0.2s ease;
    background: rgba(255, 255, 255, 0.01);
    border: 1px solid rgba(255, 255, 255, 0.02);
    position: relative;
  }

  .session-item:hover {
    background: rgba(255, 255, 255, 0.025);
    border-color: rgba(255, 255, 255, 0.04);
    transform: translateX(2px);
  }

  .session-item.selected {
    background: rgba(139, 92, 246, 0.15);
    border-color: rgba(139, 92, 246, 0.4);
    box-shadow:
      0 0 0 1px rgba(139, 92, 246, 0.15),
      0 4px 12px rgba(0, 0, 0, 0.2);
  }

  .session-item.selected:hover {
    background: rgba(139, 92, 246, 0.2);
  }

  /* Running indicator - subtle glow effect */
  .session-item.running:not(.selected) {
    background: rgba(255, 255, 255, 0.015);
  }

  .session-main {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .session-name {
    flex: 1;
    font-size: 13px;
    font-weight: 500;
    color: #d4d4d8;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    letter-spacing: 0.01em;
  }

  .rename-input {
    flex: 1;
    font-size: 13px;
    font-weight: 500;
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

  .session-item:hover .session-name span {
    color: #e4e4e7;
  }

  .session-item.selected .session-name span {
    color: #fff;
    font-weight: 600;
  }

  .multi-agent-icons {
    display: flex;
    align-items: center;
    gap: 3px;
    opacity: 0.7;
    flex-shrink: 0;
  }

  .badges {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .badge {
    font-size: 9px;
    font-weight: 700;
    padding: 2px 5px;
    border-radius: 4px;
    text-transform: uppercase;
    letter-spacing: 0.02em;
  }

  .badge.favorite {
    color: #fbbf24;
    background: transparent;
    text-shadow: 0 0 10px rgba(251, 191, 36, 0.8);
    font-size: 12px;
    padding: 0;
  }

  .badge.resume {
    color: #a78bfa;
    background: rgba(139, 92, 246, 0.1);
    border: 1px solid rgba(139, 92, 246, 0.25);
    font-size: 10px;
    padding: 0 4px;
  }

  .badge.yolo {
    color: #ff6b6b;
    background: rgba(255, 107, 107, 0.1);
    border: 1px solid rgba(255, 107, 107, 0.25);
    font-size: 8px;
  }

  .status-text {
    font-size: 10px;
    line-height: 14px;
    height: 14px;
    color: #888888;
    margin-top: 6px;
    margin-left: 18px;
    font-style: italic;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .status-text.tab-status {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-top: 3px;
  }

  .status-text.tab-status:first-child {
    margin-top: 6px;
  }

  .status-text.tab-status span {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .status-text.busy {
    color: #FFA500;
  }

  .status-text.waiting {
    color: #00CED1;
  }

  /* Stopped sessions are slightly faded */
  .session-item:not(.running) {
    opacity: 0.7;
  }

  .session-item:not(.running):hover {
    opacity: 0.85;
  }

  .session-item:not(.running).selected {
    opacity: 1;
  }

  /* Drag & Drop styles */
  .session-item.dragging {
    opacity: 0.5;
    transform: scale(0.98);
  }

  .session-item.drag-over {
    border-color: rgba(139, 92, 246, 0.6);
    background: rgba(139, 92, 246, 0.15);
    box-shadow: 0 0 0 2px rgba(139, 92, 246, 0.2);
  }

  .session-item.drag-over::before {
    content: '';
    position: absolute;
    top: -2px;
    left: 0;
    right: 0;
    height: 2px;
    background: #8b5cf6;
    border-radius: 1px;
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
  .session-item.compact {
    padding: 5px 10px;
    margin: 1px 0;
    border-radius: 6px;
  }

  .session-item.compact .session-main {
    gap: 6px;
  }

  .session-item.compact .session-name {
    font-size: 12px;
  }

  .session-item.compact .status-text {
    margin-top: 2px;
    margin-left: 14px;
    font-size: 9px;
    line-height: 12px;
    height: 12px;
  }

  .session-item.compact .status-text.tab-status {
    gap: 3px;
    margin-top: 1px;
  }

  .session-item.compact .status-text.tab-status:first-child {
    margin-top: 2px;
  }

  .session-item.compact .badge {
    font-size: 7px;
    padding: 1px 4px;
  }

  .session-item.compact .badge.favorite {
    font-size: 10px;
  }

  .session-item.compact .badge.resume {
    font-size: 8px;
    padding: 0 3px;
  }
</style>
