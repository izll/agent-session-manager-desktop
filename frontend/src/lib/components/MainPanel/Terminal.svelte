<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { sessions, selectedSessionId, selectedWindowIdx } from '../../stores/sessions';
  import { get } from 'svelte/store';
  import { EventsOn, EventsOff } from '../../../../wailsjs/runtime/runtime';
  import {
    createTerminal,
    attachToSession,
    detachFromSession,
    fitTerminal,
    type TerminalInstance
  } from '../../utils/terminal';
  import '@xterm/xterm/css/xterm.css';

  let containerEl: HTMLElement;
  let terminalInstance: TerminalInstance | null = null;
  let error = '';

  export let isAttached = false;
  export let active = false;

  // Get current session without reactive subscription
  function getCurrentSession() {
    const id = get(selectedSessionId);
    if (!id) return null;
    return get(sessions).find(s => s.id === id) || null;
  }

  onMount(() => {
    terminalInstance = createTerminal(containerEl, {
      fontSize: 13,
      theme: {
        background: '#0a0a0f',
        foreground: '#e4e4e7',
        cursor: '#8b5cf6',
        selection: 'rgba(139, 92, 246, 0.3)',
      }
    });

    // Debounced resize handler to prevent lag
    let resizeTimeout: ReturnType<typeof setTimeout>;

    function handleResize() {
      clearTimeout(resizeTimeout);
      resizeTimeout = setTimeout(() => {
        if (terminalInstance) {
          requestAnimationFrame(() => {
            fitTerminal(terminalInstance);
            terminalInstance.terminal.refresh(0, terminalInstance.terminal.rows - 1);
          });
        }
      }, 50);
    }

    const resizeObserver = new ResizeObserver(handleResize);
    resizeObserver.observe(containerEl);

    // Window resize listener as fallback
    window.addEventListener('resize', handleResize);

    // Capture-phase handler for Shift+PageUp/Down → send to tmux via WebSocket
    // xterm modifier encoding: ;2 = Shift
    function handleTerminalKeydown(e: KeyboardEvent) {
      if (e.shiftKey && (e.key === 'PageUp' || e.key === 'PageDown')) {
        e.preventDefault();
        e.stopPropagation();
        if (terminalInstance?.ws?.readyState === WebSocket.OPEN) {
          const seq = e.key === 'PageUp' ? '\x1b[5;2~' : '\x1b[6;2~';
          terminalInstance.ws.send(seq);
        }
      }
    }
    containerEl.addEventListener('keydown', handleTerminalKeydown, true);

    // Initial auto-attach if session is already selected and running
    const currentId = get(selectedSessionId);
    if (currentId) {
      const session = get(sessions).find(s => s.id === currentId);
      if (session && session.status === 'running') {
        setTimeout(() => {
          attachToSession(terminalInstance!, currentId, get(selectedWindowIdx))
            .then(() => {
              isAttached = true;
              attachedSessionId = currentId;
            })
            .catch(e => {
              console.error('Initial auto-attach failed:', e);
              error = String(e);
            });
        }, 100);
      }
    }

    return () => {
      clearTimeout(resizeTimeout);
      resizeObserver.disconnect();
      window.removeEventListener('resize', handleResize);
      containerEl.removeEventListener('keydown', handleTerminalKeydown, true);
    };
  });

  // Listen for session restart events (e.g., YOLO toggle)
  let unsubRestarted: (() => void) | null = null;
  onMount(() => {
    unsubRestarted = EventsOn('session:restarted', async (sessionId: string) => {
      const currentId = get(selectedSessionId);
      if (sessionId === currentId && terminalInstance) {
        // Detach from old (dead) connection
        if (isAttached) {
          await detachFromSession(terminalInstance);
          terminalInstance.terminal.clear();
          isAttached = false;
          attachedSessionId = null;
        }
        // Wait for new tmux session to be ready
        await new Promise(r => setTimeout(r, 800));
        // Reattach
        try {
          await attachToSession(terminalInstance, sessionId, get(selectedWindowIdx));
          isAttached = true;
          attachedSessionId = sessionId;
          attachedWindowIdx = get(selectedWindowIdx);
          setTimeout(() => fitTerminal(terminalInstance!), 100);
        } catch (e) {
          console.error('Reattach after restart failed:', e);
          error = String(e);
        }
      }
    });
  });

  onDestroy(async () => {
    if (unsubRestarted) unsubRestarted();
    if (terminalInstance) {
      await detachFromSession(terminalInstance);
      terminalInstance.cleanup();
    }
  });

  // Track which session and window we're attached to
  let attachedSessionId: string | null = null;
  let attachedWindowIdx: number = 0;

  // Track last known status for detecting status changes
  let lastKnownStatus: string | undefined = undefined;

  // Handle session, window, or status change - detach from old, auto-attach to new if running
  async function handleAttachmentChange(newSessionId: string | null, newWindowIdx: number, newStatus?: string) {
    if (!terminalInstance) return;

    const sessionChanged = attachedSessionId !== newSessionId;
    const windowChanged = attachedWindowIdx !== newWindowIdx;
    const statusChanged = lastKnownStatus !== newStatus;
    const sessionJustStarted = statusChanged && newStatus === 'running' && lastKnownStatus !== 'running';
    const sessionJustStopped = statusChanged && newStatus !== 'running' && lastKnownStatus === 'running';

    lastKnownStatus = newStatus;

    // If session stopped, mark as detached and clear terminal
    if (sessionJustStopped && isAttached) {
      await detachFromSession(terminalInstance);
      terminalInstance.terminal.clear();
      isAttached = false;
      attachedSessionId = null;
      return;
    }

    // If session or window changed while attached, detach first
    if (isAttached && (sessionChanged || windowChanged)) {
      await detachFromSession(terminalInstance);
      terminalInstance.terminal.clear();
      isAttached = false;
      attachedSessionId = null;
    }

    // Auto-attach to new session/window if session is running (or just started)
    const shouldAttach = !isAttached || sessionJustStarted;
    if (newSessionId && shouldAttach) {
      const newSession = get(sessions).find(s => s.id === newSessionId);
      if (newSession && newSession.status === 'running') {
        // Small delay when session just started to let tmux initialize
        if (sessionJustStarted) {
          await new Promise(r => setTimeout(r, 500));
        }
        try {
          await attachToSession(terminalInstance, newSessionId, newWindowIdx);
          isAttached = true;
          attachedSessionId = newSessionId;
          attachedWindowIdx = newWindowIdx;
          // Force refit after attach
          setTimeout(() => fitTerminal(terminalInstance!), 100);
        } catch (e) {
          console.error('Auto-attach failed:', e);
          error = String(e);
        }
      }
    }

    // Always refit on session/window change
    if (sessionChanged || windowChanged) {
      setTimeout(() => {
        if (terminalInstance) fitTerminal(terminalInstance);
      }, 50);
    }
  }

  // Get current session's status reactively
  $: currentSessionStatus = $sessions.find(s => s.id === $selectedSessionId)?.status;

  // Watch for session, window, or status changes
  $: handleAttachmentChange($selectedSessionId, $selectedWindowIdx, currentSessionStatus);

  // Fit terminal when tab becomes active (only once per activation)
  let wasActive = false;
  $: if (active && terminalInstance && !wasActive) {
    wasActive = true;
    setTimeout(() => fitTerminal(terminalInstance!), 50);
  } else if (!active) {
    wasActive = false;
  }

  export async function attach() {
    const session = getCurrentSession();
    if (!session || !terminalInstance) return;
    if (session.status !== 'running') {
      error = 'Session is not running';
      return;
    }

    error = '';
    const windowIdx = get(selectedWindowIdx);
    try {
      await attachToSession(terminalInstance, session.id, windowIdx);
      isAttached = true;
      attachedSessionId = session.id;
      attachedWindowIdx = windowIdx;
      terminalInstance.terminal.focus();
    } catch (e) {
      console.error('Failed to attach:', e);
      error = String(e);
      isAttached = false;
      attachedSessionId = null;
    }
  }

  export async function detach() {
    if (terminalInstance) {
      await detachFromSession(terminalInstance);
      isAttached = false;
      attachedSessionId = null;
      attachedWindowIdx = 0;
      terminalInstance.terminal.clear();
    }
  }
</script>

<div class="terminal-wrapper">
  <div class="terminal-container" bind:this={containerEl}></div>
</div>

<style>
  .terminal-wrapper {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: #0a0a0f;
  }

  .terminal-container {
    flex: 1;
    overflow: hidden;
  }

  .terminal-container :global(.xterm) {
    padding: 8px;
    height: 100% !important;
    box-sizing: border-box;
  }

  .terminal-container :global(.xterm-screen) {
    height: calc(100% - 16px) !important;
  }

  .terminal-container :global(.xterm-viewport) {
    height: calc(100% - 16px) !important;
    overflow-y: auto !important;
  }

  .terminal-container :global(.xterm-viewport::-webkit-scrollbar) {
    width: 6px;
  }

  .terminal-container :global(.xterm-viewport::-webkit-scrollbar-track) {
    background: transparent;
  }

  .terminal-container :global(.xterm-viewport::-webkit-scrollbar-thumb) {
    background: rgba(139, 92, 246, 0.3);
    border-radius: 3px;
  }
</style>
