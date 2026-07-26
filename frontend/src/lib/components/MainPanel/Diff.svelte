<script lang="ts">
  import { onDestroy } from 'svelte';
  import { selectedSessionId } from '../../stores/sessions';
  import { get } from 'svelte/store';
  import * as App from '../../../../wailsjs/go/main/App';
  import { ClipboardSetText } from '../../../../wailsjs/runtime/runtime';
  import type { session } from '../../../../wailsjs/go/models';
  import ConfirmDialog from '../Dialogs/ConfirmDialog.svelte';
  import { t } from '../../i18n';

  export let active = false;
  export let initialMode: 'session' | 'full' = 'session';

  interface DiffData {
    content: string;
    added: number;
    removed: number;
  }

  let diff: DiffData | null = null;
  let files: session.DiffFile[] = [];
  let selectedPath: string | null = null;
  let loading = false;
  let error = '';
  let lastSessionId: string | null = null;
  let pollTimeout: ReturnType<typeof setTimeout> | null = null;
  let loadGeneration = 0;
  let diffMode: 'session' | 'full' = 'session';
  // When a single file's diff is very large we DON'T render it automatically —
  // we warn first and let the user opt in, because rendering a huge diff is
  // heavy. `forceShowPaths` records the files the user explicitly opted into.
  let forceShowPaths: Record<string, boolean> = {};
  let copyState: 'idle' | 'copied' | 'failed' = 'idle';
  let copyResetTimeout: ReturnType<typeof setTimeout> | null = null;
  let loadedDiffKey = '';
  let copyGeneration = 0;
  let copying = false;
  let destroyed = false;
  let reverting = false;
  // ~6000 lines (or ~600 KB) counts as "large" — above this we warn first.
  const LARGE_DIFF_LINES = 6000;
  const LARGE_DIFF_BYTES = 600 * 1024;

  // Draggable width for the file list, mirroring the sidebar resizer in
  // App.svelte. Persisted so it survives switching tabs and restarts.
  const FILE_PANE_MIN = 160;
  const FILE_PANE_MAX = 640;
  const FILE_PANE_DEFAULT = 280;
  const FILE_PANE_STORAGE_KEY = 'asmgr.diff.filePaneWidth';

  let filePaneWidth = readStoredPaneWidth();
  let isResizingPane = false;
  let panesEl: HTMLDivElement;

  function readStoredPaneWidth(): number {
    const raw = Number(localStorage.getItem(FILE_PANE_STORAGE_KEY));
    if (!Number.isFinite(raw) || raw <= 0) return FILE_PANE_DEFAULT;
    return Math.min(FILE_PANE_MAX, Math.max(FILE_PANE_MIN, raw));
  }

  function startPaneResize(e: MouseEvent) {
    isResizingPane = true;
    document.addEventListener('mousemove', resizePane);
    document.addEventListener('mouseup', stopPaneResize);
    e.preventDefault();
  }

  function resizePane(e: MouseEvent) {
    if (!isResizingPane || !panesEl) return;
    // Measure from the pane container, not the window: this splitter sits
    // inside the main panel, so clientX alone would be offset by the sidebar.
    const left = panesEl.getBoundingClientRect().left;
    const next = e.clientX - left;
    filePaneWidth = Math.min(FILE_PANE_MAX, Math.max(FILE_PANE_MIN, next));
  }

  function stopPaneResize() {
    if (!isResizingPane) return;
    isResizingPane = false;
    document.removeEventListener('mousemove', resizePane);
    document.removeEventListener('mouseup', stopPaneResize);
    try {
      localStorage.setItem(FILE_PANE_STORAGE_KEY, String(Math.round(filePaneWidth)));
    } catch {
      // A full or disabled storage isn't worth surfacing; the width simply
      // won't be remembered.
    }
  }

  // Double-click the splitter to get back to the default width.
  function resetPaneWidth() {
    filePaneWidth = FILE_PANE_DEFAULT;
    try {
      localStorage.setItem(FILE_PANE_STORAGE_KEY, String(FILE_PANE_DEFAULT));
    } catch {
      // Ignored, as above.
    }
  }

  // Pending revert — filled in before the confirm dialog opens, consumed on
  // confirm. Reverting throws away the user's work, so it always goes through
  // ConfirmDialog rather than firing on click.
  let showRevertConfirm = false;
  let revertTitle = '';
  let revertMessage = '';
  let pendingRevert: (() => Promise<void>) | null = null;

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
    destroyed = true;
    copyGeneration++;
    stopPolling();
    if (copyResetTimeout) clearTimeout(copyResetTimeout);
    // Leaving the tab mid-drag would otherwise strand the document listeners.
    stopPaneResize();
  });

  function resetCopyState() {
    copyGeneration++;
    copying = false;
    copyState = 'idle';
    if (copyResetTimeout) {
      clearTimeout(copyResetTimeout);
      copyResetTimeout = null;
    }
  }

  async function copyDiff() {
    if (!diffContent || loading || copying || loadedDiffKey !== currentDiffKey) return;
    const content = diffContent;
    const generation = ++copyGeneration;
    copying = true;
    try {
      const copied = await ClipboardSetText(content);
      if (destroyed || generation !== copyGeneration || content !== diffContent) return;
      copyState = copied ? 'copied' : 'failed';
    } catch {
      if (destroyed || generation !== copyGeneration) return;
      copyState = 'failed';
    }
    copying = false;
    if (copyResetTimeout) clearTimeout(copyResetTimeout);
    copyResetTimeout = setTimeout(() => {
      if (destroyed || generation !== copyGeneration) return;
      copyState = 'idle';
      copyResetTimeout = null;
    }, 1800);
  }

  async function loadDiff() {
    const sessionId = get(selectedSessionId);
    const mode = diffMode;
    const generation = ++loadGeneration;
    const requestedKey = `${sessionId || ''}:${mode}`;
    if (!sessionId) {
      diff = null;
      files = [];
      selectedPath = null;
      loadedDiffKey = '';
      resetCopyState();
      error = '';
      return;
    }

    if (loadedDiffKey !== requestedKey) {
      diff = null;
      files = [];
      selectedPath = null;
      resetCopyState();
    }
    lastSessionId = sessionId;
    loading = true;
    error = '';

    try {
      // The raw diff still backs the copy-to-clipboard button; the file list is
      // what we render. Both come from the same snapshot so the header stats and
      // the per-file rows can't disagree.
      let result: DiffData;
      let fileResult: session.DiffFile[];
      if (mode === 'session') {
        [result, fileResult] = await Promise.all([
          App.GetSessionDiff(sessionId),
          App.GetSessionDiffFiles(sessionId),
        ]);
      } else {
        [result, fileResult] = await Promise.all([
          App.GetFullDiff(sessionId),
          App.GetFullDiffFiles(sessionId),
        ]);
      }
      if (generation !== loadGeneration || sessionId !== get(selectedSessionId) || mode !== diffMode || !active) return;
      if (diff?.content !== result.content) resetCopyState();
      diff = result;
      files = fileResult || [];
      syncSelection();
      loadedDiffKey = requestedKey;
    } catch (e) {
      if (generation !== loadGeneration || sessionId !== get(selectedSessionId) || mode !== diffMode || !active) return;
      error = String(e);
      diff = null;
      files = [];
      selectedPath = null;
      loadedDiffKey = '';
      resetCopyState();
    }
    if (generation === loadGeneration) loading = false;
    if (generation === loadGeneration && active && !pollTimeout) {
      pollTimeout = setTimeout(() => {
        pollTimeout = null;
        void loadDiff();
      }, 5000);
    }
  }

  // Keep the selection pointing at a file that still exists. After a revert the
  // file usually disappears from the diff entirely, so falling back to the first
  // remaining file avoids showing an empty right pane.
  function syncSelection() {
    if (files.length === 0) {
      selectedPath = null;
      return;
    }
    if (!selectedPath || !files.some(f => f.path === selectedPath)) {
      selectedPath = files[0].path;
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

  function selectFile(path: string) {
    selectedPath = path;
  }

  // Split a path into directory + basename so the list can dim the directory and
  // keep the filename readable at a glance.
  function splitPath(path: string): { dir: string; name: string } {
    const idx = path.lastIndexOf('/');
    if (idx < 0) return { dir: '', name: path };
    return { dir: path.slice(0, idx + 1), name: path.slice(idx + 1) };
  }

  function statusLabel(status: string): string {
    return $t(`diff.status.${status}`);
  }

  // Git's own letters, so the badge reads the same in every language and
  // matches what `git status` shows. The full word is in the tooltip.
  const STATUS_LETTERS: Record<string, string> = {
    modified: 'M',
    added: 'A',
    deleted: 'D',
    renamed: 'R',
  };

  function statusLetter(status: string): string {
    return STATUS_LETTERS[status] || '?';
  }

  // --- Revert -------------------------------------------------------------

  function askRevertFile(file: session.DiffFile) {
    if (reverting) return;
    // An untracked file has no committed version to restore — reverting DELETES
    // it. Say so explicitly; the generic "discard changes" wording would be a
    // lie here.
    const isAdded = file.status === 'added';
    revertTitle = isAdded ? $t('diff.revertFileAddedTitle') : $t('diff.revertFileTitle');
    revertMessage = isAdded
      ? $t('diff.revertFileAddedMessage', { file: file.path })
      : $t('diff.revertFileMessage', { file: file.path });
    pendingRevert = () => App.RevertDiffFile(sessionIdForRevert(), file.path, diffMode === 'session');
    showRevertConfirm = true;
  }

  function askRevertHunk(file: session.DiffFile, hunk: session.DiffHunk) {
    if (reverting) return;
    revertTitle = $t('diff.revertHunkTitle');
    revertMessage = $t('diff.revertHunkMessage', { file: file.path, header: hunk.header });
    // Send the patch back VERBATIM. Rebuilding it here would let a stale view
    // apply cleanly against a file that has since moved on — the round-trip is
    // exactly what makes git refuse.
    const patch = hunk.patch;
    pendingRevert = () => App.RevertDiffHunk(sessionIdForRevert(), patch);
    showRevertConfirm = true;
  }

  function sessionIdForRevert(): string {
    return get(selectedSessionId) || '';
  }

  async function confirmRevert() {
    const action = pendingRevert;
    pendingRevert = null;
    if (!action || reverting) return;
    reverting = true;
    try {
      await action();
      if (destroyed) return;
      error = '';
      // Refresh so the view reflects what's actually on disk now.
      await loadDiff();
    } catch (e) {
      if (destroyed) return;
      // A failed revert must be visible — the Go message explains that the file
      // changed since the diff was rendered.
      error = String(e);
    }
    if (!destroyed) reverting = false;
  }

  function cancelRevert() {
    pendingRevert = null;
  }

  // --- Rendering ----------------------------------------------------------

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

  $: diffContent = diff?.content || '';
  $: currentDiffKey = `${$selectedSessionId || ''}:${diffMode}`;
  $: selectedFile = files.find(f => f.path === selectedPath) || null;
  // Split here rather than in the markup so the header can dim the directory
  // and keep the file name intact when the path is too long to fit.
  $: selectedParts = splitPath(selectedFile?.path || '');

  // Only ONE file renders at a time now, so the large-diff guard measures the
  // selected file rather than the whole diff — a big repo with many small files
  // is perfectly readable and shouldn't trip the warning.
  $: selectedContent = selectedFile && !selectedFile.binary
    ? selectedFile.hunks.map(h => `${h.header}\n${h.body}`).join('\n')
    : '';
  $: isLargeFile =
    selectedContent.length > LARGE_DIFF_BYTES ||
    (selectedContent ? countLines(selectedContent) : 0) > LARGE_DIFF_LINES;
  $: forceShow = !!selectedPath && !!forceShowPaths[selectedPath];

  function showSelectedAnyway() {
    if (!selectedPath) return;
    forceShowPaths = { ...forceShowPaths, [selectedPath]: true };
  }

  // Reset the "show anyway" opt-ins only when the SESSION changes, not on every
  // content update. The diff polls every 5s and its content changes as the
  // agent works; resetting on content (the old behaviour) snapped the user back
  // to the warning every few seconds while they were reading the diff.
  let forceShowSessionId: string | null = null;
  $: if ($selectedSessionId !== forceShowSessionId) {
    forceShowSessionId = $selectedSessionId;
    forceShowPaths = {};
  }

  // Only parse when the tab is active AND (the file is small OR the user opted
  // in). Parsing a huge diff string is itself expensive, so we skip it entirely
  // while showing the warning.
  $: shouldRender = active && !!selectedFile && !selectedFile.binary && (!isLargeFile || forceShow);
  $: renderedHunks = shouldRender && selectedFile
    ? buildHunkViews(selectedFile)
    : [];

  // Build the per-hunk line lists, honouring MAX_DIFF_LINES across the whole
  // file so one pathological hunk can't blow the budget on its own.
  function buildHunkViews(file: session.DiffFile) {
    let budget = MAX_DIFF_LINES;
    return file.hunks.map(hunk => {
      const all = parseDiff(hunk.body);
      const lines = budget > 0 ? all.slice(0, budget) : [];
      budget -= lines.length;
      return { hunk, lines, hidden: all.length - lines.length };
    });
  }

  $: hiddenLineCount = renderedHunks.reduce((n, h) => n + h.hidden, 0);
  $: fileCount = files.length;
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
      <button class="copy-btn" on:click={copyDiff} disabled={!diffContent || loading || copying || loadedDiffKey !== currentDiffKey} title={$t('diff.copy')}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          {#if copyState === 'copied'}
            <polyline points="20 6 9 17 4 12"/>
          {:else}
            <rect x="9" y="9" width="11" height="11" rx="2"/>
            <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>
          {/if}
        </svg>
        {copyState === 'copied' ? $t('diff.copied') : copyState === 'failed' ? $t('diff.copyFailed') : $t('diff.copy')}
      </button>
      <button class="refresh-btn" on:click={() => loadDiff()} disabled={loading}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class:spinning={loading}>
          <path d="M23 4v6h-6M1 20v-6h6"/>
          <path d="M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"/>
        </svg>
      </button>
    </div>
  </div>

  {#if loading && !diff}
    <div class="diff-state loading">{$t('diff.loading')}</div>
  {:else if error && files.length === 0}
    <div class="diff-state error">{error}</div>
  {:else if !diff || files.length === 0}
    <div class="diff-state no-diff">
      <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
        <path d="M12 22c5.523 0 10-4.477 10-10S17.523 2 12 2 2 6.477 2 12s4.477 10 10 10z"/>
        <path d="M8 12l2 2 4-4"/>
      </svg>
      <span>{$t('diff.noChanges')}</span>
      <span class="no-diff-hint">
        {diffMode === 'session' ? $t('diff.sessionHint') : $t('diff.fullHint')}
      </span>
    </div>
  {:else}
    <!-- A revert that failed still leaves a usable diff on screen, so the error
         sits above the panes instead of replacing them. -->
    {#if error}
      <div class="revert-error">{error}</div>
    {/if}

    <div class="diff-body" bind:this={panesEl} class:resizing={isResizingPane}>
      <div class="file-pane" style="width: {filePaneWidth}px">
        <div class="file-pane-header">{$t('diff.filesCount', { count: fileCount })}</div>
        <div class="file-list">
          {#each files as file (file.path)}
            {@const parts = splitPath(file.path)}
            <div
              class="file-row"
              class:selected={file.path === selectedPath}
              role="button"
              tabindex="0"
              on:click={() => selectFile(file.path)}
              on:keydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); selectFile(file.path); } }}
            >
              <div class="file-main">
                <span class="file-status {file.status}" title={statusLabel(file.status)}>{statusLetter(file.status)}</span>
                <span class="file-path" title={file.path}>
                  {#if parts.dir}<span class="file-dir">{parts.dir}</span>{/if}<span class="file-name">{parts.name}</span>
                </span>
              </div>
              <div class="file-meta">
                <span class="stat added">+{file.added}</span>
                <span class="stat removed">-{file.removed}</span>
                <button
                  class="revert-btn file-revert"
                  disabled={reverting}
                  title={$t('diff.revertFile')}
                  on:click|stopPropagation={() => askRevertFile(file)}
                >
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M3 7v6h6"/>
                    <path d="M3.51 13a9 9 0 1 0 2.13-9.36L3 7"/>
                  </svg>
                </button>
              </div>
            </div>
          {/each}
        </div>
      </div>

      <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
      <div
        class="pane-resizer"
        role="separator"
        aria-orientation="vertical"
        title={$t('diff.resizeHint')}
        on:mousedown={startPaneResize}
        on:dblclick={resetPaneWidth}
      ></div>

      <div class="diff-pane">
        <!-- The list shows only the basename, so the full path belongs here —
             otherwise two same-named files in different directories are
             indistinguishable once selected. -->
        {#if selectedFile}
          <div class="selected-path" title={selectedFile.path}>
            <span class="selected-status {selectedFile.status}" title={statusLabel(selectedFile.status)}>
              {statusLetter(selectedFile.status)}
            </span>
            {#if selectedFile.status === 'renamed' && selectedFile.oldPath && selectedFile.oldPath !== selectedFile.path}
              <span class="selected-old">{selectedFile.oldPath}</span>
              <span class="selected-arrow">→</span>
            {/if}
            <span class="selected-text"><span class="selected-dir">{selectedParts.dir}</span><span class="selected-name">{selectedParts.name}</span></span>
            <span class="selected-stats">
              <span class="stat added">+{selectedFile.added}</span>
              <span class="stat removed">-{selectedFile.removed}</span>
            </span>
          </div>
        {/if}

        <div class="diff-content">
          {#if !selectedFile}
            <div class="diff-state no-diff">
              <span>{$t('diff.selectFile')}</span>
            </div>
          {:else if selectedFile.binary}
            <div class="diff-state no-diff">
              <span>{$t('diff.binaryFile')}</span>
            </div>
          {:else if selectedFile.hunks.length === 0}
            <div class="diff-state no-diff">
              <span>{$t('diff.noHunks')}</span>
            </div>
          {:else if isLargeFile && !forceShow}
            <div class="large-diff-warning">
              <svg width="44" height="44" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
                <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
                <line x1="12" y1="9" x2="12" y2="13"/>
                <line x1="12" y1="17" x2="12.01" y2="17"/>
              </svg>
              <span class="large-diff-title">
                {$t('diff.largeFileTitle', { count: selectedFile.added + selectedFile.removed })}
              </span>
              <span class="large-diff-hint">{$t('diff.largeFileHint')}</span>
              <button class="large-diff-show" on:click={showSelectedAnyway}>
                {$t('diff.showAnyway', { count: MAX_DIFF_LINES })}
              </button>
            </div>
          {:else}
            <div class="diff-lines">
              {#each renderedHunks as view (view.hunk.index)}
                <div class="hunk">
                  <div class="diff-line header hunk-header">
                    <code>{view.hunk.header}</code>
                    <button
                      class="revert-btn hunk-revert"
                      disabled={reverting}
                      title={$t('diff.revertHunk')}
                      on:click={() => selectedFile && askRevertHunk(selectedFile, view.hunk)}
                    >
                      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M3 7v6h6"/>
                        <path d="M3.51 13a9 9 0 1 0 2.13-9.36L3 7"/>
                      </svg>
                      {$t('diff.revert')}
                    </button>
                  </div>
                  {#each view.lines as line}
                    <div class="diff-line {line.type}">
                      <code>{line.text}</code>
                    </div>
                  {/each}
                </div>
              {/each}
              {#if hiddenLineCount > 0}
                <div class="diff-line meta diff-truncated">
                  <code>{$t('diff.truncated', { count: hiddenLineCount })}</code>
                </div>
              {/if}
            </div>
          {/if}
        </div>
      </div>
    </div>
  {/if}
</div>

<ConfirmDialog
  bind:show={showRevertConfirm}
  title={revertTitle}
  message={revertMessage}
  confirmText={$t('diff.revert')}
  cancelText={$t('common.cancel')}
  variant="danger"
  on:confirm={confirmRevert}
  on:cancel={cancelRevert}
/>

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

  .copy-btn,
  .refresh-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 28px;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 6px;
    color: #9ca3af;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .copy-btn {
    gap: 6px;
    padding: 0 9px;
    font-size: 11px;
  }

  .refresh-btn {
    width: 28px;
  }

  .copy-btn:hover:not(:disabled),
  .refresh-btn:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.1);
    color: white;
  }

  .copy-btn:disabled,
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

  /* Two-pane layout: file list left, selected file's hunks right. */
  .diff-body {
    flex: 1;
    display: flex;
    min-height: 0;
  }

  /* Width comes from the inline style so it can be dragged; flex must not
     shrink it or the drag would fight the layout. */
  .file-pane {
    flex-shrink: 0;
    display: flex;
    flex-direction: column;
    min-height: 0;
    border-right: 1px solid rgba(255, 255, 255, 0.05);
    background: rgba(0, 0, 0, 0.2);
  }

  /* Sits between the panes; the negative margins let a thin visual divider
     have a comfortable grab area without shifting the layout. */
  .pane-resizer {
    flex: 0 0 1px;
    margin: 0 -3px;
    width: 7px;
    min-width: 7px;
    cursor: col-resize;
    background: transparent;
    z-index: 5;
  }
  .pane-resizer:hover {
    background: rgba(139, 92, 246, 0.3);
  }
  /* While dragging, keep the cursor and kill text selection everywhere in the
     panes — otherwise the drag selects file names as it passes over them. */
  .diff-body.resizing {
    cursor: col-resize;
    user-select: none;
  }
  .diff-body.resizing .pane-resizer {
    background: rgba(139, 92, 246, 0.5);
  }

  .file-pane-header {
    padding: 8px 12px;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #6b7280;
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  }

  .file-list {
    flex: 1;
    overflow: auto;
    min-height: 0;
  }

  .file-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 6px 10px;
    cursor: pointer;
    border-left: 2px solid transparent;
    transition: background 0.15s ease;
  }

  .file-row:hover {
    background: rgba(255, 255, 255, 0.04);
  }

  .file-row.selected {
    background: rgba(139, 92, 246, 0.12);
    border-left-color: #8b5cf6;
  }

  .file-main {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  .file-status {
    flex-shrink: 0;
    width: 16px;
    height: 16px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 4px;
    font-size: 10px;
    font-weight: 700;
    font-family: monospace;
  }

  .file-status.modified {
    background: rgba(96, 165, 250, 0.15);
    color: #60a5fa;
  }

  .file-status.added {
    background: rgba(34, 197, 94, 0.15);
    color: #4ade80;
  }

  .file-status.deleted {
    background: rgba(239, 68, 68, 0.15);
    color: #f87171;
  }

  .file-status.renamed {
    background: rgba(251, 191, 36, 0.15);
    color: #fbbf24;
  }

  .file-path {
    font-size: 12px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    direction: rtl;
    text-align: left;
  }

  .file-dir {
    color: #6b7280;
  }

  .file-name {
    color: #e4e4e7;
    font-weight: 600;
  }

  .file-meta {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
    font-size: 11px;
    font-family: monospace;
    font-weight: 600;
  }

  .revert-btn {
    display: flex;
    align-items: center;
    gap: 4px;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 4px;
    color: #9ca3af;
    cursor: pointer;
    padding: 2px 5px;
    font-size: 10px;
    font-family: inherit;
    transition: all 0.15s ease;
  }

  .revert-btn:hover:not(:disabled) {
    background: rgba(239, 68, 68, 0.15);
    border-color: rgba(239, 68, 68, 0.4);
    color: #f87171;
  }

  .revert-btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  /* Only reveal the per-file revert on hover/selection — a delete-your-work
     button shouldn't sit permanently under the cursor in a long list. */
  .file-revert {
    opacity: 0;
  }

  .file-row:hover .file-revert,
  .file-row.selected .file-revert {
    opacity: 1;
  }

  .revert-error {
    padding: 8px 16px;
    font-size: 12px;
    color: #f87171;
    background: rgba(239, 68, 68, 0.1);
    border-bottom: 1px solid rgba(239, 68, 68, 0.25);
  }

  /* Wraps the path header + the scrolling diff, so only the diff scrolls and
     the path stays put while reading a long file. */
  .diff-pane {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }

  .selected-path {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 7px 12px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
    background: rgba(0, 0, 0, 0.2);
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 11px;
    color: #d4d4d8;
    flex-shrink: 0;
  }
  /* The directory is allowed to shrink away, but the file name never does —
     that's the part worth keeping when the path is too long for the pane. */
  .selected-text {
    display: flex;
    flex: 1;
    min-width: 0;
    white-space: nowrap;
    user-select: text;
  }
  .selected-dir {
    overflow: hidden;
    text-overflow: ellipsis;
    color: #6b7280;
  }
  .selected-name {
    flex-shrink: 0;
    font-weight: 600;
  }
  .selected-old {
    color: #6b7280;
    text-decoration: line-through;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 40%;
  }
  .selected-arrow { color: #6b7280; flex-shrink: 0; }
  .selected-stats { display: flex; gap: 6px; flex-shrink: 0; }
  .selected-status {
    flex-shrink: 0;
    width: 16px;
    text-align: center;
    border-radius: 3px;
    font-size: 10px;
    font-weight: 700;
    line-height: 16px;
  }
  .selected-status.modified { background: rgba(234, 179, 8, 0.15); color: #eab308; }
  .selected-status.added { background: rgba(34, 197, 94, 0.15); color: #22c55e; }
  .selected-status.deleted { background: rgba(239, 68, 68, 0.15); color: #ef4444; }
  .selected-status.renamed { background: rgba(139, 92, 246, 0.15); color: #a78bfa; }

  .diff-content {
    flex: 1;
    overflow: auto;
    min-width: 0;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 12px;
    user-select: text;
    -webkit-user-select: text;
  }

  .diff-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: #4b5563;
    gap: 12px;
  }

  .diff-state.error {
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

  .hunk-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .diff-content::-webkit-scrollbar,
  .file-list::-webkit-scrollbar {
    width: 6px;
  }

  .diff-content::-webkit-scrollbar-track,
  .file-list::-webkit-scrollbar-track {
    background: transparent;
  }

  .diff-content::-webkit-scrollbar-thumb,
  .file-list::-webkit-scrollbar-thumb {
    background: rgba(139, 92, 246, 0.3);
    border-radius: 3px;
  }
</style>
