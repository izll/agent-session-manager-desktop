<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { get } from 'svelte/store';
  import SessionTree from './lib/components/Sidebar/SessionTree.svelte';
  import ProjectSelector from './lib/components/Sidebar/ProjectSelector.svelte';
  import MainPanel from './lib/components/MainPanel/MainPanel.svelte';
  import NewSessionDialog from './lib/components/Dialogs/NewSessionDialog.svelte';
  import NewGroupDialog from './lib/components/Dialogs/NewGroupDialog.svelte';
  import GlobalSearchDialog from './lib/components/Dialogs/GlobalSearchDialog.svelte';
  import HelpDialog from './lib/components/Dialogs/HelpDialog.svelte';
  import UpdateDialog from './lib/components/Dialogs/UpdateDialog.svelte';
  import ImportDialog from './lib/components/Dialogs/ImportDialog.svelte';
  import SettingsDialog from './lib/components/Dialogs/SettingsDialog.svelte';
  import SessionColorDialog from './lib/components/Dialogs/SessionColorDialog.svelte';
  import ConfirmDialog from './lib/components/Dialogs/ConfirmDialog.svelte';
  import StopDialog from './lib/components/Dialogs/StopDialog.svelte';
  import StartDialog from './lib/components/Dialogs/StartDialog.svelte';
  import ResumeChoiceDialog from './lib/components/Dialogs/ResumeChoiceDialog.svelte';
  import ResumeSessionPickerDialog from './lib/components/Dialogs/ResumeSessionPickerDialog.svelte';
  import type { Session } from './lib/stores/sessions';
  import { loadSessions, selectedSession, selectedSessionId, selectedWindowIdx, startSession, stopSession, deleteSession, toggleFavorite, reorderSession, selectPrevSession, selectNextSession } from './lib/stores/sessions';
  import { loadProjects } from './lib/stores/projects';
  import { loadSettings } from './lib/stores/settings';
  import { agents, loadAgents } from './lib/stores/agents';
  import { startSidebarPolling, stopSidebarPolling } from './lib/stores/sidebarPolling';
  import { WindowMinimise, WindowToggleMaximise, Quit, EventsOn, EventsOff, EventsEmit } from '../wailsjs/runtime/runtime';
  import * as DictationService from '../wailsjs/go/main/DictationService';
  import { IsDevMode } from '../wailsjs/go/main/App';
  import asmgrIcon from './assets/icons/asmgr.svg';

  // Dev mode
  let devMode = false;

  // Dictation state
  let dictationEnabled = false;
  let dictationListening = false;

  let showNewSessionDialog = false;
  let showNewGroupDialog = false;
  let showGlobalSearch = false;

  let showHelpDialog = false;
  let showUpdateDialog = false;
  let showImportDialog = false;
  let showSettingsDialog = false;
  let showColorDialog = false;
  let colorDialogSession: Session | null = null;
  let showDeleteConfirm = false;
  let showQuitConfirm = false;
  let showStopDialog = false;
  let showStartDialog = false;
  let showResumeChoice = false;
  let showResumeSessionPicker = false;
  let pendingResumeSession: Session | null = null;

  // Sidebar state
  let sidebarWidth = 288; // default w-72 = 288px
  let sidebarCollapsed = false;
  let isResizing = false;
  const SIDEBAR_MIN = 200;
  const SIDEBAR_MAX = 500;
  const SIDEBAR_COLLAPSED = 56;

  function startResize(e: MouseEvent) {
    isResizing = true;
    document.addEventListener('mousemove', resize);
    document.addEventListener('mouseup', stopResize);
    e.preventDefault();
  }

  function resize(e: MouseEvent) {
    if (!isResizing) return;
    const newWidth = e.clientX;
    if (newWidth >= SIDEBAR_MIN && newWidth <= SIDEBAR_MAX) {
      sidebarWidth = newWidth;
      sidebarCollapsed = false;
    }
  }

  function stopResize() {
    isResizing = false;
    document.removeEventListener('mousemove', resize);
    document.removeEventListener('mouseup', stopResize);
  }

  function toggleSidebar() {
    sidebarCollapsed = !sidebarCollapsed;
  }

  $: actualSidebarWidth = sidebarCollapsed ? SIDEBAR_COLLAPSED : sidebarWidth;

  // Global keyboard shortcut handler
  function handleKeydown(e: KeyboardEvent) {
    // Don't handle shortcuts when typing in input fields or contenteditable
    const target = e.target as HTMLElement;
    if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable) {
      return;
    }

    // Don't handle shortcuts when any dialog is open (check for dialog-overlay in DOM)
    const dialogOpen = document.querySelector('.dialog-overlay') !== null;
    if (dialogOpen) {
      return; // Let the dialog handle its own keyboard events
    }

    // Don't handle shortcuts when dictation buffer panel is visible
    if (document.querySelector('.dictation-buffer')) {
      return;
    }

    // Ctrl+F for global search
    if ((e.ctrlKey || e.metaKey) && e.key === 'f') {
      e.preventDefault();
      showGlobalSearch = true;
      return;
    }

    // Ctrl+Up/Down for reordering sessions
    if (e.ctrlKey && e.key === 'ArrowUp') {
      e.preventDefault();
      if ($selectedSessionId) {
        reorderSession($selectedSessionId, -1);
      }
      return;
    }
    if (e.ctrlKey && e.key === 'ArrowDown') {
      e.preventDefault();
      if ($selectedSessionId) {
        reorderSession($selectedSessionId, 1);
      }
      return;
    }

    // Alt+Arrow Up/Down for session navigation (works even with terminal focus)
    if (e.altKey && e.key === 'ArrowUp') {
      e.preventDefault();
      selectPrevSession();
      return;
    }
    if (e.altKey && e.key === 'ArrowDown') {
      e.preventDefault();
      selectNextSession();
      return;
    }

    // Alt+F for search focus
    if (e.altKey && e.key === 'f') {
      e.preventDefault();
      const searchInput = document.querySelector('.search-input') as HTMLInputElement;
      searchInput?.focus();
      return;
    }

    // n for new session
    if (e.key === 'n' && !e.ctrlKey) {
      e.preventDefault();
      showNewSessionDialog = true;
      return;
    }

    // g for new group
    if (e.key === 'g' && !e.ctrlKey) {
      e.preventDefault();
      showNewGroupDialog = true;
      return;
    }

    // * for toggle favorite
    if (e.key === '*' || (e.key === '8' && e.shiftKey)) {
      e.preventDefault();
      if ($selectedSessionId) {
        toggleFavorite($selectedSessionId);
      }
      return;
    }

    // s for start session
    if (e.key === 's' && !e.ctrlKey) {
      e.preventDefault();
      handleStart();
      return;
    }

    // x for stop session
    if (e.key === 'x') {
      e.preventDefault();
      if ($selectedSession && $selectedSession.status === 'running') {
        handleStop();
      }
      return;
    }

    // d for delete session
    if (e.key === 'd' && !e.ctrlKey) {
      e.preventDefault();
      handleDelete();
      return;
    }

    // ? for help
    if (e.key === '?') {
      e.preventDefault();
      showHelpDialog = true;
      return;
    }

    // u for update check
    if (e.key === 'u' || e.key === 'U') {
      e.preventDefault();
      showUpdateDialog = true;
      return;
    }

    // i for import sessions
    if (e.key === 'i' || e.key === 'I') {
      e.preventDefault();
      showImportDialog = true;
      return;
    }
  }

  // Handle terminal navigation events (from xterm key interceptor)
  function handleTerminalNav(e: CustomEvent<{ direction: 'up' | 'down' }>) {
    if (e.detail.direction === 'up') {
      selectPrevSession();
    } else {
      selectNextSession();
    }
  }

  onMount(async () => {
    window.addEventListener('keydown', handleKeydown);
    window.addEventListener('terminal-nav', handleTerminalNav as EventListener);

    await Promise.all([
      loadProjects(),
      loadSessions(),
      loadSettings(),
      loadAgents()
    ]);

    // Check dev mode
    try { devMode = await IsDevMode(); } catch(_) {}

    // Start combined sidebar polling (activities + status lines)
    startSidebarPolling();

    // Initialize dictation service and listen for state changes
    initDictation();
  });

  onDestroy(() => {
    window.removeEventListener('keydown', handleKeydown);
    window.removeEventListener('terminal-nav', handleTerminalNav as EventListener);
    stopSidebarPolling();
    EventsOff('dictation:state');
    EventsOff('dictation:error');
  });

  async function initDictation() {
    try {
      const settings = await DictationService.GetDictationSettings();
      dictationEnabled = settings.enabled;
      if (dictationEnabled) {
        await DictationService.Initialize();
      }
      // Listen for state changes
      EventsOn('dictation:state', (listening: boolean) => {
        dictationListening = listening;
      });
      EventsOn('dictation:error', (error: {title: string, message: string}) => {
        console.error('Dictation error:', error.title, error.message);
        // Could show a toast notification here
      });
    } catch (e) {
      console.error('Failed to initialize dictation:', e);
    }
  }

  async function handleDictationEnabledChange(event: CustomEvent<boolean>) {
    const enabled = event.detail;
    dictationEnabled = enabled;
    // Notify other components (like TabBar) about the change
    EventsEmit('dictation:enabledChange', enabled);
    if (enabled) {
      try {
        await DictationService.Initialize();
      } catch (e) {
        console.error('Failed to initialize dictation:', e);
      }
    }
  }

  function handleNewSession() {
    showNewSessionDialog = true;
  }

  function handleNewGroup() {
    showNewGroupDialog = true;
  }

  function handleDelete() {
    if (!$selectedSession) return;
    showDeleteConfirm = true;
  }

  async function confirmDelete() {
    if (!$selectedSession) return;
    await deleteSession($selectedSession.id);
  }

  function handleQuit() {
    showQuitConfirm = true;
  }

  function confirmQuit() {
    Quit();
  }

  function handleStop() {
    if (!$selectedSession || $selectedSession.status !== 'running') return;
    showStopDialog = true;
  }

  async function handleStopSession() {
    if (!$selectedSession) return;
    // Stop all followed windows first
    if ($selectedSession.followedWindows && $selectedSession.followedWindows.length > 0) {
      for (const window of $selectedSession.followedWindows) {
        try {
          await stopSession(window.id);
        } catch (e) {
          console.error('Failed to stop followed window:', e);
        }
      }
    }
    // Then stop the main session
    await stopSession($selectedSession.id);
  }

  async function handleStopTab() {
    if (!$selectedSession) return;
    const windowIdx = get(selectedWindowIdx);

    if (windowIdx === 0) {
      // Stopping main session window
      await stopSession($selectedSession.id);
    } else {
      // Stopping followed window
      const followedWindow = $selectedSession.followedWindows?.[windowIdx - 1];
      if (followedWindow) {
        await stopSession(followedWindow.id);
      }
    }
  }

  function handleStart() {
    if (!$selectedSession || $selectedSession.status === 'running') return;
    // Check if agent supports resume
    const agentConfig = $agents.find(a => a.type === $selectedSession.agent);
    if (agentConfig?.supportsResume) {
      pendingResumeSession = $selectedSession;
      showResumeChoice = true;
      return;
    }
    // No resume support, start directly
    if ($selectedSession.followedWindows && $selectedSession.followedWindows.length > 0) {
      showStartDialog = true;
    } else {
      startSession($selectedSession.id);
    }
  }

  async function handleStartSession() {
    if (!$selectedSession) return;
    // Start the main session (which will restore all followed windows)
    await startSession($selectedSession.id);
  }

  async function handleStartTab() {
    if (!$selectedSession) return;
    const windowIdx = get(selectedWindowIdx);

    if (windowIdx === 0) {
      // Starting main session window
      await startSession($selectedSession.id);
    } else {
      // Starting specific followed window - for now just start the whole session
      // TODO: implement individual tab start if needed
      await startSession($selectedSession.id);
    }
  }

  function handleResume() {
    if (!$selectedSession || $selectedSession.status === 'running') return;
    pendingResumeSession = $selectedSession;
    showResumeSessionPicker = true;
  }

  // Resume choice handlers
  function handleResumeNewSession() {
    if (!pendingResumeSession) return;
    startSession(pendingResumeSession.id);
    pendingResumeSession = null;
  }

  function handleResumeContinueExisting() {
    if (!pendingResumeSession) return;
    showResumeSessionPicker = true;
  }

  async function handleResumeSessionSelect(event: CustomEvent<{ resumeId: string }>) {
    if (!pendingResumeSession) return;

    const { resumeId } = event.detail;
    await startSession(pendingResumeSession.id, resumeId);
    pendingResumeSession = null;
  }

  function handleResumeRestartWithTabs() {
    if (!pendingResumeSession) return;
    // Start with existing tab layout (same as the start dialog flow)
    startSession(pendingResumeSession.id);
    pendingResumeSession = null;
  }

  function handleResumeCancel() {
    pendingResumeSession = null;
  }
