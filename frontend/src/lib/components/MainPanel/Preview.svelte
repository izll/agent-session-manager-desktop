<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { FitAddon } from '@xterm/addon-fit';
  import { selectedSessionId } from '../../stores/sessions';
  import { get } from 'svelte/store';
  import * as App from '../../../../wailsjs/go/main/App';
  import { t } from '../../i18n';

  let containerEl: HTMLElement;
  let terminal: Terminal | null = null;
  let fitAddon: FitAddon | null = null;
  let pollTimeout: ReturnType<typeof setTimeout> | null = null;
  let previewGeneration = 0;
  let lastContent = '';
  let lastSessionId = '';

  export let activity: 'idle' | 'busy' | 'waiting' = 'idle';

  onMount(() => {
    terminal = new Terminal({
      cursorBlink: false,
      disableStdin: true,
      fontSize: 12,
      lineHeight: 1.1,
      fontFamily: 'monospace',
      cols: 120,
      rows: 40,
      theme: {
        background: '#0a0a0f',
        foreground: '#e4e4e7',
        cursor: '#8b5cf6',
        selectionBackground: 'rgba(139, 92, 246, 0.3)',
      },
      convertEol: true,
    });

    fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(containerEl);

    // Delay fit to ensure container is rendered
    setTimeout(() => {
      fitAddon?.fit();
    }, 100);

    // Start polling
    startPolling();

    // Handle resize with debounce
    let resizeTimeout: ReturnType<typeof setTimeout>;
    const resizeObserver = new ResizeObserver(() => {
      clearTimeout(resizeTimeout);
      resizeTimeout = setTimeout(() => {
        fitAddon?.fit();
      }, 50);
    });
    resizeObserver.observe(containerEl);

    return () => {
      resizeObserver.disconnect();
      clearTimeout(resizeTimeout);
    };
  });

  onDestroy(() => {
    stopPolling();
    terminal?.dispose();
  });

  // Handle session change
  function handleSessionChange(sessionId: string | null) {
    if (sessionId && sessionId !== lastSessionId) {
      previewGeneration++;
      lastSessionId = sessionId;
      lastContent = '';
      // Reset and refit terminal on session change
      if (terminal) {
        terminal.clear();
        setTimeout(() => fitAddon?.fit(), 50);
      }
      void updatePreview(true);
    }
  }

  $: handleSessionChange($selectedSessionId);

  function startPolling() {
    if (!pollTimeout) void updatePreview(true);
  }

  function stopPolling() {
    previewGeneration++;
    if (pollTimeout) {
      clearTimeout(pollTimeout);
      pollTimeout = null;
    }
  }

  async function updatePreview(scheduleNext = false) {
    const sessionId = get(selectedSessionId);
    if (!sessionId || !terminal) return;
    const generation = ++previewGeneration;

    try {
      const data = await App.GetPreview(sessionId, 100);
      if (generation === previewGeneration && sessionId === get(selectedSessionId) && data && terminal) {
        // Only update terminal if content actually changed
        if (data.content !== lastContent) {
          lastContent = data.content;
          terminal.clear();
          terminal.write(data.content);
        }
        activity = data.activity as 'idle' | 'busy' | 'waiting';
      }
    } catch (e) {
      if (generation === previewGeneration && sessionId === get(selectedSessionId)) {
        console.error('Preview update failed:', e);
      }
    } finally {
      if (scheduleNext && generation === previewGeneration && terminal) {
        pollTimeout = setTimeout(() => {
          pollTimeout = null;
          void updatePreview(true);
        }, 500);
      }
    }
  }
</script>

<div class="preview-container">
  <div class="preview-header">
    <span class="preview-title">{$t('preview.livePreview')}</span>
    <span class="activity-badge {activity}">
      {#if activity === 'busy'}
        <span class="activity-dot"></span>
        {$t('preview.working')}
      {:else if activity === 'waiting'}
        <span class="activity-dot"></span>
        {$t('preview.waiting')}
      {:else}
        {$t('preview.idle')}
      {/if}
    </span>
  </div>
  <div class="terminal-container" bind:this={containerEl}></div>
</div>

<style>
  .preview-container {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: #0a0a0f;
  }

  .preview-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 16px;
    background: rgba(0, 0, 0, 0.3);
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  }

  .preview-title {
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #6b7280;
  }

  .activity-badge {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 11px;
    font-weight: 500;
    padding: 4px 10px;
    border-radius: 12px;
    background: rgba(255, 255, 255, 0.05);
    color: #6b7280;
  }

  .activity-badge.busy {
    background: rgba(34, 197, 94, 0.1);
    color: #4ade80;
  }

  .activity-badge.waiting {
    background: rgba(234, 179, 8, 0.1);
    color: #fbbf24;
  }

  .activity-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: currentColor;
    animation: pulse-dot 1.5s ease-in-out infinite;
  }

  @keyframes pulse-dot {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.4; }
  }

  .terminal-container {
    flex: 1;
    overflow: hidden;
    position: relative;
  }

  .terminal-container :global(.xterm) {
    padding: 12px;
    height: 100% !important;
    width: 100% !important;
  }

  .terminal-container :global(.xterm-screen) {
    width: 100% !important;
  }

  .terminal-container :global(.xterm-viewport) {
    overflow-y: auto !important;
    width: 100% !important;
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
