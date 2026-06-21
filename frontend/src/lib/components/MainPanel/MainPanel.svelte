<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import TabBar from './TabBar.svelte';
  import Terminal from './Terminal.svelte';
  import Notes from './Notes.svelte';
  import Diff from './Diff.svelte';
  import TaskPanel from './TaskPanel.svelte';
  import ForkDialog from '../Dialogs/ForkDialog.svelte';
  import { sessions, selectedSessionId, selectedWindowIdx, toggleAutoYes, cycleYoloMode } from '../../stores/sessions';
  import { agents } from '../../stores/agents';
  import { tabStatuses } from '../../stores/statusLines';
  import { get } from 'svelte/store';
  import { t } from '../../i18n';

  const dispatch = createEventDispatcher();

  let activeView: 'terminal' | 'diff' | 'notes' | 'tasks' = 'terminal';
  let terminalAttached = false;
  let showForkDialog = false;
  let localNotesCache: Record<string, string> = {}; // sessionId:windowIdx -> notes
  let diffMode: 'session' | 'full' = 'session';
  let terminalComponent: Terminal;
  let fullDiffActive = false;

  // Check if current session supports fork (Claude only)
  function getCurrentSession() {
    const id = get(selectedSessionId);
    if (!id) return null;
    return get(sessions).find(s => s.id === id) || null;
  }

  $: currentSession = $sessions.find(s => s.id === $selectedSessionId);
  $: canFork = currentSession?.agent === 'claude' && currentSession?.status === 'running';
  $: agentConfig = $agents.find(a => a.type === currentSession?.agent);
  $: canAutoYes = agentConfig?.supportsAutoYes && currentSession?.status === 'running';

  // Live YOLO (bypass-permissions) state for the CURRENTLY SELECTED tab, read
  // from the pane status bar. When the session RUNS we trust ONLY this live
  // value — never the stored launch flag — so a Shift+Tab toggle to auto mode
  // turns the indicator off even though the session was launched with --yolo.
  // When NOT running there's no pane to read, so fall back to the stored flag.
  $: liveYolo = (() => {
    if (currentSession?.status !== 'running') return !!currentSession?.autoYes;
    const list = $selectedSessionId ? $tabStatuses[$selectedSessionId] : undefined;
    const ts = list?.find(t => t.windowIdx === ($selectedWindowIdx ?? 0));
    return !!ts?.yolo; // running → live only (no stored-flag fallback)
  })();

  // Get current tab's resume session ID
  $: currentResumeId = (() => {
    if (!currentSession) return '';
    if ($selectedWindowIdx === 0) return currentSession.resumeSessionId || '';
    const fw = currentSession.followedWindows?.find(w => w.index === $selectedWindowIdx);
    return fw?.resumeSessionId || '';
  })();

  // Get current tab's notes (with local cache for immediate updates)
  $: currentTabNotes = (() => {
    if (!currentSession) return '';
    const cacheKey = `${currentSession.id}:${$selectedWindowIdx}`;
    if (localNotesCache[cacheKey] !== undefined) {
      return localNotesCache[cacheKey];
    }
    if ($selectedWindowIdx === 0) return currentSession.notes || '';
    const fw = currentSession.followedWindows?.find(w => w.index === $selectedWindowIdx);
    return fw?.notes || '';
  })();

  function handleNotesChange(e: CustomEvent<{ sessionId: string, windowIdx: number, notes: string }>) {
    const { sessionId, windowIdx, notes } = e.detail;
    localNotesCache[`${sessionId}:${windowIdx}`] = notes;
    localNotesCache = localNotesCache; // Trigger reactivity
  }

  // Truncate path for display
  function truncatePath(path: string, maxLen: number = 50): string {
    if (!path || path.length <= maxLen) return path;
    const parts = path.split('/');
    if (parts.length <= 3) return path;
    return '.../' + parts.slice(-3).join('/');
  }
</script>

