<script lang="ts">
  import { createEventDispatcher, onMount, onDestroy } from 'svelte';
  import { browserViewRequested, clearBrowserViewRequest } from '../../stores/fileJump';
  import TabBar from './TabBar.svelte';
  import Terminal from './Terminal.svelte';
  import Notes from './Notes.svelte';
  import Diff from './Diff.svelte';
  import FileBrowser from './FileBrowser.svelte';
  import TaskPanel from './TaskPanel.svelte';
  import ForkDialog from '../Dialogs/ForkDialog.svelte';
  import { sessions, selectedSessionId, selectedWindowIdx, selectSession, selectWindow, toggleAutoYes, cycleYoloMode } from '../../stores/sessions';
  import { agents } from '../../stores/agents';
  import { tabStatuses } from '../../stores/statusLines';
  // For the count on the tasks tab. TaskPanel is mounted whether or not its
  // view is open and loads on mount, so the number is right without anyone
  // having opened it.
  import { taskStats } from '../../stores/tasks';
  import { settings, saveSettings } from '../../stores/settings';
  import { activeProjectId } from '../../stores/projects';
  import { gitBranch, refreshGitBranch, revalidateGitBranch } from '../../stores/gitBranch';
  import GitBranchBadge from '../common/GitBranchBadge.svelte';
  import { get } from 'svelte/store';
  import { resolveViewBarHidden } from '../../utils/terminalThemes';
  import * as App from '../../../../wailsjs/go/main/App';
  import { t } from '../../i18n';
  import Toast from '../common/Toast.svelte';
  import { afterUnsavedChanges } from '../../stores/unsavedChanges';

  const dispatch = createEventDispatcher();

  export let visible = true;
  export let terminalFocusAllowed = true;

  // Diff is not one of these: it lives in the tab bar (fullDiffActive), so the
  // view bar never selects it.
  type ViewName = 'terminal' | 'notes' | 'tasks' | 'browser';
  let activeView: ViewName = 'terminal';
  let terminalAttached = false;
  let showForkDialog = false;
  let localNotesCache: Record<string, string> = {}; // project:sessionId:windowIdx -> notes
  let terminalComponent: Terminal;
  let fullDiffActive = false;
  let focusedTerminalPane: 'primary' | 'secondary' = 'primary';
  let lastPrimaryTarget = '';

  // Sidebar/session navigation always targets the primary pane. If the user
  // previously typed in the pinned secondary pane, hand focus ownership back
  // when the selected session or tab changes so input cannot remain stuck on
  // the old split target.
  $: {
    const primaryTarget = `${$selectedSessionId || ''}:${$selectedWindowIdx ?? 0}`;
    if (primaryTarget !== lastPrimaryTarget) {
      lastPrimaryTarget = primaryTarget;
      focusedTerminalPane = 'primary';
    }
  }

  $: markedSession = $sessions.find(s => s.id === $settings.markedSessionId) || null;
  $: markedWindowIdx = $settings.markedWindowIdx || 0;
  $: markedTabName = (() => {
    if (!markedSession) return '';
    if (markedWindowIdx === 0) return markedSession.name;
    const tab = markedSession.followedWindows?.find((w: any) => w.index === markedWindowIdx);
    return tab?.name || `${markedSession.name} · ${markedWindowIdx}`;
  })();
  $: splitDuplicate = !!markedSession &&
    markedSession.id === $selectedSessionId && markedWindowIdx === $selectedWindowIdx;
  $: splitEnabled = $settings.splitView && !!markedSession;

  function toggleSplitView() {
    if ($settings.splitView) {
      void saveSettings({ splitView: false, markedSessionId: '', markedWindowIdx: 0 });
      focusedTerminalPane = 'primary';
      return;
    }
    if (!$selectedSessionId) return;
    void saveSettings({
      splitView: true,
      markedSessionId: $selectedSessionId,
      markedWindowIdx: $selectedWindowIdx ?? 0
    });
  }

  function pinCurrentToSplit() {
    if (!$selectedSessionId) return;
    void saveSettings({
      splitView: true,
      markedSessionId: $selectedSessionId,
      markedWindowIdx: $selectedWindowIdx ?? 0
    });
  }

  function swapSplitTargets() {
    if (!markedSession || !$selectedSessionId) return;
    const target = {
      projectId: $activeProjectId,
      primarySessionId: $selectedSessionId,
      primaryWindowIdx: $selectedWindowIdx ?? 0,
      secondarySessionId: markedSession.id,
      secondaryWindowIdx: markedWindowIdx,
    };
    // A view owning an unsaved buffer (notably FileBrowser) must approve the
    // navigation before either half of the split is changed. Previously the
    // selection and pinned target were swapped first; cancelling FileBrowser's
    // later prompt restored only the selection and left both panes pointing at
    // the old primary session.
    afterUnsavedChanges(() => {
      // Confirmations are asynchronous. Do not apply an approved swap to a
      // replacement project, selection or split target that appeared while a
      // modal was open.
      if ($activeProjectId !== target.projectId ||
          !$settings.splitView ||
          $selectedSessionId !== target.primarySessionId ||
          ($selectedWindowIdx ?? 0) !== target.primaryWindowIdx ||
          $settings.markedSessionId !== target.secondarySessionId ||
          ($settings.markedWindowIdx ?? 0) !== target.secondaryWindowIdx ||
          !$sessions.some(session => session.id === target.secondarySessionId)) return;
      selectSession(target.secondarySessionId);
      selectWindow(target.secondaryWindowIdx);
      void saveSettings({
        splitView: true,
        markedSessionId: target.primarySessionId,
        markedWindowIdx: target.primaryWindowIdx
      });
      focusedTerminalPane = 'primary';
    });
  }

  function handleSetView(e: Event) {
    const view = (e as CustomEvent<{ view: 'terminal' | 'diff' | 'notes' | 'tasks' | 'browser' }>).detail?.view;
    if (!view) return;
    // Diff lives in the tab bar, not the view bar — route the palette there so
    // its "open diff" action still lands somewhere. Outside a git repo there
    // is no diff to open, so the request is simply ignored.
    if (view === 'diff') {
      // The tab's repository, not the session's: one session can hold tabs in
      // different directories, and the palette should open the diff the tab
      // actually has.
      if (tabIsGitRepo) {
        fullDiffActive = true;
      }
      return;
    }
    fullDiffActive = false;
    selectView(view);
  }

  function tabViewKey(sessionId: string | null, windowIdx: number): string {
    return `${sessionId || ''}:${windowIdx}`;
  }

  // The diff shown ABOVE the current view, rather than instead of it, so a
  // change can be read beside the agent that made it.
  let diffAbove = false;

  // Reset the view on tab/session change.
  //
  // This block deliberately does NOT read activeView. Doing so would make
  // activeView one of its dependencies, so every view change would re-enter it
  // and race with the assignment that caused it.
  let lastViewKey = '';
  $: {
    const key = tabViewKey($selectedSessionId, $selectedWindowIdx ?? 0);
    if (key !== lastViewKey) {
      lastViewKey = key;
      // Coming back to a tab shows its terminal, whatever was on screen when
      // you left it.
      //
      // The view used to be restored per tab, on the reasoning that the tab
      // itself is remembered so its view should be too. In use that read as the
      // app losing track: going agent tab → diff → another agent tab → back
      // brought up the diff, when what you returned for was the agent. A diff
      // or a notes panel is somewhere you go to look at something, not a place
      // the tab lives, and the cost of guessing wrong is high — the diff covers
      // the whole panel, so the agent you meant to check is not even behind it.
      //
      // The per-tab memories this used to keep went with it: nothing read them
      // once arrival stopped depending on them.
      fullDiffActive = false;
      diffAbove = false;
      activeView = 'terminal';
    }
  }

  // The diff asks for the browser when a file is opened from it. Handled here
  // because this is the component that owns which view is showing.
  $: if ($browserViewRequested) {
    clearBrowserViewRequest();
    // Leaving the full-screen diff as well as switching the view. The diff is
    // not one of the selectable views — it covers the panel and hides the
    // selector entirely — so selecting 'browser' underneath it changed which
    // view would show once the diff closed, and nothing on screen moved.
    fullDiffActive = false;
    selectView('browser');
  }

  function selectView(view: ViewName) {
    activeView = view;
  }

  // Height in pixels rather than a fraction. The pane below is a terminal
  // measured in whole rows, and a fraction of a resized window lands between
  // two of them — the diff creeps a pixel at a time and the terminal reflows
  // for it. A pixel height stays put when the window changes.
  const DIFF_ABOVE_DEFAULT = 300;
  const DIFF_ABOVE_MIN = 120;
  // Leaves room for a usable terminal underneath: without a floor the splitter
  // can be dragged until the pane below is a sliver that cannot be dragged back.
  const BELOW_MIN = 140;
  let diffAboveHeight = DIFF_ABOVE_DEFAULT;
  let draggingSplitter = false;
  let splitterProjectId = '';
  // Settings are project-scoped while this component survives project
  // switches. Re-apply B's saved height after A -> B instead of keeping A's
  // pixels and eventually saving them over B's preference. The key changes
  // only when the authoritative setting/project changes, so ordinary pointer
  // movement is never fought by the reactive block.
  let restoredHeightKey = '';
  $: {
    const storedHeight = $settings.diffAboveHeight || DIFF_ABOVE_DEFAULT;
    const heightKey = `${$activeProjectId}:${storedHeight}`;
    if (!draggingSplitter && heightKey !== restoredHeightKey) {
      restoredHeightKey = heightKey;
      diffAboveHeight = storedHeight;
    }
  }
  let activeSplitterCleanup: (() => void) | null = null;

  function toggleDiffAbove() {
    if (!tabIsGitRepo) return;
    diffAbove = !diffAbove;
    if (!lastViewKey) return;
  }

  function startSplitterDrag(event: MouseEvent) {
    activeSplitterCleanup?.();
    event.preventDefault();
    draggingSplitter = true;
    splitterProjectId = $activeProjectId;
    const stackTop = (event.currentTarget as HTMLElement)
      .parentElement?.getBoundingClientRect().top ?? 0;
    const stackHeight = (event.currentTarget as HTMLElement)
      .parentElement?.getBoundingClientRect().height ?? 0;

    const onMove = (move: MouseEvent) => {
      const wanted = move.clientY - stackTop;
      const ceiling = Math.max(DIFF_ABOVE_MIN, stackHeight - BELOW_MIN);
      diffAboveHeight = Math.min(Math.max(wanted, DIFF_ABOVE_MIN), ceiling);
    };
    const cleanup = () => {
      draggingSplitter = false;
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
      if (activeSplitterCleanup === cleanup) activeSplitterCleanup = null;
    };
    const onUp = () => {
      const projectId = splitterProjectId;
      splitterProjectId = '';
      cleanup();
      // A project switch can complete while the pointer is held down. The
      // pixels belong to the project where the gesture began, never whichever
      // project happens to be active at mouseup.
      if (projectId !== $activeProjectId) return;
      void saveSettings({ diffAboveHeight }, projectId);
    };
    activeSplitterCleanup = cleanup;
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
  }

  function resetDiffHeight() {
    diffAboveHeight = DIFF_ABOVE_DEFAULT;
    void saveSettings({ diffAboveHeight });
  }

  // Leaves activeView alone: hiding the diff reveals whichever view the tab was
  // already on — you opened the diff over notes, so closing it puts notes back.
  // Clicking the tab you are on asks for the terminal separately, through
  // showTerminal.
  function closeFullDiff() {
    fullDiffActive = false;
  }

  /**
   * The view bar's Diff button.
   *
   * A toggle rather than a plain select, because the diff covers the panel
   * instead of sitting in the view rotation: pressing it again has to give the
   * tab back, and the view underneath is whatever it was — closeFullDiff
   * deliberately leaves activeView alone.
   */
  // Never leave the diff open on a tab that has none.
  //
  // Switching to a tab outside a repository would otherwise show an empty diff
  // over it, with the button that opened it now greyed out — no way back except
  // guessing. Guarded on the tab rather than the session, since that is what
  // the button is guarded on.
  $: if (fullDiffActive && !tabIsGitRepo) {
    closeFullDiff();
  }

  function toggleFullDiff() {
    if (!tabIsGitRepo) return;
    if (fullDiffActive) {
      closeFullDiff();
      return;
    }
    fullDiffActive = true;
  }

  onMount(() => window.addEventListener('main-panel:set-view', handleSetView));
  onDestroy(() => {
    window.removeEventListener('main-panel:set-view', handleSetView);
    activeSplitterCleanup?.();
  });

  // Regaining focus is when the directory and the branch are most likely to
  // have changed behind our back — the user just switched away to a terminal
  // or an editor. The directory is re-resolved first; if it moved, the reactive
  // chain refreshes the branch for the new one, so revalidate only has to cover
  // the case where the directory stayed put but its branch changed.
  function revalidateOnFocus() {
    startPathPolling();
    void refreshLiveTabPath();
    void revalidateGitBranch();
  }

  // A pane's directory moves without the app being told: `cd`, but equally
  // pushd/popd, a script, or the agent itself running a shell command. There is
  // no event to hook, so the only way to follow it is to ask periodically.
  //
  // Only ever for the tab on screen, and only while the window has focus —
  // stopping on blur is what keeps this off the background entirely. The 2s
  // period matches the activity/status-line pollers this app already runs, and
  // one tmux query costs ~4ms.
  const PATH_POLL_MS = 2000;
  let pathPollTimer: ReturnType<typeof setInterval> | null = null;

  function startPathPolling() {
    if (pathPollTimer) return;
    pathPollTimer = setInterval(() => {
      void refreshLiveTabPath();
    }, PATH_POLL_MS);
  }

  function stopPathPolling() {
    if (!pathPollTimer) return;
    clearInterval(pathPollTimer);
    pathPollTimer = null;
  }

  onMount(() => {
    window.addEventListener('focus', revalidateOnFocus);
    window.addEventListener('blur', stopPathPolling);
    // document.hasFocus() rather than assuming: the panel can mount while the
    // window is already in the background.
    if (document.hasFocus()) startPathPolling();
  });
  onDestroy(() => {
    window.removeEventListener('focus', revalidateOnFocus);
    window.removeEventListener('blur', stopPathPolling);
    stopPathPolling();
  });

  // Check if current session supports fork (Claude only)
  function getCurrentSession() {
    const id = get(selectedSessionId);
    if (!id) return null;
    return get(sessions).find(s => s.id === id) || null;
  }

  // The bar's visibility resolves tab → global, like the font size. The tab
  // value is tri-state so "show this one" survives a global hide.
  // The agent of the tab on screen, which is not the session's agent once a
  // session has tabs of its own — a Codex tab inside a Claude session is
  // still Codex.
  $: currentTabAgent = (() => {
    const s = currentSession;
    if (!s) return '';
    const idx = $selectedWindowIdx ?? 0;
    if (idx === (s.mainWindowIndex ?? 0)) return s.agent;
    const fw = (s.followedWindows || []).find((f: any) => f.index === idx);
    return fw?.agent || s.agent;
  })();

  // Same for the path: a tab can be opened in its own directory, so showing
  // the session's would be wrong there. This is only what the tab was
  // CONFIGURED with — the pane may have been `cd`-ed elsewhere since.
  $: configuredTabPath = (() => {
    const s = currentSession;
    if (!s) return '';
    const idx = $selectedWindowIdx ?? 0;
    if (idx === (s.mainWindowIndex ?? 0)) return s.path;
    const fw = (s.followedWindows || []).find((f: any) => f.index === idx);
    return fw?.work_dir || s.path;
  })();

  // The pane's real directory, resolved from tmux. Empty until the first
  // answer arrives, and reset on every tab change so the previous tab's
  // directory can never be shown against the new one.
  let liveTabPath = '';
  let liveTabPathTarget = '';
  let liveTabPathGeneration = 0;

  async function refreshLiveTabPath() {
    const sessionId = $selectedSessionId;
    const windowIdx = $selectedWindowIdx ?? 0;
    if (!sessionId) {
      liveTabPath = '';
      return;
    }
    const generation = ++liveTabPathGeneration;
    try {
      const resolved = await App.GetTabWorkingDirectory(sessionId, windowIdx);
      // A slow reply for a tab the user has already left must not overwrite
      // the answer for the tab now on screen.
      if (generation !== liveTabPathGeneration) return;
      liveTabPath = resolved || '';
    } catch (e) {
      console.error('Failed to resolve the tab working directory:', e);
      if (generation === liveTabPathGeneration) liveTabPath = '';
    }
  }

  // Session and tab changes both land here. The guard variable is assigned
  // INSIDE the block: Svelte orders reactive statements by dependency, not by
  // source position, so a guard assigned elsewhere could be updated first and
  // swallow the change.
  $: {
    // Session ids can be reused after import/restore and across projects.
    // Include project identity so a same-id tab cannot retain the old pane's
    // cwd (and hand that stale root to git/file views) after project switch.
    const target = `${$activeProjectId}:${$selectedSessionId || ''}:${$selectedWindowIdx ?? 0}`;
    if (target !== liveTabPathTarget) {
      liveTabPathTarget = target;
      liveTabPath = '';
      void refreshLiveTabPath();
      void refreshTabIsGitRepo();
    }
  }

  /**
   * Whether THIS tab's directory is in a git repository.
   *
   * The session-wide flag answers for the session path, and one session can
   * hold tabs in a repository and outside one — so it offered the diff on a tab
   * that has none, and withheld it from a tab that does.
   *
   * Starts true so the button is not greyed out for the moment before the
   * answer arrives: a control that flickers from disabled to enabled reads as
   * broken, and being briefly wrong in the permissive direction costs only an
   * empty pane if clicked in that instant.
   */
  let tabIsGitRepo = true;

  async function refreshTabIsGitRepo() {
    const sessionId = $selectedSessionId;
    const windowIdx = $selectedWindowIdx ?? 0;
    if (!sessionId) {
      tabIsGitRepo = false;
      return;
    }
    const generation = liveTabPathGeneration;
    try {
      const answer = await App.TabIsGitRepo(sessionId, windowIdx);
      // A slow reply for a tab already left must not grey out the one now on
      // screen — the same guard the directory lookup beside it uses.
      if (generation !== liveTabPathGeneration) return;
      tabIsGitRepo = answer;
    } catch (e) {
      console.error('Failed to resolve whether the tab is a git repository:', e);
      if (generation === liveTabPathGeneration) tabIsGitRepo = false;
    }
  }

  // What the status bar shows and what git is asked about: the pane's real
  // directory, falling back to the configured one until tmux has answered (or
  // when there is no pane to ask).
  $: currentTabPath = liveTabPath || configuredTabPath;

  // Covers both refresh triggers at once: currentTabPath changes when the
  // session changes, when the tab changes, and when a `cd` moves the pane into
  // another repository. Off means we never ask at all.
  $: if ($settings.gitBranchDisplay !== 'off') void refreshGitBranch(
    $activeProjectId,
    $selectedSessionId || '',
    $selectedWindowIdx ?? 0,
    currentTabPath,
  );

  // The bottom bar resolves the same way as the view bar: tab → the default
  // for its kind. Shared helper so the two can't drift apart.
  $: statusBarHidden = (() => {
    const s = currentSession;
    if (!s) return !!$settings.hideStatusBar;
    const idx = $selectedWindowIdx ?? 0;
    const main = s.mainWindowIndex ?? 0;
    const fw = (s.followedWindows || []).find((f: any) => f.index === idx);
    const tabState = idx === main ? (s.hideStatusBar || 0) : (fw?.hide_status_bar || 0);
    return resolveViewBarHidden(
      tabState, $settings.hideStatusBar, $settings.agentHideStatusBar, currentTabAgent);
  })();

  $: viewBarHidden = (() => {
    const s = currentSession;
    if (!s) return !!$settings.hideViewBar;
    const idx = $selectedWindowIdx ?? 0;
    const main = s.mainWindowIndex ?? 0;
    const fw = (s.followedWindows || []).find((f: any) => f.index === idx);
    const tabState = idx === main ? (s.hideViewBar || 0) : (fw?.hide_view_bar || 0);
    return resolveViewBarHidden(
      tabState, $settings.hideViewBar, $settings.agentHideViewBar, currentTabAgent);
  })();

  // Notes and Tasks have no exit of their own, so hiding the bar while one is
  // open would strand the user — bounce those back to the terminal.
  //
  // The browser is exempt: it has its own close button (added for exactly this
  // reason), so opening it on a tab whose view bar is hidden still leaves a way
  // back out. Without the exemption it would be bounced away the moment it
  // opened.
  $: if (viewBarHidden && activeView !== 'terminal' && activeView !== 'browser') {
    activeView = 'terminal';
  }

  $: currentSession = $sessions.find(s => s.id === $selectedSessionId);

  // The TAB's agent, not the session's — a Claude session can hold a terminal
  // tab, and a Codex session can hold a Claude one. Testing the session offered
  // Fork on tabs with no conversation at all, and withheld it from the tabs it
  // was made for. currentTabAgent above already resolves this for the status
  // bars; canFork simply never used it.
  // Asked of the agent list rather than named here: the backend derives it
  // from the agent's own configuration, so an agent that gains the ability
  // gets the button without this line having to hear about it. Hard-coded to
  // Claude, Codex kept the button hidden after its fork subcommand worked.
  $: canFork = ($agents.find(a => a.type === currentTabAgent)?.supportsFork ?? false)
    && currentSession?.status === 'running';
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
    // resume_session_id, not resumeSessionId: SessionInfo is built for the UI
    // and renames its fields, but the tabs inside it are the STORED structure
    // passed straight through, so they keep the names the file uses. Read as
    // camelCase this was always undefined — a tab never showed its conversation
    // id, which only became obvious on a forked tab, where the id is the point.
    return fw?.resume_session_id || '';
  })();

  // Get current tab's notes (with local cache for immediate updates)
  $: currentTabNotes = (() => {
    if (!currentSession) return '';
    const cacheKey = `${$activeProjectId}:${currentSession.id}:${$selectedWindowIdx}`;
    if (localNotesCache[cacheKey] !== undefined) {
      return localNotesCache[cacheKey];
    }
    if ($selectedWindowIdx === 0) return currentSession.notes || '';
    const fw = currentSession.followedWindows?.find(w => w.index === $selectedWindowIdx);
    return fw?.notes || '';
  })();

  function handleNotesChange(e: CustomEvent<{ sessionId: string, windowIdx: number, notes: string }>) {
    const { sessionId, windowIdx, notes } = e.detail;
    localNotesCache[`${$activeProjectId}:${sessionId}:${windowIdx}`] = notes;
    localNotesCache = localNotesCache; // Trigger reactivity
  }

  // Truncate path for display
  function truncatePath(path: string, maxLen: number = 50): string {
    if (!path || path.length <= maxLen) return path;
    // Both separators: this shows a working directory, which on Windows is
    // backslash-separated and would otherwise never be shortened at all.
    const parts = path.split(/[/\\]/);
    if (parts.length <= 3) return path;
    return '.../' + parts.slice(-3).join('/');
  }

  // Open the directory shown in the status bar in the desktop's file manager.
  //
  // Sends the path rather than the session id: the status bar shows the TAB's
  // directory, resolved live from the pane, so a tab opened elsewhere — or
  // cd-ed since — opens what the user is actually looking at.
  let folderErrorMessage = '';
  let showFolderError = false;
  let folderErrorRevision = 0;
  async function handleOpenFolder() {
    if (!currentTabPath) return;
    try {
      await App.OpenFolder(currentTabPath);
    } catch (e) {
      const key = String(e);
      const translated = $t(key);
      folderErrorMessage = translated === key ? String(e) : translated;
      folderErrorRevision++;
      showFolderError = true;
    }
  }
