<script lang="ts">
  import { onDestroy } from 'svelte';
  import { selectedSessionId } from '../../stores/sessions';
  import { get } from 'svelte/store';
  import * as App from '../../../../wailsjs/go/main/App';
  import { ClipboardSetText } from '../../../../wailsjs/runtime/runtime';
  import type { session } from '../../../../wailsjs/go/models';
  import ConfirmDialog from '../Dialogs/ConfirmDialog.svelte';
  import { t } from '../../i18n';
  import { settings, saveSettings } from '../../stores/settings';
  import { buildTreeRows, type TreeRow } from '../../utils/fileTree';
  import { fileTypeOf } from '../../utils/fileTypes';

  export let active = false;
  export let initialMode: 'session' | 'full' = 'session';

  interface DiffData {
    content: string;
    added: number;
    removed: number;
  }

  let diff: DiffData | null = null;
  let files: session.DiffFileSummary[] = [];
  let selectedPath: string | null = null;
  // The selected file's hunks, fetched on demand and kept for as long as the
  // list is unchanged — reopening a file already looked at should be instant.
  let fileCache: Record<string, session.DiffFile | null> = {};
  let loadingFile = false;
  // Identifies the current set of changes, so the cache is dropped when the
  // files or their line counts move.
  let listKey = '';
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
    if (!files.length || loading || copying || loadedDiffKey !== currentDiffKey) return;
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;
    const generation = ++copyGeneration;
    copying = true;
    try {
      // Fetched here rather than kept in memory: this is the whole diff, the
      // one thing that is genuinely expensive, and it is only needed when the
      // button is actually pressed.
      const whole = diffMode === 'session'
        ? await App.GetSessionDiff(sessionId)
        : await App.GetFullDiff(sessionId);
      if (destroyed || generation !== copyGeneration) return;
      const content = whole?.content || '';
      if (!content) { copying = false; return; }
      const copied = await ClipboardSetText(content);
      if (destroyed || generation !== copyGeneration) return;
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
      // Only the file LIST is fetched here. Loading every file's contents up
      // front is what made this view unusable while something was writing a lot
      // of files — a build dropping its output into the tree produced a diff the
      // webview never finished rendering. A file's hunks are fetched when it is
      // opened, so the cost of listing does not depend on what the files hold.
      const fileResult = mode === 'session'
        ? await App.GetSessionDiffFileList(sessionId)
        : await App.GetFullDiffFileList(sessionId);
      if (generation !== loadGeneration || sessionId !== get(selectedSessionId) || mode !== diffMode || !active) return;
      const summaries = fileResult || [];
      const totals = summaries.reduce(
        (acc, f) => ({ added: acc.added + (f.added || 0), removed: acc.removed + (f.removed || 0) }),
        { added: 0, removed: 0 }
      );
      const nextKey = summaries.map(f => `${f.path}:${f.added}:${f.removed}`).join('|');
      if (listKey !== nextKey) {
        resetCopyState();
        listKey = nextKey;
        fileCache = {};
      }
      diff = { content: '', added: totals.added, removed: totals.removed };
      files = summaries;
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

  function askRevertFile(file: session.DiffFileSummary) {
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

  $: currentDiffKey = `${$selectedSessionId || ''}:${diffMode}`;
  // Grouped by what happened to the file, because those are different kinds of
  // change to review: a modification is read line by line, a new file is read
  // as a whole, and a deletion usually only needs confirming. Mixed together
  // they are hard to scan, especially when a run has touched many files.
  //
  // Order is deliberate: modified first (the usual work), then added, then
  // renamed, then deleted last (least often what you came to look at).
  const GROUP_ORDER = ['modified', 'added', 'renamed', 'deleted'] as const;
  type StatusFilter = 'all' | typeof GROUP_ORDER[number];

  // Which kinds of change are on screen.
  //
  // Starts on modified, because that is nearly always the work being reviewed
  // and it is what a flood of new files buries: one real repository here had
  // ten modified files against 2291 untracked ones from an IDE directory that
  // is not in .gitignore. The reset below turns this back to 'all' when there
  // is nothing modified, so a change made entirely of new files still shows
  // rather than opening on an empty list.
  let statusFilter: StatusFilter = 'modified';

  // Counts come from the unfiltered list, so a button always says how many
  // files it would show — including the one currently active.
  $: filterCounts = {
    all: files.length,
    modified: files.filter(f => (f.status || 'modified') === 'modified').length,
    added: files.filter(f => f.status === 'added').length,
    renamed: files.filter(f => f.status === 'renamed').length,
    deleted: files.filter(f => f.status === 'deleted').length,
  };
  // Only offer a filter there is something to filter to.
  $: availableFilters = (['all', ...GROUP_ORDER] as StatusFilter[])
    .filter(f => f === 'all' || filterCounts[f] > 0);
  // A filter whose files have all gone (reverted, or the build finished) would
  // leave an empty list with no obvious way back.
  $: if (statusFilter !== 'all' && filterCounts[statusFilter] === 0) statusFilter = 'all';

  // Back to the default when the diff being shown changes. Carrying a filter
  // across sessions means the one session where a flood of new files makes it
  // matter is the one that has lost it, because you widened the filter
  // somewhere else.
  let filterKey = '';
  $: {
    const key = `${$selectedSessionId || ''}:${diffMode}`;
    if (key !== filterKey) {
      filterKey = key;
      statusFilter = 'modified';
    }
  }

  $: visibleFiles = statusFilter === 'all'
    ? files
    : files.filter(f => (f.status || 'modified') === statusFilter);

  // Keep the selection on something the filter actually shows: leaving it on a
  // hidden file means the pane displays a diff for a row the user cannot see.
  $: if (visibleFiles.length && selectedPath &&
         !visibleFiles.some(f => f.path === selectedPath)) {
    selectFile(visibleFiles[0].path);
  }

  $: fileGroups = GROUP_ORDER
    .map(status => ({
      status,
      label: $t(`diff.group.${status}`),
      files: visibleFiles.filter(f => (f.status || 'modified') === status),
    }))
    .filter(g => g.files.length > 0);

  $: selectedSummary = files.find(f => f.path === selectedPath) || null;
  $: selectedFile = selectedPath ? fileCache[selectedPath] ?? null : null;
  // Fetch the hunks the first time a file is opened.
  $: if (selectedPath && !(selectedPath in fileCache)) void loadSelectedFile(selectedPath);

  async function loadSelectedFile(path: string) {
    const sessionId = get(selectedSessionId);
    const mode = diffMode;
    if (!sessionId) return;
    loadingFile = true;
    try {
      const loaded = mode === 'session'
        ? await App.GetSessionDiffForFile(sessionId, path)
        : await App.GetFullDiffForFile(sessionId, path);
      // Ignore a result that arrived after the user moved on, so a slow file
      // cannot overwrite the one now on screen.
      if (path !== selectedPath || mode !== diffMode || sessionId !== get(selectedSessionId)) return;
      fileCache = { ...fileCache, [path]: loaded };
    } catch (e) {
      if (path !== selectedPath) return;
      // Cached as null: a file that fails to load should not be retried on
      // every reactive pass.
      fileCache = { ...fileCache, [path]: null };
      error = String(e);
    } finally {
      if (path === selectedPath) loadingFile = false;
    }
  }
  // Split here rather than in the markup so the header can dim the directory
  // and keep the file name intact when the path is too long to fit.
  $: selectedParts = splitPath(selectedSummary?.path || '');

  // Only ONE file renders at a time now, so the large-diff guard measures the
  // selected file rather than the whole diff — a big repo with many small files
  // is perfectly readable and shouldn't trip the warning.
  $: selectedContent = selectedFile && !selectedFile.binary && selectedFile.hunks
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

  // --- File tree ----------------------------------------------------------

  // Directories the user has folded shut. Everything else is open: opening the
  // diff means wanting to see what changed, so expanded is the useful default.
  let collapsedDirs = new Set<string>();
  // Stored inverted (see Settings.DiffFlatFileList): the tree is the default,
  // and an unset flag has to mean "tree" for configs written before this.
  $: treeView = !$settings.diffFlatFileList;
  // Rebuilt whenever the file list or the fold state changes; the builder is
  // cheap (one pass per file) and the list is at most a few hundred rows.
  $: treeRows = treeView ? buildTreeRows<session.DiffFileSummary>(visibleFiles, collapsedDirs) : [];

  function toggleTreeView() {
    void saveSettings({ diffFlatFileList: treeView });
  }

  function toggleDir(path: string) {
    // Reassign rather than mutate — Svelte 3 doesn't track Set mutations.
    const next = new Set(collapsedDirs);
    if (next.has(path)) next.delete(path);
    else next.add(path);
    collapsedDirs = next;
  }

  // Fold state is keyed by directory path, so it would silently apply to a
  // different repo's directories after a session switch. Reset it with the
  // session; the tracking variable is assigned INSIDE the block because Svelte
  // 3 orders reactive statements by dependency, not by source position.
  let collapseSessionId: string | null = null;
  $: if ($selectedSessionId !== collapseSessionId) {
    collapseSessionId = $selectedSessionId;
    collapsedDirs = new Set<string>();
  }

  // Indent step in px. Small enough that a deep tree still leaves room for the
  // file name in a narrow pane, wide enough to read as a level.
  const TREE_INDENT = 12;

  // Hoisted out of the markup: Svelte's template can't narrow the row union,
  // and casts aren't allowed there.
  function rowFile(row: TreeRow<session.DiffFileSummary>): session.DiffFileSummary {
    return (row as { kind: 'file'; file: session.DiffFileSummary }).file;
  }
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
      <button class="copy-btn" on:click={copyDiff} disabled={!files.length || loading || copying || loadedDiffKey !== currentDiffKey} title={$t('diff.copy')}>
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
        <div class="file-pane-header">
          <span>{$t('diff.filesCount', { count: fileCount })}</span>
          <button
            class="view-toggle"
            class:active={treeView}
            title={treeView ? $t('diff.showFlatList') : $t('diff.showTree')}
            aria-pressed={treeView}
            on:click={toggleTreeView}
          >
            <!-- Both glyphs are drawn symmetrically within the 24px box (4..20),
                 so neither sits visibly off-centre in the square button. -->
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <!-- Both glyphs use the same three rows at y=6/12/18 spanning
                   x=4..20, so the button's weight doesn't shift as it toggles.
                   Kept coarse: at 14px, detail finer than ~4 units muddies. -->
              {#if treeView}
                <!-- Flat list: bullet + rule per row. -->
                <circle cx="5" cy="6" r="1.1" fill="currentColor" stroke="none"/>
                <circle cx="5" cy="12" r="1.1" fill="currentColor" stroke="none"/>
                <circle cx="5" cy="18" r="1.1" fill="currentColor" stroke="none"/>
                <line x1="9" y1="6" x2="20" y2="6"/>
                <line x1="9" y1="12" x2="20" y2="12"/>
                <line x1="9" y1="18" x2="20" y2="18"/>
              {:else}
                <!-- Tree: rows at two indent levels. A literal tree — spine
                     plus elbows plus branches — needs more strokes than 14px
                     can hold; drawn that way it read as a letter (first "T",
                     then "E"). Indentation alone is what the view is about,
                     and it survives the size. -->
                <line x1="4" y1="6" x2="20" y2="6"/>
                <line x1="10" y1="12" x2="20" y2="12"/>
                <line x1="10" y1="18" x2="20" y2="18"/>
              {/if}
            </svg>
          </button>
        </div>
        {#if files.length > 0}
          <div class="status-filters">
            {#each availableFilters as f (f)}
              <button
                class="status-filter"
                class:active={statusFilter === f}
                on:click={() => statusFilter = f}
              >
                {f === 'all' ? $t('diff.filterAll') : $t(`diff.group.${f}`)}
                <span class="status-filter-count">{filterCounts[f]}</span>
              </button>
            {/each}
          </div>
        {/if}
        {#if treeView}
          <div class="file-list">
            {#each treeRows as row (row.kind + ':' + row.path)}
              {#if row.kind === 'dir'}
                <div
                  class="tree-dir"
                  style="padding-left: {10 + row.depth * TREE_INDENT}px"
                  role="button"
                  tabindex="0"
                  aria-expanded={!collapsedDirs.has(row.path)}
                  title={row.path}
                  on:click={() => toggleDir(row.path)}
                  on:keydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggleDir(row.path); } }}
                >
                  <div class="file-main">
                    <svg
                      class="tree-caret"
                      class:collapsed={collapsedDirs.has(row.path)}
                      width="12" height="12" viewBox="0 0 24 24"
                      fill="none" stroke="currentColor" stroke-width="2"
                    >
                      <polyline points="6 9 12 15 18 9"/>
                    </svg>
                    <span class="tree-dir-name">{row.label}</span>
                    <span class="tree-dir-count">{row.fileCount}</span>
                  </div>
                  <div class="file-meta">
                    <span class="stat added">+{row.added}</span>
                    <span class="stat removed">-{row.removed}</span>
                  </div>
                </div>
              {:else}
                {@const file = rowFile(row)}
                {@const type = fileTypeOf(file.path)}
                <div
                  class="file-row tree-file"
                  class:selected={file.path === selectedPath}
                  style="padding-left: {10 + row.depth * TREE_INDENT}px"
                  role="button"
                  tabindex="0"
                  on:click={() => selectFile(file.path)}
                  on:keydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); selectFile(file.path); } }}
                >
                  <div class="file-main">
                    <span class="file-status {file.status}" title={statusLabel(file.status)}>{statusLetter(file.status)}</span>
                    <span
                      class="file-type-dot"
                      style="background: {type.colour}"
                      title="{$t('diff.fileType')}: {type.label}"
                    ></span>
                    <span class="file-path tree-file-path" title={file.path}>
                      <span class="file-name">{splitPath(file.path).name}</span>
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
              {/if}
            {/each}
          </div>
        {:else}
        <div class="file-list">
          {#each fileGroups as group (group.status)}
            <div class="file-group-header">
              <span class="file-group-name">{group.label}</span>
              <span class="file-group-count">{group.files.length}</span>
            </div>
          {#each group.files as file (file.path)}
            {@const parts = splitPath(file.path)}
            {@const type = fileTypeOf(file.path)}
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
                <span
                  class="file-type-dot"
                  style="background: {type.colour}"
                  title="{$t('diff.fileType')}: {type.label}"
                ></span>
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
          {/each}
        </div>
        {/if}
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
    font-size: 13px;
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
    font-size: 13px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #6b7280;
  }

  .diff-stats {
    display: flex;
    gap: 8px;
    font-size: 13px;
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
    font-size: 12px;
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
    background: rgba(var(--accent-rgb), 0.3);
  }
  /* While dragging, keep the cursor and kill text selection everywhere in the
     panes — otherwise the drag selects file names as it passes over them. */
  .diff-body.resizing {
    cursor: col-resize;
    user-select: none;
  }
  .diff-body.resizing .pane-resizer {
    background: rgba(var(--accent-rgb), 0.5);
  }

  .file-pane-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 4px 8px 4px 12px;
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #6b7280;
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  }

  .view-toggle {
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: 24px;
    height: 24px;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 4px;
    color: #6b7280;
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .view-toggle:hover {
    background: rgba(255, 255, 255, 0.08);
    color: #e4e4e7;
  }
  .view-toggle.active {
    background: rgba(var(--accent-rgb), 0.15);
    border-color: rgba(var(--accent-rgb), 0.35);
    color: var(--accent-light);
  }

  /* Directory rows read as structure, not as content: no status badge, no
     revert, and a dimmer weight than the file names underneath them. */
  .tree-dir {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 5px 10px;
    cursor: pointer;
    border-left: 2px solid transparent;
    transition: background 0.15s ease;
  }
  .tree-dir:hover {
    background: rgba(255, 255, 255, 0.04);
  }

  .tree-caret {
    flex-shrink: 0;
    color: #6b7280;
    transition: transform 0.15s ease;
  }
  .tree-caret.collapsed {
    transform: rotate(-90deg);
  }

  .tree-dir-name {
    font-size: 13px;
    font-weight: 600;
    color: #9ca3af;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    direction: rtl;
    text-align: left;
  }

  .tree-dir-count {
    flex-shrink: 0;
    padding: 0 5px;
    border-radius: 8px;
    background: rgba(255, 255, 255, 0.06);
    color: #6b7280;
    font-size: 11px;
    font-family: monospace;
    font-weight: 600;
    line-height: 15px;
  }

  /* Inside the tree the directory is already the row above, so the file row
     shows only the basename — and it must not be reversed like the flat
     list's full path. */
  .tree-file-path {
    direction: ltr;
  }

  .file-list {
    flex: 1;
    overflow: auto;
    min-height: 0;
  }

  /* Shown whenever there are files. Hiding the row until several kinds of
     change existed meant it was missing exactly when someone went looking for
     it, and a filter you have to discover by accident is not much use. */
  .status-filters {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    padding: 8px 10px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  }

  .status-filter {
    display: flex;
    align-items: center;
    gap: 5px;
    padding: 3px 8px;
    border: 1px solid transparent;
    border-radius: 5px;
    background: rgba(255, 255, 255, 0.04);
    color: #9ca3af;
    font-size: 11px;
    cursor: pointer;
    transition: background 0.12s, color 0.12s, border-color 0.12s;
  }

  .status-filter:hover {
    background: rgba(255, 255, 255, 0.08);
    color: #d1d5db;
  }

  .status-filter.active {
    background: rgba(139, 92, 246, 0.16);
    border-color: rgba(139, 92, 246, 0.4);
    color: #c4b5fd;
  }

  .status-filter-count {
    font-size: 10px;
    opacity: 0.75;
  }

  /* A quiet divider rather than a heading: the groups are there to make the
     list scannable, not to compete with the file names for attention. */
  .file-group-header {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 10px 4px;
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: #6b7280;
  }

  .file-group-header:first-child {
    padding-top: 4px;
  }

  .file-group-count {
    padding: 0 5px;
    border-radius: 8px;
    background: rgba(255, 255, 255, 0.06);
    font-size: 9px;
    font-weight: 500;
    letter-spacing: 0;
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
    background: rgba(var(--accent-rgb), 0.12);
    border-left-color: var(--accent);
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
    font-size: 11px;
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

  /* A dot rather than a second lettered badge: the status letter already owns
     that slot, the pane is narrow and resizable, and a per-language glyph would
     have to shrink or truncate as the pane narrows. A 6px dot costs a fixed
     14px whatever the type is, so the filename never loses room — and the
     type's own token stays available in the tooltip. */
  .file-type-dot {
    flex-shrink: 0;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    /* The colours are chosen to carry at full strength on the pane background;
       dimming them is what would make the neutral default vanish. */
    opacity: 0.85;
  }
  .file-row:hover .file-type-dot,
  .file-row.selected .file-type-dot {
    opacity: 1;
  }

  .file-path {
    font-size: 13px;
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

  /* In the flat list the weight separates the name from the dimmed directory
     prefix it shares a line with. A tree row has no prefix — the directory is
     the row above — so the emphasis is noise there. */
  .tree-file .file-name {
    font-weight: 400;
  }

  .file-meta {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
    font-size: 12px;
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
    font-size: 11px;
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
    font-size: 13px;
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
    font-size: 12px;
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
    font-size: 11px;
    font-weight: 700;
    line-height: 16px;
  }
  .selected-status.modified { background: rgba(234, 179, 8, 0.15); color: #eab308; }
  .selected-status.added { background: rgba(34, 197, 94, 0.15); color: #22c55e; }
  .selected-status.deleted { background: rgba(239, 68, 68, 0.15); color: #ef4444; }
  .selected-status.renamed { background: rgba(var(--accent-rgb), 0.15); color: var(--accent-light); }

  .diff-content {
    flex: 1;
    overflow: auto;
    min-width: 0;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 13px;
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
    font-size: 13px;
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
    color: var(--accent);
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
    background: rgba(var(--accent-rgb), 0.3);
    border-radius: 3px;
  }
</style>
