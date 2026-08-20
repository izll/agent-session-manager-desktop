<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import { get } from 'svelte/store';
  import SessionTree from './lib/components/Sidebar/SessionTree.svelte';
  import ProjectSelector from './lib/components/Sidebar/ProjectSelector.svelte';
  import MainPanel from './lib/components/MainPanel/MainPanel.svelte';
  import ProjectDashboard from './lib/components/Dashboard/ProjectDashboard.svelte';
  import AllTasks from './lib/components/Dashboard/AllTasks.svelte';
  import UndoToast from './lib/components/common/UndoToast.svelte';
  import NewSessionDialog from './lib/components/Dialogs/NewSessionDialog.svelte';
  import NewGroupDialog from './lib/components/Dialogs/NewGroupDialog.svelte';
  import GlobalSearchDialog from './lib/components/Dialogs/GlobalSearchDialog.svelte';
  import BgAgentsDialog from './lib/components/Dialogs/BgAgentsDialog.svelte';
  import HelpDialog from './lib/components/Dialogs/HelpDialog.svelte';
  import UpdateDialog from './lib/components/Dialogs/UpdateDialog.svelte';
  import ImportDialog from './lib/components/Dialogs/ImportDialog.svelte';
  import SessionFileDialog from './lib/components/Dialogs/SessionFileDialog.svelte';
  import SettingsDialog from './lib/components/Dialogs/SettingsDialog.svelte';
  import { autoFocusDialog, autoFocusField } from './lib/utils/dialogActions';
  import LogDialog from './lib/components/Dialogs/LogDialog.svelte';
  import QuickJumpDialog from './lib/components/Dialogs/QuickJumpDialog.svelte';
  import GitHistoryDialog from './lib/components/Dialogs/GitHistoryDialog.svelte';
  import RecoveryCenterDialog from './lib/components/Dialogs/RecoveryCenterDialog.svelte';
  import CommandPalette from './lib/components/Dialogs/CommandPalette.svelte';
  import CommandPickerDialog from './lib/components/Dialogs/CommandPickerDialog.svelte';
  import CommandManagerDialog from './lib/components/Dialogs/CommandManagerDialog.svelte';
  import SessionTemplateDialog from './lib/components/Dialogs/SessionTemplateDialog.svelte';
  import SessionColorDialog from './lib/components/Dialogs/SessionColorDialog.svelte';
  import ConfirmDialog from './lib/components/Dialogs/ConfirmDialog.svelte';
  import StopDialog from './lib/components/Dialogs/StopDialog.svelte';
  import StartDialog from './lib/components/Dialogs/StartDialog.svelte';
  import ResumeChoiceDialog from './lib/components/Dialogs/ResumeChoiceDialog.svelte';
  import ResumeSessionPickerDialog from './lib/components/Dialogs/ResumeSessionPickerDialog.svelte';
  import type { Session } from './lib/stores/sessions';
  import { error as sessionError } from './lib/stores/sessions';
  import { appError } from './lib/stores/appErrors';
  import { sessions, loadSessions, selectSession, selectWindow, selectedSession, selectedSessionId, selectedWindowIdx, startSession, stopSession, stopTab, restartTab, restartTabWithResume, deleteSession, toggleFavorite, reorderSession, selectPrevSession, selectNextSession } from './lib/stores/sessions';
  import { activities } from './lib/stores/activities';
  import { statusLines, tabStatuses } from './lib/stores/statusLines';
  import { QuickReplyTab, ExportSessions, PendingUpdate, AddQuickJump } from '../wailsjs/go/main/App';
  import { loadProjects, otherInstancePID, refreshLockStatus } from './lib/stores/projects';
  import { appView, goBack, showTasksView } from './lib/stores/navigation';
  import { openTaskCount, watchOpenCount, refreshOpenCount } from './lib/stores/taskAlerts';
  import { loadSettings, settings } from './lib/stores/settings';
  import GitBranchBadge from './lib/components/common/GitBranchBadge.svelte';
  import { agents, loadAgents } from './lib/stores/agents';
  import { startSidebarPolling, stopSidebarPolling } from './lib/stores/sidebarPolling';
  import { WindowMinimise, WindowToggleMaximise, Quit, EventsOn, EventsOff, EventsEmit } from '../wailsjs/runtime/runtime';
  import * as DictationService from '../wailsjs/go/main/DictationService';
  import { IsDevMode, GetMultiplexerStatus, InstallMultiplexer, UnfinishedTasksForSession, GetTabWorkingDirectory } from '../wailsjs/go/main/App';
  import asmgrIcon from './assets/icons/asmgr.svg';
  import { applyUITheme, DEFAULT_UI_THEME } from './lib/utils/uiThemes';
  import { t, isRTL, loadTranslations } from './lib/i18n';
  import { focusTerminal } from './lib/utils/focus';
  import { shortcutForEvent, capturingShortcut } from './lib/stores/shortcuts';
  import Toast from './lib/components/common/Toast.svelte';

  // The accent lives in CSS variables, so applying a theme is one write to
  // the root element — no component needs to know about it.
  $: applyUITheme($settings.uiTheme || DEFAULT_UI_THEME, $settings.uiAccent);

  // Dev mode
  let devMode = false;

  // Single-instance guard: when another instance owns this project, the
  // backend refuses terminal attaches. Show a dismissable banner so the user
  // knows why terminals won't connect (instead of silent black tabs). The PID
  // lives in the projects store so it updates on every project switch.
  let lockBannerDismissed = false;

  // Every session runs inside a terminal multiplexer — tmux on Linux and macOS,
  // psmux on Windows — so without one the app can do nothing at all. Said here,
  // once, rather than left for the user to discover by creating a session and
  // being refused. Not dismissable: unlike the lock banner, this does not
  // resolve on its own and nothing works until it is fixed.
  let missingMultiplexer: { name: string; hint: string; canInstall: boolean } | null = null;
  // Windows can install psmux itself, with winget. Nothing else can — see
  // session/multiplexer_install_other.go for why the other platforms only
  // print the command.
  let installingMultiplexer = false;
  let multiplexerInstallError = '';

  async function installMultiplexer() {
    if (installingMultiplexer) return;
    installingMultiplexer = true;
    multiplexerInstallError = '';
    try {
      await InstallMultiplexer();
      const s = await GetMultiplexerStatus();
      // Cleared only once the backend agrees it can find it — winget exiting 0
      // is not proof this process can run it.
      missingMultiplexer = s?.available === false
        ? { name: s.name, hint: s.hint || '', canInstall: !!s.canInstall }
        : null;
    } catch (e: any) {
      multiplexerInstallError = e?.message || String(e);
    } finally {
      installingMultiplexer = false;
    }
  }
  // Reset the "dismissed" state whenever the lock owner changes (e.g. after
  // a project switch), so the banner reappears for a newly-locked project.
  let prevOtherPID = 0;
  $: if ($otherInstancePID !== prevOtherPID) {
    prevOtherPID = $otherInstancePID;
    lockBannerDismissed = false;
  }

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
  let showBgAgents = false;

  let showHelpDialog = false;
  let showUpdateDialog = false;
  /** Version found by the daily background check; drives the header dot. */
  let availableUpdate = '';
  let showImportDialog = false;
  let showFileImportDialog = false;
  /** Export failures are rare but must not vanish silently. */
  let exportError = '';
  let showSettingsDialog = false;
  let showLogDialog = false;
  let showQuickJump = false;
  let showSessionError = false;
  let sessionErrorMessage = '';
  // Cleared after showing, so the same failure can be reported again if it
  // recurs — a store holding the last error forever would show it only once.
  $: if ($sessionError) {
    sessionErrorMessage = $sessionError;
    showSessionError = true;
    sessionError.set(null);
  }
  // The same treatment for failures reported from anywhere else — settings
  // saves in particular, which used to reload the UI without a word.
  $: if ($appError) {
    sessionErrorMessage = $appError;
    showSessionError = true;
    appError.set(null);
  }

  let showGitHistory = false;
  let showRecoveryCenter = false;
  let showCommandPalette = false;
  /** Saved-command library: Ctrl+P picker and its editor. */
  let showCommandPicker = false;
  let showCommandManager = false;
  /** The picker closes itself before handing over to the editor. */
  function openCommandManager() {
    showCommandManager = true;
  }
  /** Session templates: the manager, and the template it should open on. */
  let showTemplateDialog = false;
  let templateToUse = '';
  let showColorDialog = false;
  let colorDialogSession: Session | null = null;
  let showDeleteConfirm = false;
  let showQuitConfirm = false;
  /** True from the moment quitting is confirmed until the window goes away. */
  let quitting = false;
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
  let pendingResumePath: string | null = null;

  // Track "any dialog open" to restore terminal focus after the last one closes.
  // Without this, closing a dialog leaves focus on the dialog's overlay/buttons,
  // so subsequent keystrokes don't reach the terminal.
  let anyDialogOpen = false;
  let prevAnyDialogOpen = false;
  $: anyDialogOpen =
    showNewSessionDialog || showNewGroupDialog || showGlobalSearch || showBgAgents ||
    showHelpDialog || showUpdateDialog || showImportDialog || showFileImportDialog ||
    showSettingsDialog || showRecoveryCenter || showCommandPalette || showColorDialog || showDeleteConfirm ||
    showLogDialog || showQuickJump || showGitHistory || quickJumpPrompt || quickJumpNaming ||
    showCommandPicker || showCommandManager || showTemplateDialog ||
    showQuitConfirm || showStopDialog || showStartDialog ||
    showResumeChoice || showResumeSessionPicker;
  $: if (prevAnyDialogOpen && !anyDialogOpen) {
    // Dialog just closed — return focus to the terminal
    focusTerminal();
  }
  $: prevAnyDialogOpen = anyDialogOpen;

  // Sidebar state
  const SIDEBAR_MIN = 200;
  const SIDEBAR_MAX = 500;
  const SIDEBAR_COLLAPSED = 40;
  const SIDEBAR_DEFAULT = 288;
  const SIDEBAR_STORAGE_KEY = 'asmgr.sidebar.width';

  function readStoredSidebarWidth(): number {
    const raw = Number(localStorage.getItem(SIDEBAR_STORAGE_KEY));
    if (!Number.isFinite(raw) || raw <= 0) return SIDEBAR_DEFAULT;
    return Math.min(SIDEBAR_MAX, Math.max(SIDEBAR_MIN, raw));
  }

  let sidebarWidth = readStoredSidebarWidth();
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

  function storeSidebarWidth(px: number) {
    try {
      localStorage.setItem(SIDEBAR_STORAGE_KEY, String(Math.round(px)));
    } catch {
      // A full or disabled storage isn't worth surfacing; the width simply
      // won't be remembered.
    }
  }

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
    storeSidebarWidth(sidebarWidth);
  }

  // Double-click the handle to get the default width back, as the diff view's
  // splitter already does. Persisted too — without that, "reset" and "restart"
  // would do the same thing and the width would never have been yours.
  function resetSidebarWidth() {
    sidebarWidth = SIDEBAR_DEFAULT;
    sidebarCollapsed = false;
    storeSidebarWidth(SIDEBAR_DEFAULT);
  }

  function toggleSidebar() {
    if (!sidebarCollapsed) {
      collapseBlockUntil = Date.now() + 400;
    }
    sidebarCollapsed = !sidebarCollapsed;
    sidebarOverlayOpen = false;
  }

  $: actualSidebarWidth = sidebarCollapsed ? SIDEBAR_COLLAPSED : sidebarWidth;

  // Tabs currently waiting for user input — drives the header ⏳ badge and
  // the attention inbox. Per-TAB granularity via tabStatuses; sessions whose
  // tab list doesn't surface the waiting state fall back to a window-0 row.
  interface WaitingTab {
    sessionId: string;
    sessionName: string;
    color: string;
    windowIdx: number;
    tabName: string;
    statusLine: string;
  }
  let showWaitingPanel = false;
  $: waitingTabs = $sessions.flatMap((s): WaitingTab[] => {
    if (s.status !== 'running') return [];
    const tabs = ($tabStatuses[s.id] || [])
      .filter(t => t.activity === 'waiting')
      .map(t => ({
        sessionId: s.id, sessionName: s.name, color: s.color || '',
        windowIdx: t.windowIdx, tabName: t.name || t.agent || '',
        statusLine: t.statusLine || '',
      }));
    if (tabs.length === 0 && $activities[s.id] === 'waiting') {
      return [{ sessionId: s.id, sessionName: s.name, color: s.color || '',
        windowIdx: 0, tabName: '', statusLine: $statusLines[s.id] || '' }];
    }
    return tabs;
  });
  $: if (waitingTabs.length === 0) showWaitingPanel = false;

  function jumpToWaiting(tab: WaitingTab) {
    showWaitingPanel = false;
    selectSession(tab.sessionId);
    selectWindow(tab.windowIdx);
    focusTerminal();
  }

  async function quickReply(tab: WaitingTab, action: string) {
    try {
      await QuickReplyTab(tab.sessionId, tab.windowIdx, action);
    } catch (e) {
      console.error('Quick reply failed:', e);
    }
  }

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
  /**
   * Which favourite slot a key event names, 1-9, or 0 for none.
   *
   * Reads e.code rather than e.key: with Shift held, a digit key reports a
   * punctuation character, and which one depends on the layout — on a
   * Hungarian keyboard Shift+2 is not "2" but a quote.
   */
  function favouriteSlot(e: KeyboardEvent): number {
    // Stops at 7: Ctrl+Shift+8 already toggles the favourite mark, named for
    // the "*" it produces on many layouts. Numbering past a gap would put a
    // badge on a row whose key does something else entirely.
    const m = /^Digit([1-7])$/.exec(e.code);
    return m ? Number(m[1]) : 0;
  }

  /**
   * The favourites in sidebar order, unfiltered.
   *
   * Deliberately not the `favourites` store, which narrows with the search
   * box: a shortcut whose target changes as you type would be unusable.
   */
  $: favouriteTargets = $sessions.filter(s => s.favorite);

  /**
   * Add what is on screen to the quick-jump list, asking which "it" is meant.
   *
   * The tab and the session are both reasonable answers and the app cannot
   * tell them apart from the keystroke: adding the tab pins one place, adding
   * the session lands on whichever tab was last open there. Guessing would be
   * wrong half the time, and this list is meant to be built deliberately —
   * its order assigns the number keys.
   */
  let quickJumpPrompt = false;

  /** 0 = this tab, 1 = the whole session. The tab leads, being the common
   *  case and the one the keystroke was pressed from. */
  let quickJumpChoice = 0;

  function addCurrentToQuickJump() {
    if (!$selectedSessionId) return;
    quickJumpChoice = 0;
    quickJumpPrompt = true;
  }

  function quickJumpPromptKeys(e: KeyboardEvent) {
    switch (e.key) {
      case 'ArrowDown':
      case 'ArrowRight':
        e.preventDefault();
        quickJumpChoice = 1;
        break;
      case 'ArrowUp':
      case 'ArrowLeft':
        e.preventDefault();
        quickJumpChoice = 0;
        break;
      case 'Enter':
        e.preventDefault();
        quickJumpAddChoice(quickJumpChoice === 0 ? ($selectedWindowIdx ?? 0) : -1);
        break;
    }
  }

  /**
   * Having chosen what to add, name it.
   *
   * Asked for rather than derived, because the tab's own name is "claude" or
   * "shell" in a dozen places at once and a list of those is unreadable. The
   * suggestion is what the entry would otherwise have been called, so pressing
   * Enter straight through costs nothing and still gives a sensible name.
   */
  let quickJumpNaming = false;
  let quickJumpWindowIdx = -1;
  let quickJumpName = '';

  /**
   * The context menus ask for the same naming dialog.
   *
   * They can name a session other than the selected one, so the target comes
   * with the event rather than being read from the selection.
   */
  let quickJumpTargetSession = '';

  function openQuickJumpNaming(sessionId: string, windowIdx: number) {
    quickJumpTargetSession = sessionId;
    quickJumpWindowIdx = windowIdx;
    quickJumpName = suggestedQuickJumpName(windowIdx, sessionId);
    quickJumpNaming = true;
  }

  /**
   * Which directory the commit history should open on.
   *
   * The session's own path is not it. A tab can be opened in a directory of its
   * own, and the pane may have been `cd`-ed somewhere else since — so browsing
   * history from a tab pointed at another repository showed the session's
   * history instead of the one on screen.
   *
   * Resolved from tmux, the same way the panel resolves the path it displays.
   * Falls back to the tab's configured directory, then to the session's.
   */
  let gitHistoryPath = '';

  async function resolveGitHistoryPath(): Promise<string> {
    const session = get(selectedSession);
    if (!session) return '';
    const windowIdx = get(selectedWindowIdx) ?? 0;
    try {
      const live = await GetTabWorkingDirectory(session.id, windowIdx);
      if (live) return live;
    } catch (e) {
      console.error('Failed to resolve the tab working directory:', e);
    }
    const followed = (session.followedWindows || []).find((f: any) => f.index === windowIdx);
    return followed?.work_dir || session.path || '';
  }

  async function handleShowGitHistory() {
    // Resolved before showing, so the dialog never opens against the previous
    // tab's repository and then swaps under the reader.
    gitHistoryPath = await resolveGitHistoryPath();
    showGitHistory = true;
  }

  function handleQuickJumpAdd(e: CustomEvent<{ sessionId: string; windowIdx: number }>) {
    openQuickJumpNaming(e.detail.sessionId, e.detail.windowIdx);
  }

  function quickJumpAddChoice(windowIdx: number) {
    quickJumpPrompt = false;
    openQuickJumpNaming($selectedSessionId ?? '', windowIdx);
  }

  /**
   * "{session} — {tab}" for a tab, the session's name for a session.
   *
   * A tab needs both halves: its own name says what it runs, and the session's
   * says which project it runs in, and neither alone identifies it in a list.
   */
  function suggestedQuickJumpName(windowIdx: number, sessionId?: string): string {
    const session = sessionId
      ? $sessions.find((s) => s.id === sessionId)
      : $selectedSession;
    if (!session) return '';
    if (windowIdx < 0) return session.name ?? '';

    const tab = (session.followedWindows ?? []).find((w: any) => w.index === windowIdx);
    const tabName = tab?.name || tab?.agent || (windowIdx === 0 ? (session.agent ?? '') : `#${windowIdx}`);
    return tabName ? `${session.name} - ${tabName}` : (session.name ?? '');
  }

  /** Appended at the end, so adding something never renumbers what is there. */
  async function confirmQuickJumpAdd() {
    quickJumpNaming = false;
    if (!quickJumpTargetSession) return;
    try {
      // A name left as suggested is stored as no name, so the entry keeps
      // following its session and tab rather than pinning what they are called
      // today.
      const chosen = quickJumpName.trim();
      const suggested = suggestedQuickJumpName(quickJumpWindowIdx, quickJumpTargetSession);
      await AddQuickJump(quickJumpTargetSession, quickJumpWindowIdx,
        chosen === suggested ? '' : chosen);
      showQuickJump = true;
    } catch (err) {
      console.error('Adding to the quick-jump list failed:', err);
    }
  }

  function jumpToFavourite(slot: number) {
    const target = favouriteTargets[slot - 1];
    if (!target) return;
    selectSession(target.id);
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && quickJumpNaming) {
      e.preventDefault();
      quickJumpNaming = false;
      return;
    }

    if (e.key === 'Escape' && quickJumpPrompt) {
      e.preventDefault();
      quickJumpPrompt = false;
      return;
    }

    // Close sidebar overlay on Escape
    if (e.key === 'Escape' && sidebarOverlayOpen) {
      sidebarOverlayOpen = false;
      return;
    }

    // FAST PATH: this handler runs in the capture phase on EVERY keystroke,
    // including ordinary typing into the terminal.
    //
    // A shortcut needs a modifier, so a plain key can leave immediately.
    //
    // This test comes BEFORE any DOM work on purpose. The two querySelector()
    // calls below walk a document holding thousands of xterm cell spans, and
    // running them on every character dominated the per-keystroke cost —
    // profiling showed ~48 keydown/s while typing pegging the main thread.
    // Rebinding cannot introduce a modifier-less shortcut: the editor refuses
    // one, for exactly this reason.
    if (!e.ctrlKey && !e.metaKey && !e.altKey) return;

    // While the editor is recording a combination, the keys pressed must not
    // also run the actions they are bound to.
    if ($capturingShortcut) return;

    const shortcutId = shortcutForEvent(e);
    const favouriteSlotNo = (e.ctrlKey || e.metaKey) && e.shiftKey && !e.altKey
      ? favouriteSlot(e) : 0;
    if (!shortcutId && favouriteSlotNo <= 0) return;

    // Don't handle shortcuts when any dialog is open
    const dialogOpen = showCommandPalette || document.querySelector('.dialog-overlay') !== null;

    // The palette and the command picker answer even from inside a dialog, so
    // they are handled before the dialog check swallows everything else.
    if (shortcutId === 'palette.open') {
      e.preventDefault();
      e.stopPropagation();
      if (!dialogOpen) showCommandPalette = true;
      return;
    }
    if (shortcutId === 'commands.picker') {
      e.preventDefault();
      e.stopPropagation();
      if (!dialogOpen) showCommandPicker = true;
      return;
    }
    if (favouriteSlotNo > 0) {
      e.preventDefault();
      e.stopPropagation();
      if (!dialogOpen) jumpToFavourite(favouriteSlotNo);
      return;
    }
    if (dialogOpen) return;

    // Don't handle shortcuts when dictation buffer panel is visible
    if (document.querySelector('.dictation-buffer')) return;

    switch (shortcutId) {
      case 'session.prev':
        e.preventDefault();
        selectPrevSession();
        return;
      case 'session.next':
        e.preventDefault();
        selectNextSession();
        return;
      case 'search.global':
        e.preventDefault();
        showGlobalSearch = true;
        return;
      case 'terminal.search':
        e.preventDefault();
        window.dispatchEvent(new CustomEvent('terminal:search-toggle'));
        return;
      case 'session.new':
        e.preventDefault();
        showNewSessionDialog = true;
        return;
      case 'group.new':
        e.preventDefault();
        showNewGroupDialog = true;
        return;
      case 'session.start':
        e.preventDefault();
        handleStart();
        return;
      case 'session.stop':
        e.preventDefault();
        if ($selectedSession && $selectedSession.status === 'running') {
          handleStop();
        }
        return;
      case 'session.delete':
        e.preventDefault();
        handleDelete();
        return;
      case 'session.favorite':
        e.preventDefault();
        if ($selectedSessionId) toggleFavorite($selectedSessionId);
        return;
      case 'history.show':
        e.preventDefault();
        e.stopPropagation();
        void handleShowGitHistory();
        return;
      case 'quickJump.open':
        e.preventDefault();
        // stopPropagation as well: preventDefault only cancels the browser's
        // own action, and xterm listens for keys itself. Without it Ctrl+J
        // still reached the terminal as LF — a newline in the agent's
        // composer every time the window was opened.
        e.stopPropagation();
        showQuickJump = true;
        return;
      case 'quickJump.add':
        e.preventDefault();
        e.stopPropagation();
        addCurrentToQuickJump();
        return;
      case 'help.show':
        e.preventDefault();
        showHelpDialog = true;
        return;
      case 'update.check':
        e.preventDefault();
        showUpdateDialog = true;
        return;
      case 'sessions.import':
        e.preventDefault();
        showImportDialog = true;
        return;
      case 'session.moveUp':
        e.preventDefault();
        if ($selectedSessionId) reorderSession($selectedSessionId, -1);
        return;
      case 'session.moveDown':
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

  function handleCommandStart() {
    handleStart();
  }

  function handleCommandStop() {
    handleStop();
  }

  function handleCommandTemplates(e: CustomEvent<{ templateId?: string }>) {
    handleTemplates(e.detail?.templateId || '');
  }

  // Open-task badge on the task button. Polled rather than pushed: the count
  // comes from reading every project's task file, which the backend has no
  // reason to watch continuously.
  let stopOpenTaskWatch: (() => void) | null = null;

  onMount(async () => {
    stopOpenTaskWatch = watchOpenCount();

    GetMultiplexerStatus().then((s) => {
      missingMultiplexer = s?.available === false
        ? { name: s.name, hint: s.hint || '', canInstall: !!s.canInstall }
        : null;
    }).catch(() => { /* an older backend has no such call; say nothing */ });

    // The backend checks for a release once a day, shortly after launch. It
    // only ever notifies — a dot on the update button, not a popup that
    // interrupts what the user is doing.
    EventsOn('update:available', (info: { version: string; current: string }) => {
      availableUpdate = info?.version || '';
    });

    // Show an update found by an earlier run straight away. The daily throttle
    // means today's launch may not check at all, and a pending update should
    // not disappear just because it was discovered yesterday.
    try {
      availableUpdate = await PendingUpdate();
    } catch {
      // Not worth surfacing: the background check will notice it again.
    }

    // Capture phase so the terminal (xterm) can't swallow Ctrl+Shift combos.
    window.addEventListener('keydown', handleKeydown, true);
    window.addEventListener('terminal-nav', handleTerminalNav as EventListener);
    window.addEventListener('quickjump:add', handleQuickJumpAdd as EventListener);
    window.addEventListener('git:show-history', handleShowGitHistory);
    window.addEventListener('command:start-selected', handleCommandStart);
    window.addEventListener('command:stop-selected', handleCommandStop);
    window.addEventListener('command:templates', handleCommandTemplates as EventListener);

    await Promise.all([
      loadProjects(),
      loadSessions(),
      loadSettings(),
      loadAgents()
    ]);

    // Load i18n translations from saved language setting
    const currentSettings = get(settings);
    await loadTranslations(currentSettings.language || 'en');

    // Reopen the session that was selected when the app last closed, and with
    // it the tab that session remembers. Validated against the list that
    // actually loaded: a session deleted since, or belonging to a project not
    // open now, must not leave the panel pointing at nothing.
    const remembered = currentSettings.restoreLastSession ? currentSettings.lastSessionId : '';
    if (remembered && !get(selectedSessionId) && get(sessions).some(s => s.id === remembered)) {
      selectSession(remembered);
    }

    // Check dev mode
    try { devMode = await IsDevMode(); } catch(_) {}

    // Detect a second instance holding this project's lock (store-backed so
    // it updates on project switches too).
    await refreshLockStatus();

    // Start combined sidebar polling (activities + status lines)
    startSidebarPolling();

    // Initialize dictation service and listen for state changes
    initDictation();
  });

  onDestroy(() => {
    stopOpenTaskWatch?.();
    window.removeEventListener('keydown', handleKeydown, true);
    window.removeEventListener('terminal-nav', handleTerminalNav as EventListener);
    window.removeEventListener('quickjump:add', handleQuickJumpAdd as EventListener);
    window.removeEventListener('git:show-history', handleShowGitHistory);
    window.removeEventListener('command:start-selected', handleCommandStart);
    window.removeEventListener('command:stop-selected', handleCommandStop);
    window.removeEventListener('command:templates', handleCommandTemplates as EventListener);
    stopSidebarPolling();
    EventsOff('update:available');
    EventsOff('dictation:state');
    EventsOff('dictation:error');
  });

  // Dictation failures surface here as a toast. Without one the only sign of
  // trouble was the indicator flipping to "recording" and going quiet — which
  // is what someone with no microphone sees.
  let dictationErrorMessage = '';
  let showDictationError = false;

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
        // The backend sends translation keys, so an unknown key still shows
        // something readable rather than an empty toast.
        const title = $t(`dictation.${error.title}`);
        const message = $t(`dictation.${error.message}`);
        dictationErrorMessage = message.startsWith('dictation.')
          ? (title.startsWith('dictation.') ? error.message : title)
          : message;
        showDictationError = true;
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

  // Export writes every session in the current project; the native save
  // dialog is where the user picks the file, so there is nothing to configure
  // here first.
  async function exportSessions() {
    showSettingsDialog = false;
    try {
      await ExportSessions([]);
    } catch (e) {
      exportError = String(e);
    }
  }

  function handleNewSession() {
    showNewSessionDialog = true;
  }

  function handleNewGroup() {
    showNewGroupDialog = true;
  }

  // Opening with no preselected template lands on the list; the palette passes
  // an id to jump straight to "create a session from this one".
  function handleTemplates(templateId = '') {
    templateToUse = templateId;
    showTemplateDialog = true;
  }

  // Tasks still open on the session about to be deleted.
  //
  // Looked up before the dialog opens rather than shown after it: the point is
  // to change the decision, and a warning that arrives once the work is gone is
  // not a warning. Only unfinished tasks count — a session whose work is all
  // done should close without an extra click, or the prompt becomes noise
  // people learn to dismiss.
  let pendingTasks: { title: string }[] = [];

  async function handleDelete() {
    if (!$selectedSession) return;
    try {
      pendingTasks = await UnfinishedTasksForSession($selectedSession.id);
    } catch {
      // A session with no task store is the common case, not an error. Failing
      // to check must not block deletion.
      pendingTasks = [];
    }
    showDeleteConfirm = true;
  }

  async function confirmDelete() {
    if (!$selectedSession) return;
    await deleteSession($selectedSession.id);
  }

  function handleQuit() {
    showQuitConfirm = true;
  }

  /**
   * Shutdown is not instant: the app saves where each session left off, detaches
   * its mirrors and reaps the processes it started, and on a machine with
   * several busy sessions that takes long enough to look like a hang.
   *
   * The overlay goes up first and the quit is asked for a frame later, so the
   * message is painted before the main thread is busy tearing things down —
   * requesting both in the same frame would show nothing at all.
   */
  async function confirmQuit() {
    quitting = true;
    // tick() first: setting the flag only queues the DOM update, and a frame
    // callback can run before Svelte has applied it — so the frame we waited
    // for would paint the old DOM, without the overlay in it.
    await tick();
    // Then two frames, so the painted overlay is on screen before Quit() takes
    // the main thread for the length of the teardown.
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
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

  async function handleResume() {
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
      // The tab's directory too: agents index their conversations by working
      // directory, so a tab opened elsewhere would otherwise be offered the
      // session directory's history.
      pendingResumePath = await resolveGitHistoryPath();
    } else {
      pendingResumeWindowIdx = null;
      pendingResumeAgent = null;
      pendingResumePath = null;
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

<svelte:window on:click={() => { if (showWaitingPanel) showWaitingPanel = false; }} />

<main class="app-container h-screen flex flex-col text-white overflow-hidden" style="--sidebar-width: {actualSidebarWidth}px" dir={$isRTL ? 'rtl' : 'ltr'}>
  {#if exportError}
    <div class="lock-banner export-error">
      <span>{exportError}</span>
      <button class="lock-dismiss" on:click={() => exportError = ''}>×</button>
    </div>
  {/if}
  {#if quitting}
    <!-- Covers everything, and is not dismissable: the app is on its way out
         and nothing behind this can be acted on any more. -->
    <div class="quit-overlay">
      <div class="quit-box">
        <!-- A still icon, not a spinner. Quit() holds the main thread for the
             whole teardown, so CSS animation stops the moment it starts — a
             frozen spinner says "hung", which is the opposite of what this is
             here to say. -->
        <svg class="quit-icon" width="26" height="26" viewBox="0 0 24 24" fill="none"
             stroke="currentColor" stroke-width="1.8" stroke-linecap="round">
          <path d="M18.36 6.64a9 9 0 1 1-12.73 0"/>
          <line x1="12" y1="2" x2="12" y2="12"/>
        </svg>
        <strong>{$t('quit.inProgress')}</strong>
        <span>{$t('quit.inProgressDetail')}</span>
      </div>
    </div>
  {/if}
  {#if missingMultiplexer}
    <div class="lock-banner multiplexer-missing">
      <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M12 9v4"/><path d="M12 17h.01"/>
        <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
      </svg>
      <span>
        {$t('multiplexer.missing', { name: missingMultiplexer.name, hint: missingMultiplexer.hint })}
        {#if multiplexerInstallError}
          — {$t('multiplexer.installFailed', { error: multiplexerInstallError })}
        {/if}
      </span>
      {#if missingMultiplexer.canInstall}
        <button class="multiplexer-install" on:click={installMultiplexer} disabled={installingMultiplexer}>
          {installingMultiplexer ? $t('multiplexer.installing') : $t('multiplexer.install')}
        </button>
      {/if}
    </div>
  {/if}
  {#if $otherInstancePID > 0 && !lockBannerDismissed}
    <div class="lock-banner">
      <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>
      </svg>
      <span>{$t('lock.otherInstance', { pid: $otherInstancePID })}</span>
      <button class="lock-dismiss" on:click={() => lockBannerDismissed = true}>×</button>
    </div>
  {/if}
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
      {#if $appView === 'dashboard'}
        <span class="header-session-name">{$t('dashboard.title')}</span>
      {:else if $appView === 'tasks'}
        <!-- The task view spans every project, so the project name and the
             session's git branch would both be describing something the page
             is not about. -->
        <span class="header-session-name">{$t('allTasks.title')}</span>
      {:else if $selectedSession}
        <span class="header-session-name" style={$selectedSession.color ? `color: ${$selectedSession.color}` : ''}>{$selectedSession.name}</span>
        {#if ($settings.gitBranchDisplay || 'header') === 'header'}
          <GitBranchBadge variant="header" />
        {/if}
      {/if}
    </div>

    <div class="flex items-center gap-3" style="--wails-draggable:no-drag">
      <div class="header-text-actions">
        <button class="btn btn-ghost" on:click={() => showGlobalSearch = true} title={$t('header.globalSearch')}>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="11" cy="11" r="8"/>
            <path d="M21 21l-4.35-4.35"/>
          </svg>
          {$t('app.search')}
        </button>
        <button class="btn btn-ghost palette-trigger" on:click={() => showCommandPalette = true} title={$t('palette.title')}>
          <!-- Corner-out arrow: this palette jumps you to a session or view. -->
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>
            <path d="M15 3h6v6M10 14L21 3"/>
          </svg>
          {$t('palette.title')}
        </button>
        <!-- The saved-command library, distinct from the palette above: that
             one jumps to sessions and views, this one runs shell commands. -->
        <button
          class="btn btn-ghost palette-trigger"
          on:click={() => showCommandPicker = true}
          title={$t('header.commands')}
        >
          <!-- Terminal prompt: this one runs shell commands. -->
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <rect x="3" y="4" width="18" height="16" rx="2"/>
            <path d="M7 9l3 3-3 3M13 15h4"/>
          </svg>
          {$t('header.commands')}
        </button>
      </div>
      <div class="header-divider-vertical actions-divider"></div>
      <div class="header-icons">
      {#if waitingTabs.length > 0}
        <!-- Attention inbox: tabs waiting for user input, with one-click
             answers so none of them gets forgotten. -->
        <div class="waiting-wrap">
          <button class="btn waiting-badge" on:click|stopPropagation={() => showWaitingPanel = !showWaitingPanel}
            title={$t('header.waitingSessions', { n: waitingTabs.length })}>
            ⏳ {waitingTabs.length}
          </button>
          {#if showWaitingPanel}
            <div class="waiting-panel" on:click|stopPropagation>
              {#each waitingTabs as tab (tab.sessionId + ':' + tab.windowIdx)}
                <div class="waiting-row">
                  <button class="waiting-target" on:click={() => jumpToWaiting(tab)}
                    title={$t('waiting.jump')}>
                    <span class="waiting-name" style={tab.color && !tab.color.startsWith('gradient-') ? `color:${tab.color}` : ''}>{tab.sessionName}</span>
                    {#if tab.tabName}<span class="waiting-tab">› {tab.tabName}</span>{/if}
                    {#if tab.statusLine}<span class="waiting-status">{tab.statusLine}</span>{/if}
                  </button>
                  <div class="waiting-actions">
                    <button title="Enter" on:click={() => quickReply(tab, 'enter')}>↵</button>
                    <button title="y + Enter" on:click={() => quickReply(tab, 'y')}>y</button>
                    <button title="n + Enter" on:click={() => quickReply(tab, 'n')}>n</button>
                    <button title="Esc" on:click={() => quickReply(tab, 'esc')}>Esc</button>
                  </div>
                </div>
              {/each}
            </div>
          {/if}
        </div>
      {/if}
        <button class="btn btn-ghost btn-icon" on:click={() => showBgAgents = true} title={$t('bgAgents.open')}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <rect x="5" y="7" width="14" height="12" rx="2"/>
            <path d="M12 7V4M8 4h8"/>
            <circle cx="9.5" cy="12.5" r="0.5" fill="currentColor"/>
            <circle cx="14.5" cy="12.5" r="0.5" fill="currentColor"/>
            <path d="M9 16h6"/>
          </svg>
        </button>
        <!-- Only shown when there is something to install: an icon that appears
             for a reason is easier to notice than one that is always there. -->
        {#if availableUpdate}
          <button
            class="btn btn-ghost btn-icon update-btn has-update"
            on:click={() => { showUpdateDialog = true; }}
            title={$t('header.updateAvailable', { version: availableUpdate })}
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
              <polyline points="7 10 12 15 17 10"/>
              <line x1="12" y1="15" x2="12" y2="3"/>
            </svg>
            <span class="update-dot"></span>
          </button>
        {/if}
        <button class="btn btn-ghost btn-icon" on:click={() => showHelpDialog = true} title={$t('header.help')}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="12" cy="12" r="10"/>
            <path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/>
            <circle cx="12" cy="17" r="1" fill="currentColor"/>
          </svg>
        </button>
        <button
          class="btn btn-ghost btn-icon task-button"
          class:active-view={$appView === 'tasks'}
          on:click={() => { if ($appView === 'tasks') { goBack(); void refreshOpenCount(); } else { showTasksView(); } }}
          title={$t('allTasks.title')}
        >
          <!-- A checklist, drawn to match the other header icons: same 16px
               box, same stroke weight, currentColor so it follows the theme.
               An emoji here rendered in the font's own colour and size and
               stood out against every neighbour. -->
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M9 11l3 3L22 4"/>
            <path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11"/>
          </svg>
          {#if $openTaskCount > 0}
            <!-- The number, not just a dot: "three left" and "thirty" call for
                 different reactions. Capped at 99 so a long backlog cannot
                 stretch the header. -->
            <span class="task-badge">{$openTaskCount > 99 ? '99+' : $openTaskCount}</span>
          {/if}
        </button>
        <button class="btn btn-ghost btn-icon" on:click={() => showSettingsDialog = true} title={$t('header.settings')}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="12" cy="12" r="3"/>
            <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"/>
          </svg>
        </button>
        <button class="btn btn-ghost btn-icon" on:click={() => showRecoveryCenter = true} title={$t('recovery.open')}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <polyline points="3 6 5 6 21 6"/>
            <path d="M19 6l-1 15H6L5 6m3 0V3h8v3"/>
            <path d="M9 11h6M9 15h6"/>
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
        <!-- svelte-ignore a11y-no-static-element-interactions -->
        <div
          class="resize-handle"
          on:mousedown={startResize}
          on:dblclick={resetSidebarWidth}
          title={$t('sidebar.resizeHint')}
        ></div>
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
      <div class="main-view" class:hidden-view={$appView !== 'session'}>
        <MainPanel
          visible={$appView === 'session'}
          terminalFocusAllowed={!anyDialogOpen}
          on:openColorDialog={() => { colorDialogSession = $selectedSession; showColorDialog = true; }}
          on:requestStop={handleStop}
          on:requestStart={handleStart}
          on:requestResume={handleResume}
        />
      </div>
      {#if $appView === 'dashboard'}
        <div class="main-view">
          <ProjectDashboard on:newSession={handleNewSession} />
        </div>
      {:else if $appView === 'tasks'}
        <div class="main-view">
          <AllTasks />
        </div>
      {/if}
    </div>
  </div>

  <!-- Floats above everything, so an action taken in any view can be taken
       back from where it happened. -->
  <UndoToast />

  <!-- Dialogs -->
  <NewSessionDialog bind:show={showNewSessionDialog} />
  <NewGroupDialog bind:show={showNewGroupDialog} />
  <BgAgentsDialog bind:show={showBgAgents} />
<GlobalSearchDialog bind:show={showGlobalSearch} />
  <HelpDialog bind:show={showHelpDialog} />
  <UpdateDialog
    bind:show={showUpdateDialog}
    on:installed={() => availableUpdate = ''}
    on:checked={(e) => availableUpdate = e.detail}
  />
  <ImportDialog bind:show={showImportDialog} />
  <SessionFileDialog bind:show={showFileImportDialog} />
  <SettingsDialog
    bind:show={showSettingsDialog}
    on:dictationEnabledChange={handleDictationEnabledChange}
    on:openUpdate={() => { showSettingsDialog = false; showUpdateDialog = true; }}
    on:openLogs={() => { showSettingsDialog = false; showLogDialog = true; }}
    on:openImport={() => { showSettingsDialog = false; showImportDialog = true; }}
    on:openFileImport={() => { showSettingsDialog = false; showFileImportDialog = true; }}
    on:exportSessions={exportSessions}
  />
  <RecoveryCenterDialog bind:show={showRecoveryCenter} />
  <CommandPalette bind:show={showCommandPalette} />
  <CommandPickerDialog
    bind:show={showCommandPicker}
    sessionId={$selectedSessionId || ''}
    windowIdx={$selectedWindowIdx}
    onOpenManager={openCommandManager}
  />
  <CommandManagerDialog bind:show={showCommandManager} />
  <SessionTemplateDialog bind:show={showTemplateDialog} useTemplateId={templateToUse} />
  <!-- Opened from the tab bar's colour button. The sidebar's context-menu
       entry opens its own instance from SessionItem, which is rendered in
       three places and would otherwise have to forward the event up. -->
  <SessionColorDialog bind:show={showColorDialog} session={colorDialogSession} />
  <ConfirmDialog
    bind:show={showDeleteConfirm}
    title={$t('confirm.deleteSession')}
    message={pendingTasks.length
      ? $t('confirm.deleteSessionMessage', { name: $selectedSession?.name || '' }) + '\n\n' +
        $t('confirm.deleteSessionTasks', { count: String(pendingTasks.length) }) + '\n' +
        pendingTasks.slice(0, 5).map((task) => '• ' + task.title).join('\n') +
        (pendingTasks.length > 5 ? '\n…' : '')
      : $t('confirm.deleteSessionMessage', { name: $selectedSession?.name || '' })}
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
  <LogDialog bind:show={showLogDialog} />

  {#if quickJumpPrompt}
    <!-- svelte-ignore a11y-click-events-have-key-events -->
    <div
      class="dialog-overlay"
      on:click|self={() => (quickJumpPrompt = false)}
      on:keydown={quickJumpPromptKeys}
    >
      <div class="dialog-content quick-add">
        <div class="dialog-header">
          <h2>{$t('quickJump.addTitle')}</h2>
          <button class="close-btn" on:click={() => (quickJumpPrompt = false)}
            aria-label={$t('common.close')}>
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <line x1="18" y1="6" x2="6" y2="18"/>
              <line x1="6" y1="6" x2="18" y2="18"/>
            </svg>
          </button>
        </div>

        <div class="quick-add-body" use:autoFocusDialog>
          <p class="quick-add-question">{$t('quickJump.addQuestion')}</p>
          <button
            class="quick-add-choice"
            class:chosen={quickJumpChoice === 0}
            on:click={() => quickJumpAddChoice($selectedWindowIdx ?? 0)}
            on:mouseenter={() => (quickJumpChoice = 0)}
            on:focus={() => (quickJumpChoice = 0)}
          >
            <strong>{$t('quickJump.addTab')}</strong>
            <span>{$t('quickJump.addTabDesc')}</span>
          </button>
          <button
            class="quick-add-choice"
            class:chosen={quickJumpChoice === 1}
            on:click={() => quickJumpAddChoice(-1)}
            on:mouseenter={() => (quickJumpChoice = 1)}
            on:focus={() => (quickJumpChoice = 1)}
          >
            <strong>{$t('quickJump.addSession')}</strong>
            <span>{$t('quickJump.addSessionDesc')}</span>
          </button>
        </div>

        <div class="dialog-footer">
          <button class="btn-cancel" on:click={() => (quickJumpPrompt = false)}>
            {$t('common.cancel')}
          </button>
        </div>
      </div>
    </div>
  {/if}

  {#if quickJumpNaming}
    <!-- svelte-ignore a11y-click-events-have-key-events -->
    <div class="dialog-overlay" on:click|self={() => (quickJumpNaming = false)}>
      <div class="dialog-content quick-add">
        <div class="dialog-header">
          <h2>{$t('quickJump.nameTitle')}</h2>
          <button class="close-btn" on:click={() => (quickJumpNaming = false)}
            aria-label={$t('common.close')}>
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <line x1="18" y1="6" x2="6" y2="18"/>
              <line x1="6" y1="6" x2="18" y2="18"/>
            </svg>
          </button>
        </div>

        <div class="quick-add-body" use:autoFocusField>
          <p class="quick-add-question">{$t('quickJump.nameQuestion')}</p>
          <input
            class="quick-name-input"
            bind:value={quickJumpName}
            placeholder={$t('quickJump.namePlaceholder')}
            on:keydown={(e) => {
              if (e.key === 'Enter') { e.preventDefault(); confirmQuickJumpAdd(); }
              else if (e.key === 'Escape') { e.preventDefault(); quickJumpNaming = false; }
            }}
          />
        </div>

        <div class="dialog-footer">
          <button class="btn-cancel" on:click={() => (quickJumpNaming = false)}>
            {$t('common.cancel')}
          </button>
          <button class="btn-primary" on:click={confirmQuickJumpAdd}>
            {$t('common.save')}
          </button>
        </div>
      </div>
    </div>
  {/if}

  <GitHistoryDialog bind:show={showGitHistory} path={gitHistoryPath} />

  <QuickJumpDialog
    bind:show={showQuickJump}
    on:jump={(e) => {
      selectSession(e.detail.sessionId);
      // A negative index means the entry is the session itself, so whichever
      // tab was last open there is the right destination.
      if (e.detail.windowIdx >= 0) selectWindow(e.detail.windowIdx);
      focusTerminal();
    }}
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
    hasTabs={(pendingResumeSession?.followedWindows?.length || 0) > 0}
    on:newSession={handleResumeNewSession}
    on:continueExisting={handleResumeContinueExisting}
    on:restartWithTabs={handleResumeRestartWithTabs}
    on:cancel={handleResumeCancel}
  />
  <ResumeSessionPickerDialog
    bind:show={showResumeSessionPicker}
    session={pendingResumeSession}
    agentOverride={pendingResumeAgent}
    pathOverride={pendingResumePath}
    on:select={handleResumeSessionSelect}
    on:cancel={handleResumeCancel}
  />
</main>

<Toast bind:show={showDictationError} message={dictationErrorMessage} variant="error" duration={9000} />

<!-- Every failure the sessions store records.
     It had 26 writers and no reader, so anything failing outside a component
     with its own toast — deleting from the sidebar, renaming, reordering,
     switching project — failed in silence. -->
<Toast bind:show={showSessionError} message={sessionErrorMessage} variant="error" duration={9000} />

<style>
.lock-banner {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 16px;
    background: rgba(251, 146, 60, 0.14);
    border-bottom: 1px solid rgba(251, 146, 60, 0.3);
    color: #fdba74;
    font-size: 13px;
    flex-shrink: 0;
  }
  .lock-banner svg { flex-shrink: 0; }
  .lock-banner span { flex: 1; }
  .lock-dismiss {
    border: 0; background: transparent; color: #fdba74;
    cursor: pointer; font-size: 18px; line-height: 1; padding: 0 4px;
  }
  .lock-dismiss:hover { color: #fff; }

    .waiting-badge {
    color: #67e8f9;
    background: rgba(0, 206, 209, 0.12);
    border: 1px solid rgba(0, 206, 209, 0.3);
    border-radius: 8px;
    padding: 5px 10px;
    font-size: 13px;
    font-weight: 650;
    cursor: pointer;
    transition: background 0.15s ease;
  }
  .waiting-badge:hover {
    background: rgba(0, 206, 209, 0.22);
  }
  .waiting-wrap { position: relative; display: flex; }
  .waiting-panel {
    position: absolute;
    top: calc(100% + 8px);
    right: 0;
    z-index: 100;
    min-width: 380px;
    max-width: 520px;
    max-height: 60vh;
    overflow-y: auto;
    padding: 6px;
    border-radius: 10px;
    border: 1px solid rgba(0, 206, 209, 0.25);
    background: rgba(12, 12, 20, 0.98);
    box-shadow: 0 12px 34px rgba(0, 0, 0, 0.5);
  }
  .waiting-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 6px;
    border-radius: 7px;
  }
  .waiting-row:hover { background: rgba(0, 206, 209, 0.07); }
  .waiting-target {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: baseline;
    gap: 7px;
    background: none;
    border: 0;
    padding: 4px 2px;
    text-align: left;
    cursor: pointer;
  }
  .waiting-name { color: #e4e4e7; font-size: 13px; font-weight: 650; white-space: nowrap; }
  .waiting-tab { color: #8b8b95; font-size: 12px; white-space: nowrap; }
  .waiting-status { color: #67e8f9; font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
  .waiting-actions { display: flex; gap: 4px; flex-shrink: 0; }
  .waiting-actions button {
    min-width: 26px;
    padding: 3px 6px;
    border-radius: 6px;
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(255, 255, 255, 0.05);
    color: #d4d4d8;
    font-size: 12px;
    cursor: pointer;
  }
  .waiting-actions button:hover { border-color: rgba(0, 206, 209, 0.5); color: #67e8f9; }

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

  /* Scrollbars are defined once, in style.css. A second global rule lived here
     at 6px and, being loaded after that file, quietly won everywhere — which is
     why widening the one in style.css appeared to do nothing at all. */

  .app-container {
    background: linear-gradient(135deg, #0a0a0f 0%, #0f0f1a 50%, #0a0a0f 100%);
  }

  .main-content {
    position: relative;
  }

  .main-view {
    position: absolute;
    inset: 0;
    overflow: hidden;
  }

  .hidden-view {
    visibility: hidden;
    pointer-events: none;
  }

  .btn.active-view {
    color: var(--accent-lighter);
    background: rgba(var(--accent-rgb), 0.15);
    border-color: rgba(var(--accent-rgb), 0.3);
  }

  .header-text-actions {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .header-text-actions .btn {
    flex-shrink: 0;
  }

  .header {
    background: linear-gradient(180deg, rgba(var(--accent-rgb), 0.08) 0%, transparent 100%);
    border-bottom: 1px solid rgba(var(--accent-rgb), 0.15);
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
    font-size: 10px;
    font-weight: 500;
    color: var(--accent-light);
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

  .header-divider-vertical.actions-divider {
    height: 20px;
    align-self: auto;
    margin: 0 8px;
  }

  /* The count of open tasks, on the task button.
     The button needs position:relative for the badge to anchor to it —
     .btn-icon does not set it, and without it the badge positioned against the
     page instead and sat outside the button entirely. */
  .task-button {
    position: relative;
  }

  .task-badge {
    position: absolute;
    /* Tucked into the corner rather than hung outside it: the header buttons
       sit close together, and a badge overhanging by its own width overlapped
       the neighbouring one. */
    top: 1px;
    right: 1px;
    min-width: 13px;
    height: 13px;
    padding: 0 3px;
    box-sizing: border-box;
    border-radius: 999px;
    /* Neutral, not red: this counts work outstanding, not something wrong. */
    background: rgba(107, 114, 128, 0.95);
    color: #fff;
    font-size: 9px;
    font-weight: 600;
    line-height: 13px;
    text-align: center;
    /* The icon is drawn with a 2px stroke; a hairline of the button's own
       background keeps the badge from touching it. */
    box-shadow: 0 0 0 1.5px var(--bg-primary, #0f172a);
    pointer-events: none;
  }

  .header-session-name {
    font-size: 14px;
    font-weight: 500;
    color: #d4d4d8;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
    /* Shrinks before the branch badge does, so a long session name truncates
       instead of pushing the branch out of the header. */
    flex: 0 1 auto;
  }

  .sidebar {
    position: relative;
    background: linear-gradient(180deg, rgba(15, 15, 26, 0.9) 0%, rgba(10, 10, 15, 0.95) 100%);
    border-right: 1px solid rgba(var(--accent-rgb), 0.1);
    box-shadow: 4px 0 24px rgba(0, 0, 0, 0.3);
  }

  .sidebar.collapsed {
    overflow: hidden;
  }

  .resizing {
    cursor: col-resize;
    user-select: none;
  }

  /* Widened to match the splitters in the diff, file browser and history
     dialog. Absolutely positioned, so this takes no width from the sidebar —
     it straddles the edge, half over the sidebar and half over the panel. */
  .resize-handle {
    position: absolute;
    top: 0;
    right: -5px;
    width: 11px;
    height: 100%;
    cursor: col-resize;
    z-index: 10;
  }

  .resize-handle:hover {
    background: rgba(var(--accent-rgb), 0.3);
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
    background: rgba(var(--accent-rgb), 0.15);
    border-color: rgba(var(--accent-rgb), 0.3);
    color: var(--accent-light);
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
    background: rgba(var(--accent-rgb), 0.15);
    border-color: rgba(var(--accent-rgb), 0.3);
    color: var(--accent-light);
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

  /* A new release is worth noticing but never worth interrupting for, so it
     shows as a dot on the existing button rather than a dialog. */
  .export-error {
    background: rgba(239, 68, 68, 0.12);
    border-bottom-color: rgba(239, 68, 68, 0.3);
    color: #fca5a5;
  }
  /* Red rather than the lock banner's amber: nothing in the app works until
     this is resolved, where a locked project still allows everything but
     terminals. The install command is selectable so it can be copied. */
  .multiplexer-missing {
    background: rgba(239, 68, 68, 0.12);
    border-bottom-color: rgba(239, 68, 68, 0.3);
    color: #fca5a5;
    user-select: text;
  }
  .quit-overlay {
    position: fixed;
    inset: 0;
    z-index: 1000;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(17, 21, 30, 0.85);
    backdrop-filter: blur(2px);
  }
  .quit-box {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    padding: 26px 34px;
    border-radius: 10px;
    background: #1f2430;
    border: 1px solid rgba(255, 255, 255, 0.1);
    text-align: center;
    max-width: 380px;
  }
  .quit-box strong { font-size: 15px; }
  .quit-box span { font-size: 12px; opacity: 0.7; line-height: 1.5; }
  .quit-icon {
    color: rgba(var(--accent-rgb), 0.9);
  }

  .multiplexer-install {
    flex-shrink: 0;
    border: 1px solid rgba(239, 68, 68, 0.5);
    background: rgba(239, 68, 68, 0.15);
    color: #fecaca;
    border-radius: 4px;
    padding: 3px 12px;
    font-size: 12px;
    cursor: pointer;
  }
  .multiplexer-install:hover:not(:disabled) { background: rgba(239, 68, 68, 0.28); }
  .multiplexer-install:disabled { opacity: 0.6; cursor: default; }

  .update-btn {
    position: relative;
  }
  .update-btn.has-update {
    color: #4ade80;
  }
  .update-dot {
    position: absolute;
    top: 5px;
    right: 5px;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: #4ade80;
    box-shadow: 0 0 0 2px rgba(15, 15, 26, 0.9);
  }

  .header-icons {
    display: flex;
    align-items: center;
    gap: 4px;
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
    border-left: 1px solid rgba(var(--accent-rgb), 0.1);
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
    border-right: 1px solid rgba(var(--accent-rgb), 0.15);
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
    border-left: 1px solid rgba(var(--accent-rgb), 0.15);
    box-shadow: -8px 0 32px rgba(0, 0, 0, 0.5);
  }

  :global([dir="rtl"]) .expand-btn svg {
    transform: scaleX(-1);
  }



  /* Matching the settings dialog rather than inheriting whatever the shared
     overlay provides, which drew a circle here. */
  .close-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    background: rgba(255, 255, 255, 0.05);
    border: none;
    border-radius: 8px;
    color: #6b7280;
    cursor: pointer;
    transition: all 0.2s ease;
  }
  .close-btn:hover {
    background: rgba(255, 255, 255, 0.1);
    color: white;
  }

  /* The quick-jump add prompt: two choices, stated plainly enough that neither
     needs thinking about. */
  .quick-add {
    width: min(460px, 92vw);
    max-width: min(460px, 92vw);
  }
  .quick-add-body {
    padding: 4px 20px 12px;
  }
  .quick-add-question {
    margin: 0 0 14px;
    opacity: 0.75;
    font-size: 0.9em;
  }
  .quick-add-choice {
    display: flex;
    flex-direction: column;
    gap: 3px;
    width: 100%;
    padding: 11px 14px;
    margin-bottom: 8px;
    text-align: left;
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 8px;
    background: rgba(255, 255, 255, 0.03);
    color: inherit;
    cursor: pointer;
  }
  /* Only `chosen` marks the selection. Focus is deliberately not styled here:
     the arrows move the choice without moving focus, so a focus ring left on
     the first button showed two options highlighted at once. Focus still
     follows a click or Tab, and those set `chosen` too. */
  .quick-add-choice.chosen {
    border-color: var(--accent, #61afef);
    background: rgba(255, 255, 255, 0.07);
  }
  .quick-add-choice:focus {
    outline: none;
  }
  .quick-add-choice span {
    font-size: 0.82em;
    opacity: 0.65;
  }

  /* The name field in the quick-jump add dialog. */
  .quick-name-input {
    width: 100%;
    padding: 9px 12px;
    border: 1px solid rgba(255, 255, 255, 0.14);
    border-radius: 8px;
    background: rgba(0, 0, 0, 0.28);
    color: inherit;
    font: inherit;
    outline: none;
  }
  .quick-name-input:focus {
    border-color: var(--accent, #61afef);
  }
</style>
