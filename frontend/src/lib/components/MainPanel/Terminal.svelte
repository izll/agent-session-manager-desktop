<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { sessions, selectedSessionId, selectedWindowIdx } from '../../stores/sessions';
  import { settings } from '../../stores/settings';
  import { get } from 'svelte/store';
  import { EventsOn } from '../../../../wailsjs/runtime/runtime';
  import { LogFrontend } from '../../../../wailsjs/go/main/App';
  import { TerminalPool } from '../../utils/terminalPool';
  import { setTerminalRenderer } from '../../utils/terminal';
  import { t } from '../../i18n';
  import '@xterm/xterm/css/xterm.css';

  let poolContainerEl: HTMLElement;
  let pool: TerminalPool | null = null;
  let error = '';

  export let isAttached = false;
  export let active = false;

  const terminalOptions = {
    fontSize: 13,
    theme: {
      background: '#0a0a0f',
      foreground: '#e4e4e7',
      cursor: '#8b5cf6',
      selection: 'rgba(139, 92, 246, 0.3)',
    }
  };

  // Get current session without reactive subscription
  function getCurrentSession() {
    const id = get(selectedSessionId);
    if (!id) return null;
    return get(sessions).find(s => s.id === id) || null;
  }

  // Focus the active terminal (called via 'terminal:focus' global event)
  function focusActive() {
    if (!pool || !active) return;
    const entry = pool.getActive();
    if (entry) {
      entry.terminalInstance.terminal.focus();
    }
  }

  function handleFocusEvent() {
    // Use RAF so DOM/focus updates settle first (e.g., after a dialog closes)
    requestAnimationFrame(focusActive);
  }

  // --- Scrollback search (Ctrl+Shift+L) -------------------------------
  let searchOpen = false;
  let searchQuery = '';
  let searchInputEl: HTMLInputElement | null = null;

  function handleSearchToggle() {
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
    const attempt = (delay: number, remaining: number) => {
      setTimeout(async () => {
        if (!pool) return;
        if (get(selectedSessionId) !== sessionId) return;
        if (windowIdx !== null && get(selectedWindowIdx) !== windowIdx) return;
        const session = get(sessions).find(s => s.id === sessionId);
        if (session?.status !== 'running') return;
        try {
          await pool.show(sessionId, get(selectedWindowIdx));
          isAttached = true;
          if (active) {
            requestAnimationFrame(() => requestAnimationFrame(() => pool?.focusActive()));
          }
          LogFrontend(`reshow ok session=${sessionId} win=${get(selectedWindowIdx)}`);
        } catch (err) {
          console.error('Re-show after pool destroy failed:', err);
          LogFrontend(`reshow FAILED session=${sessionId} win=${get(selectedWindowIdx)} remaining=${remaining}: ${err}`);
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
    pool = new TerminalPool(poolContainerEl, terminalOptions);

    window.addEventListener('terminal:focus', handleFocusEvent);
    window.addEventListener('terminal:search-toggle', handleSearchToggle);
    window.addEventListener('terminal:destroy-window', handleDestroyWindow as EventListener);
    window.addEventListener('terminal:destroy-session', handleDestroySession as EventListener);

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
    const currentId = get(selectedSessionId);
    if (currentId) {
      const session = get(sessions).find(s => s.id === currentId);
      if (session && session.status === 'running') {
        setTimeout(async () => {
          try {
            await pool!.show(currentId, get(selectedWindowIdx));
            isAttached = true;
          } catch (e) {
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
      window.removeEventListener('terminal:focus', handleFocusEvent);
      window.removeEventListener('terminal:search-toggle', handleSearchToggle);
      window.removeEventListener('terminal:destroy-window', handleDestroyWindow as EventListener);
      window.removeEventListener('terminal:destroy-session', handleDestroySession as EventListener);
      poolContainerEl.removeEventListener('keydown', handleTerminalKeydown, true);
    };
  });

  // Listen for session restart events
  let unsubRestarted: (() => void) | null = null;
  onMount(() => {
    unsubRestarted = EventsOn('session:restarted', async (sessionId: string) => {
      const currentId = get(selectedSessionId);
      if (sessionId === currentId && pool) {
        // Destroy old terminal for this session
        await pool.destroy(sessionId);
        isAttached = false;

        // Wait for new tmux session to be ready
        await new Promise(r => setTimeout(r, 800));

        // Create fresh terminal and show it
        try {
          await pool.show(sessionId, get(selectedWindowIdx));
          isAttached = true;
        } catch (e) {
          console.error('Reattach after restart failed:', e);
          error = String(e);
        }
      }
    });
  });

  onDestroy(async () => {
    if (unsubRestarted) unsubRestarted();
    if (stopGraceTimer) clearTimeout(stopGraceTimer);
    if (pool) {
      await pool.destroyAll();
    }
  });

  // Track last known status for detecting changes
  let lastKnownStatus: string | undefined = undefined;
  let lastSessionId: string | null = null;
  let lastWindowIdx: number = 0;
  let stopGraceTimer: ReturnType<typeof setTimeout> | null = null;

  // Handle session/window/status changes via pool show/destroy
  async function handlePoolChange(newSessionId: string | null, newWindowIdx: number, newStatus?: string) {
    if (!pool) return;

    const statusChanged = lastKnownStatus !== newStatus;
    const sessionJustStopped = statusChanged && newStatus !== 'running' && lastKnownStatus === 'running';
    const sessionJustStarted = statusChanged && newStatus === 'running' && lastKnownStatus !== 'running';
    const sessionChanged = lastSessionId !== newSessionId;
    const windowChanged = lastWindowIdx !== newWindowIdx;

    // If status came back to running, cancel any pending stop grace timer
    if (sessionJustStarted && stopGraceTimer) {
      clearTimeout(stopGraceTimer);
      stopGraceTimer = null;
    }

    lastKnownStatus = newStatus;
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
          pool.hideAll();
          await pool.destroy(stoppedSessionId);
          isAttached = false;
        }
      }, 3000);
      // Don't destroy yet, just hide
      pool.hideAll();
      isAttached = false;
      return;
    }

    // Session is running → show (creates if needed)
    if (newSessionId && newStatus === 'running' && (sessionChanged || windowChanged || sessionJustStarted)) {
      // Small delay when session just started to let tmux initialize
      if (sessionJustStarted) {
        await new Promise(r => setTimeout(r, 500));
      }
      try {
        await pool.show(newSessionId, newWindowIdx);
        isAttached = true;
        // Ensure the freshly-shown terminal grabs focus on session/tab switch.
        // pool.show() focuses internally, but a couple of rAFs later we focus
        // again in case layout/visibility wasn't settled the first time — this
        // is what was missing when the terminal lost focus on switch.
        if (active) {
          requestAnimationFrame(() => requestAnimationFrame(() => pool?.focusActive()));
        }
      } catch (e) {
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
  $: currentSessionStatus = $sessions.find(s => s.id === $selectedSessionId)?.status;

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
  $: handlePoolChange($selectedSessionId, $selectedWindowIdx, currentSessionStatus);

  // Apply a renderer change (canvas/webgl/dom) from Settings immediately:
  // update the factory default for new terminals AND recreate the currently
  // open one so the switch takes effect without restarting the app.
  let lastRenderer: string | undefined;
  $: {
    const r = $settings?.terminalRenderer || 'canvas';
    setTerminalRenderer(r as 'canvas' | 'webgl' | 'dom');
    if (lastRenderer !== undefined && lastRenderer !== r && pool) {
      pool.recreateActiveForRenderer();
    }
    lastRenderer = r;
  }

  // Fit and focus terminal when tab becomes active
  let wasActive = false;
  $: if (active && pool && !wasActive) {
    wasActive = true;
    // Use requestAnimationFrame to ensure DOM is visible before fitting/focusing
    requestAnimationFrame(() => {
      if (!pool || !active) return;
      pool.fitActive();
      const activeEntry = pool.getActive();
      if (activeEntry) {
        activeEntry.terminalInstance.terminal.focus();
      }
    });
  } else if (!active) {
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
    const windowIdx = get(selectedWindowIdx);
    try {
      await pool.show(session.id, windowIdx);
      isAttached = true;
    } catch (e) {
      console.error('Failed to attach:', e);
      error = String(e);
      isAttached = false;
    }
  }

  export async function detach() {
    if (pool) {
      const currentId = get(selectedSessionId);
      if (currentId) {
        await pool.destroy(currentId);
      }
      isAttached = false;
    }
  }
</script>

<div class="terminal-wrapper">
  <div class="terminal-pool-container" bind:this={poolContainerEl}></div>
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
    border: 1px solid rgba(139, 92, 246, 0.35);
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
    font-size: 12px;
  }
  .terminal-search input::placeholder { color: #52525b; }
  .search-nav {
    border: 0;
    background: transparent;
    color: #a1a1aa;
    cursor: pointer;
    font-size: 11px;
    padding: 2px 4px;
    border-radius: 4px;
  }
  .search-nav:hover { color: #e4e4e7; background: rgba(139, 92, 246, 0.15); }
  .search-nav.close { font-size: 15px; line-height: 1; }

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

  .terminal-pool-container :global(.xterm) {
    padding: 8px;
    height: 100% !important;
    box-sizing: border-box;
  }

  .terminal-pool-container :global(.xterm-screen) {
    height: calc(100% - 16px) !important;
  }

  .terminal-pool-container :global(.xterm-viewport) {
    height: calc(100% - 16px) !important;
    overflow-y: auto !important;
  }

  .terminal-pool-container :global(.xterm-viewport::-webkit-scrollbar) {
    width: 6px;
  }

  .terminal-pool-container :global(.xterm-viewport::-webkit-scrollbar-track) {
    background: transparent;
  }

  .terminal-pool-container :global(.xterm-viewport::-webkit-scrollbar-thumb) {
    background: rgba(139, 92, 246, 0.3);
    border-radius: 3px;
  }
</style>
