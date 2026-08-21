<script lang="ts">
  import { onMount, onDestroy, createEventDispatcher } from 'svelte';
  import { sessions, selectedSessionId, selectedWindowIdx, loadSessions } from '../../stores/sessions';
  import { settings } from '../../stores/settings';
  import { activeProjectId } from '../../stores/projects';
  import { get } from 'svelte/store';
  import { EventsOn } from '../../../../wailsjs/runtime/runtime';
  import { LogFrontend, SetTabFontSize } from '../../../../wailsjs/go/main/App';
  import { TerminalPool } from '../../utils/terminalPool';
  import { setTerminalRenderer, setTerminalCopyMode, setTerminalFontFamily, setTerminalThemeContext, defaultTerminalRenderer } from '../../utils/terminal';
  import { t } from '../../i18n';
  import { matchesShortcut } from '../../stores/shortcuts';
  import '@xterm/xterm/css/xterm.css';

  let poolContainerEl: HTMLElement;
  let pool: TerminalPool | null = null;
  // Whether the visible pane is waiting for its redraw after coming back from
  // the background. Owned here rather than read from the instance so Svelte
  // sees the change.
  let awaitingRedraw = false;
  let error = '';
  let mounted = false;
  const pendingTimeouts = new Set<ReturnType<typeof setTimeout>>();
  const dispatch = createEventDispatcher();

  export let isAttached = false;
  export let active = false;
  export let sessionId: string | null | undefined = undefined;
  export let windowIdx: number | undefined = undefined;
  export let focusOwner = true;
  export let focusAllowed = true;

  $: targetSessionId = sessionId === undefined ? $selectedSessionId : sessionId;
  $: targetWindowIdx = windowIdx === undefined ? $selectedWindowIdx : windowIdx;

  function currentTargetSessionId(): string | null {
    return sessionId === undefined ? get(selectedSessionId) : sessionId;
  }

  function currentTargetWindowIdx(): number {
    return windowIdx === undefined ? get(selectedWindowIdx) : windowIdx;
  }

  // Neither colours nor font size belong here: both come from Settings via
  // createTerminal(), and options passed in are spread last — a value here
  // would silently win over the user's choice.
  const terminalOptions = {};

  // Persist a Ctrl+wheel resize against the tab it happened in.
  let fontSizeSaveTimer: ReturnType<typeof setTimeout> | null = null;
  function handleFontSizeGesture(e: CustomEvent<{ size: number }>) {
    const sid = currentTargetSessionId();
    if (!sid || !e.detail) return;
    const projectId = get(activeProjectId);
    const widx = currentTargetWindowIdx();
    const size = e.detail.size;
    // A wheel gesture fires many events; only the size it settles on matters.
    if (fontSizeSaveTimer) clearTimeout(fontSizeSaveTimer);
    fontSizeSaveTimer = setTimeout(() => {
      fontSizeSaveTimer = null;
      // The backend's active project is process-global and may have changed
      // while this gesture was being debounced. The frontend check avoids a
      // needless call; expectedProjectId makes the backend mutation itself
      // fail closed if selection changes in the gap after this check.
      if (get(activeProjectId) !== projectId) return;
      void SetTabFontSize(sid, widx, size, projectId)
        // Refresh the session list so anything reading the stored size — the
        // tab menu's reset entry, for one — sees that this tab now has its
        // own. Without it the menu stays greyed out until the next reload.
        .then(() => {
          if (get(activeProjectId) === projectId) return loadSessions();
        })
        .catch((err) => {
          LogFrontend(`SetTabFontSize failed session=${sid} win=${widx}: ${err}`);
        });
    }, 400);
  }

  /**
   * Drop this tab's own size so it follows the global setting again. The
   * pane is updated straight away; the stored value is cleared behind it.
   */
  async function resetFontSizeForCurrentTab(
    target?: { sessionId: string; windowIdx: number },
    persist = true,
  ) {
    const sid = target?.sessionId ?? currentTargetSessionId();
    if (!sid) return;
    const projectId = get(activeProjectId);
    const widx = target?.windowIdx ?? currentTargetWindowIdx();
    // Only touch the live pane if it is the one being reset; the stored value
    // still has to be cleared either way.
    const affectsVisiblePane =
      sid === currentTargetSessionId() && widx === currentTargetWindowIdx();
    // A pending Ctrl+scroll save would otherwise write the old size back.
    if (affectsVisiblePane && fontSizeSaveTimer) {
      clearTimeout(fontSizeSaveTimer);
      fontSizeSaveTimer = null;
    }
    if (affectsVisiblePane) {
      const entry = pool?.getActive();
      if (entry) {
        entry.themeCtx = { ...(entry.themeCtx || {}), fontSize: 0 };
      }
      pool?.applyFontSize();
    }
    if (!persist) return;
    try {
      await SetTabFontSize(sid, widx, 0, projectId);
      if (get(activeProjectId) !== projectId) return;
      await loadSessions();
    } catch (err) {
      LogFrontend(`reset font size failed session=${sid} win=${widx}: ${err}`);
    }
  }

  // The tab context menu lives in another component, so it asks for the reset
  // through an event rather than reaching into this one's pool.
  function handleResetFontSizeEvent(e: CustomEvent<{ sessionId: string; windowIdx: number }>) {
    if (!e.detail) return;
    // In split view both panes listen. `mine` chooses the pane whose xterm must
    // repaint; focusOwner below chooses the one backend writer.
    const mine = e.detail.sessionId === currentTargetSessionId() &&
                 e.detail.windowIdx === currentTargetWindowIdx();
    if (!mine && !focusOwner) return;
    // Every split pane receives this window event. The pane showing the target
    // updates its xterm immediately, while exactly one focus owner persists the
    // setting. When those are different panes this deliberately becomes one
    // local-only update plus one backend-only update, not two writes.
    void resetFontSizeForCurrentTab(e.detail, focusOwner);
  }

  // Get current session without reactive subscription
  function getCurrentSession() {
    const id = currentTargetSessionId();
    if (!id) return null;
    return get(sessions).find(s => s.id === id) || null;
  }

  // Focus the active terminal (called via 'terminal:focus' global event)
  function focusActive() {
    if (!pool || !active || !focusOwner || !focusAllowed) return;
    const entry = pool.getActive();
    if (entry) {
      entry.terminalInstance.terminal.focus();
    }
  }

  function handleFocusEvent() {
    // Use RAF so DOM/focus updates settle first (e.g., after a dialog closes)
    requestAnimationFrame(focusActive);
  }

  function schedule(callback: () => void, delay: number) {
    const timeout = setTimeout(() => {
      pendingTimeouts.delete(timeout);
      if (mounted) callback();
    }, delay);
    pendingTimeouts.add(timeout);
  }

  // --- Scrollback search (Ctrl+Shift+L) -------------------------------
  let searchOpen = false;
  let searchQuery = '';
  let searchInputEl: HTMLInputElement | null = null;

  function handleSearchToggle() {
    if (!focusOwner) return;
    searchOpen = !searchOpen;
    if (searchOpen) {
      requestAnimationFrame(() => searchInputEl?.focus());
    } else {
      closeSearch();
    }
  }

  function activeSearchAddon() {
    return pool?.getActive()?.terminalInstance.searchAddon || null;
  }

  function runSearch(incremental: boolean) {
    const addon = activeSearchAddon();
    if (!addon || !searchQuery) return;
    try { addon.findNext(searchQuery, { incremental }); } catch { /* addon not ready */ }
  }

  function searchStep(forward: boolean) {
    const addon = activeSearchAddon();
    if (!addon || !searchQuery) return;
    try {
      if (forward) addon.findNext(searchQuery); else addon.findPrevious(searchQuery);
    } catch { /* addon not ready */ }
  }

  function closeSearch() {
    searchOpen = false;
    searchQuery = '';
    try { activeSearchAddon()?.clearDecorations(); } catch { /* no-op */ }
    pool?.focusActive();
  }

  function handleSearchKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault();
      searchStep(!e.shiftKey);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      closeSearch();
    }
    e.stopPropagation();
  }

  // Drop a single window's cached PoolEntry. Triggered after a tab is
  // deleted so that a later tab reusing the same window index doesn't
  // inherit the killed pane's stale WebSocket + xterm DOM.
  async function handleDestroyWindow(e: Event) {
    const ev = e as CustomEvent<{ sessionId: string; windowIdx: number }>;
    if (!pool || !ev.detail) return;
    // Await the teardown so the re-show can't overlap it: getOrCreate must
    // build a FRESH entry, not revive the one being destroyed. The re-show
    // must be scheduled even if the teardown throws (xterm dispose race) —
    // an uncaught await here once silently killed the re-show entirely.
    try {
      await pool.destroyWindow(ev.detail.sessionId, ev.detail.windowIdx);
    } catch (err) {
      LogFrontend(`destroyWindow FAILED session=${ev.detail.sessionId} win=${ev.detail.windowIdx}: ${err}`);
    }
    scheduleReshowIfViewing(ev.detail.sessionId, ev.detail.windowIdx);
  }

  // Drop every PoolEntry belonging to a session. Triggered by start/stop
  // because the backend tears down the whole tmux session (and its
  // grouped gui_* mirrors) — a cached WebSocket would point at a dead
  // mirror after start/stop. Required in addition to the status-change
  // grace-period handler below: a fast Stop→Start sequence never sees a
  // sustained 'stopped' state and slips through that guard.
  async function handleDestroySession(e: Event) {
    const ev = e as CustomEvent<{ sessionId: string }>;
    if (!pool || !ev.detail) return;
    try {
      await pool.destroy(ev.detail.sessionId);
    } catch (err) {
      LogFrontend(`destroySession FAILED session=${ev.detail.sessionId}: ${err}`);
    }
    scheduleReshowIfViewing(ev.detail.sessionId, null);
  }

  // Rebuild the PoolEntry the user is currently looking at after a destroy
  // event removed it. Tab restart (respawn-pane) keeps the session 'running'
  // and the window index unchanged, so handlePoolChange sees no transition to
  // react to and nothing re-creates the WebSocket — the pane just goes black
  // until a manual detach/attach. Same for StartSession on an already-running
  // session (StartDialog's "Entire Session" while only a tab was stopped):
  // the pool is dropped up-front and the backend errors out with "already
  // running". The delay lets loadSessions() land first, so a genuinely
  // stopped session reads 'stopped' here and stays owned by the
  // status-change grace-period path.
  function scheduleReshowIfViewing(sessionId: string, windowIdx: number | null) {
    if (currentTargetSessionId() !== sessionId) return;
    if (windowIdx !== null && currentTargetWindowIdx() !== windowIdx) return;
    const operationRevision = ++poolChangeGeneration;
    const attempt = (delay: number, remaining: number) => {
      schedule(async () => {
        if (!mounted || !pool || operationRevision !== poolChangeGeneration) return;
        if (currentTargetSessionId() !== sessionId) return;
        if (windowIdx !== null && currentTargetWindowIdx() !== windowIdx) return;
        const session = get(sessions).find(s => s.id === sessionId);
        if (session?.status !== 'running') return;
        const targetWindowIdx = currentTargetWindowIdx();
        const projectId = get(activeProjectId);
        try {
          await pool.show(projectId, sessionId, targetWindowIdx, () => mounted && active && focusOwner && focusAllowed, themeCtxFor(sessionId, targetWindowIdx));
          if (!mounted || operationRevision !== poolChangeGeneration ||
              get(activeProjectId) !== projectId ||
              currentTargetSessionId() !== sessionId ||
              currentTargetWindowIdx() !== targetWindowIdx) return;
          isAttached = true;
          if (active && focusOwner && focusAllowed) {
            requestAnimationFrame(() => requestAnimationFrame(focusActive));
          }
          LogFrontend(`reshow ok session=${sessionId} win=${targetWindowIdx}`);
        } catch (err) {
          if (!mounted || operationRevision !== poolChangeGeneration ||
              get(activeProjectId) !== projectId ||
              currentTargetSessionId() !== sessionId ||
              currentTargetWindowIdx() !== targetWindowIdx) return;
          console.error('Re-show after pool destroy failed:', err);
          LogFrontend(`reshow FAILED session=${sessionId} win=${targetWindowIdx} remaining=${remaining}: ${err}`);
          // A transient WebSocket failure right after a respawn used to leave
          // the pane black until a manual detach/attach — retry with backoff.
          if (remaining > 0) {
            attempt(Math.min(delay * 2, 3000), remaining - 1);
          }
        }
      }, delay);
    };
    attempt(300, 3);
  }

  onMount(() => {
    mounted = true;
    pool = new TerminalPool(poolContainerEl, terminalOptions);
    pool.onAwaitingRedraw = (waiting) => { awaitingRedraw = waiting; };

    // Ctrl+wheel already resized the pane; store it so the tab keeps that size
    // next time. Saving is best-effort — a failed write must not undo what the
    // user just did on screen.
    poolContainerEl.addEventListener('terminal:fontsize', handleFontSizeGesture as EventListener);

    window.addEventListener('terminal:reset-fontsize', handleResetFontSizeEvent as EventListener);
    window.addEventListener('terminal:focus', handleFocusEvent);
    window.addEventListener('terminal:search-toggle', handleSearchToggle);
    window.addEventListener('terminal:destroy-window', handleDestroyWindow as EventListener);
    window.addEventListener('terminal:destroy-session', handleDestroySession as EventListener);
    window.addEventListener('terminal:destroy-all', handleDestroyAll);

    // Debounced resize handler
    let resizeTimeout: ReturnType<typeof setTimeout>;

    function handleResize() {
      clearTimeout(resizeTimeout);
      resizeTimeout = setTimeout(() => {
        if (pool) {
          // Skip resize when container is hidden (display: none) — prevents
          // sending 0×0 resize to tmux which breaks the terminal session
          const rect = poolContainerEl.getBoundingClientRect();
          if (rect.width === 0 || rect.height === 0) return;

          requestAnimationFrame(() => {
            pool!.fitActive();
            const active = pool!.getActive();
            if (active) {
              active.terminalInstance.terminal.refresh(0, active.terminalInstance.terminal.rows - 1);
            }
          });
        }
      }, 50);
    }

    const resizeObserver = new ResizeObserver(handleResize);
    resizeObserver.observe(poolContainerEl);

    window.addEventListener('resize', handleResize);

    // Capture-phase handler for Shift+PageUp/Down → send to tmux via WebSocket
    function handleTerminalKeydown(e: KeyboardEvent) {
      // Resets the zoom, as in a browser. Caught here rather than sent to the
      // shell: the default (Ctrl+0) has no terminal meaning, so nothing is lost.
      //
      // e.code is checked as well as the binding because on layouts where the
      // digit row needs Shift, e.key is the symbol rather than the digit — and
      // Ctrl+0 is the one shortcut users reach for by its position.
      const isFontReset = matchesShortcut(e, 'terminal.fontReset') ||
        (e.ctrlKey && !e.shiftKey && !e.altKey && e.code === 'Digit0');
      if (isFontReset) {
        e.preventDefault();
        e.stopPropagation();
        void resetFontSizeForCurrentTab();
        return;
      }
      if (e.shiftKey && (e.key === 'PageUp' || e.key === 'PageDown')) {
        e.preventDefault();
        e.stopPropagation();
        const activeEntry = pool?.getActive();
        if (activeEntry?.terminalInstance.ws?.readyState === WebSocket.OPEN) {
          const seq = e.key === 'PageUp' ? '\x1b[5;2~' : '\x1b[6;2~';
          activeEntry.terminalInstance.ws.send(seq);
        }
      }
    }
    poolContainerEl.addEventListener('keydown', handleTerminalKeydown, true);

    // Initial auto-attach if session is already selected and running
    const currentId = currentTargetSessionId();
    const initialWindowIdx = currentTargetWindowIdx();
    const initialProjectId = get(activeProjectId);
    if (currentId) {
      const session = get(sessions).find(s => s.id === currentId);
      if (session && session.status === 'running') {
        const operationRevision = ++poolChangeGeneration;
        schedule(async () => {
          try {
            if (!pool || operationRevision !== poolChangeGeneration ||
                get(activeProjectId) !== initialProjectId ||
                currentTargetSessionId() !== currentId ||
                currentTargetWindowIdx() !== initialWindowIdx) return;
            await pool.show(initialProjectId, currentId, initialWindowIdx, () => mounted && active && focusOwner && focusAllowed, themeCtxFor(currentId, initialWindowIdx));
            if (!mounted || operationRevision !== poolChangeGeneration ||
                get(activeProjectId) !== initialProjectId ||
                currentTargetSessionId() !== currentId ||
                currentTargetWindowIdx() !== initialWindowIdx) return;
            isAttached = true;
          } catch (e) {
            if (!mounted || operationRevision !== poolChangeGeneration ||
                get(activeProjectId) !== initialProjectId ||
                currentTargetSessionId() !== currentId ||
                currentTargetWindowIdx() !== initialWindowIdx) return;
            console.error('Initial auto-attach failed:', e);
            error = String(e);
          }
        }, 100);
      }
    }

    return () => {
      clearTimeout(resizeTimeout);
      resizeObserver.disconnect();
      window.removeEventListener('resize', handleResize);
      window.removeEventListener('terminal:reset-fontsize', handleResetFontSizeEvent as EventListener);
    window.removeEventListener('terminal:focus', handleFocusEvent);
      window.removeEventListener('terminal:search-toggle', handleSearchToggle);
      window.removeEventListener('terminal:destroy-window', handleDestroyWindow as EventListener);
      window.removeEventListener('terminal:destroy-session', handleDestroySession as EventListener);
      window.removeEventListener('terminal:destroy-all', handleDestroyAll);
      poolContainerEl.removeEventListener('keydown', handleTerminalKeydown, true);
    };
  });

  async function handleDestroyAll() {
    if (!pool) return;
    await pool.destroyAll();
    isAttached = false;
  }

  // Listen for session restart events
  let unsubRestarted: (() => void) | null = null;
  onMount(() => {
    unsubRestarted = EventsOn('session:restarted', async (sessionId: string) => {
      const currentId = currentTargetSessionId();
      const restartWindowIdx = currentTargetWindowIdx();
      const restartProjectId = get(activeProjectId);
      if (sessionId === currentId && pool) {
        const operationRevision = ++poolChangeGeneration;
        // Destroy old terminal for this session
        await pool.destroy(sessionId);
        if (!mounted || operationRevision !== poolChangeGeneration || !pool) return;
        isAttached = false;

        // Wait for new tmux session to be ready
        await new Promise(r => setTimeout(r, 800));
        if (!mounted || operationRevision !== poolChangeGeneration ||
            get(activeProjectId) !== restartProjectId ||
            sessionId !== currentTargetSessionId() ||
            restartWindowIdx !== currentTargetWindowIdx() || !pool) return;

        // Create fresh terminal and show it
        try {
          await pool.show(restartProjectId, sessionId, restartWindowIdx, () => mounted && active && focusOwner && focusAllowed, themeCtxFor(sessionId, restartWindowIdx));
          if (!mounted || operationRevision !== poolChangeGeneration ||
              get(activeProjectId) !== restartProjectId ||
              sessionId !== currentTargetSessionId() ||
              restartWindowIdx !== currentTargetWindowIdx()) return;
          isAttached = true;
        } catch (e) {
          if (!mounted || operationRevision !== poolChangeGeneration ||
              get(activeProjectId) !== restartProjectId ||
              sessionId !== currentTargetSessionId() ||
              restartWindowIdx !== currentTargetWindowIdx()) return;
          console.error('Reattach after restart failed:', e);
          error = String(e);
        }
      }
    });
  });

  onDestroy(async () => {
    mounted = false;
    poolChangeGeneration++;
    for (const timeout of pendingTimeouts) clearTimeout(timeout);
    pendingTimeouts.clear();
    if (fontSizeSaveTimer) clearTimeout(fontSizeSaveTimer);
    poolContainerEl?.removeEventListener('terminal:fontsize', handleFontSizeGesture as EventListener);
    if (unsubRestarted) unsubRestarted();
    if (stopGraceTimer) clearTimeout(stopGraceTimer);
    const oldPool = pool;
    pool = null;
    if (oldPool) await oldPool.dispose();
  });

  // Track last known status for detecting changes
  let lastKnownStatus: string | undefined = undefined;
  let lastProjectId = '';
  let lastSessionId: string | null = null;
  let lastWindowIdx: number = 0;
  let stopGraceTimer: ReturnType<typeof setTimeout> | null = null;
  let poolChangeGeneration = 0;

  // Handle session/window/status changes via pool show/destroy
  async function handlePoolChange(newSessionId: string | null, newWindowIdx: number, newStatus: string | undefined, projectId: string) {
    if (!pool) return;
    const generation = ++poolChangeGeneration;
    if (projectId !== get(activeProjectId)) return;

    const projectChanged = !!lastProjectId && lastProjectId !== projectId;
    const statusChanged = lastKnownStatus !== newStatus;
    const sessionJustStopped = !projectChanged && statusChanged && newStatus !== 'running' && lastKnownStatus === 'running';
    const sessionJustStarted = !projectChanged && statusChanged && newStatus === 'running' && lastKnownStatus !== 'running';
    const sessionChanged = lastSessionId !== newSessionId;
    const windowChanged = lastWindowIdx !== newWindowIdx;

    // Project switches can reuse the exact same session/window/status tuple.
    // Tear down A's sockets before showing B: keeping the old keyed entries
    // leaves rejected reconnect timers and dead xterms resident forever, and
    // without projectChanged in the show condition B is never attached at all.
    if (projectChanged) {
      if (stopGraceTimer) {
        clearTimeout(stopGraceTimer);
        stopGraceTimer = null;
      }
      pool.hideAll();
      isAttached = false;
      await pool.destroyAll();
      if (!mounted || generation !== poolChangeGeneration || !pool ||
          projectId !== get(activeProjectId)) return;
    }

    // If status came back to running, cancel any pending stop grace timer
    if (sessionJustStarted && stopGraceTimer) {
      clearTimeout(stopGraceTimer);
      stopGraceTimer = null;
    }

    lastKnownStatus = newStatus;
    lastProjectId = projectId;
    lastSessionId = newSessionId;
    lastWindowIdx = newWindowIdx;

    // Session stopped → wait grace period before destroying (protects against tmux status flicker)
    if (sessionJustStopped && newSessionId) {
      const stoppedSessionId = newSessionId;
      stopGraceTimer = setTimeout(async () => {
        stopGraceTimer = null;
        // Re-check: is the session still stopped?
        const currentSession = get(sessions).find(s => s.id === stoppedSessionId);
        if (currentSession && currentSession.status !== 'running' && pool) {
          const stillViewingStoppedSession = currentTargetSessionId() === stoppedSessionId;
          if (stillViewingStoppedSession) pool.hideAll();
          await pool.destroy(stoppedSessionId);
          if (stillViewingStoppedSession && currentTargetSessionId() === stoppedSessionId) {
            isAttached = false;
          }
        }
      }, 3000);
      // Don't destroy yet, just hide
      pool.hideAll();
      isAttached = false;
      return;
    }

    // Session is running → show (creates if needed)
    if (newSessionId && newStatus === 'running' && (projectChanged || sessionChanged || windowChanged || sessionJustStarted)) {
      // Small delay when session just started to let tmux initialize
      if (sessionJustStarted) {
        await new Promise(r => setTimeout(r, 500));
        if (!mounted || generation !== poolChangeGeneration ||
            get(activeProjectId) !== projectId || !pool) return;
      }
      try {
        await pool.show(projectId, newSessionId, newWindowIdx, () => mounted && active && focusOwner && focusAllowed, themeCtxFor(newSessionId, newWindowIdx));
        if (!mounted || generation !== poolChangeGeneration ||
            get(activeProjectId) !== projectId ||
            currentTargetSessionId() !== newSessionId || currentTargetWindowIdx() !== newWindowIdx) return;
        isAttached = true;
        // Ensure the freshly-shown terminal grabs focus on session/tab switch.
        // pool.show() focuses internally, but a couple of rAFs later we focus
        // again in case layout/visibility wasn't settled the first time — this
        // is what was missing when the terminal lost focus on switch.
        if (active && focusOwner && focusAllowed) {
          requestAnimationFrame(() => requestAnimationFrame(focusActive));
        }
      } catch (e) {
        if (!mounted || generation !== poolChangeGeneration ||
            get(activeProjectId) !== projectId ||
            currentTargetSessionId() !== newSessionId || currentTargetWindowIdx() !== newWindowIdx) return;
        console.error('Pool show failed:', e);
        LogFrontend(`pool show FAILED session=${newSessionId} win=${newWindowIdx}: ${e}`);
        error = String(e);
        isAttached = false;
      }
      return;
    }

    // If session is not running or no session selected, hide all terminals
    if (!newSessionId || newStatus !== 'running') {
      pool.hideAll();
      isAttached = false;
    }
  }

  // Get current session's status reactively
  $: currentSessionStatus = $sessions.find(s => s.id === targetSessionId)?.status;

  // Show placeholder when no running session is active
  $: showPlaceholder = !isAttached;

  const placeholderIcons = [
    '\u{1F634}', '\u{1F60C}', '\u{1F3D6}\u{FE0F}', '\u{1F995}', '\u{1F47B}',
    '\u{1F680}', '\u{1F319}', '\u{1F50C}', '\u{1F9CA}', '\u{1F916}',
  ];
  const placeholderKeys = [
    'terminal.napping', 'terminal.waiting', 'terminal.vacation',
    'terminal.noSession', 'terminal.crickets', 'terminal.launch',
    'terminal.resting', 'terminal.plugIn', 'terminal.frozen', 'terminal.notFound',
  ];

  let placeholderIdx = 0;
  $: if (showPlaceholder) {
    placeholderIdx = Math.floor(Math.random() * placeholderKeys.length);
  }

  // Watch for session, window, or status changes
  $: handlePoolChange(targetSessionId, targetWindowIdx, currentSessionStatus, $activeProjectId);

  // Apply a renderer change (canvas/webgl/dom) from Settings immediately:
  // update the factory default for new terminals AND recreate the currently
  // open one so the switch takes effect without restarting the app.
  let lastRenderer: string | undefined;
  $: {
    const r = $settings?.terminalRenderer || defaultTerminalRenderer();
    setTerminalRenderer(r as 'canvas' | 'webgl' | 'dom');
    if (lastRenderer !== undefined && lastRenderer !== r && pool) {
      // Pass the palette explicitly: the rebuild drops every pooled entry, and
      // with it the theme each pane was resolved with. Resolving it fresh here
      // rather than reusing what the entry held also means a theme changed in
      // the same visit to Settings is picked up.
      //
      // Read through the helpers, NOT $-store references: a store reference
      // here would make this block depend on the selected session, so it would
      // re-run on every session change and rebuild all terminals each time.
      pool.recreateActiveForRenderer(
        () => mounted && active && focusOwner && focusAllowed,
        themeCtxFor(currentTargetSessionId(), currentTargetWindowIdx())
      );
    }
    lastRenderer = r;
  }

  // Copy-on-select mode. No terminal needs recreating: the handler reads the
  // current value at mouseup, so open panes pick the change up immediately.
  $: setTerminalCopyMode(($settings?.terminalCopyMode || 'shift') as 'shift' | 'select');

  // Font choice. Applied to open panes directly — xterm re-measures its glyphs
  // and reflows, so no terminal has to be rebuilt.
  let lastFont: string | undefined;
  $: {
    const f = $settings?.terminalFontFamily || '';
    setTerminalFontFamily(f);
    if (lastFont !== undefined && lastFont !== f && pool) {
      pool.applyFontFamily();
    }
    lastFont = f;
  }

  // Apply palette settings immediately. Unlike the renderer, xterm accepts a
  // new theme on a live instance, so open terminals are repainted in place —
  // each re-resolving its own tab → agent → global palette.
  let lastThemeKey: string | undefined;
  $: {
    const ctx = {
      terminalDefault: $settings?.terminalTheme || 'asmgr',
      agentDefault: ($settings as any)?.agentDefaultTheme || 'asmgr',
      agentThemes: ($settings as any)?.agentTerminalThemes || {},
      customThemes: ($settings as any)?.customTerminalThemes || [],
      fontSize: $settings?.terminalFontSize || 0,
      agentFontSize: $settings?.agentFontSize || 0,
    };
    setTerminalThemeContext(ctx);
    const key = JSON.stringify(ctx);
    if (lastThemeKey !== undefined && lastThemeKey !== key && pool) {
      pool.applyTheme();
      // Font size changes the row/column count, so this also refits every
      // pane and pushes the new geometry to tmux.
      pool.applyFontSize();
    }
    lastThemeKey = key;
  }

  /** Palette inputs for the pane we're about to show. */
  function themeCtxFor(sid: string | null, widx: number) {
    const session = get(sessions).find(s => s.id === sid);
    if (!session) return {};
    const main = session.mainWindowIndex ?? 0;
    if (widx === main) {
      return {
        tabTheme: session.terminalTheme || '',
        agent: session.agent,
        fontSize: session.terminalFontSize || 0,
      };
    }
    const fw = (session.followedWindows || []).find((f: any) => f.index === widx);
    return {
      tabTheme: (fw as any)?.terminal_theme || '',
      agent: fw?.agent || session.agent,
      fontSize: (fw as any)?.terminal_font_size || 0,
    };
  }

  // Fit and focus terminal when tab becomes active
  let wasActive = false;
  $: if (active && focusOwner && focusAllowed && pool && !wasActive) {
    wasActive = true;
    // Use requestAnimationFrame to ensure DOM is visible before fitting/focusing
    requestAnimationFrame(() => {
      if (!pool || !active || !focusOwner || !focusAllowed) return;
      pool.fitActive();
      const activeEntry = pool.getActive();
      if (activeEntry) {
        activeEntry.terminalInstance.terminal.focus();
      }
    });
  } else if (!active || !focusOwner || !focusAllowed) {
    wasActive = false;
  }

  export async function attach() {
    const session = getCurrentSession();
    if (!session || !pool) return;
    if (session.status !== 'running') {
      error = 'Session is not running';
      return;
    }

    error = '';
    const windowIdx = currentTargetWindowIdx();
    const projectId = get(activeProjectId);
    const operationRevision = ++poolChangeGeneration;
    try {
      await pool.show(projectId, session.id, windowIdx, () => mounted && active && focusOwner && focusAllowed, themeCtxFor(session.id, windowIdx));
      if (!mounted || operationRevision !== poolChangeGeneration ||
          get(activeProjectId) !== projectId ||
          currentTargetSessionId() !== session.id || currentTargetWindowIdx() !== windowIdx) return;
      isAttached = true;
    } catch (e) {
      if (!mounted || operationRevision !== poolChangeGeneration ||
          get(activeProjectId) !== projectId ||
          currentTargetSessionId() !== session.id ||
          currentTargetWindowIdx() !== windowIdx) return;
      console.error('Failed to attach:', e);
      error = String(e);
      isAttached = false;
    }
  }

  export async function detach() {
    if (pool) {
      const currentId = currentTargetSessionId();
      const currentWindowIdx = currentTargetWindowIdx();
      const operationRevision = ++poolChangeGeneration;
      if (currentId) {
        await pool.destroy(currentId);
      }
      if (mounted && operationRevision === poolChangeGeneration &&
          currentTargetSessionId() === currentId && currentTargetWindowIdx() === currentWindowIdx) {
        isAttached = false;
      }
    }
  }
