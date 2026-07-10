<script lang="ts">
  import { onDestroy } from 'svelte';
  import { selectedSessionId } from '../../stores/sessions';
  import { get } from 'svelte/store';
  import * as App from '../../../../wailsjs/go/main/App';
  import { t } from '../../i18n';

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
  let pollTimeout: ReturnType<typeof setTimeout> | null = null;
  let loadGeneration = 0;
  let diffMode: 'session' | 'full' = 'session';
  // When a diff is very large we DON'T render it automatically — we warn first
  // and let the user opt in, because rendering a huge diff is heavy. `forceShow`
  // is set by the "show anyway" button; reset whenever the diff content changes.
  let forceShow = false;
  // ~6000 lines (or ~600 KB) counts as "large" — above this we warn first.
  const LARGE_DIFF_LINES = 6000;
  const LARGE_DIFF_BYTES = 600 * 1024;

  // Start/stop polling based on active state
  function startPolling() {
    if (!pollTimeout) void loadDiff();
  }

  function stopPolling() {
    loadGeneration++;
    if (pollTimeout) {
      clearTimeout(pollTimeout);
      pollTimeout = null;
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
    const mode = diffMode;
    const generation = ++loadGeneration;
    if (!sessionId) {
      diff = null;
      error = '';
      return;
    }

    lastSessionId = sessionId;
    loading = true;
    error = '';

    try {
      let result: DiffData;
      if (mode === 'session') {
        result = await App.GetSessionDiff(sessionId);
      } else {
        result = await App.GetFullDiff(sessionId);
      }
      if (generation !== loadGeneration || sessionId !== get(selectedSessionId) || mode !== diffMode || !active) return;
      diff = result;
    } catch (e) {
      if (generation !== loadGeneration || sessionId !== get(selectedSessionId) || mode !== diffMode || !active) return;
      error = String(e);
      diff = null;
    }
    if (generation === loadGeneration) loading = false;
    if (generation === loadGeneration && active && !pollTimeout) {
      pollTimeout = setTimeout(() => {
        pollTimeout = null;
        void loadDiff();
      }, 5000);
    }
  }

  // Reload when session changes — but ONLY while the Diff tab is actually
  // visible. Previously this ran on EVERY session switch even when the Diff tab
  // was hidden, so switching to a session with a huge repo (WebErp) fetched and
  // parsed its enormous diff in the background — freezing the UI on a plain tab
  // switch. Gating on `active` means the diff only ever runs when you're looking
  // at it.
  $: if (active && $selectedSessionId !== lastSessionId) {
    loadDiff();
  }

  // Reload when mode changes
  function handleModeChange(mode: 'session' | 'full') {
    diffMode = mode;
    loadDiff();
  }

  // Cheap line count: counts newlines without allocating a big array (split()).
  function countLines(content: string): number {
    let n = 1;
    for (let i = 0; i < content.length; i++) {
      if (content.charCodeAt(i) === 10) n++;
    }
    return n;
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

  // Cap how many diff lines we render. The diff view renders one <div><code>
  // per line with NO virtualisation, so a huge repo's diff (e.g. WebErp, tens
  // of thousands of lines) would insert tens of thousands of DOM nodes
  // synchronously on every session/tab switch — freezing the entire main thread
  // (the UI, even the profiler, locked up). A diff that large is not human-
  // readable inline anyway; we render the first MAX_DIFF_LINES and show a notice
  // with the real total. This is what made WebErp 2 "freeze on typing/switching"
  // while small-diff sessions (asmgr-desktop) stayed fluid.
  const MAX_DIFF_LINES = 2000;

  // Detect a large diff from the raw content WITHOUT parsing it (cheap: a length
  // check + one newline count). If it's large we show a warning instead of
  // rendering, until the user clicks "show anyway".
  $: diffContent = diff?.content || '';
  $: isLargeDiff =
    diffContent.length > LARGE_DIFF_BYTES ||
    (diffContent ? countLines(diffContent) : 0) > LARGE_DIFF_LINES;
  // Reset the "show anyway" opt-in only when the SESSION changes, not on every
  // content update. The diff polls every 5s and its content changes as the
  // agent works; resetting on content (the old behaviour) snapped the user back
  // to the warning every few seconds while they were reading the diff.
  let forceShowSessionId: string | null = null;
  $: if ($selectedSessionId !== forceShowSessionId) {
    forceShowSessionId = $selectedSessionId;
    forceShow = false;
  }

  // Only parse when the tab is active AND (the diff is small OR the user opted
  // in). Parsing a huge diff string is itself expensive, so we skip it entirely
  // while showing the warning.
  $: shouldRender = active && !!diffContent && (!isLargeDiff || forceShow);
  $: allDiffLines = shouldRender ? parseDiff(diffContent) : [];
  $: diffLines = allDiffLines.length > MAX_DIFF_LINES
    ? allDiffLines.slice(0, MAX_DIFF_LINES)
    : allDiffLines;
  $: diffTruncated = allDiffLines.length > MAX_DIFF_LINES;
</script>

<div class="diff-container">
  <div class="diff-header">
    <div class="header-left">
      <span class="diff-title">{diffMode === 'session' ? $t('diff.session') : $t('diff.full')}</span>
      {#if diff}
        <div class="diff-stats">
          <span class="stat added">+{diff.added}</span>
          <span class="stat removed">-{diff.removed}</span>
        </div>
      {/if}
    </div>
    <div class="header-right">
      <button class="refresh-btn" on:click={() => loadDiff()} disabled={loading}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class:spinning={loading}>
          <path d="M23 4v6h-6M1 20v-6h6"/>
          <path d="M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"/>
        </svg>
      </button>
    </div>
  </div>

  <div class="diff-content">
    {#if loading && !diff}
      <div class="loading">{$t('diff.loading')}</div>
    {:else if error}
      <div class="error">{error}</div>
    {:else if !diff || !diff.content}
      <div class="no-diff">
        <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
          <path d="M12 22c5.523 0 10-4.477 10-10S17.523 2 12 2 2 6.477 2 12s4.477 10 10 10z"/>
          <path d="M8 12l2 2 4-4"/>
        </svg>
        <span>{$t('diff.noChanges')}</span>
        <span class="no-diff-hint">
          {diffMode === 'session' ? $t('diff.sessionHint') : $t('diff.fullHint')}
        </span>
      </div>
    {:else if isLargeDiff && !forceShow}
      <div class="large-diff-warning">
        <svg width="44" height="44" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
          <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
          <line x1="12" y1="9" x2="12" y2="13"/>
          <line x1="12" y1="17" x2="12.01" y2="17"/>
        </svg>
        <span class="large-diff-title">Nagy diff ({diff.added + diff.removed} változás)</span>
        <span class="large-diff-hint">
          A diff túl nagy az automatikus megjelenítéshez (megfagyaszthatja a felületet).
          Csak akkor jelenítsd meg, ha tényleg szükséges.
        </span>
        <button class="large-diff-show" on:click={() => (forceShow = true)}>
          Mégis megjelenítés (első {MAX_DIFF_LINES} sor)
        </button>
      </div>
    {:else}
      <div class="diff-lines">
        {#each diffLines as line}
          <div class="diff-line {line.type}">
            <code>{line.text}</code>
          </div>
        {/each}
        {#if diffTruncated}
          <div class="diff-line meta diff-truncated">
            <code>… {allDiffLines.length - MAX_DIFF_LINES} további sor elrejtve (a diff túl nagy az inline megjelenítéshez)</code>
          </div>
        {/if}
      </div>
    {/if}
  </div>
</div>

<style>
  .large-diff-warning {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 10px;
    height: 100%;
    padding: 24px;
    text-align: center;
    color: #fbbf24;
  }
  .large-diff-title {
    font-size: 15px;
    font-weight: 600;
  }
  .large-diff-hint {
    font-size: 12px;
    color: #a1a1aa;
    max-width: 360px;
    line-height: 1.5;
  }
  .large-diff-show {
    margin-top: 6px;
    background: rgba(251, 191, 36, 0.15);
    color: #fbbf24;
    border: 1px solid rgba(251, 191, 36, 0.4);
    border-radius: 6px;
    padding: 6px 14px;
    font-size: 13px;
    cursor: pointer;
    transition: background 0.15s ease;
  }
  .large-diff-show:hover {
    background: rgba(251, 191, 36, 0.25);
  }

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