<div class="main-panel h-full flex flex-col">
  {#if $selectedSessionId}
    <!-- Tab Bar - shows windows/tabs within a session -->
    <TabBar
      {fullDiffActive}
      {activeView}
      on:openColorDialog={() => dispatch('openColorDialog')}
      on:openFullDiff={() => { fullDiffActive = true; diffMode = 'full'; }}
      on:closeFullDiff={() => { fullDiffActive = false; activeView = 'terminal'; }}
      on:requestStop={() => dispatch('requestStop')}
      on:requestStart={() => dispatch('requestStart')}
      on:requestResume={() => dispatch('requestResume')}
      on:openSettings={() => dispatch('openSettings')}
    />

    {#if fullDiffActive}
      <!-- Full Diff View -->
      <div class="flex-1 overflow-hidden content-area">
        <Diff active={true} initialMode="full" />
      </div>
    {:else}
      <!-- View Selector -->
      <div class="view-tabs">
        <div class="view-tabs-left">
          <button
            class="view-tab {activeView === 'terminal' ? 'active' : ''}"
            on:click={() => activeView = 'terminal'}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polyline points="4 17 10 11 4 5"/>
              <line x1="12" y1="19" x2="20" y2="19"/>
            </svg>
            {$t('mainPanel.terminal')}
          </button>
          <button
            class="view-tab {activeView === 'diff' ? 'active' : ''}"
            on:click={() => { diffMode = 'session'; activeView = 'diff'; }}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M18 13v6a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2h6"/>
              <polyline points="15 3 21 3 21 9"/>
              <line x1="10" y1="14" x2="21" y2="3"/>
            </svg>
            {$t('mainPanel.diff')}
          </button>
          <button
            class="view-tab {activeView === 'notes' ? 'active' : ''}"
            on:click={() => activeView = 'notes'}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z"/>
              <polyline points="14 2 14 8 20 8"/>
              <line x1="16" y1="13" x2="8" y2="13"/>
              <line x1="16" y1="17" x2="8" y2="17"/>
            </svg>
            {$t('mainPanel.notes')}
          </button>
          <button
            class="view-tab {activeView === 'tasks' ? 'active' : ''}"
            on:click={() => activeView = 'tasks'}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M9 11l3 3L22 4"/>
              <path d="M21 12v7a2 2 0 01-2 2H5a2 2 0 01-2-2V5a2 2 0 012-2h11"/>
            </svg>
            {$t('mainPanel.tasks')}
          </button>
        </div>
        <div class="view-tabs-right">
          {#if canAutoYes}
            <button
              class="yolo-btn"
              class:active={liveYolo}
              on:click|stopPropagation={async () => {
                if (!currentSession) return;
                try {
                  // Cycle the live mode via Shift+Tab (no restart); the
                  // indicator updates from the pane on the next poll.
                  await cycleYoloMode(currentSession.id, $selectedWindowIdx ?? 0);
                } catch (e) {
                  console.error('YOLO cycle failed:', e);
                }
              }}
              title={liveYolo ? $t('mainPanel.yoloOn') : $t('mainPanel.yoloEnable')}
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/>
              </svg>
              {liveYolo ? $t('mainPanel.yoloLabel') + ' ⚡' : $t('mainPanel.yoloLabel')}
            </button>
          {/if}
          {#if canFork}
            <button class="fork-btn" on:click={() => showForkDialog = true} title={$t('mainPanel.forkTitle')}>
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <circle cx="12" cy="18" r="3"/>
                <circle cx="6" cy="6" r="3"/>
                <circle cx="18" cy="6" r="3"/>
                <path d="M6 9v3a3 3 0 003 3h6a3 3 0 003-3V9"/>
                <path d="M12 12v3"/>
              </svg>
              {$t('mainPanel.fork')}
            </button>
          {/if}
          {#if terminalAttached}
            <button class="terminal-btn detach" on:click={() => terminalComponent?.detach()} title={$t('mainPanel.detachTitle')}>
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M18 6L6 18M6 6l12 12"/>
              </svg>
              {$t('mainPanel.detach')}
            </button>
          {:else if currentSession?.status === 'running'}
            <button class="terminal-btn attach" on:click={() => terminalComponent?.attach()} title={$t('mainPanel.attachTitle')}>
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <polyline points="4 17 10 11 4 5"/>
                <line x1="12" y1="19" x2="20" y2="19"/>
              </svg>
              {$t('mainPanel.attach')}
            </button>
          {/if}
        </div>
      </div>

      <!-- Content Area - Keep components mounted, use CSS to show/hide -->
      <div class="flex-1 overflow-hidden content-area">
        <div class="view-panel" class:active={activeView === 'terminal'}>
          <Terminal bind:this={terminalComponent} bind:isAttached={terminalAttached} active={activeView === 'terminal'} />
        </div>
        <div class="view-panel" class:active={activeView === 'diff'}>
          <Diff active={activeView === 'diff'} initialMode={diffMode} />
        </div>
        <div class="view-panel" class:active={activeView === 'notes'}>
          <Notes active={activeView === 'notes'} on:notesChange={handleNotesChange} />
        </div>
        <div class="view-panel" class:active={activeView === 'tasks'}>
          <TaskPanel active={activeView === 'tasks'} on:taskSent={() => activeView = 'terminal'} />
        </div>
      </div>
    {/if}

    <!-- Status Bar -->
    <div class="status-bar">
      <div class="status-left">
        <!-- Path -->
        <div class="status-item" title={currentSession?.path}>
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2z"/>
          </svg>
          <span class="status-path">{truncatePath(currentSession?.path || '')}</span>
        </div>

        {#if currentResumeId}
          <span class="status-divider"></span>
          <!-- Session ID -->
          <div class="status-item" title="Session ID: {currentResumeId}">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="3" y="11" width="18" height="11" rx="2" ry="2"/>
              <path d="M7 11V7a5 5 0 0110 0v4"/>
            </svg>
            <span class="status-id">{currentResumeId.slice(0, 8)}...</span>
          </div>
        {/if}

        {#if currentTabNotes}
          <span class="status-divider"></span>
          <!-- Notes preview -->
          <div class="status-item notes-preview" title={currentTabNotes}>
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z"/>
              <polyline points="14 2 14 8 20 8"/>
            </svg>
            <span class="status-notes">{currentTabNotes.slice(0, 30)}{currentTabNotes.length > 30 ? '...' : ''}</span>
          </div>
        {/if}
      </div>

      <div class="status-right">
        <span class="agent-badge">{currentSession?.agent}</span>
      </div>
    </div>
  {:else}
    <!-- No Session Selected -->
    <div class="empty-panel">
      <div class="empty-content">
        <div class="empty-logo">
          <svg width="80" height="80" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
            <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.41 0-8-3.59-8-8s3.59-8 8-8 8 3.59 8 8-3.59 8-8 8z" fill="url(#emptyGrad)"/>
            <circle cx="8.5" cy="10.5" r="1.5" fill="url(#emptyGrad)"/>
            <circle cx="15.5" cy="10.5" r="1.5" fill="url(#emptyGrad)"/>
            <path d="M12 16c-1.48 0-2.75-.81-3.45-2h6.9c-.7 1.19-1.97 2-3.45 2z" fill="url(#emptyGrad)"/>
            <defs>
              <linearGradient id="emptyGrad" x1="2" y1="2" x2="22" y2="22">
                <stop offset="0%" stop-color="#4b5563"/>
                <stop offset="100%" stop-color="#374151"/>
              </linearGradient>
            </defs>
          </svg>
        </div>
        <h2>{$t('mainPanel.selectSession')}</h2>
        <p>{$t('mainPanel.selectSessionHint')}</p>
      </div>
    </div>
  {/if}
</div>

<ForkDialog bind:show={showForkDialog} />

<style>
  .main-panel {
    background: linear-gradient(180deg, rgba(15, 15, 26, 0.8) 0%, rgba(10, 10, 15, 0.9) 100%);
  }

  .view-tabs {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 12px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
    background: rgba(0, 0, 0, 0.2);
  }

  .view-tabs-left {
    display: flex;
    gap: 4px;
  }

  .view-tabs-right {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .terminal-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 12px;
    font-size: 12px;
    font-weight: 500;
    border-radius: 8px;
    border: none;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .terminal-btn.attach {
    background: rgba(34, 197, 94, 0.15);
    border: 1px solid rgba(34, 197, 94, 0.3);
    color: #4ade80;
  }

  .terminal-btn.attach:hover {
    background: rgba(34, 197, 94, 0.25);
    box-shadow: 0 0 12px rgba(34, 197, 94, 0.2);
  }

  .terminal-btn.detach {
    background: rgba(239, 68, 68, 0.15);
    border: 1px solid rgba(239, 68, 68, 0.3);
    color: #f87171;
  }

  .terminal-btn.detach:hover {
    background: rgba(239, 68, 68, 0.25);
    box-shadow: 0 0 12px rgba(239, 68, 68, 0.2);
  }

  .view-tab {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 14px;
    font-size: 13px;
    font-weight: 500;
    color: #9ca3af;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 8px;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .view-tab:hover:not(:disabled) {
    color: white;
    background: rgba(255, 255, 255, 0.05);
  }

  .view-tab.active {
    color: white;
    background: linear-gradient(135deg, rgba(139, 92, 246, 0.2) 0%, rgba(99, 102, 241, 0.15) 100%);
    border-color: rgba(139, 92, 246, 0.3);
  }

  .view-tab:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .content-area {
    background: rgba(0, 0, 0, 0.1);
    position: relative;
  }

  .view-panel {
    position: absolute;
    inset: 0;
    display: none;
  }

  .view-panel.active {
    display: flex;
    flex-direction: column;
  }

  .status-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 16px;
    background: rgba(0, 0, 0, 0.3);
    border-top: 1px solid rgba(255, 255, 255, 0.05);
    font-size: 12px;
  }

  .status-left {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .status-right {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .status-item {
    display: flex;
    align-items: center;
    gap: 6px;
    color: #6b7280;
  }

  .status-item svg {
    flex-shrink: 0;
  }

  .status-path {
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 11px;
    color: #9ca3af;
  }

  .status-id {
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 11px;
    color: #a78bfa;
  }

  .status-notes {
    font-size: 11px;
    color: #6b7280;
    font-style: italic;
  }

  .notes-preview {
    max-width: 200px;
    overflow: hidden;
  }

  .status-divider {
    width: 1px;
    height: 12px;
    background: rgba(255, 255, 255, 0.1);
  }

  .agent-badge {
    padding: 2px 8px;
    background: rgba(139, 92, 246, 0.2);
    border-radius: 4px;
    font-size: 11px;
    color: #a78bfa;
    text-transform: capitalize;
  }

  .yolo-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 5px 12px;
    background: rgba(107, 107, 107, 0.15);
    border: 1px solid rgba(107, 107, 107, 0.3);
    border-radius: 6px;
    font-size: 12px;
    font-weight: 500;
    color: #9ca3af;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .yolo-btn:hover {
    background: rgba(255, 107, 107, 0.2);
    border-color: rgba(255, 107, 107, 0.4);
    color: #ff6b6b;
  }

  .yolo-btn.active {
    background: rgba(255, 107, 107, 0.2);
    border-color: rgba(255, 107, 107, 0.5);
    color: #ff6b6b;
    box-shadow: 0 0 12px rgba(255, 107, 107, 0.15);
  }

  .yolo-btn.active:hover {
    background: rgba(255, 107, 107, 0.3);
    border-color: rgba(255, 107, 107, 0.6);
    box-shadow: 0 0 15px rgba(255, 107, 107, 0.25);
  }

  .fork-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 5px 12px;
    background: linear-gradient(135deg, rgba(139, 92, 246, 0.2) 0%, rgba(99, 102, 241, 0.15) 100%);
    border: 1px solid rgba(139, 92, 246, 0.3);
    border-radius: 6px;
    font-size: 12px;
    font-weight: 500;
    color: #a78bfa;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .fork-btn:hover {
    background: linear-gradient(135deg, rgba(139, 92, 246, 0.3) 0%, rgba(99, 102, 241, 0.25) 100%);
    border-color: rgba(139, 92, 246, 0.5);
    box-shadow: 0 0 15px rgba(139, 92, 246, 0.2);
  }

  .status-divider {
    width: 1px;
    height: 12px;
    background: rgba(255, 255, 255, 0.1);
  }

  .empty-panel {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .empty-content {
    text-align: center;
  }

  .empty-logo {
    margin-bottom: 24px;
    opacity: 0.5;
  }

  .empty-content h2 {
    font-size: 20px;
    font-weight: 600;
    color: #6b7280;
    margin-bottom: 8px;
  }

  .empty-content p {
    font-size: 14px;
    color: #4b5563;
  }
</style>
