<script lang="ts">
  import { onDestroy } from 'svelte';
  import { selectedSessionId } from '../../stores/sessions';
  import { get } from 'svelte/store';
  import * as App from '../../../../wailsjs/go/main/App';

  export let active = false;
  export let initialMode: 'session' | 'full' = 'session';

  interface DiffData {
    content: string;
    added: number;
    removed: number;
  }

  let diff: DiffData | null = null;
  let loading = false;
  let error = '';
  let lastSessionId: string | null = null;
  let pollInterval: ReturnType<typeof setInterval> | null = null;
  let diffMode: 'session' | 'full' = 'session';

  // Start/stop polling based on active state
  function startPolling() {
    if (!pollInterval) {
      loadDiff();
      pollInterval = setInterval(loadDiff, 5000); // Increased to 5 seconds
    }
  }

  function stopPolling() {
    if (pollInterval) {
      clearInterval(pollInterval);
      pollInterval = null;
    }
  }

  // React to initialMode changes
  $: if (initialMode !== diffMode) {
    diffMode = initialMode;
    if (active) {
      loadDiff();
    }
  }

  // React to active state changes
  $: if (active) {
    startPolling();
  } else {
    stopPolling();
  }

  onDestroy(() => {
    stopPolling();
  });

  async function loadDiff() {
    const sessionId = get(selectedSessionId);
    if (!sessionId) {
      diff = null;
      error = '';
      return;
    }

    lastSessionId = sessionId;
    loading = true;
    error = '';

    try {
      if (diffMode === 'session') {
        diff = await App.GetSessionDiff(sessionId);
      } else {
        diff = await App.GetFullDiff(sessionId);
      }
    } catch (e) {
      error = String(e);
      diff = null;
    }
    loading = false;
  }

  // Reload when session changes
  $: if ($selectedSessionId !== lastSessionId) {
    loadDiff();
  }

  // Reload when mode changes
  function handleModeChange(mode: 'session' | 'full') {
    diffMode = mode;
    loadDiff();
  }

  // Parse diff content into lines with colors
  function parseDiff(content: string) {
    return content.split('\n').map(line => {
      let type: 'add' | 'remove' | 'header' | 'context' | 'meta' = 'context';
      if (line.startsWith('+') && !line.startsWith('+++')) {
        type = 'add';
      } else if (line.startsWith('-') && !line.startsWith('---')) {
        type = 'remove';
      } else if (line.startsWith('@@')) {
        type = 'header';
      } else if (line.startsWith('diff ') || line.startsWith('index ') ||
                 line.startsWith('+++') || line.startsWith('---')) {
        type = 'meta';
      }
      return { text: line, type };
    });
  }

  $: diffLines = diff?.content ? parseDiff(diff.content) : [];
</script>

<div class="diff-container">
  <div class="diff-header">
    <div class="header-left">
      <span class="diff-title">{diffMode === 'session' ? 'Session' : 'Full'}</span>
      {#if diff}
        <div class="diff-stats">
          <span class="stat added">+{diff.added}</span>
          <span class="stat removed">-{diff.removed}</span>
        </div>
      {/if}
    </div>
    <div class="header-right">
      <button class="refresh-btn" on:click={loadDiff} disabled={loading}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class:spinning={loading}>
          <path d="M23 4v6h-6M1 20v-6h6"/>
          <path d="M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"/>
        </svg>
      </button>
    </div>
  </div>

  <div class="diff-content">
    {#if loading && !diff}
      <div class="loading">Loading diff...</div>
    {:else if error}
      <div class="error">{error}</div>
    {:else if !diff || !diff.content}
      <div class="no-diff">
        <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
          <path d="M12 22c5.523 0 10-4.477 10-10S17.523 2 12 2 2 6.477 2 12s4.477 10 10 10z"/>
          <path d="M8 12l2 2 4-4"/>
        </svg>
        <span>No changes detected</span>
        <span class="no-diff-hint">
          {diffMode === 'session' ? 'Changes since session started will appear here' : 'Uncommitted changes will appear here'}
        </span>
      </div>
    {:else}
      <div class="diff-lines">
        {#each diffLines as line}
          <div class="diff-line {line.type}">
            <code>{line.text}</code>
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>

<style>
  .diff-container {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: #0a0a0f;
  }

  .diff-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 16px;
    background: rgba(0, 0, 0, 0.3);
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  }

  .header-left {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .header-right {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .diff-title {
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #6b7280;
  }

  .diff-stats {
    display: flex;
    gap: 8px;
    font-size: 12px;
    font-weight: 600;
    font-family: monospace;
  }

  .stat.added {
    color: #4ade80;
  }

  .stat.removed {
    color: #f87171;
  }

  .refresh-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 6px;
    color: #9ca3af;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .refresh-btn:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.1);
    color: white;
  }

  .refresh-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .refresh-btn svg.spinning {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }

  .diff-content {
    flex: 1;
    overflow: auto;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 12px;
  }

  .loading, .error, .no-diff {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: #4b5563;
    gap: 12px;
  }

  .error {
    color: #f87171;
  }

  .no-diff-hint {
    font-size: 12px;
    opacity: 0.6;
  }

  .diff-lines {
    padding: 12px 0;
  }

  .diff-line {
    padding: 1px 16px;
    line-height: 1.6;
    white-space: pre;
  }

  .diff-line code {
    font-family: inherit;
    font-size: inherit;
  }

  .diff-line.add {
    background: rgba(34, 197, 94, 0.1);
    color: #4ade80;
  }

  .diff-line.remove {
    background: rgba(239, 68, 68, 0.1);
    color: #f87171;
  }

  .diff-line.header {
    color: #60a5fa;
    background: rgba(96, 165, 250, 0.1);
    margin-top: 8px;
  }

  .diff-line.meta {
    color: #8b5cf6;
    font-weight: 600;
    margin-top: 16px;
  }

  .diff-line.context {
    color: #9ca3af;
  }

  .diff-content::-webkit-scrollbar {
    width: 6px;
  }

  .diff-content::-webkit-scrollbar-track {
    background: transparent;
  }

  .diff-content::-webkit-scrollbar-thumb {
    background: rgba(139, 92, 246, 0.3);
    border-radius: 3px;
  }
</style>
