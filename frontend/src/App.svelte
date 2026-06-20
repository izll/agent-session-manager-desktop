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
  import { loadSessions, selectedSession, selectedSessionId, selectedWindowIdx, startSession, stopSession, stopTab, restartTab, restartTabWithResume, deleteSession, toggleFavorite, reorderSession, selectPrevSession, selectNextSession } from './lib/stores/sessions';
  import { loadProjects } from './lib/stores/projects';
  import { loadSettings, settings } from './lib/stores/settings';
  import { agents, loadAgents } from './lib/stores/agents';
  import { startSidebarPolling, stopSidebarPolling } from './lib/stores/sidebarPolling';
  import { WindowMinimise, WindowToggleMaximise, Quit, EventsOn, EventsOff, EventsEmit } from '../wailsjs/runtime/runtime';
  import * as DictationService from '../wailsjs/go/main/DictationService';
  import { IsDevMode } from '../wailsjs/go/main/App';
  import asmgrIcon from './assets/icons/asmgr.svg';
  import { t, isRTL, loadTranslations } from './lib/i18n';
  import { focusTerminal } from './lib/utils/focus';

  // Dev mode
  let devMode = false;

  function openDevTools() {
    // Wails internal message to open WebKit inspector
    (window as any).WailsInvoke?.('wails:showInspector');
  }

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
  let pendingResumeWindowIdx: number | null = null; // non-null means tab-level resume
  // Agent type to feed into the resume picker. For tab-level resumes this is
  // the tab's own agent (might differ from the main session agent), otherwise
  // it stays null and the dialog falls back to session.agent.
  let pendingResumeAgent: string | null = null;

  // Track "any dialog open" to restore terminal focus after the last one closes.
  // Without this, closing a dialog leaves focus on the dialog's overlay/buttons,
  // so subsequent keystrokes don't reach the terminal.
  let anyDialogOpen = false;
  let prevAnyDialogOpen = false;
  $: anyDialogOpen =
    showNewSessionDialog || showNewGroupDialog || showGlobalSearch ||
    showHelpDialog || showUpdateDialog || showImportDialog ||
    showSettingsDialog || showColorDialog || showDeleteConfirm ||
    showQuitConfirm || showStopDialog || showStartDialog ||
    showResumeChoice || showResumeSessionPicker;
  $: if (prevAnyDialogOpen && !anyDialogOpen) {
    // Dialog just closed — return focus to the terminal
    focusTerminal();
  }
  $: prevAnyDialogOpen = anyDialogOpen;

  // Sidebar state
  let sidebarWidth = 288; // default w-72 = 288px
  let sidebarCollapsed = false;
  let sidebarOverlayOpen = false;
  let isResizing = false;
  let collapseBlockUntil = 0;

  function handleCollapsedHoverEnter() {
    if (!sidebarCollapsed || Date.now() < collapseBlockUntil) return;
    sidebarOverlayOpen = true;
  }

  function handleCollapsedHoverLeave() {
    sidebarOverlayOpen = false;
  }
  const SIDEBAR_MIN = 200;
  const SIDEBAR_MAX = 500;
  const SIDEBAR_COLLAPSED = 40;

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
    if (!sidebarCollapsed) {
      collapseBlockUntil = Date.now() + 400;
    }
    sidebarCollapsed = !sidebarCollapsed;
    sidebarOverlayOpen = false;
  }

  $: actualSidebarWidth = sidebarCollapsed ? SIDEBAR_COLLAPSED : sidebarWidth;

  // Auto-close overlay when session selection changes
  let prevSelectedId = $selectedSessionId;
  $: if (sidebarOverlayOpen && $selectedSessionId !== prevSelectedId) {
    sidebarOverlayOpen = false;
    prevSelectedId = $selectedSessionId;
  }

  // Global keyboard shortcut handler.
  // All app shortcuts use Ctrl+Shift+<Letter> so they work everywhere —
  // including while the terminal has focus — without clashing with tmux
  // bindings (Ctrl+<x>) or shell bindings (Ctrl+N/P/etc.). The listener
  // is registered in the capture phase so the terminal doesn't swallow
  // these combos before we see them.
  function handleKeydown(e: KeyboardEvent) {
    // Close sidebar overlay on Escape
    if (e.key === 'Escape' && sidebarOverlayOpen) {
      sidebarOverlayOpen = false;
      return;
    }

    // FAST PATH: this handler runs in the capture phase on EVERY keystroke,
    // including ordinary typing into the terminal. All our shortcuts need a
    // modifier (Ctrl/Cmd+Shift) or Alt+Arrow. Bail out immediately for plain
    // keys BEFORE doing any DOM work — the two querySelector() calls below
    // walk a document that contains thousands of xterm cell spans and were
    // running on every character, which dominated the per-keystroke JS cost
    // (profiling: ~48 keydown/s while typing pegged the main thread).
    const mod = (e.ctrlKey || e.metaKey) && e.shiftKey;
    const altArrow = e.altKey && !e.ctrlKey && !e.shiftKey &&
      (e.key === 'ArrowUp' || e.key === 'ArrowDown');
    if (!mod && !altArrow) return;

    // Don't handle shortcuts when any dialog is open
    const dialogOpen = document.querySelector('.dialog-overlay') !== null;
    if (dialogOpen) return;

    // Don't handle shortcuts when dictation buffer panel is visible
    if (document.querySelector('.dictation-buffer')) return;

    // --- Navigation (work even inside input fields) ---

    // Ctrl+Shift+↑/↓ — session navigation
    if (mod && e.key === 'ArrowUp') {
      e.preventDefault();
      selectPrevSession();
      return;
    }
    if (mod && e.key === 'ArrowDown') {
      e.preventDefault();
      selectNextSession();
      return;
    }

    // Alt+↑/↓ kept as an additional way to navigate (no modifier conflict
    // with Ctrl+Shift+arrows some users map to word-wise selection).
    if (e.altKey && !e.ctrlKey && !e.shiftKey && e.key === 'ArrowUp') {
      e.preventDefault();
      selectPrevSession();
      return;
    }
    if (e.altKey && !e.ctrlKey && !e.shiftKey && e.key === 'ArrowDown') {
      e.preventDefault();
      selectNextSession();
      return;
    }

    if (!mod) return;

    // Normalise the letter — Ctrl+Shift+N gives e.key === 'N' (uppercase).
    // e.code is layout-dependent; use the lowercased key instead.
    const key = e.key.toLowerCase();

    switch (key) {
      case 'f': // global search
        e.preventDefault();
        showGlobalSearch = true;
        return;
      case 'n': // new session
        e.preventDefault();
        showNewSessionDialog = true;
        return;
      case 'g': // new group
        e.preventDefault();
        showNewGroupDialog = true;
        return;
      case 's': // start / resume selected
        e.preventDefault();
        handleStart();
        return;
      case 'x': // stop selected
        e.preventDefault();
        if ($selectedSession && $selectedSession.status === 'running') {
          handleStop();
        }
        return;
      case 'd': // delete selected
        e.preventDefault();
        handleDelete();
        return;
      case '8': // '*' on many layouts — toggle favorite
        e.preventDefault();
        if ($selectedSessionId) toggleFavorite($selectedSessionId);
        return;
      case 'h': // help
        e.preventDefault();
        showHelpDialog = true;
        return;
      case 'u': // update check
        e.preventDefault();
        showUpdateDialog = true;
        return;
      case 'i': // import sessions
        e.preventDefault();
        showImportDialog = true;
        return;
      case 'k': // reorder selected up
        e.preventDefault();
        if ($selectedSessionId) reorderSession($selectedSessionId, -1);
        return;
      case 'j': // reorder selected down
        e.preventDefault();
        if ($selectedSessionId) reorderSession($selectedSessionId, 1);
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
    // Capture phase so the terminal (xterm) can't swallow Ctrl+Shift combos.
    window.addEventListener('keydown', handleKeydown, true);
    window.addEventListener('terminal-nav', handleTerminalNav as EventListener);

    await Promise.all([
      loadProjects(),
      loadSessions(),
      loadSettings(),
      loadAgents()
    ]);

    // Load i18n translations from saved language setting
    const currentSettings = get(settings);
    await loadTranslations(currentSettings.language || 'en');

    // Check dev mode
    try { devMode = await IsDevMode(); } catch(_) {}

    // Start combined sidebar polling (activities + status lines)
    startSidebarPolling();

    // Initialize dictation service and listen for state changes
    initDictation();
  });

  onDestroy(() => {
    window.removeEventListener('keydown', handleKeydown, true);
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
    // kill-session kills all tmux windows at once
    await stopSession($selectedSession.id);
  }

  async function handleStopTab() {
    if (!$selectedSession) return;
    const windowIdx = get(selectedWindowIdx);
    // StopTab kills the tmux window (or entire session for window 0)
    await stopTab($selectedSession.id, windowIdx);
  }

  function handleStart() {
    if (!$selectedSession || $selectedSession.status === 'running') return;
    // If session has a saved resume ID, restart with it directly (no dialog)
    if ($selectedSession.resumeSessionId) {
      startSession($selectedSession.id, $selectedSession.resumeSessionId);
      return;
    }
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
    await restartTab($selectedSession.id, windowIdx);
  }

  function handleResume() {
    if (!$selectedSession) return;
    pendingResumeSession = $selectedSession;
    // Check if this is a tab-level resume (session running but tab stopped)
    if ($selectedSession.status === 'running') {
      const winIdx = get(selectedWindowIdx);
      pendingResumeWindowIdx = winIdx;
      // Pick the agent of the tab being resumed, not the parent session.
      // Otherwise a Codex tab inside a Claude session would list Claude
      // conversations in the picker.
      let agent: string | null = null;
      if (winIdx === 0) {
        agent = $selectedSession.agent;
      } else if ($selectedSession.followedWindows) {
        const fw = $selectedSession.followedWindows.find((f: any) => f.index === winIdx);
        if (fw?.agent) agent = fw.agent;
      }
      pendingResumeAgent = agent;
    } else {
      pendingResumeWindowIdx = null;
      pendingResumeAgent = null;
    }
    showResumeSessionPicker = true;
  }

  // Resume choice handlers
  function handleResumeNewSession() {
    if (!pendingResumeSession) return;
    if (pendingResumeWindowIdx !== null) {
      restartTab(pendingResumeSession.id, pendingResumeWindowIdx);
    } else {
      startSession(pendingResumeSession.id);
    }
    pendingResumeSession = null;
    pendingResumeWindowIdx = null;
  }

  function handleResumeContinueExisting() {
    if (!pendingResumeSession) return;
    showResumeSessionPicker = true;
  }

  async function handleResumeSessionSelect(event: CustomEvent<{ resumeId: string }>) {
    if (!pendingResumeSession) return;

    const { resumeId } = event.detail;
    if (pendingResumeWindowIdx !== null) {
      // Tab-level resume: restart just this tab with the selected resume ID
      await restartTabWithResume(pendingResumeSession.id, pendingResumeWindowIdx, resumeId);
    } else {
      await startSession(pendingResumeSession.id, resumeId);
    }
    pendingResumeSession = null;
    pendingResumeWindowIdx = null;
    pendingResumeAgent = null;
  }

  function handleResumeRestartWithTabs() {
    if (!pendingResumeSession) return;
    if (pendingResumeWindowIdx !== null) {
      restartTab(pendingResumeSession.id, pendingResumeWindowIdx);
    } else {
      // Start with existing tab layout, resuming the saved session
      const savedResumeId = pendingResumeSession.resumeSessionId || '';
      startSession(pendingResumeSession.id, savedResumeId || undefined);
    }
    pendingResumeSession = null;
    pendingResumeWindowIdx = null;
  }

  function handleResumeCancel() {
    pendingResumeSession = null;
    pendingResumeWindowIdx = null;
    pendingResumeAgent = null;
  }
</script>

<main class="app-container h-screen flex flex-col text-white overflow-hidden" style="--sidebar-width: {actualSidebarWidth}px" dir={$isRTL ? 'rtl' : 'ltr'}>
  <!-- Header (draggable titlebar) -->
  <header class="header flex items-center justify-between py-3" style="--wails-draggable:drag; padding-left: 0; padding-right: 10px;"
    on:contextmenu|preventDefault={devMode ? openDevTools : undefined}>
    <div class="header-left">
      <div class="header-logo-section" style="--wails-draggable:no-drag">
        <div class="logo-icon">
          <img src={asmgrIcon} alt="ASMGR" width={sidebarCollapsed ? 20 : 28} height={sidebarCollapsed ? 20 : 28} />
        </div>
        {#if !sidebarCollapsed}
          <span class="logo-text">{$t('app.title')}<sup class="logo-suffix">{$t('app.titleSuffix')}</sup></span>
          {#if devMode}<span class="dev-badge">DEV&nbsp;</span>{/if}
        {/if}
      </div>
      <div class="header-divider-vertical"></div>
      {#if $selectedSession}
        <span class="header-session-name" style={$selectedSession.color ? `color: ${$selectedSession.color}` : ''}>{$selectedSession.name}</span>
      {/if}
    </div>

    <div class="flex items-center gap-3" style="--wails-draggable:no-drag">
      <button class="btn btn-ghost" on:click={() => showGlobalSearch = true} title={$t('header.globalSearch')}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="11" cy="11" r="8"/>
          <path d="M21 21l-4.35-4.35"/>
        </svg>
        {$t('app.search')}
      </button>
      <div class="header-icons">
        <button class="btn btn-ghost btn-icon" on:click={() => showImportDialog = true} title={$t('header.importSessions')}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
            <polyline points="17 8 12 3 7 8"/>
            <line x1="12" y1="3" x2="12" y2="15"/>
          </svg>
        </button>
        <button class="btn btn-ghost btn-icon" on:click={() => showUpdateDialog = true} title={$t('header.checkUpdates')}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
            <polyline points="7 10 12 15 17 10"/>
            <line x1="12" y1="15" x2="12" y2="3"/>
          </svg>
        </button>
        <button class="btn btn-ghost btn-icon" on:click={() => showHelpDialog = true} title={$t('header.help')}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="12" cy="12" r="10"/>
            <path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/>
            <circle cx="12" cy="17" r="1" fill="currentColor"/>
          </svg>
        </button>
        <button class="btn btn-ghost btn-icon" on:click={() => showSettingsDialog = true} title={$t('header.settings')}>
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
        <button class="window-btn minimize" on:click={WindowMinimise} title={$t('header.minimize')}>
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="5" y1="12" x2="19" y2="12"/>
          </svg>
        </button>
        <button class="window-btn maximize" on:click={WindowToggleMaximise} title={$t('header.maximize')}>
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <rect x="4" y="4" width="16" height="16" rx="2"/>
          </svg>
        </button>
        <button class="window-btn close" on:click={handleQuit} title={$t('header.close')}>
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/>
            <line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>
    </div>
  </header>

  <!-- Main Content -->
  <div class="flex-1 flex overflow-hidden relative" class:resizing={isResizing}>
    <!-- Sidebar -->
    {#if !sidebarCollapsed}
      <aside class="sidebar flex flex-col" style="width: var(--sidebar-width)">
        <div class="p-3 border-b border-white/5">
          <ProjectSelector />
        </div>
        <div class="flex-1 overflow-hidden">
          <SessionTree onNewSession={handleNewSession} onNewGroup={handleNewGroup} onCollapse={toggleSidebar} />
        </div>
        <div class="resize-handle" on:mousedown={startResize}></div>
      </aside>
    {:else}
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div class="collapsed-sidebar-wrapper" on:mouseenter={handleCollapsedHoverEnter} on:mouseleave={handleCollapsedHoverLeave}>
        <aside class="sidebar collapsed" style="width: {SIDEBAR_COLLAPSED}px">
          <div class="collapsed-strip">
            <button class="expand-btn" on:click|stopPropagation={toggleSidebar} title={$t('sidebar.expandSidebar')}>
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <polyline points="9 18 15 12 9 6"/>
              </svg>
            </button>
          </div>
        </aside>
        {#if sidebarOverlayOpen}
          <div class="sidebar-overlay" style="width: {sidebarWidth}px">
            <div class="p-3 border-b border-white/5">
              <ProjectSelector />
            </div>
            <div class="flex-1 overflow-hidden">
              <SessionTree onNewSession={handleNewSession} onNewGroup={handleNewGroup} onCollapse={() => { sidebarOverlayOpen = false; toggleSidebar(); }} />
            </div>
          </div>
        {/if}
      </div>
      {#if sidebarOverlayOpen}
        <!-- svelte-ignore a11y-no-static-element-interactions -->
        <div class="sidebar-overlay-backdrop" on:click={() => sidebarOverlayOpen = false}></div>
      {/if}
    {/if}

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
    title={$t('confirm.deleteSession')}
    message={$t('confirm.deleteSessionMessage', { name: $selectedSession?.name || '' })}
    confirmText={$t('confirm.deleteConfirm')}
    cancelText={$t('common.cancel')}
    variant="danger"
    on:confirm={confirmDelete}
  />
  <ConfirmDialog
    bind:show={showQuitConfirm}
    title={$t('confirm.quitApp')}
    message={$t('confirm.quitMessage')}
    confirmText={$t('confirm.quitConfirm')}
    cancelText={$t('common.cancel')}
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
    agentOverride={pendingResumeAgent}
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
    /* No backdrop-filter: an always-visible blurred region forces WebKit to
       re-gaussian-blur (and full-window repaint) on every frame anything
       behind it changes — the dominant cause of ~90% renderer CPU with
       several running sessions. The header's own gradient is enough. */
  }

  .logo-icon {
    display: flex;
    align-items: center;
    justify-content: center;
    filter: drop-shadow(0 0 8px rgba(168, 85, 247, 0.4));
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
    justify-content: center;
    gap: 10px;
    width: var(--sidebar-width);
    padding-left: 0;
    flex-shrink: 0;
  }


  .header-divider-vertical {
    width: 1px;
    align-self: stretch;
    background: rgba(255, 255, 255, 0.1);
    margin: 0 16px 0 0;
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

  .collapsed-sidebar-wrapper {
    position: relative;
    display: flex;
    height: 100%;
    z-index: 50;
  }

  .collapsed-strip {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
  }

  .expand-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 6px;
    color: #6b7280;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .expand-btn:hover {
    background: rgba(139, 92, 246, 0.15);
    border-color: rgba(139, 92, 246, 0.3);
    color: #a78bfa;
  }

  .sidebar.collapsed {
    overflow: hidden;
    flex-shrink: 0;
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

  /* RTL support */
  :global([dir="rtl"]) .header-left {
    flex-direction: row-reverse;
  }

  :global([dir="rtl"]) .sidebar {
    border-right: none;
    border-left: 1px solid rgba(139, 92, 246, 0.1);
    box-shadow: -4px 0 24px rgba(0, 0, 0, 0.3);
  }

  :global([dir="rtl"]) .resize-handle {
    right: auto;
    left: -3px;
  }

  :global([dir="rtl"]) .logo-icon {
    margin-left: 0;
    margin-right: 4px;
  }

  :global([dir="rtl"]) .logo-suffix {
    margin-left: 0;
    margin-right: 2px;
  }

  :global([dir="rtl"]) .dev-badge {
    margin-left: 0;
    margin-right: 4px;
  }

  :global([dir="rtl"]) .header-icons {
    margin-left: 0;
    margin-right: 12px;
  }

  :global([dir="rtl"]) .collapse-btn {
    right: auto;
    left: 12px;
  }

  :global([dir="rtl"]) .collapse-btn svg {
    transform: scaleX(-1);
  }

  /* Sidebar overlay */
  .sidebar-overlay-backdrop {
    position: fixed;
    inset: 0;
    z-index: 40;
  }

  .sidebar-overlay {
    background: linear-gradient(180deg, rgba(15, 15, 26, 0.97) 0%, rgba(10, 10, 15, 0.99) 100%);
    border-right: 1px solid rgba(139, 92, 246, 0.15);
    box-shadow: 8px 0 32px rgba(0, 0, 0, 0.5);
    display: flex;
    flex-direction: column;
    flex-shrink: 0;
    animation: overlaySlideIn 0.15s ease-out;
  }

  @keyframes overlaySlideIn {
    from { opacity: 0; transform: translateX(-8px); }
    to   { opacity: 1; transform: translateX(0); }
  }

  :global([dir="rtl"]) .sidebar-overlay {
    border-right: none;
    border-left: 1px solid rgba(139, 92, 246, 0.15);
    box-shadow: -8px 0 32px rgba(0, 0, 0, 0.5);
  }

  :global([dir="rtl"]) .expand-btn svg {
    transform: scaleX(-1);
  }

</style>