</script>

<main class="app-container h-screen flex flex-col text-white overflow-hidden" style="--sidebar-width: {actualSidebarWidth}px">
  <!-- Header (draggable titlebar) -->
  <header class="header flex items-center justify-between px-5 py-3" style="--wails-draggable:drag">
    <div class="header-left">
      <div class="header-logo-section" style="--wails-draggable:no-drag">
        {#if !sidebarCollapsed}
          <div class="logo-icon">
            <img src={asmgrIcon} alt="ASMGR" width="28" height="28" />
          </div>
          <span class="logo-text">Agent Session Manager<sup class="logo-suffix">desktop</sup></span>
          {#if devMode}<span class="dev-badge">DEV&nbsp;</span>{/if}
        {:else}
          <button class="collapse-btn header-expand" on:click={toggleSidebar} title="Expand sidebar">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polyline points="9 18 15 12 9 6"/>
            </svg>
          </button>
        {/if}
      </div>
      <div class="header-divider-vertical"></div>
      {#if $selectedSession}
        <span class="header-session-name" style={$selectedSession.color ? `color: ${$selectedSession.color}` : ''}>{$selectedSession.name}</span>
      {/if}
    </div>

    <div class="flex items-center gap-3" style="--wails-draggable:no-drag">
      <button class="btn btn-ghost" on:click={() => showGlobalSearch = true} title="Global Search (Ctrl+F)">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="11" cy="11" r="8"/>
          <path d="M21 21l-4.35-4.35"/>
        </svg>
        Search
      </button>
      <div class="header-icons">
        <button class="btn btn-ghost btn-icon" on:click={() => showImportDialog = true} title="Import Sessions (I)">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
            <polyline points="17 8 12 3 7 8"/>
            <line x1="12" y1="3" x2="12" y2="15"/>
          </svg>
        </button>
        <button class="btn btn-ghost btn-icon" on:click={() => showUpdateDialog = true} title="Check for Updates (U)">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
            <polyline points="7 10 12 15 17 10"/>
            <line x1="12" y1="15" x2="12" y2="3"/>
          </svg>
        </button>
        <button class="btn btn-ghost btn-icon" on:click={() => showHelpDialog = true} title="Help (?)">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="12" cy="12" r="10"/>
            <path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/>
            <circle cx="12" cy="17" r="1" fill="currentColor"/>
          </svg>
        </button>
        <button class="btn btn-ghost btn-icon" on:click={() => showSettingsDialog = true} title="Settings">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="12" cy="12" r="3"/>
            <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"/>
          </svg>
        </button>
      </div>

      <!-- Window controls divider -->
      <div class="window-divider"></div>

      <!-- Window controls -->
      <div class="window-controls">
        <button class="window-btn minimize" on:click={WindowMinimise} title="Minimize">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="5" y1="12" x2="19" y2="12"/>
          </svg>
        </button>
        <button class="window-btn maximize" on:click={WindowToggleMaximise} title="Maximize">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <rect x="4" y="4" width="16" height="16" rx="2"/>
          </svg>
        </button>
        <button class="window-btn close" on:click={handleQuit} title="Close">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/>
            <line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>
    </div>
  </header>

  <!-- Main Content -->
  <div class="flex-1 flex overflow-hidden" class:resizing={isResizing}>
    <!-- Sidebar -->
    <aside class="sidebar flex flex-col" class:collapsed={sidebarCollapsed} style="width: var(--sidebar-width)">
      {#if !sidebarCollapsed}
        <div class="p-3 border-b border-white/5">
          <ProjectSelector />
        </div>
        <div class="flex-1 overflow-hidden">
          <SessionTree onNewSession={handleNewSession} onNewGroup={handleNewGroup} onCollapse={toggleSidebar} />
        </div>
      {:else}
        <div class="collapsed-sidebar">
          <button class="collapse-btn expand" on:click={toggleSidebar} title="Expand sidebar">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polyline points="9 18 15 12 9 6"/>
            </svg>
          </button>
        </div>
      {/if}
      <!-- Resize Handle -->
      <div class="resize-handle" on:mousedown={startResize}></div>
    </aside>

    <!-- Main Panel -->
    <div class="main-content flex-1 overflow-hidden">
      <MainPanel
        on:openColorDialog={() => { colorDialogSession = $selectedSession; showColorDialog = true; }}
        on:requestStop={handleStop}
        on:requestStart={handleStart}
        on:requestResume={handleResume}
      />
    </div>
  </div>

  <!-- Dialogs -->
  <NewSessionDialog bind:show={showNewSessionDialog} />
  <NewGroupDialog bind:show={showNewGroupDialog} />
  <GlobalSearchDialog bind:show={showGlobalSearch} />
  <HelpDialog bind:show={showHelpDialog} />
  <UpdateDialog bind:show={showUpdateDialog} />
  <ImportDialog bind:show={showImportDialog} />
  <SettingsDialog bind:show={showSettingsDialog} on:dictationEnabledChange={handleDictationEnabledChange} />
  <SessionColorDialog bind:show={showColorDialog} session={colorDialogSession} />
  <ConfirmDialog
    bind:show={showDeleteConfirm}
    title="Delete Session"
    message="Are you sure you want to delete &quot;{$selectedSession?.name}&quot;? This action cannot be undone."
    confirmText="Delete"
    cancelText="Cancel"
    variant="danger"
    on:confirm={confirmDelete}
  />
  <ConfirmDialog
    bind:show={showQuitConfirm}
    title="Quit Application"
    message="Are you sure you want to quit? All running sessions will remain active in the background."
    confirmText="Quit"
    cancelText="Cancel"
    variant="warning"
    on:confirm={confirmQuit}
  />
  <StopDialog
    bind:show={showStopDialog}
    sessionName={$selectedSession?.name || ''}
    hasFollowedWindows={($selectedSession?.followedWindows?.length || 0) > 0}
    on:stopSession={handleStopSession}
    on:stopTab={handleStopTab}
  />
  <StartDialog
    bind:show={showStartDialog}
    sessionName={$selectedSession?.name || ''}
    hasFollowedWindows={($selectedSession?.followedWindows?.length || 0) > 0}
    on:startSession={handleStartSession}
    on:startTab={handleStartTab}
  />
  <ResumeChoiceDialog
    bind:show={showResumeChoice}
    session={pendingResumeSession}
    hasTabs={pendingResumeSession?.followedWindows?.length > 0}
    on:newSession={handleResumeNewSession}
    on:continueExisting={handleResumeContinueExisting}
    on:restartWithTabs={handleResumeRestartWithTabs}
    on:cancel={handleResumeCancel}
  />
  <ResumeSessionPickerDialog
    bind:show={showResumeSessionPicker}
    session={pendingResumeSession}
    on:select={handleResumeSessionSelect}
    on:cancel={handleResumeCancel}
  />
</main>

<style>
  :global(body) {
    margin: 0;
    padding: 0;
    font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen,
      Ubuntu, Cantarell, 'Open Sans', 'Helvetica Neue', sans-serif;
    background: #0a0a0f;
  }

  :global(*) {
    box-sizing: border-box;
  }

  :global(::-webkit-scrollbar) {
    width: 6px;
    height: 6px;
  }

  :global(::-webkit-scrollbar-track) {
    background: transparent;
  }

  :global(::-webkit-scrollbar-thumb) {
    background: rgba(139, 92, 246, 0.3);
    border-radius: 3px;
  }

  :global(::-webkit-scrollbar-thumb:hover) {
    background: rgba(139, 92, 246, 0.5);
  }

  .app-container {
    background: linear-gradient(135deg, #0a0a0f 0%, #0f0f1a 50%, #0a0a0f 100%);
  }

  .header {
    background: linear-gradient(180deg, rgba(139, 92, 246, 0.08) 0%, transparent 100%);
    border-bottom: 1px solid rgba(139, 92, 246, 0.15);
    backdrop-filter: blur(10px);
  }

  .logo-icon {
    display: flex;
    align-items: center;
    justify-content: center;
    filter: drop-shadow(0 0 8px rgba(168, 85, 247, 0.4));
    margin-left: 4px;
  }

  .logo-text {
    font-size: 14px;
    font-weight: 600;
    color: #e4e4e7;
    white-space: nowrap;
    text-shadow: 0 0 6px rgba(168, 85, 247, 0.3);
  }

  .logo-suffix {
    font-size: 9px;
    font-weight: 500;
    color: #a78bfa;
    margin-left: 2px;
    vertical-align: super;
    opacity: 0.8;
  }

  .dev-badge {
    font-size: 14px;
    font-weight: 700;
    color: #ffc800;
    margin-left: 4px;
  }

  .header-left {
    display: flex;
    align-items: center;
    min-width: 0;
    flex: 1;
  }

  .header-logo-section {
    display: flex;
    align-items: center;
    gap: 12px;
    width: calc(var(--sidebar-width) - 16px);
    flex-shrink: 0;
  }

  .header-divider-vertical {
    width: 1px;
    align-self: stretch;
    background: rgba(255, 255, 255, 0.1);
    margin: 0 16px;
    flex-shrink: 0;
  }

  .header-session-name {
    font-size: 14px;
    font-weight: 500;
    color: #d4d4d8;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
    flex: 1;
  }

  .sidebar {
    position: relative;
    background: linear-gradient(180deg, rgba(15, 15, 26, 0.9) 0%, rgba(10, 10, 15, 0.95) 100%);
    border-right: 1px solid rgba(139, 92, 246, 0.1);
    box-shadow: 4px 0 24px rgba(0, 0, 0, 0.3);
  }

  .sidebar.collapsed {
    overflow: hidden;
  }

  .resizing {
    cursor: col-resize;
    user-select: none;
  }

  .resize-handle {
    position: absolute;
    top: 0;
    right: -3px;
    width: 6px;
    height: 100%;
    cursor: col-resize;
    z-index: 10;
  }

  .resize-handle:hover {
    background: rgba(139, 92, 246, 0.3);
  }

  .collapse-btn {
    position: absolute;
    bottom: 12px;
    right: 12px;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 6px;
    color: #6b7280;
    cursor: pointer;
    transition: all 0.2s ease;
    z-index: 5;
  }

  .collapse-btn:hover {
    background: rgba(139, 92, 246, 0.15);
    border-color: rgba(139, 92, 246, 0.3);
    color: #a78bfa;
  }

  .collapse-btn.expand {
    position: static;
    margin: 12px auto;
  }

  .collapse-btn.header-expand {
    position: static;
    margin: 0;
  }

  .collapsed-sidebar {
    display: flex;
    flex-direction: column;
    align-items: center;
    padding-top: 8px;
    height: 100%;
  }

  .main-content {
    background: linear-gradient(180deg, rgba(15, 15, 26, 0.5) 0%, rgba(10, 10, 15, 0.7) 100%);
  }

  .btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 32px;
    padding: 0 16px;
    margin: 2px 0;
    font-size: 13px;
    font-weight: 500;
    border-radius: 8px;
    border: none;
    cursor: pointer;
    transition: all 0.2s ease;
    color: white;
  }

  .btn-ghost {
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.1);
    color: #a1a1aa;
  }

  .btn-ghost:hover {
    background: rgba(255, 255, 255, 0.1);
    border-color: rgba(255, 255, 255, 0.2);
    color: white;
  }

  .btn-icon {
    width: 32px;
    padding: 0;
    justify-content: center;
  }

  .header-icons {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-left: 12px;
  }

  .window-divider {
    width: 1px;
    height: 20px;
    background: rgba(255, 255, 255, 0.1);
    margin: 0 8px;
  }

  .window-controls {
    display: flex;
    gap: 4px;
  }

  .window-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    background: transparent;
    border: none;
    border-radius: 8px;
    color: #6b7280;
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .window-btn:hover {
    background: rgba(255, 255, 255, 0.1);
    color: #e4e4e7;
  }

  .window-btn.close:hover {
    background: rgba(239, 68, 68, 0.2);
    color: #f87171;
  }

</style>
