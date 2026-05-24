<script lang="ts">
  import SessionItem from './SessionItem.svelte';
  import GroupItem from './GroupItem.svelte';
  import {
    sessions,
    favorites,
    groups,
    sessionsByGroup,
    ungroupedSessions,
    searchFilter,
    moveSessionToIndex,
    assignToGroup
  } from '../../stores/sessions';
  import { onDestroy } from 'svelte';
  import { activities, getActivity } from '../../stores/activities';
  import { statusLines, spinnerTexts, tabStatuses, getStatusLine } from '../../stores/statusLines';
  import { settings } from '../../stores/settings';
  import { t } from '../../i18n';

  export let onNewSession: () => void;
  export let onNewGroup: () => void;
  export let onCollapse: () => void;

  let ungroupedDragOver = false;
  let sessionListEl: HTMLDivElement;
  let autoScrollRAF: number | null = null;

  // Auto-scroll zone: covers top/bottom 40% of the list for large responsive zone
  // Speed increases as cursor gets closer to the edge
  const SCROLL_ZONE_RATIO = 0.4; // 40% of list height from each edge
  const SCROLL_SPEED_MIN = 2;
  const SCROLL_SPEED_MAX = 20;

  function handleListDragOver(e: DragEvent) {
    if (!sessionListEl) return;
    const rect = sessionListEl.getBoundingClientRect();
    const y = e.clientY - rect.top;
    const height = rect.height;
    const zone = Math.max(80, height * SCROLL_ZONE_RATIO);

    if (autoScrollRAF) cancelAnimationFrame(autoScrollRAF);

    if (y < zone) {
      // Near top - scroll up, speed increases toward edge
      const intensity = 1 - (y / zone);
      const speed = SCROLL_SPEED_MIN + (SCROLL_SPEED_MAX - SCROLL_SPEED_MIN) * (intensity * intensity);
      autoScrollStep(-speed);
    } else if (y > height - zone) {
      // Near bottom - scroll down
      const intensity = 1 - ((height - y) / zone);
      const speed = SCROLL_SPEED_MIN + (SCROLL_SPEED_MAX - SCROLL_SPEED_MIN) * (intensity * intensity);
      autoScrollStep(speed);
    } else {
      autoScrollRAF = null;
    }
  }

  function autoScrollStep(delta: number) {
    if (!sessionListEl) return;
    sessionListEl.scrollTop += delta;
    autoScrollRAF = requestAnimationFrame(() => autoScrollStep(delta));
  }

  function stopAutoScroll() {
    if (autoScrollRAF) {
      cancelAnimationFrame(autoScrollRAF);
      autoScrollRAF = null;
    }
  }

  // Allow mouse wheel scrolling during drag (browsers may block default scroll during DnD)
  let isDragging = false;

  function handleDragStartGlobal() {
    isDragging = true;
  }

  function handleDragEndGlobal() {
    isDragging = false;
    stopAutoScroll();
  }

  onDestroy(() => {
    stopAutoScroll();
  });

  async function handleSessionDrop(e: CustomEvent<{ sourceId: string; targetIndex: number }>) {
    const { sourceId, targetIndex } = e.detail;
    await moveSessionToIndex(sourceId, targetIndex);
  }

  function handleUngroupedDragOver(e: DragEvent) {
    e.preventDefault();
    if (e.dataTransfer) {
      e.dataTransfer.dropEffect = 'move';
    }
    ungroupedDragOver = true;
  }

  function handleUngroupedDragLeave() {
    ungroupedDragOver = false;
  }

  async function handleUngroupedDrop(e: DragEvent) {
    e.preventDefault();
    ungroupedDragOver = false;
    if (!e.dataTransfer) return;

    try {
      const data = JSON.parse(e.dataTransfer.getData('text/plain'));
      if (data.id) {
        const session = $sessions.find(s => s.id === data.id);
        if (session && session.groupId) {
          await assignToGroup(data.id, '');
        }
      }
    } catch {
      // Invalid drop data
    }
  }

  // Count sessions by status
  $: statusCounts = (() => {
    let busy = 0, waiting = 0, idle = 0, stopped = 0;
    for (const session of $sessions) {
      if (session.status !== 'running') {
        stopped++;
      } else {
        const activity = $activities[session.id];
        if (activity === 'busy') busy++;
        else if (activity === 'waiting') waiting++;
        else idle++;
      }
    }
    return { busy, waiting, idle, stopped };
  })();