</script>

<div class="terminal-wrapper" on:mousedown={() => dispatch('focus')}>
  <div class="terminal-pool-container" bind:this={poolContainerEl}></div>
  {#if awaitingRedraw}
    <!-- A tab returning from the background shows a stale frame until the
         queued redraw arrives. Without this the wait reads as a hang. -->
    <div class="redraw-pending" aria-live="polite">
      <span class="redraw-spinner"></span>
      <span>{$t('terminal.refreshing')}</span>
    </div>
  {/if}
  {#if searchOpen}
    <div class="terminal-search">
      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/></svg>
      <input
        bind:this={searchInputEl}
        bind:value={searchQuery}
        placeholder={$t('terminal.searchPlaceholder')}
        on:input={() => runSearch(true)}
        on:keydown={handleSearchKeydown}
      />
      <button class="search-nav" title={$t('terminal.searchPrev')} on:click={() => searchStep(false)}>▲</button>
      <button class="search-nav" title={$t('terminal.searchNext')} on:click={() => searchStep(true)}>▼</button>
      <button class="search-nav close" title="Esc" on:click={closeSearch}>×</button>
    </div>
  {/if}
  {#if showPlaceholder}
    <div class="terminal-placeholder">
      <span class="placeholder-icon">{placeholderIcons[placeholderIdx]}</span>
      <p class="placeholder-msg">{$t(placeholderKeys[placeholderIdx])}</p>
    </div>
  {/if}
</div>

<style>
  .terminal-wrapper {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: #0a0a0f;
    /* positioning context for the floating search bar */
    position: relative;
  }

  .terminal-search {
    position: absolute;
    top: 8px;
    right: 14px;
    z-index: 20;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 5px 8px;
    border-radius: 8px;
    border: 1px solid rgba(var(--accent-rgb), 0.35);
    background: rgba(15, 15, 26, 0.95);
    box-shadow: 0 6px 20px rgba(0, 0, 0, 0.4);
    color: #71717a;
  }
  .terminal-search input {
    width: 200px;
    background: transparent;
    border: 0;
    outline: 0;
    color: #e4e4e7;
    font-size: 13px;
  }
  .terminal-search input::placeholder { color: #52525b; }
  .search-nav {
    border: 0;
    background: transparent;
    color: #a1a1aa;
    cursor: pointer;
    font-size: 12px;
    padding: 2px 4px;
    border-radius: 4px;
  }
  .search-nav:hover { color: #e4e4e7; background: rgba(var(--accent-rgb), 0.15); }
  .search-nav.close { font-size: 15px; line-height: 1; }

  /* Centred and non-interactive: it reports on the pane behind it rather than
     replacing it, so the stale content stays visible and clickable. */
  .redraw-pending {
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 14px;
    border-radius: 8px;
    background: rgba(24, 24, 28, 0.88);
    border: 1px solid rgba(255, 255, 255, 0.08);
    color: #d1d5db;
    font-size: 12px;
    pointer-events: none;
    z-index: 5;
  }

  .redraw-spinner {
    width: 12px;
    height: 12px;
    border: 2px solid rgba(255, 255, 255, 0.18);
    border-top-color: #a5b4fc;
    border-radius: 50%;
    animation: redraw-spin 0.7s linear infinite;
  }

  @keyframes redraw-spin {
    to { transform: rotate(360deg); }
  }

  .terminal-pool-container {
    flex: 1;
    overflow: hidden;
    position: relative;
  }

  .terminal-placeholder {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 1rem;
    pointer-events: none;
    user-select: none;
    z-index: 10;
  }

  .placeholder-icon {
    font-size: 64px;
    line-height: 1;
    opacity: 0.5;
    filter: grayscale(0.3);
  }

  .placeholder-msg {
    font-family: 'JetBrains Mono', 'Menlo', 'Monaco', 'Consolas', monospace;
    font-size: 13px;
    color: rgba(228, 228, 231, 0.3);
    margin: 0;
    letter-spacing: 0.03em;
  }

  .terminal-pool-container :global(.terminal-pool-entry) {
    overflow: hidden;
  }

  /* Even padding on all four sides.
     A wider right side was tried while chasing a line whose background stopped
     short of the frame; that turned out to be the screen and row elements being
     sized to the columns rather than to the space — fixed below — so the
     padding can stay symmetrical. */
  .terminal-pool-container :global(.xterm) {
    padding: 8px;
    height: 100% !important;
    box-sizing: border-box;
  }

  /* The screen is set to the columns' own width — the renderer writes
     `_screenElement.style.width = canvas.width`, which is cols × cellWidth —
     so stretching the rows inside it buys nothing while their parent is still
     short. Both have to be widened for a line's background to reach the frame. */
  .terminal-pool-container :global(.xterm-screen) {
    height: calc(100% - 16px) !important;
    width: 100% !important;
  }

  /* Each row is sized to the columns it holds, not to the space available:
     the DOM renderer sets `element.style.width = canvas.width`, which is
     cols × cellWidth — 1730px of a 1744px area on this machine. A line with a
     background colour therefore stopped 14px short of the frame, showing the
     panel's black behind it, which reads as a rendering fault rather than as
     the end of the text.

     Stretching the rows lets that background run to the edge. The text is
     unaffected: the cells are still laid out at their own width, and the extra
     is empty space at the end of the line. */
  .terminal-pool-container :global(.xterm-rows > div) {
    width: 100% !important;
  }

  /* xterm hard-codes the viewport to #000 (xterm.css), and that element sits
     behind the rows — so on any theme that is not black, a band of it shows
     wherever the rows do not reach: past the last column, and below the last
     line. The colour is published as --xterm-background when the terminal is
     built and again on a theme change. */
  .terminal-pool-container :global(.xterm-viewport) {
    background-color: var(--xterm-background, #000) !important;
    height: calc(100% - 16px) !important;
    overflow-y: auto !important;
  }

  /* No scrollbar on a terminal pane.
     tmux owns the wheel here: its WheelUpPane binding enters copy mode and
     pages the whole history, far more than the few thousand lines xterm keeps.
     A bar sized against xterm's buffer therefore describes the wrong document
     — it sat near the bottom of a track while most of the history was tmux's,
     which says less than nothing.

     The mouse is set on every session the app creates, including the mirrors
     the terminal actually attaches to (terminal_ws.go), so this holds even
     where tmux itself defaults to `mouse off`. Scrolling is untouched; only the
     indicator goes. */
  .terminal-pool-container :global(.xterm-viewport::-webkit-scrollbar) {
    width: 0;
    height: 0;
  }

  /* xterm draws its own scrollbar as an element rather than leaving it to the
     browser — .xterm-scrollable-element > .scrollbar, taken from VS Code — so
     ::-webkit-scrollbar above never touched it. That is the one actually on
     screen. */
  .terminal-pool-container :global(.xterm-scrollable-element > .scrollbar) {
    display: none !important;
  }



</style>