</script>

<div class="main-panel h-full flex flex-col">
  {#if $selectedSessionId}
    <!-- Tab Bar - shows windows/tabs within a session -->
    <TabBar
      {fullDiffActive}
      {activeView}
      {visible}
      on:openFullDiff={() => {
        fullDiffActive = true;
      }}
      on:closeFullDiff={closeFullDiff}
      on:showTerminal={() => selectView('terminal')}
      on:openColorDialog={() => dispatch('openColorDialog')}
      on:requestStop={() => dispatch('requestStop')}
      on:requestStart={() => dispatch('requestStart')}
      on:requestResume={() => dispatch('requestResume')}
      on:openSettings={() => dispatch('openSettings')}
    />

    <!-- The view bar stays put while the diff is open.
         It used to be replaced by the diff, which left no sign of where you had
         come from and no button to go back — the way out was the same Diff
         control that was no longer on screen. Keeping the bar means the view
         you left is still marked, and pressing Diff again returns to it. -->
      <!-- View Selector. Hidden on request to give the terminal another row;
           the tab bar keeps a button to bring it back, and the views stay
           reachable from the command palette meanwhile. -->
      <div class="view-tabs" class:is-hidden={viewBarHidden}>
        <div class="view-tabs-left">
          <button
            class="view-tab {activeView === 'terminal' ? 'active' : ''}"
            class:behind-diff={fullDiffActive && activeView === 'terminal'}
            on:click={() => selectView('terminal')}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polyline points="4 17 10 11 4 5"/>
              <line x1="12" y1="19" x2="20" y2="19"/>
            </svg>
            {$t('mainPanel.terminal')}
          </button>
          <button
            class="view-tab {activeView === 'notes' ? 'active' : ''}"
            class:behind-diff={fullDiffActive && activeView === 'notes'}
            on:click={() => selectView('notes')}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z"/>
              <polyline points="14 2 14 8 20 8"/>
              <line x1="16" y1="13" x2="8" y2="13"/>
              <line x1="16" y1="17" x2="8" y2="17"/>
            </svg>
            {$t('mainPanel.notes')}
            <!-- A dot when this tab has a note, so it is visible without
                 opening the view. A count would be false precision: there is
                 one note per tab, and it either exists or it doesn't. -->
            {#if currentTabNotes}
              <span class="tab-dot" title={currentTabNotes}></span>
            {/if}
          </button>
          <!-- Always available: a task list is the app's own, stored by it and
               usable without anything installed. The Task Master setting gates
               the actions that shell out to npx, inside the panel. -->
          <button
            class="view-tab {activeView === 'tasks' ? 'active' : ''}"
            class:behind-diff={fullDiffActive && activeView === 'tasks'}
            on:click={() => selectView('tasks')}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M9 11l3 3L22 4"/>
              <path d="M21 12v7a2 2 0 01-2 2H5a2 2 0 01-2-2V5a2 2 0 012-2h11"/>
            </svg>
            {$t('mainPanel.tasks')}
            <!-- How many tasks are still waiting to be started, so the number
                 is visible without opening the view — the same reason the notes
                 tab carries a dot. A count rather than a dot because the size
                 of the backlog is the thing worth knowing, and hidden at zero,
                 where a badge would only say there is nothing to say. -->
            {#if $taskStats.pending > 0}
              <span
                class="tab-badge"
                title={$t('tasks.pendingCount', { count: $taskStats.pending })}
              >{$taskStats.pending}</span>
            {/if}
          </button>
          <!-- Always offered: unlike the diff, a browser only needs a
               directory, and every session has one. -->
          <button
            class="view-tab {activeView === 'browser' ? 'active' : ''}"
            class:behind-diff={fullDiffActive && activeView === 'browser'}
            on:click={() => selectView('browser')}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>
            </svg>
            {$t('mainPanel.browser')}
          </button>
          <!-- Beside Files, because it answers the same question about the same
               directory: what is in this tab, and what changed in it. It used
               to live in the tab bar, which put it a level above the thing it
               describes — one session can hold tabs in different directories,
               each with its own diff.

               Disabled rather than hidden outside a repository: a control that
               disappears reads as a bug, while a greyed one with a reason says
               what the tab is. -->
          <button
            class="view-tab {fullDiffActive ? 'active' : ''}"
            disabled={!tabIsGitRepo}
            on:click={() => toggleFullDiff()}
            title={tabIsGitRepo ? $t('tabBar.fullDiff') : $t('history.notARepository')}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M9 3v12"/><circle cx="9" cy="18" r="3"/>
              <circle cx="9" cy="6" r="3"/><path d="M15 21V9"/>
              <circle cx="15" cy="6" r="3"/><path d="M15 9a6 6 0 0 1-6 6"/>
            </svg>
            {$t('tabBar.diffLabel')}
          </button>
        </div>
        <div class="view-tabs-right">
          <!-- Shows the diff above the current view instead of replacing it.
               Hidden rather than disabled, unlike the Diff button beside Files:
               this one is an extra way to arrange a view that is already
               reachable, so its absence takes nothing away. -->
          {#if tabIsGitRepo}
            <button
              class="split-btn"
              class:active={diffAbove}
              on:click={toggleDiffAbove}
              title={diffAbove ? $t('diff.hideAbove') : $t('diff.showAbove')}
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <rect x="3" y="4" width="18" height="16" rx="2"/>
                <line x1="3" y1="12" x2="21" y2="12"/>
              </svg>
              {$t('diff.above')}
            </button>
          {/if}
          <button
            class="split-btn"
            class:active={splitEnabled}
            on:click={toggleSplitView}
            disabled={activeView !== 'terminal'}
            title={splitEnabled ? $t('split.close') : $t('split.pinCurrent')}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="3" y="4" width="18" height="16" rx="2"/>
              <line x1="12" y1="4" x2="12" y2="20"/>
            </svg>
            {$t('split.label')}
          </button>
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

      {#if fullDiffActive}
        <!-- The diff fills the area below the view bar, rather than replacing
             the bar as well. The view you left stays marked there, and the Diff
             button that brought you here is still on screen to take you back —
             it was the only way back, and it used to vanish with the bar. -->
        <div class="flex-1 overflow-hidden content-area">
          <Diff active={visible} initialMode="full" />
        </div>
      {:else}
      <!-- Content Area - Keep components mounted, use CSS to show/hide -->
      <div class="content-stack" class:with-diff={diffAbove}>
        {#if diffAbove}
          <!-- The diff sits above whichever view is open, not only the
               terminal: reviewing a change while reading the file it came
               from is as useful as reviewing it beside the agent. -->
          <div class="diff-above" style="height: {diffAboveHeight}px">
            <Diff active={visible} initialMode="full" />
          </div>
          <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
          <div
            class="diff-splitter"
            class:dragging={draggingSplitter}
            role="separator"
            aria-orientation="horizontal"
            title={$t('diff.dragToResize')}
            on:mousedown={startSplitterDrag}
            on:dblclick={resetDiffHeight}
          ></div>
        {/if}
      <div class="flex-1 overflow-hidden content-area">
        <div class="view-panel" class:active={activeView === 'terminal'}>
          <div class="terminal-layout" class:split={splitEnabled}>
            <div class="terminal-pane primary" class:focused={focusedTerminalPane === 'primary'}>
              <Terminal
                bind:this={terminalComponent}
                bind:isAttached={terminalAttached}
                active={visible && activeView === 'terminal'}
                focusOwner={focusedTerminalPane === 'primary'}
                focusAllowed={terminalFocusAllowed}
                on:focus={() => focusedTerminalPane = 'primary'}
              />
            </div>
            {#if splitEnabled && markedSession}
              <div class="terminal-pane secondary" class:focused={focusedTerminalPane === 'secondary'}>
                <div class="split-header">
                  <div class="split-title">
                    <strong>{markedSession.name}</strong>
                    <span>› {markedTabName}</span>
                  </div>
                  <div class="split-actions">
                    <button on:click={pinCurrentToSplit} title={$t('split.useCurrent')}>◎</button>
                    <button on:click={swapSplitTargets} title={$t('split.swap')}>⇄</button>
                    <button on:click={toggleSplitView} title={$t('split.close')}>×</button>
                  </div>
                </div>
                <div class="split-terminal">
                  {#if splitDuplicate}
                    <div class="split-placeholder">
                      <span>◫</span>
                      <strong>{$t('split.sameTab')}</strong>
                      <small>{$t('split.sameTabHint')}</small>
                    </div>
                  {:else}
                    <Terminal
                      sessionId={markedSession.id}
                      windowIdx={markedWindowIdx}
                      active={visible && activeView === 'terminal'}
                      focusOwner={focusedTerminalPane === 'secondary'}
                      focusAllowed={terminalFocusAllowed}
                      on:focus={() => focusedTerminalPane = 'secondary'}
                    />
                  {/if}
                </div>
              </div>
            {/if}
          </div>
        </div>
        <div class="view-panel" class:active={activeView === 'notes'}>
          <Notes active={visible && activeView === 'notes'} on:notesChange={handleNotesChange} />
        </div>
        <!-- Mounted unconditionally now. It used to be kept out of the tree
             because TaskPanel asked Task Master for its status from onMount,
             which spawned npx on every launch whether or not anyone opened the
             view. That question is only asked when the setting is on, so the
             panel costs nothing while it is off. -->
        <div class="view-panel" class:active={activeView === 'tasks'}>
          <TaskPanel active={visible && activeView === 'tasks'} on:taskSent={() => selectView('terminal')} />
        </div>
        <div class="view-panel" class:active={activeView === 'browser'}>
          <FileBrowser active={visible && activeView === 'browser'} on:close={() => selectView('terminal')} />
        </div>
      </div>
      </div>
    {/if}

    <!-- Status Bar -->
    <div class="status-bar" class:is-hidden={statusBarHidden}>
      <div class="status-left">
        <!-- Open the current directory in the desktop's file manager.
             Sits in the status bar rather than the header because the path it
             opens is the one shown right beside it, which is per TAB: a tab can
             be opened in its own directory, and the pane may have been cd-ed
             elsewhere since. -->
        <button
          class="status-item status-folder"
          title={currentTabPath ? $t('mainPanel.openInFileManager') : ''}
          disabled={!currentTabPath}
          on:click={handleOpenFolder}
        >
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2z"/>
          </svg>
        </button>

        <!-- Path -->
        <div class="status-item" title={currentTabPath}>
          <span class="status-path">{truncatePath(currentTabPath)}</span>
        </div>

        {#if $settings.gitBranchDisplay === 'statusbar' && $gitBranch}
          <span class="status-divider"></span>
          <!-- Git branch -->
          <div class="status-item">
            <GitBranchBadge variant="statusbar" />
          </div>
        {/if}

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

      </div>

      <div class="status-right">
        <span class="agent-badge">{currentTabAgent}</span>
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

<Toast bind:show={showFolderError} message={folderErrorMessage} revision={folderErrorRevision} variant="error" duration={9000} />

<ForkDialog bind:show={showForkDialog} />

<style>
  .main-panel {
    background: linear-gradient(180deg, rgba(15, 15, 26, 0.8) 0%, rgba(10, 10, 15, 0.9) 100%);
  }

  /* Not ".hidden": Tailwind ships that name as a global utility, so the
     component's own rule and the utility both apply and neither name is
     protected by Svelte's scoping. Harmless while both mean display:none,
     but it is the same trap that made the shortcut rows position:fixed. */
  .view-tabs.is-hidden {
    display: none;
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
    font-size: 13px;
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
    color: var(--accent-ink);
    background: linear-gradient(135deg, rgba(var(--accent-rgb), 0.2) 0%, rgba(99, 102, 241, 0.15) 100%);
    border-color: rgba(var(--accent-rgb), 0.3);
  }

  .view-tab:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  /* The view the diff is covering.
     Still marked, because that is how you can tell where pressing Diff again
     will put you back — but flatter than the active one, so two tabs do not
     both claim to be what is on screen. */
  .view-tab.behind-diff {
    background: rgba(255, 255, 255, 0.04);
    border-color: transparent;
    color: #9ca3af;
  }

  /* Column, so the diff and the view below it share the height rather than
     overlapping. Without with-diff it is a plain wrapper and the view keeps
     the whole area, exactly as before. */
  .content-stack {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .diff-above {
    /* flex-shrink so a small window cannot push the pane below off-screen;
       the inline height is the wish, this is the limit. */
    flex-shrink: 1;
    min-height: 0;
    overflow: hidden;
    border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  }
  .diff-splitter {
    flex-shrink: 0;
    height: 6px;
    cursor: row-resize;
    background: rgba(255, 255, 255, 0.06);
  }
  .diff-splitter:hover,
  .diff-splitter.dragging {
    background: rgba(var(--accent-rgb), 0.5);
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

  .terminal-layout {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    width: 100%;
    height: 100%;
    min-width: 0;
    min-height: 0;
  }

  .terminal-layout.split {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 1px;
    background: rgba(var(--accent-rgb), 0.28);
  }

  .terminal-pane {
    position: relative;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
    background: #0a0a0f;
  }

  .terminal-pane.focused {
    box-shadow: inset 0 0 0 1px rgba(var(--accent-rgb), 0.35);
  }

  .terminal-pane.secondary {
    display: flex;
    flex-direction: column;
  }

  .split-header {
    height: 34px;
    flex: 0 0 34px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 0 8px 0 11px;
    background: rgba(17, 17, 27, 0.96);
    border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  }

  .split-title {
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 6px;
    overflow: hidden;
    font-size: 11px;
  }

  .split-title strong,
  .split-title span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .split-title strong { color: #d4d4d8; }
  .split-title span { color: #71717a; }
  .split-actions { display: flex; gap: 3px; }
  .split-actions button {
    width: 23px;
    height: 23px;
    border: 0;
    border-radius: 5px;
    background: transparent;
    color: #71717a;
    cursor: pointer;
  }
  .split-actions button:hover { background: rgba(255,255,255,.07); color: var(--accent-pale); }
  .split-terminal { position: relative; flex: 1; min-height: 0; overflow: hidden; }
  .split-placeholder {
    height: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 7px;
    color: #52525b;
    text-align: center;
  }
  .split-placeholder span { font-size: 28px; color: var(--accent-dark); }
  .split-placeholder strong { font-size: 13px; color: #a1a1aa; }
  .split-placeholder small { max-width: 260px; font-size: 11px; }

  .status-bar.is-hidden {
    display: none;
  }

  .status-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 16px;
    background: rgba(0, 0, 0, 0.3);
    border-top: 1px solid rgba(255, 255, 255, 0.05);
    font-size: 13px;
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

  /* The folder icon doubles as the button that opens it, so it has to look
     clickable without growing the status bar — hence a bare button reset with
     hover feedback rather than a control of its own. */
  .status-folder {
    background: none;
    border: none;
    padding: 2px 4px;
    margin: 0 -2px 0 0;
    border-radius: 4px;
    cursor: pointer;
    color: inherit;
    transition: background 0.12s, color 0.12s;
  }

  .status-folder:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.08);
    color: #a5b4fc;
  }

  .status-folder:disabled {
    cursor: default;
    opacity: 0.5;
  }

  .status-path {
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 12px;
    color: #9ca3af;
  }

  .status-id {
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 12px;
    color: var(--accent-light);
  }


  /* Marks a view tab as having content. Deliberately small and accent-toned:
     it is a hint, not a badge demanding attention. */
  .tab-dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--accent-light);
    opacity: 0.6;
    flex-shrink: 0;
  }

  /* The pending count on the tasks tab. A pill rather than a bare number, so a
     two-digit backlog does not read as part of the label, and small enough that
     it does not change the height of the row of tabs. */
  .tab-badge {
    min-width: 16px;
    height: 16px;
    padding: 0 4px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: 8px;
    background: rgba(156, 163, 175, 0.18);
    color: #d1d5db;
    font-size: 11px;
    font-weight: 600;
    line-height: 1;
    font-variant-numeric: tabular-nums;
    flex-shrink: 0;
  }
  /* On the open tab it takes the accent, as the label does. */
  .view-tab.active .tab-badge {
    background: rgba(var(--accent-rgb), 0.22);
    color: var(--accent-light);
  }

  .status-divider {
    width: 1px;
    height: 12px;
    background: rgba(255, 255, 255, 0.1);
  }

  .agent-badge {
    padding: 2px 8px;
    background: rgba(var(--accent-rgb), 0.2);
    border-radius: 4px;
    font-size: 12px;
    color: var(--accent-light);
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
    font-size: 13px;
    font-weight: 500;
    color: #9ca3af;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  /* Matches .yolo-btn and .fork-btn, its neighbours in view-tabs-right — not
     the view tabs on the left, which are a taller control of a different kind. */
  .split-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 5px 12px;
    background: rgba(var(--accent-rgb), 0.08);
    border: 1px solid rgba(var(--accent-rgb), 0.2);
    border-radius: 6px;
    color: #8b8b95;
    font-size: 13px;
    cursor: pointer;
  }
  .split-btn:hover:not(:disabled),
  .split-btn.active { color: var(--accent-lighter); background: rgba(var(--accent-rgb), 0.16); border-color: rgba(var(--accent-rgb), 0.38); }
  .split-btn:disabled { opacity: .35; cursor: not-allowed; }

  @media (max-width: 820px) {
    .terminal-layout.split {
      grid-template-columns: minmax(0, 1fr);
      grid-template-rows: repeat(2, minmax(0, 1fr));
    }
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
    background: linear-gradient(135deg, rgba(var(--accent-rgb), 0.2) 0%, rgba(99, 102, 241, 0.15) 100%);
    border: 1px solid rgba(var(--accent-rgb), 0.3);
    border-radius: 6px;
    font-size: 13px;
    font-weight: 500;
    color: var(--accent-light);
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .fork-btn:hover {
    background: linear-gradient(135deg, rgba(var(--accent-rgb), 0.3) 0%, rgba(99, 102, 241, 0.25) 100%);
    border-color: rgba(var(--accent-rgb), 0.5);
    box-shadow: 0 0 15px rgba(var(--accent-rgb), 0.2);
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