</script>

<div class="session-tree" class:compact={$settings?.compactList}>
  <!-- Status Summary -->
  {#if $sessions.length > 0}
    <div class="status-summary">
      <span class="status-item busy" title={$t('sidebar.statusBusy')} class:dimmed={statusCounts.busy === 0}>
        <span class="status-dot"></span>
        <span class="status-count">{statusCounts.busy}</span>
      </span>
      <span class="status-item waiting" title={$t('sidebar.statusWaiting')} class:dimmed={statusCounts.waiting === 0}>
        <span class="status-dot"></span>
        <span class="status-count">{statusCounts.waiting}</span>
      </span>
      <span class="status-item idle" title={$t('sidebar.statusIdle')} class:dimmed={statusCounts.idle === 0}>
        <span class="status-dot"></span>
        <span class="status-count">{statusCounts.idle}</span>
      </span>
      <span class="status-item stopped" title={$t('sidebar.statusStopped')} class:dimmed={statusCounts.stopped === 0}>
        <span class="status-dot"></span>
        <span class="status-count">{statusCounts.stopped}</span>
      </span>
    </div>
  {/if}

  <!-- Search -->
  <div class="search-container">
    <div class="search-box">
      <svg class="search-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <circle cx="11" cy="11" r="8"/>
        <path d="M21 21l-4.35-4.35"/>
      </svg>
      <input
        type="text"
        placeholder={$t('sidebar.searchPlaceholder')}
        class="search-input"
        bind:value={$searchFilter}
      />
    </div>
  </div>

  <!-- Session List -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div class="session-list" bind:this={sessionListEl} on:dragover={handleListDragOver} on:dragstart={handleDragStartGlobal} on:dragend={handleDragEndGlobal} on:drop={handleDragEndGlobal}>
    <!-- Favorites -->
    {#if $favorites.length > 0}
      <div class="section">
        <div class="section-header favorites">
          <span class="star">★</span>
          {$t('sidebar.favorites')}
        </div>
        {#each $favorites as session, i (session.id)}
          <SessionItem {session} index={$sessions.findIndex(s => s.id === session.id)} activity={getActivity(session.id, $activities)} statusLine={getStatusLine(session.id, $statusLines)} spinnerText={$spinnerTexts[session.id] || ''} tabStatuses={$tabStatuses[session.id] || []} on:drop={handleSessionDrop} />
        {/each}
      </div>
    {/if}

    <!-- Sessions header (after favorites) -->
    {#if $groups.length > 0 || $ungroupedSessions.length > 0}
      <div
        class="section-header sessions-label"
        on:dragover={handleUngroupedDragOver}
        on:dragleave={handleUngroupedDragLeave}
        on:drop={handleUngroupedDrop}
      >
        {$t('sidebar.sessions')}
      </div>
    {/if}

    <!-- Groups.
         When a search filter is active, hide groups that have no matches —
         otherwise the one group that DOES have a hit gets buried among
         empty headers and is hard to find. Without a filter we still show
         every group (the user wants to see the structure). -->
    {#each $groups as group (group.id)}
      {@const groupSessions = $sessionsByGroup.get(group.id) || []}
      {#if !$searchFilter.trim() || groupSessions.length > 0}
        <GroupItem
          {group}
          sessions={groupSessions}
          on:sessionDrop={handleSessionDrop}
        />
      {/if}
    {/each}

    <!-- Ungrouped -->
    {#if $ungroupedSessions.length > 0}
      <div class="section">
        {#each $ungroupedSessions as session (session.id)}
          <SessionItem {session} index={$sessions.findIndex(s => s.id === session.id)} activity={getActivity(session.id, $activities)} statusLine={getStatusLine(session.id, $statusLines)} spinnerText={$spinnerTexts[session.id] || ''} tabStatuses={$tabStatuses[session.id] || []} on:drop={handleSessionDrop} />
        {/each}
      </div>
    {/if}

    <!-- Empty state -->
    {#if $favorites.length === 0 && $groups.length === 0 && $ungroupedSessions.length === 0}
      <div class="empty-state">
        <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
          <path d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z"/>
        </svg>
        <p>{$t('sidebar.noSessions')}</p>
        <button class="create-first-btn" on:click={onNewSession}>
          {$t('sidebar.createFirst')}
        </button>
      </div>
    {:else if $searchFilter.trim() && $favorites.length === 0 && $ungroupedSessions.length === 0 && Array.from($sessionsByGroup.values()).every((arr) => arr.length === 0)}
      <!-- Filter is active but nothing matches anywhere. Without this the
           sidebar looks blankly empty and the user can't tell whether the
           filter wiped everything out or the project is actually empty. -->
      <div class="no-matches">
        {$t('sidebar.noMatches')}
      </div>
    {/if}
  </div>

  <!-- Footer Buttons -->
  <div class="footer">
    <div class="footer-buttons">
      <button class="new-btn session" on:click={onNewSession} title={$t('sidebar.newSession')}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
          <line x1="12" y1="5" x2="12" y2="19"/>
          <line x1="5" y1="12" x2="19" y2="12"/>
        </svg>
        {$t('sidebar.session')}
      </button>
      <button class="new-btn group" on:click={onNewGroup} title={$t('sidebar.newGroup')}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2z"/>
        </svg>
        {$t('sidebar.group')}
      </button>
      <button class="collapse-btn" on:click={onCollapse} title={$t('sidebar.collapseSidebar')}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <polyline points="15 18 9 12 15 6"/>
        </svg>
      </button>
    </div>
  </div>
</div>

<style>
  .session-tree {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: transparent;
  }

  .status-summary {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 12px;
    padding: 8px 12px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  }

  .status-item {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    font-weight: 500;
  }

  .status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
  }

  .status-item.busy .status-dot {
    background: #FFA500;
    box-shadow: 0 0 6px rgba(255, 165, 0, 0.6);
    animation: pulse 1.5s ease-in-out infinite;
  }

  .status-item.waiting .status-dot {
    background: #00CED1;
    box-shadow: 0 0 6px rgba(0, 206, 209, 0.6);
  }

  .status-item.idle .status-dot {
    background: #888888;
  }

  .status-item.stopped .status-dot {
    background: #FF5F87;
    box-shadow: 0 0 6px rgba(255, 95, 135, 0.4);
  }

  .status-count {
    color: #9ca3af;
  }

  .status-item.busy .status-count {
    color: #FFA500;
  }

  .status-item.waiting .status-count {
    color: #00CED1;
  }

  .status-item.stopped .status-count {
    color: #FF5F87;
  }

  @keyframes pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.5; }
  }

  .status-item.dimmed {
    opacity: 0.3;
  }

  .status-item.dimmed .status-dot {
    box-shadow: none;
    animation: none;
  }

  .search-container {
    padding: 12px;
  }

  .search-box {
    position: relative;
    display: flex;
    align-items: center;
  }

  .search-icon {
    position: absolute;
    left: 10px;
    color: #6b7280;
    pointer-events: none;
  }

  .search-input {
    width: 100%;
    padding: 8px 12px 8px 34px;
    background: rgba(0, 0, 0, 0.3);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 8px;
    font-size: 13px;
    color: #e4e4e7;
    outline: none;
    transition: all 0.2s ease;
  }

  .search-input::placeholder {
    color: #6b7280;
  }

  .search-input:focus {
    border-color: rgba(139, 92, 246, 0.5);
    background: rgba(139, 92, 246, 0.05);
    box-shadow: 0 0 0 2px rgba(139, 92, 246, 0.1);
  }

  .session-list {
    flex: 1;
    overflow-y: auto;
    padding: 0 8px;
  }

  .section {
    margin-bottom: 16px;
  }

  .section-header {
    padding: 8px 12px;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #6b7280;
  }

  .section-header.favorites {
    color: #fbbf24;
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .section-header .star {
    font-size: 12px;
    text-shadow: 0 0 6px rgba(251, 191, 36, 0.6);
  }

  .section-header.droppable {
    cursor: pointer;
    border-radius: 6px;
    margin: 0 4px;
    transition: all 0.15s ease;
  }

  .section-header.droppable:hover {
    background: rgba(255, 255, 255, 0.03);
  }

  .section-header.drag-over {
    background: rgba(139, 92, 246, 0.2);
    box-shadow: 0 0 0 2px rgba(139, 92, 246, 0.3), inset 0 0 15px rgba(139, 92, 246, 0.1);
  }

  .section-header.sessions-label {
    margin-top: 0;
    padding-top: 4px;
    margin-bottom: 4px;
  }

  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 40px 20px;
    color: #6b7280;
    text-align: center;
  }

  .empty-state svg {
    margin-bottom: 12px;
    opacity: 0.5;
  }

  .empty-state p {
    margin: 0 0 16px;
    font-size: 14px;
  }

  .no-matches {
    padding: 16px 12px;
    color: #6b7280;
    font-size: 12px;
    text-align: center;
    font-style: italic;
  }

  .create-first-btn {
    padding: 8px 16px;
    background: rgba(139, 92, 246, 0.15);
    border: 1px solid rgba(139, 92, 246, 0.3);
    border-radius: 8px;
    font-size: 13px;
    font-weight: 500;
    color: #a78bfa;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .create-first-btn:hover {
    background: rgba(139, 92, 246, 0.25);
    border-color: rgba(139, 92, 246, 0.5);
    box-shadow: 0 0 15px rgba(139, 92, 246, 0.2);
  }

  .footer {
    padding: 12px;
    border-top: 1px solid rgba(255, 255, 255, 0.05);
  }

  .footer-buttons {
    display: flex;
    gap: 8px;
  }

  .new-btn {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    padding: 10px 12px;
    border: none;
    border-radius: 8px;
    font-size: 12px;
    font-weight: 600;
    color: white;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .new-btn.session {
    background: linear-gradient(135deg, #8b5cf6 0%, #6366f1 100%);
    box-shadow: 0 2px 10px rgba(139, 92, 246, 0.3);
  }

  .new-btn.session:hover {
    transform: translateY(-1px);
    box-shadow: 0 4px 15px rgba(139, 92, 246, 0.4);
  }

  .new-btn.group {
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.1);
    color: #9ca3af;
  }

  .new-btn.group:hover {
    background: rgba(255, 255, 255, 0.1);
    border-color: rgba(255, 255, 255, 0.2);
    color: white;
  }

  .new-btn:active {
    transform: translateY(0);
  }

  .collapse-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 40px;
    flex-shrink: 0;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 8px;
    color: #6b7280;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .collapse-btn:hover {
    background: rgba(139, 92, 246, 0.15);
    border-color: rgba(139, 92, 246, 0.3);
    color: #a78bfa;
  }

  /* Compact mode */
  .session-tree.compact .search-container {
    padding: 8px 12px;
  }

  .session-tree.compact .search-input {
    padding: 5px 10px 5px 30px;
    font-size: 12px;
  }

  .session-tree.compact .status-summary {
    padding: 5px 12px;
    gap: 8px;
  }

  .session-tree.compact .status-item {
    font-size: 11px;
  }

  .session-tree.compact .status-dot {
    width: 6px;
    height: 6px;
  }

  .session-tree.compact .section {
    margin-bottom: 8px;
  }

  .session-tree.compact .section-header {
    padding: 4px 10px;
    font-size: 10px;
  }

  .session-tree.compact .footer {
    padding: 8px 12px;
  }

  .session-tree.compact .new-btn {
    padding: 7px 10px;
    font-size: 11px;
  }
</style>
