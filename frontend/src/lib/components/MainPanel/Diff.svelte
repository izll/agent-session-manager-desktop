<script lang="ts">
  import { onDestroy, tick } from 'svelte';
  import { requestFileJump } from '../../stores/fileJump';

  /**
   * Leave the diff for the file browser, where the file can be edited.
   *
   * Jumps to the first changed line rather than the top: the reason for opening
   * a file from a diff is the change, and a long file would otherwise arrive
   * scrolled nowhere near it.
   */
  function openFileInBrowser(path: string) {
    // Where the reader is looking, when that can be established: opening the
    // file at the line showing at the top of the diff means the editor arrives
    // at the change being read, rather than at the file's first change — which
    // in a long file can be thousands of lines away.
    //
    // Only for the file currently displayed; the button on another file in the
    // list has no viewport of its own to ask, and falls back to its first hunk.
    let line: number | undefined;
    if (path === selectedPath) {
      if (sideBySide) {
        line = sideBySideView?.topVisibleNewLine() ?? undefined;
      } else if (wholeFileView) {
        // The whole-file view lists the file itself, so its row index IS the
        // line number — 0-based there, 1-based in an editor.
        const at = virtualLines?.firstVisibleLine();
        if (typeof at === 'number') line = at + 1;
      }
      // The unified hunks view is neither: its rows are hunk lines with gaps
      // between them, so a row index means nothing to the file. It falls
      // through to the first hunk below.
    }
    if (line === undefined && selectedFile?.path === path) {
      // Only the loaded file carries its hunks; the list holds summaries, which
      // have the counts but not the content. For any other file there is
      // nothing to aim at, so the browser opens it at the top.
      const firstHunk = selectedFile.hunks?.[0];
      line = firstHunk ? parseHunkHeader(firstHunk.header).newStart : undefined;
    }

    requestFileJump(path, line);
  }
  import { selectedSessionId, selectedWindowIdx } from '../../stores/sessions';
  import { parseDiff, buildHunkViews } from '../../utils/diffParse';
  import { get } from 'svelte/store';
  import * as App from '../../../../wailsjs/go/main/App';
  import { ClipboardSetText } from '../../../../wailsjs/runtime/runtime';
  import type { session } from '../../../../wailsjs/go/models';
  import ConfirmDialog from '../Dialogs/ConfirmDialog.svelte';
  import { t } from '../../i18n';
  import VirtualLines from './VirtualLines.svelte';
  import SideBySideDiff from './SideBySideDiff.svelte';
  import { matchesShortcut } from '../../stores/shortcuts';
  import { memoHighlightLine } from '../../utils/highlightLine';
  import { cachedLanguage, loadLanguage } from '../../utils/codemirror';
  import type { LanguageSupport } from '@codemirror/language';
  import { settings, saveSettings } from '../../stores/settings';
  import { buildTreeRows, type TreeRow } from '../../utils/fileTree';
  import { fileTypeOf } from '../../utils/fileTypes';
  import { rememberPlace, recallPlace, noteListKey, cacheDiff, cachedDiff, invalidateDiffCache } from '../../utils/diffViewState';
  import { buildBlockPatch } from '../../utils/blockPatch';
  import { parseHunkHeader } from '../../utils/sideBySide';

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
  type DiffTarget = { sessionId: string; windowIdx: number; mode: 'session' | 'full'; root: string };
  let pendingRevert: { target: DiffTarget; run: () => Promise<void> } | null = null;
  let loadedRoot = '';

  function currentTarget(): DiffTarget | null {
    const sessionId = get(selectedSessionId);
    if (!sessionId) return null;
    if (!loadedRoot || loadedDiffKey !== currentDiffKey) return null;
    return { sessionId, windowIdx: tabIdx(), mode: diffMode, root: loadedRoot };
  }

  function isCurrentTarget(target: DiffTarget): boolean {
    return target.sessionId === get(selectedSessionId) &&
      target.windowIdx === tabIdx() && target.mode === diffMode &&
      target.root === loadedRoot && loadedDiffKey === currentDiffKey;
  }

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
    // Where the review had got to, so coming back resumes rather than restarts.
    savePlace();
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
    if (!files.length || loading || copying || loadedDiffKey !== currentDiffKey || !loadedRoot) return;
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;
    const generation = ++copyGeneration;
    copying = true;
    try {
      // Fetched here rather than kept in memory: this is the whole diff, the
      // one thing that is genuinely expensive, and it is only needed when the
      // button is actually pressed.
      const whole = diffMode === 'session'
        ? await App.GetSessionDiff(sessionId, tabIdx(), loadedRoot)
        : await App.GetFullDiff(sessionId, tabIdx(), loadedRoot);
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
    const windowIdx = tabIdx();
    const mode = diffMode;
    const generation = ++loadGeneration;
    const requestedKey = `${sessionId || ''}:${windowIdx}:${mode}`;
    if (!sessionId) {
      diff = null;
      files = [];
      selectedPath = null;
      loadedDiffKey = '';
      loadedRoot = '';
      resetCopyState();
      error = '';
      return;
    }

    if (loadedDiffKey !== requestedKey) {
      loadedRoot = '';
      diff = null;
      files = [];
      selectedPath = null;
      resetCopyState();
    }
    // Keyed on session AND tab, matching the guard that calls this: a tab with
    // its own directory is a different diff even within one session.
    lastSessionId = `${sessionId}:${windowIdx}`;
    loading = files.length === 0;
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
      const root = await App.GetTabWorkingDirectory(sessionId, windowIdx);
      if (generation !== loadGeneration || sessionId !== get(selectedSessionId) ||
          windowIdx !== tabIdx() || mode !== diffMode || !active) return;
      if (!root) throw new Error('diff target has no working directory');
      const rootCacheKey = `${requestedKey}\x1f${root}`;
      if (loadedDiffKey !== requestedKey || loadedRoot !== root) {
        const cached = cachedDiff(rootCacheKey) as { diff: DiffData | null; files: session.DiffFileSummary[] } | null;
        if (cached) {
          diff = cached.diff;
          files = cached.files;
          loadedDiffKey = requestedKey;
          loadedRoot = root;
        } else {
          diff = null;
          files = [];
          selectedPath = null;
          resetCopyState();
        }
      }
      const fileResult = mode === 'session'
        ? await App.GetSessionDiffFileList(sessionId, windowIdx, root)
        : await App.GetFullDiffFileList(sessionId, windowIdx, root);
      if (generation !== loadGeneration || sessionId !== get(selectedSessionId) ||
          windowIdx !== tabIdx() || mode !== diffMode || !active) return;
      const summaries = fileResult || [];
      const totals = summaries.reduce(
        (acc, f) => ({ added: acc.added + (f.added || 0), removed: acc.removed + (f.removed || 0) }),
        { added: 0, removed: 0 }
      );
      const nextKey = summaries.map(f => `${f.path}:${f.added}:${f.removed}`).join('|');
      // Compared against a record kept outside the component. Held inside, it
      // came back empty after a tab switch, so the first load always looked
      // like a change — dropping the very places the switch was meant to
      // preserve, and the view never resumed.
      //
      // The places are forgotten with it: the files changed underneath, so the
      // cached contents are stale, and so is any offset into them.
      if (noteListKey(sessionId, nextKey, windowIdx, mode, root)) {
        resetCopyState();
        fileCache = {};
      }
      diff = { content: '', added: totals.added, removed: totals.removed };
      files = summaries;
      loadedDiffKey = requestedKey;
      loadedRoot = root;
      syncSelection();
      cacheDiff(rootCacheKey, { diff, files });
    } catch (e) {
      if (generation !== loadGeneration || sessionId !== get(selectedSessionId) ||
          windowIdx !== tabIdx() || mode !== diffMode || !active) return;
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

  /**
   * Which file this session had open, so leaving the diff tab and coming back
   * resumes rather than restarts.
   *
   * Switching tabs destroys this component — the two mount points are in
   * exclusive branches — so anything worth keeping has to live outside it.
   * Written on selection rather than on destroy, since a component being torn
   * down is a poor moment to start saving.
   */
  function rememberOpenFile(path: string) {
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;
    const current = get(settings).diffLastFile ?? {};
    const target = `${sessionId}:${tabIdx()}:${diffMode}`;
    if (current[target] === path) return;
    void saveSettings({ diffLastFile: { ...current, [target]: path } });
  }

  // Keep the selection pointing at a file that still exists. After a revert the
  // file usually disappears from the diff entirely, so falling back to the first
  // remaining file avoids showing an empty right pane.
  function syncSelection() {
    if (files.length === 0) {
      selectedPath = null;
      return;
    }
    if (selectedPath && files.some(f => f.path === selectedPath)) return;

    // Back to what was open before the tab switch, if it is still in the diff.
    const sessionId = get(selectedSessionId);
    const remembered = sessionId
      ? get(settings).diffLastFile?.[`${sessionId}:${tabIdx()}:${diffMode}`] ??
        get(settings).diffLastFile?.[sessionId]
      : null;
    selectedPath = remembered && files.some(f => f.path === remembered)
      ? remembered
      : files[0].path;
  }

  // Reload when session changes — but ONLY while the Diff tab is actually
  // visible. Previously this ran on EVERY session switch even when the Diff tab
  // was hidden, so switching to a session with a huge repo (WebErp) fetched and
  // parsed its enormous diff in the background — freezing the UI on a plain tab
  // switch. Gating on `active` means the diff only ever runs when you're looking
  // at it.
  $: diffKey = `${$selectedSessionId ?? ''}:${$selectedWindowIdx ?? 0}`;
  $: if (active && diffKey !== lastSessionId) {
    loadDiff();
  }

  // Reload when mode changes
  function handleModeChange(mode: 'session' | 'full') {
    diffMode = mode;
    // The cache is keyed on the mode, but a revert or a mode change means the
    // old list is wrong for the new one too — drop it rather than risk showing
    // a file that is no longer in the diff.
    invalidateDiffCache();
    fileCache = {};
    loadDiff();
  }

  function selectFile(path: string) {
    // Where the file being left had got to, so returning to it resumes. On
    // destroy alone this was lost the moment another file was picked.
    savePlace();
    selectedPath = path;
    rememberOpenFile(path);
    // Picking a file from the list is not stepping, so it carries no direction:
    // a hint left over from the last press would point somewhere the user was
    // no longer heading.
    stepDirection = null;
  }

  // Split a path into directory + basename so the list can dim the directory and
  // keep the filename readable at a glance.
  function splitPath(path: string): { dir: string; name: string } {
    const idx = path.lastIndexOf('/');
    if (idx < 0) return { dir: '', name: path };
    return { dir: path.slice(0, idx + 1), name: path.slice(idx + 1) };
  }

  // Takes the translate function rather than reading $t inside: called from
  // the markup, Svelte re-runs this only when its arguments change, so a
  // language switch would leave the old wording on screen.
  function statusLabel(status: string, tr: typeof $t): string {
    return tr(`diff.status.${status}`);
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
    const target = currentTarget();
    if (!target) return;
    // An untracked file has no committed version to restore — reverting DELETES
    // it. Say so explicitly; the generic "discard changes" wording would be a
    // lie here.
    const isAdded = file.status === 'added';
    revertTitle = isAdded ? $t('diff.revertFileAddedTitle') : $t('diff.revertFileTitle');
    revertMessage = isAdded
      ? $t('diff.revertFileAddedMessage', { file: file.path })
      : $t('diff.revertFileMessage', { file: file.path });
    pendingRevert = {
      target,
      run: () => App.RevertDiffFile(target.sessionId, file.path, target.mode === 'session', target.windowIdx, target.root),
    };
    showRevertConfirm = true;
  }

  function askRevertHunk(file: session.DiffFile, hunk: session.DiffHunk) {
    if (reverting) return;
    const target = currentTarget();
    if (!target) return;
    revertTitle = $t('diff.revertHunkTitle');
    revertMessage = $t('diff.revertHunkMessage', { file: file.path, header: hunk.header });
    // Where to look once the file is rewritten: the line the hunk starts at, in
    // the old file's numbering, which is what reverting restores.
    returnToLine = Math.max(1, parseHunkHeader(hunk.header).oldStart);
    // Send the patch back VERBATIM. Rebuilding it here would let a stale view
    // apply cleanly against a file that has since moved on — the round-trip is
    // exactly what makes git refuse.
    const patch = hunk.patch;
    pendingRevert = {
      target,
      run: () => App.RevertDiffHunk(target.sessionId, patch, target.windowIdx, target.root),
    };
    showRevertConfirm = true;
  }

  /**
   * Revert one block of changes from the side-by-side view.
   *
   * In hunks-only view a hunk IS one change, so its own patch is sent back
   * verbatim — the round trip is what makes git refuse a patch that no longer
   * fits, and rebuilding it would lose that.
   *
   * Whole-file view asks git for the file as a single hunk, so that patch
   * covers every change in the file and reverting one block would discard all
   * of them. The arrows used to be disabled there for that reason, which left
   * a button on screen that did nothing. Instead the patch is cut down to the
   * block: see utils/blockPatch.
   */
  function askRevertBlock(detail: { hunkIndex: number; fromRow: number; toRow: number }) {
    if (reverting || !selectedFile) return;
    const target = currentTarget();
    if (!target) return;
    const hunk = selectedFile.hunks.find((h) => h.index === detail.hunkIndex);
    if (!hunk) return;

    if (!wholeFileView) {
      askRevertHunk(selectedFile, hunk);
      return;
    }

    // The rows the columns report are paired ones; the patch is built from the
    // hunk's own lines, which pairing reorders.
    const range = sideBySideView?.hunkRange(detail.fromRow, detail.toRow);
    if (!range) return;

    const { oldStart, newStart } = parseHunkHeader(hunk.header);
    const patch = buildBlockPatch(
      selectedFile.path,
      selectedFile.oldPath || null,
      // parseDiff keeps each line's leading +/-/space; the patch builder adds
      // its own marker, so the original has to come off or every line would
      // carry two.
      parseDiff(hunk.body).map((line) => ({
        type: line.type,
        text: line.type === 'add' || line.type === 'remove' || line.type === 'context'
          ? line.text.slice(1)
          : line.text,
      })),
      oldStart,
      newStart,
      range.from,
      range.to,
    );
    if (!patch) return;

    /**
     * Where to look after the file is rewritten.
     *
     * In the OLD file's numbering, which is what reverting restores. Walking
     * the hunk to the block counts the lines that exist in that file: removals
     * and context, not additions.
     *
     * The count lands ON the block's first line in the old file — the line the
     * change happened at, which is exactly where to look. It is not the line
     * ABOVE: subtracting one as well as scrolling it a third of the way down
     * put the view consistently higher than the place being worked on.
     */
    const lines = parseDiff(hunk.body);
    let blockLine = oldStart;
    for (let at = 0; at < range.from; at++) {
      if (lines[at].type === 'remove' || lines[at].type === 'context') blockLine++;
    }
    returnToLine = Math.max(1, blockLine);

    revertTitle = $t('diff.revertHunkTitle');
    revertMessage = $t('diff.revertBlockMessage', { file: selectedFile.path });
    pendingRevert = {
      target,
      run: () => App.RevertDiffHunk(target.sessionId, patch, target.windowIdx, target.root),
    };
    showRevertConfirm = true;
  }

  /**
   * The line to come back to after a revert, in the file's NEW numbering.
   *
   * Reverting rewrites the file, so the diff is reloaded and the remembered
   * scroll position is dropped with it — an offset into a file that has just
   * changed lands somewhere arbitrary. That is right for a change made outside
   * the app, but here the change is one the user just made, at a place they
   * were looking at, and being thrown back to the top of a long file loses it.
   *
   * A line number rather than an offset: the reverted lines are gone, so every
   * pixel below them has moved.
   */
  let returnToLine: number | null = null;

  /**
   * Go back to the line the reverted block sat under.
   *
   * The file has to be loaded again first: it was rewritten, and the view is
   * rebuilt from the new contents. Only then does a line number mean anything.
   */
  async function returnAfterRevert() {
    const line = returnToLine;
    returnToLine = null;
    if (line === null || !selectedPath) return;

    await waitForFileLoad(selectedPath);
    if (destroyed || !selectedPath) return;

    if (sideBySide) {
      await sideBySideView?.scrollToOldLine(line);
      return;
    }

    // The unified renderers count lines of the DIFF, not of the file, and
    // flatLines carries no line numbers — so the file's numbering is walked
    // alongside it to find where that line ended up.
    const at = unifiedPositionOf(line);
    if (at < 0) return;
    if (wholeFileView) {
      await virtualLines?.scrollToLine(at);
      return;
    }
    // The hunk list renders per hunk rather than per line, so the nearest thing
    // to scroll to is the hunk that line now falls in.
    const hunk = flatLines[at]?.hunkIndex ?? -1;
    if (hunk >= 0) {
      scrollToThird(hunkListEl, hunkListEl?.querySelector(`[data-hunk="${hunk}"]`) ?? null);
    }
  }

  /**
   * Where a line of the file sits among the diff's lines.
   *
   * flatLines holds the diff, not the file, and carries no line numbers, so the
   * file's numbering is walked alongside it.
   *
   * The OLD file's numbering, matching what the revert restores and what
   * returnToLine records: added lines do not exist in it and so do not advance
   * the count; removed and unchanged ones do. Walking the new numbering instead
   * put the view off by however many lines the file had gained above the point
   * being looked for.
   */
  function unifiedPositionOf(fileLine: number): number {
    let number = 0;
    for (let at = 0; at < flatLines.length; at++) {
      const type = flatLines[at].type;
      if (type === 'header' || type === 'meta') {
        // A hunk header restarts the count wherever that hunk begins.
        if (type === 'header') {
          const parsed = parseHunkHeader(flatLines[at].text);
          number = parsed.oldStart - 1;
        }
        continue;
      }
      if (type === 'add') continue;
      number++;
      if (number >= fileLine) return at;
    }
    return -1;
  }

  /**
   * Which tab the diff is for.
   *
   * A tab can be opened in a directory of its own, so the session's path is the
   * wrong repository for it — and reverting from that diff wrote into the
   * session's repository, a file the user never saw on screen.
   */
  function tabIdx(): number {
    return get(selectedWindowIdx) ?? 0;
  }

  async function confirmRevert() {
    const action = pendingRevert;
    pendingRevert = null;
    if (!action || reverting) return;
    if (!isCurrentTarget(action.target)) {
      error = 'The selected session, tab, or diff mode changed. Reopen the diff before reverting.';
      return;
    }
    reverting = true;
    try {
      await action.run();
      if (destroyed || !isCurrentTarget(action.target)) {
        returnToLine = null;
        return;
      }
      error = '';
      // The cached list describes the tree before the revert, so it has to go
      // — otherwise the refresh below would show the reverted file as still
      // changed until the fetch landed.
      invalidateDiffCache();
      // Refresh so the view reflects what's actually on disk now.
      await loadDiff();
      if (destroyed || !isCurrentTarget(action.target)) {
        returnToLine = null;
        return;
      }
      // And come back to where the reverted block was. loadDiff drops the
      // remembered position, rightly — the file has changed — but the change is
      // one just made here, at a place being looked at, so returning to the top
      // of a long file loses it.
      await returnAfterRevert();
    } catch (e) {
      if (destroyed || !isCurrentTarget(action.target)) return;
      // A failed revert must be visible — the Go message explains that the file
      // changed since the diff was rendered.
      error = String(e);
    } finally {
      if (!destroyed) reverting = false;
    }
  }

  function cancelRevert() {
    pendingRevert = null;
    // Nothing was rewritten, so there is nowhere to return to — left set, it
    // would move the view on whatever reloaded the diff next.
    returnToLine = null;
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
  // Cap how many diff lines we render. The diff view renders one <div><code>
  // per line with NO virtualisation, so a huge repo's diff (e.g. WebErp, tens
  // of thousands of lines) would insert tens of thousands of DOM nodes
  // synchronously on every session/tab switch — freezing the entire main thread
  // (the UI, even the profiler, locked up). A diff that large is not human-
  // readable inline anyway; we render the first MAX_DIFF_LINES and show a notice
  // with the real total. This is what made WebErp 2 "freeze on typing/switching"
  // while small-diff sessions (asmgr-desktop) stayed fluid.
  const MAX_DIFF_LINES = 2000;

  $: currentDiffKey = `${$selectedSessionId || ''}:${$selectedWindowIdx ?? 0}:${diffMode}`;
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
    const key = `${$selectedSessionId || ''}:${$selectedWindowIdx ?? 0}:${diffMode}`;
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
  // The cache key carries the view, because the two need different content
  // from git: whole-file asks for every line around the changes, hunks-only for
  // three lines of context. Keyed together, switching views would show the
  // previous answer.
  $: cacheKey = selectedPath && loadedRoot && loadedDiffKey === currentDiffKey
    ? `${currentDiffKey}:${loadedRoot}:${wholeFileView ? 'whole' : 'hunks'}:${selectedPath}`
    : '';
  $: selectedFile = cacheKey ? fileCache[cacheKey] ?? null : null;
  // Fetch the hunks the first time a file is opened.
  $: if (selectedPath && cacheKey && !(cacheKey in fileCache)) void loadSelectedFile(selectedPath);

  async function loadSelectedFile(path: string) {
    const sessionId = get(selectedSessionId);
    const windowIdx = tabIdx();
    const mode = diffMode;
    const whole = wholeFileView;
    const targetKey = `${sessionId || ''}:${windowIdx}:${mode}`;
    const expectedRoot = loadedRoot;
    const key = `${targetKey}:${expectedRoot}:${whole ? 'whole' : 'hunks'}:${path}`;
    if (!sessionId || !expectedRoot || loadedDiffKey !== targetKey) return;
    loadingFile = true;
    try {
      const loaded = mode === 'session'
        ? await App.GetSessionDiffForFile(sessionId, path, whole, windowIdx, expectedRoot)
        : await App.GetFullDiffForFile(sessionId, path, whole, windowIdx, expectedRoot);
      // Ignore a result that arrived after the user moved on, so a slow file
      // cannot overwrite the one now on screen.
      if (path !== selectedPath || mode !== diffMode || whole !== wholeFileView ||
          sessionId !== get(selectedSessionId) || windowIdx !== tabIdx() || expectedRoot !== loadedRoot) return;
      fileCache = { ...fileCache, [key]: loaded };
    } catch (e) {
      if (path !== selectedPath || targetKey !== currentDiffKey || whole !== wholeFileView || expectedRoot !== loadedRoot) return;
      // Cached as null: a file that fails to load should not be retried on
      // every reactive pass.
      fileCache = { ...fileCache, [key]: null };
      error = String(e);
    } finally {
      if (path === selectedPath && targetKey === currentDiffKey && whole === wholeFileView && expectedRoot === loadedRoot) loadingFile = false;
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
  let forceShowTarget = '';
  $: if (currentDiffKey !== forceShowTarget) {
    forceShowTarget = currentDiffKey;
    forceShowPaths = {};
  }

  // Only parse when the tab is active AND (the file is small OR the user opted
  // in). Parsing a huge diff string is itself expensive, so we skip it entirely
  // while showing the warning.
  // Show the change inside the file it lives in, rather than only the few lines
  // of context a diff carries. The default, because a change is usually easier
  // to judge with the code around it — and the hunks-only view is a click away
  // for when the file is large and the change is all that matters.
  //
  // Stored, so the choice survives reopening the diff.
  // Stored inverted — see Settings.DiffHunksOnly. Whole-file is the default,
  // and an absent flag has to mean that rather than "off".
  $: wholeFileView = $settings.diffHunksOnly !== true;

  /**
   * Two aligned columns rather than one with markers.
   *
   * Available in both views. It was restricted to hunks-only at first, on the
   * grounds that pairing a whole file of identical context is work for
   * nothing — but whole-file is the default, so the button was disabled from
   * the moment the diff opened, and reading a change in the file it lives in
   * is exactly when two columns help most.
   */
  $: sideBySide = $settings.diffSideBySide === true;

  function setSideBySide(on: boolean) {
    void saveSettings({ diffSideBySide: on });
  }

  function setWholeFileView(on: boolean) {
    void saveSettings({ diffHunksOnly: !on });
  }

  // Which change the next/previous buttons will move to. -1 before the first
  // jump, so the first press lands on the first change rather than the second.
  let currentHunk = -1;
  /**
   * The block the marker points at, which is not always where the cursor is.
   *
   * Entering a file puts the cursor one step BEFORE the block it scrolled to,
   * so that leaving costs the same number of presses in either direction. The
   * marker cannot follow that: it would leave the block on screen unmarked and
   * then appear only on the press that seemed to do nothing.
   */
  let markedHunk = -1;

  /**
   * Whether the walk is paused at the end of a file, offered the next one.
   *
   * Running out of changes used to move on immediately, so the name of the
   * file being entered flashed by with the file itself — there was no moment
   * in which to read it and decide. Now the first press past the last change
   * stops here and names what is next; the press after that goes.
   *
   * Cleared by anything that moves: a step in the other direction, a different
   * file, a different commit.
   */
  let atFileEdge: 1 | -1 | null = null;

  /**
   * Which way the review is moving, so only the hint ahead of you is shown.
   *
   * Both at once is noise: at the end of a small file each arrow would leave it,
   * and two labels appear announcing opposite destinations. Going down, only
   * the next file matters; going up, only the previous.
   *
   * Null until the first press — a file just opened has no direction yet, and
   * guessing one would put a label on screen nobody asked about.
   */
  let stepDirection: 1 | -1 | null = null;
  // Typed loosely: the component's instance type is only used for two exported
  // methods, and importing it as a type as well as a value is more ceremony
  // than the two calls are worth.
  let virtualLines: {
    scrollToLine(i: number, lines?: number): Promise<void>;
    scrollOffset(): number;
    restoreOffset(top: number): Promise<void>;
    firstVisibleLine(): number;
  } | null = null;

  /**
   * Find within the side-by-side view.
   *
   * Only there: the unified and whole-file views render through VirtualLines,
   * which keeps off-screen rows out of the DOM, so a search would have to be
   * built against the data rather than the markup — a separate piece of work,
   * and not what was asked for.
   */
  let showDiffFind = false;
  let diffQuery = '';
  let diffHitCount = 0;
  let diffHitAt = 0;
  let diffFindInput: HTMLInputElement | undefined;

  function runDiffSearch() {
    diffHitCount = sideBySideView?.search(diffQuery) ?? 0;
    diffHitAt = sideBySideView?.searchPosition() ?? 0;
  }

  function stepDiffSearch(direction: 1 | -1) {
    diffHitAt = sideBySideView?.stepSearch(direction) ?? 0;
  }

  function openDiffFind() {
    showDiffFind = true;
    tick().then(() => { diffFindInput?.focus(); diffFindInput?.select(); });
  }

  function closeDiffFind() {
    showDiffFind = false;
    diffQuery = '';
    diffHitCount = 0;
    diffHitAt = 0;
    sideBySideView?.search('');
  }

  /** Ctrl+F opens the bar, wherever the focus is inside the diff. */
  function handleDiffKeydown(event: KeyboardEvent) {
    if ((event.ctrlKey || event.metaKey) && event.key === 'f' && sideBySide) {
      event.preventDefault();
      openDiffFind();
    }
  }

  function handleDiffFindKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.preventDefault();
      closeDiffFind();
      return;
    }
    if (event.key === 'Enter' || event.key === 'F3' ||
        ((event.ctrlKey || event.metaKey) && event.key === 'g')) {
      event.preventDefault();
      stepDiffSearch(event.shiftKey ? -1 : 1);
    }
  }
  /** The hunk-list scroller. The whole-file view has VirtualLines to scroll
   *  for it; this one is an ordinary element and has to be told. */
  let hunkListEl: HTMLDivElement | null = null;
  let sideBySideView: {
    scrollToRow(row: number, count?: number): void;
    changeRows(): Array<{ from: number; to: number }>;
    hunkRange(from: number, to: number): { from: number; to: number } | null;
    scrollToOldLine(number: number): Promise<void>;
    scrollOffset(): number;
    restoreOffset(top: number): Promise<void>;
    search(query: string): number;
    stepSearch(direction: 1 | -1): number;
    searchPosition(): number;
    topVisibleNewLine(): number | null;
  } | null = null;

  /**
   * Bring the stepped-to hunk into view in whichever renderer is showing.
   *
   * Only the whole-file view has VirtualLines. Left to it alone, stepping in
   * the hunk list moved the marker to a hunk off-screen and appeared to do
   * nothing at all.
   */
/**
   * Scroll an element to a third of the way down its scroller.
   *
   * scrollIntoView only offers start/center/end. Centring wastes half the screen
   * on context already read — a change is read downwards, so the room below it is
   * worth more than the room above.
   */
  function scrollToThird(scroller: HTMLElement | null, target: Element | null) {
    if (!scroller || !target) return;
    const element = target as HTMLElement;
    const view = scroller.clientHeight;
    // A block taller than the room below a third would run off the bottom, so
    // a long one starts near the top and uses the whole viewport instead.
    const margin = element.offsetHeight > view - view / 3
      ? Math.min(view / 8, 60)
      : view / 3;
    scroller.scrollTo({ top: Math.max(0, element.offsetTop - margin), behavior: 'smooth' });
  }

  async function revealHunk(hunkPos: number) {
    if (sideBySide) {
      await tick();
      // The columns pair lines up, so their rows do not match the unified
      // line positions hunkStarts holds — the nth change is the nth run of
      // changed rows there.
      const runs = sideBySideView?.changeRows() ?? [];
      const run = runs[hunkPos];
      if (run) sideBySideView?.scrollToRow(run.from, run.to - run.from + 1);
      return;
    }
    if (virtualLines) {
      // How many lines the change spans, so a tall one is not scrolled to a
      // position that runs it off the bottom.
      const start = hunkStarts[hunkPos];
      let end = start;
      while (end + 1 < flatLines.length &&
             (flatLines[end + 1].type === 'add' || flatLines[end + 1].type === 'remove')) {
        end++;
      }
      await virtualLines.scrollToLine(start, end - start + 1);
      return;
    }
    await tick();
    const index = flatLines[hunkStarts[hunkPos]]?.hunkIndex;
    if (index === undefined) return;
    scrollToThird(hunkListEl, hunkListEl?.querySelector(`[data-hunk="${index}"]`) ?? null);
  }

  /**
   * Move to the next or previous change, running past the end of the file into
   * the next one, and past the last file back to the first — the point of
   * stepping through a review is not stopping at every boundary.
   */
  async function stepChange(delta: number) {
    stepDirection = delta > 0 ? 1 : -1;
    if (!hunkStarts.length) {
      await stepFile(delta);
      return;
    }

    // A file opened by clicking it sits at -1, meaning "before the first
    // change" — which is only "before" when moving forward. Read literally,
    // stepping up from there is -2, outside the file, so the up arrow left the
    // file immediately instead of moving to its last change. Going backwards,
    // an untouched file starts after its end.
    // Already offered the next file, and asked again in the same direction:
    // this is the press that accepts it.
    if (atFileEdge === delta) {
      atFileEdge = null;
      await stepFile(delta);
      return;
    }
    atFileEdge = null;

    const from = currentHunk === -1 && delta < 0 ? hunkStarts.length : currentHunk;

    const next = from + delta;
    if (next < 0 || next >= hunkStarts.length) {
      // Out of changes here. Stop and name the file this would go to, rather
      // than arriving there before the name has been read.
      atFileEdge = delta > 0 ? 1 : -1;
      return;
    }
    currentHunk = next;
    markedHunk = next;
    await revealHunk(currentHunk);
  }

  /**
   * Move to the next or previous file that has changes, and land on the change
   * at the end we arrived from.
   *
   * The file's content is fetched, so this waits for it: a tick() is not
   * enough, and stepping without waiting looked like "the button stops working
   * at the end of a file" — it had moved on, to a file whose changes it did not
   * yet know about.
   */
  async function stepFile(delta: number) {
    // Walks the list the user can SEE, in the order they see it.
    //
    // Two things made this wrong. Stepping through `files` moved to entries the
    // status filter hides — and "modified" is the default, so most reviews have
    // one on. And the tree view groups files by directory, which reorders them:
    // walking the flat list then jumped between directories rather than working
    // down the tree, which reads as skipping the subfolders entirely.
    const walkable = stepOrder;
    if (!walkable.length) return;
    const index = walkable.findIndex(f => f.path === selectedPath);
    // Wraps around: past the last file the walk returns to the first, and back
    // past the first it returns to the last. A review is a loop — you go round
    // until nothing needs another look — and stopping dead at the end just
    // means finding the top of the list by hand.
    const count = walkable.length;
    const next = ((index + delta) % count + count) % count;

    const wanted = walkable[next].path;
    selectedPath = wanted;
    void revealInFileList(wanted);
    // Suppresses the reactive reset below, which would otherwise put us back
    // before the first change while we are waiting — losing the direction we
    // arrived in.
    pendingEntry = delta > 0 ? 'first' : 'last';
    // Claim the new path for this step. Otherwise the reset below fires the
    // moment pendingEntry is cleared and puts the cursor back to -1, undoing
    // the landing — which is what made every file cost two presses and left
    // the arrived-at change unmarked.
    hunkCursorFor = wanted;

    await waitForFileLoad(wanted);
    // The user moved on while it loaded; whatever they did wins.
    if (selectedPath !== wanted || !pendingEntry) return;

    const entry = pendingEntry;
    pendingEntry = null;
    if (!hunkStarts.length) return;
    // Land on the change at the end we came in from — first when moving
    // forward, last when moving back — so entering a file backwards does not
    // start at its top with the change just left behind off-screen above.
    //
    // Scroll to the change at the end we came in from, but set the position
    // one step BEFORE it, in our own direction of travel. Two effects, both
    // wanted:
    //
    //  - Leaving takes the same number of presses whichever way you are going.
    //    Landing on the change itself would let a single press leave a file
    //    entered backwards, so working back through a review would pass over
    //    files without ever stopping in them.
    //  - The first press then lands on the change already on screen, which is
    //    what makes the file feel entered rather than skipped through.
    // Land on the change itself, in both senses. Setting the cursor one step
    // before it made the next press move onto the change already on screen —
    // which looks like a press that did nothing, and doubled every step
    // through a review.
    //
    // Leaving still costs one press either way, because stepChange treats
    // running past either end as "go to the next file".
    const landing = entry === 'first' ? 0 : hunkStarts.length - 1;
    currentHunk = landing;
    markedHunk = landing;
    await revealHunk(landing);
  }

  /** Which end of an incoming file to land on, while its content is loading. */
  let pendingEntry: 'first' | 'last' | null = null;

  /**
   * The file the next press would leave for, or null while it stays here.
   *
   * Appears only when a press really would cross into another file, which is
   * why it shares stepChange's condition rather than approximating it. A file
   * with a single change shows it as soon as you reach that change — correct,
   * because from there the next press does leave.
   */
  function fileAfterStep(
    delta: number,
    hunk: number,
    starts: number[],
    order: session.DiffFileSummary[],
    path: string,
  ): string | null {
    // Exactly what stepChange does, including its reading of -1 as "before the
    // first change going forward, after the last going back" — otherwise the
    // hint would promise a file change the arrow does not make, or stay silent
    // about one it does.
    const from = hunk === -1 && delta < 0 ? starts.length : hunk;
    const after = from + delta;
    const stillHere = starts.length && after >= 0 && after < starts.length;
    if (stillHere) return null;
    if (!order.length) return null;
    const index = order.findIndex(f => f.path === path);
    const count = order.length;
    const next = ((index + delta) % count + count) % count;
    if (next === index) return null;   // a lone file has nowhere to go
    return splitPath(order[next].path).name;
  }

  // Everything it depends on is passed in, so Svelte can see the dependencies:
  // it tracks what a reactive statement reads directly, not what a function it
  // calls reads inside. Otherwise the label would keep whatever it said when
  // the file was opened.
  $: nextFileName = selectedPath
    ? fileAfterStep(1, currentHunk, hunkStarts, stepOrder, selectedPath)
    : null;
  $: prevFileName = selectedPath
    ? fileAfterStep(-1, currentHunk, hunkStarts, stepOrder, selectedPath)
    : null;

  // Shown while the offer stands. Predicting it from the cursor instead meant
  // the label and the jump landed on the same press, so the name was never
  // read before the file it named replaced the one on screen.
  $: hintBelow = atFileEdge === 1 ? nextFileName : null;
  $: hintAbove = atFileEdge === -1 ? prevFileName : null;


  /**
   * Bring the selected file into view in the list beside the diff.
   *
   * Stepping moves the selection without the mouse, so nothing else scrolls the
   * list — and after a few steps the highlighted row is off-screen, leaving no
   * sense of where in the review you are.
   *
   * `nearest` rather than `center`: a row already visible should not move, or
   * every step would shuffle the whole list under the eye.
   */
  async function revealInFileList(path: string) {
    await tick();
    const row = document.querySelector(`.file-row[data-path="${CSS.escape(path)}"]`);
    row?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
  }

  /**
   * Resolve once the file's lines are ready.
   *
   * Polls rather than hooking the fetch, because the content arrives through
   * the reactive chain (cache → selectedFile → flatLines) rather than from a
   * promise this can await. Bounded, so a file that never loads — an error, a
   * binary — does not leave the walk hanging.
   */
  async function waitForFileLoad(path: string): Promise<void> {
    const key = `${currentDiffKey}:${loadedRoot}:${wholeFileView ? 'whole' : 'hunks'}:${path}`;
    // ~1.5s at 25ms. Long enough for a git call on a large repository, short
    // enough that a file which never loads does not appear to hang the button.
    for (let attempt = 0; attempt < 60; attempt++) {
      await tick();
      if (selectedPath !== path) return;   // the user moved on
      // Waiting for the cache alone returned too early: the content reaches
      // hunkStarts through a chain of reactive statements — cache, selectedFile,
      // flatLines, hunkStarts — and the caller reads hunkStarts immediately
      // afterwards. Empty, it gave up on landing, so the position stayed put
      // and the next press had to do the work again.
      if (key in fileCache) {
        if (fileCache[key] === null) return;   // failed; nothing more to wait for
        if (hunkStarts.length) return;
        // Present but not yet derived: give the chain another turn.
        await tick();
        if (hunkStarts.length) return;
      }
      await new Promise((resolve) => setTimeout(resolve, 25));
    }
  }

  /**
   * Where the review is, and putting it back after a tab switch.
   *
   * The component is destroyed when the tab changes — the mount points are in
   * exclusive branches — so the scroll position and the change being stepped
   * through go with it, and coming back landed at the top of the file. The
   * state lives in a module for that reason; see utils/diffViewState.
   *
   * Which scroller answers depends on the renderer on screen, and the three do
   * not share a coordinate system, so the mode is part of what is stored.
   */
  function currentScrollOffset(): number {
    if (sideBySide) return sideBySideView?.scrollOffset() ?? 0;
    if (wholeFileView) return virtualLines?.scrollOffset() ?? 0;
    return hunkListEl?.scrollTop ?? 0;
  }

  /** The renderer showing, since an offset means nothing in a different one. */
  $: viewMode = sideBySide ? 'sbs' : wholeFileView ? 'whole' : 'hunks';

  /**
   * The last offset seen while scrolling, and what it belongs to.
   *
   * Read as it changes rather than on the way out. By the time onDestroy runs
   * Svelte has already unbound the child components, so asking them where they
   * were scrolled to returns nothing — the position saved was always 0, and a 0
   * is indistinguishable from "at the top", so nothing was ever restored.
   */
  let lastOffset = 0;
  let lastOffsetFor = '';
  /** What the tracked offset belongs to. The separator cannot occur in a path,
   *  so no two file-and-renderer pairs spell the same string. */
  const offsetOwner = (path: string, mode: string) => `${path}\x1f${mode}`;

  function trackScroll() {
    if (!selectedPath) return;
    lastOffset = currentScrollOffset();
    lastOffsetFor = offsetOwner(selectedPath, viewMode);
  }

  function savePlace() {
    const sessionId = get(selectedSessionId);
    if (!sessionId || !selectedPath) return;

    /**
     * Prefer a live reading, fall back to the tracked one.
     *
     * While the component is running the scrollers can be asked directly, and
     * that is the most current answer. During teardown they answer 0, which is
     * why the tracked value exists.
     *
     * Chosen on the ANSWER, not on whether the component reference still
     * exists. At teardown that reference is still set while the DOM element
     * inside it is already gone, so the scroller reports 0 and looks perfectly
     * bound — and the tracked 2816 was thrown away in favour of it.
     *
     * A live 0 is genuinely "at the top", and the tracked value would be 0
     * there too, so preferring the tracked one when the live reading is 0
     * costs nothing. It is only usable if it belongs to the file and renderer
     * on screen now; switching either leaves a reading from the previous one.
     */
    const fresh = lastOffsetFor === offsetOwner(selectedPath, viewMode);
    const scrollTop = currentScrollOffset() || (fresh ? lastOffset : 0);

    rememberPlace(sessionId, selectedPath, viewMode, { scrollTop, currentHunk, markedHunk }, tabIdx(), loadedRoot);
  }

  /**
   * Put the view back where it was left, if it has been here before.
   *
   * Runs after the reset above rather than instead of it, so a file opened for
   * the first time still starts before its first change.
   */
  async function restorePlace(path: string, mode: string) {
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;
    const place = recallPlace(sessionId, path, mode, tabIdx(), loadedRoot);
    if (!place) return;

    currentHunk = place.currentHunk;
    markedHunk = place.markedHunk;

    if (place.scrollTop <= 0) return;
    // The content has to be in place before an offset means anything: restoring
    // into an empty list is clamped to nothing and lands at the top.
    await waitForFileLoad(path);
    // A step, another file, or a change of renderer taking over while that
    // loaded owns the position now — the offset belongs to the mode it was
    // taken in, and means nothing in a different one.
    if (selectedPath !== path || viewMode !== mode) return;
    if (sideBySide) await sideBySideView?.restoreOffset(place.scrollTop);
    else if (wholeFileView) await virtualLines?.restoreOffset(place.scrollTop);
    else if (hunkListEl) hunkListEl.scrollTop = place.scrollTop;
  }

  /**
   * A file arrived at any other way starts before its first change.
   *
   * Stepping into one claims the path itself, so this cannot fire for it. Both
   * guards are needed: keyed on `selectedPath && !pendingEntry` alone, clearing
   * pendingEntry at the end of a step re-ran the statement and wiped the
   * landing; keyed on the path alone, it would still fire in the window
   * between the step setting the path and setting the position.
   */
  let hunkCursorFor = '';
  // viewMode is read here so Svelte orders this after it. Svelte works out the
  // order by READING the statement, and it cannot see through the call to
  // restorePlace — which does read viewMode. Without the mention this can run
  // first, and the lookup goes to a key that was never written.
  $: if (selectedPath && selectedPath !== hunkCursorFor && !pendingEntry && viewMode) {
    hunkCursorFor = selectedPath;
    // A file the review had already been through resumes where it was left; one
    // arrived at for the first time starts before its first change. Assigned
    // before the await so a step arriving in between is not overwritten by a
    // reset for a file it has already moved on from.
    currentHunk = -1;
    markedHunk = -1;
    atFileEdge = null;
    void restorePlace(selectedPath, viewMode);
  }

  // Keyboard stepping, matched against the configured bindings so it follows a
  // rebind. Only while this diff is the one on screen: two diffs can be mounted
  // at once — the full view and the one above a terminal — and both would
  // otherwise answer the same key.
  function onKeydown(event: KeyboardEvent) {
    if (!active) return;
    if (matchesShortcut(event, 'diff.nextChange')) {
      event.preventDefault();
      void stepChange(1);
    } else if (matchesShortcut(event, 'diff.prevChange')) {
      event.preventDefault();
      void stepChange(-1);
    }
  }

  // The grammar for the file being viewed, loaded on demand and only for a file
  // we are actually going to render. Null until it arrives (and for file types
  // we have no grammar for), which memoHighlightLine takes as "plain text" — so the
  // diff shows immediately and gains its colours a moment later, rather than
  // waiting on a chunk fetch.
  let lineLanguage: LanguageSupport | null = null;
  let languageForPath = '';
  $: if (selectedPath && selectedPath !== languageForPath) {
    languageForPath = selectedPath;
    lineLanguage = cachedLanguage(selectedPath);
    if (!lineLanguage) {
      const wanted = selectedPath;
      void loadLanguage(wanted).then((lang) => {
        // Guard against a slow load landing after the user moved on.
        if (wanted === languageForPath) lineLanguage = lang;
      });
    }
  }

  $: shouldRender = active && !!selectedFile && !selectedFile.binary && (!isLargeFile || forceShow);
  $: renderedHunks = shouldRender && selectedFile
    ? buildHunkViews(selectedFile, MAX_DIFF_LINES)
    : [];

  /**
   * The same lines the whole-file view renders, grouped as one hunk.
   *
   * The whole-file diff comes back from git as a single hunk covering the
   * file, so pairing it into columns needs no extra request — only the lines
   * in the shape the column builder takes.
   */
  $: wholeFileColumns = shouldRender && selectedFile && selectedFile.hunks.length > 0
    ? [{
        header: selectedFile.hunks[0].header,
        index: selectedFile.hunks[0].index,
        // Coloured here because the column view puts every line in the DOM
        // rather than virtualising, so all of them are drawn anyway. Memoised,
        // so re-pairing on a scroll does not re-parse what it already has.
        lines: flatLines.map((line) => ({
          type: line.type,
          html: memoHighlightLine(line.text, lineLanguage),
        })),
      }]
    : [];

  // Build the per-hunk line lists, honouring MAX_DIFF_LINES across the whole
  // file so one pathological hunk can't blow the budget on its own.

  // The whole file, as one flat list of lines for the virtual renderer, plus
  // where each hunk starts so the next/previous buttons can jump to it.
  //
  // Built once per file rather than per render: highlighting every line of a
  // 3000-line file is the expensive part, and it does not change while the
  // file is open.
  // Built for both renderers. It is what the whole-file view renders, and what
  // hunkStarts is derived from — and hunkStarts is how stepping knows where the
  // changes are. Left to the whole-file view alone, the hunk list had no
  // change positions at all, so every press fell through to "next file".
  // Not keyed on lineLanguage: the lines hold text, and the grammar only
  // matters where they are drawn. Rebuilding this when a grammar finishes
  // loading would throw away the list and the scroll position with it.
  $: flatLines = selectedFile && shouldRender
    ? buildFlatLines(selectedFile)
    : [];
  $: hunkStarts = flatLines.reduce((acc, line, index) => {
    const isChange = line.type === 'add' || line.type === 'remove';
    if (!isChange) return acc;
    // The start of a run: a changed line whose predecessor was not one. A
    // removal followed by its replacement is one block, not two, which is how
    // it reads on screen.
    const previous = flatLines[index - 1];
    if (!previous || (previous.type !== 'add' && previous.type !== 'remove')) {
      acc.push(index);
    }
    return acc;
  }, [] as number[]);

  /** Which hunk the stepping is inside, for marking its lines. hunkStarts
   *  holds line positions rather than hunk numbers — and a hunk with no
   *  changes contributes none — so the answer comes from the line itself. */
  $: currentHunkIndex = markedHunk >= 0 && markedHunk < hunkStarts.length
    ? (flatLines[hunkStarts[markedHunk]]?.hunkIndex ?? -1)
    : -1;

  /**
   * The run of lines making up the change stepped to.
   *
   * Marked by line range rather than by hunk: in whole-file view git is asked
   * for the whole file as a single hunk, so every change in the file shares a
   * hunk index and marking by it lit up all of them at once.
   */
  $: markedBlock = (() => {
    if (markedHunk < 0 || markedHunk >= hunkStarts.length) return { from: -1, to: -1 };
    const from = hunkStarts[markedHunk];
    let to = from;
    while (to + 1 < flatLines.length &&
           (flatLines[to + 1].type === 'add' || flatLines[to + 1].type === 'remove')) {
      to++;
    }
    return { from, to };
  })();

  /**
   * The whole diff as a flat list of lines, held as TEXT rather than as
   * coloured HTML.
   *
   * Colouring costs a parse per line — about 500ms for a 10,000-line file
   * against 0.8ms for the fifty actually on screen. Doing it here meant paying
   * for the whole file before anything could be drawn, however little of it the
   * virtual renderer went on to show. The lines are coloured where they are
   * rendered instead, through memoHighlightLine, which keeps what it has
   * already done so scrolling does not re-parse the same lines.
   */
  function buildFlatLines(file: session.DiffFile) {
    const out: Array<{ type: string; text: string; hunkIndex: number }> = [];
    file.hunks.forEach((hunk, hunkIndex) => {
      for (const line of parseDiff(hunk.body)) {
        out.push({ type: line.type, text: line.text, hunkIndex });
      }
    });
    return out;
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

  /**
   * The files in the order the list presents them, which is what next/previous
   * follows. In tree view that is the tree's own order, with directory rows
   * dropped and anything inside a folded directory already absent.
   */
  $: stepOrder = treeView
    // flatMap rather than filter+map: TypeScript does not carry the kind check
    // through a filter, so row.file would not be visible to it afterwards.
    ? treeRows.flatMap((row) => (row.kind === 'file' ? [row.file] : []))
    : visibleFiles;

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
  let collapseTarget = '';
  $: if (diffKey !== collapseTarget) {
    collapseTarget = diffKey;
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

<svelte:window on:keydown={onKeydown} />

<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
<div class="diff-container" role="region" tabindex="-1" on:keydown={handleDiffKeydown}>
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
                  data-path={file.path}
                  style="padding-left: {10 + row.depth * TREE_INDENT}px"
                  role="button"
                  tabindex="0"
                  on:click={() => selectFile(file.path)}
                  on:keydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); selectFile(file.path); } }}
                >
                  <div class="file-main">
                    <span class="file-status {file.status}" title={statusLabel(file.status, $t)}>{statusLetter(file.status)}</span>
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
                      class="revert-btn open-file"
                      title={$t('diff.openInBrowser')}
                      on:click|stopPropagation={() => openFileInBrowser(file.path)}
                    >
                      <!-- Pencil: this leaves the diff for somewhere the file
                           can be edited. -->
                      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M17 3a2.83 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5z"/>
                      </svg>
                    </button>
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
              data-path={file.path}
              role="button"
              tabindex="0"
              on:click={() => selectFile(file.path)}
              on:keydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); selectFile(file.path); } }}
            >
              <div class="file-main">
                <span class="file-status {file.status}" title={statusLabel(file.status, $t)}>{statusLetter(file.status)}</span>
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
                  class="revert-btn open-file"
                  title={$t('diff.openInBrowser')}
                  on:click|stopPropagation={() => openFileInBrowser(file.path)}
                >
                  <!-- Pencil: this leaves the diff for somewhere the file can
                       be edited. -->
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M17 3a2.83 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5z"/>
                  </svg>
                </button>
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
            <span class="selected-status {selectedFile.status}" title={statusLabel(selectedFile.status, $t)}>
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
            <span class="selected-nav">
              <button
                class="nav-btn"
                title={prevFileName
                  ? $t('diff.prevGoesTo', { file: prevFileName })
                  : $t('diff.prevChangeHint')}
                on:click={() => stepChange(-1)}
              >↑</button>
              <button
                class="nav-btn"
                title={nextFileName
                  ? $t('diff.nextGoesTo', { file: nextFileName })
                  : $t('diff.nextChangeHint')}
                on:click={() => stepChange(1)}
              >↓</button>
              <button
                class="nav-btn wide"
                class:active={wholeFileView}
                title={wholeFileView ? $t('diff.showHunksOnly') : $t('diff.showWholeFile')}
                on:click={() => setWholeFileView(!wholeFileView)}
              >{wholeFileView ? $t('diff.wholeFile') : $t('diff.hunksOnly')}</button>
              <button
                class="nav-btn"
                class:active={sideBySide}
                title={sideBySide ? $t('diff.showUnified') : $t('diff.showSideBySide')}
                on:click={() => setSideBySide(!sideBySide)}
              >⫲</button>
              {#if sideBySide}
                <!-- Only offered where it works: the other renderers keep
                     off-screen rows out of the DOM, so there is nothing there
                     to search. -->
                <button
                  class="nav-btn"
                  class:active={showDiffFind}
                  title="{$t('diff.findPlaceholder')} (Ctrl+F)"
                  on:click={() => (showDiffFind ? closeDiffFind() : openDiffFind())}
                >⌕</button>
              {/if}
              <!-- Last in the row, next to the file it acts on: this is the one
                   control here that leaves the diff. -->
              <button
                class="nav-btn"
                title={$t('diff.openInBrowser')}
                on:click={() => openFileInBrowser(selectedFile.path)}
              >✎</button>
            </span>
          </div>
        {/if}

        <!-- Wrapper so the file hints can float over the diff: diff-content is
             the scroller, and anything positioned inside it would scroll away
             with the code. -->
        <div class="diff-viewport">
          {#if hintAbove}
            <button
              class="file-hint top"
              title={$t('diff.prevGoesTo', { file: hintAbove })}
              on:click={() => stepChange(-1)}
            >↑ {hintAbove}</button>
          {/if}
          {#if hintBelow}
            <button
              class="file-hint bottom"
              title={$t('diff.nextGoesTo', { file: hintBelow })}
              on:click={() => stepChange(1)}
            >↓ {hintBelow}</button>
          {/if}
        <!-- The side-by-side view scrolls its own two panes, so this must not
             scroll as well. Left as a scroller it was the ONLY one that moved:
             the panes are sized to it, and with no height to fill they grew to
             their content instead of scrolling, so both columns rode this one
             scrollbar together — exactly the shared position the two-scroller
             layout exists to avoid, and the columns drifted apart at the first
             insertion. -->
        <div class="diff-content" class:columns={sideBySide}>
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
          {:else if sideBySide}
            {#if showDiffFind}
              <!-- Above the panes rather than floating over them: a bar over
                   code hides the very lines being searched. -->
              <div class="diff-find">
                <input
                  type="text"
                  bind:this={diffFindInput}
                  bind:value={diffQuery}
                  on:input={runDiffSearch}
                  on:keydown={handleDiffFindKeydown}
                  placeholder={$t('diff.findPlaceholder')}
                  title="{$t('notes.nextMatch')}: Enter · F3 · Ctrl+G — {$t('notes.previousMatch')}: Shift+Enter"
                />
                <span class="find-count">
                  {diffHitCount ? `${diffHitAt}/${diffHitCount}` : (diffQuery ? $t('notes.noMatches') : '')}
                </span>
                <button
                  on:click={() => stepDiffSearch(-1)}
                  disabled={!diffHitCount}
                  title="{$t('notes.previousMatch')} (Shift+Enter)"
                >↑</button>
                <button
                  on:click={() => stepDiffSearch(1)}
                  disabled={!diffHitCount}
                  title="{$t('notes.nextMatch')} (Enter · F3 · Ctrl+G)"
                >↓</button>
                <button on:click={closeDiffFind} title="{$t('common.close')} (Esc)">×</button>
              </div>
            {/if}
            <SideBySideDiff
              bind:this={sideBySideView}
              canRevert={true}
              on:revert={(e) => askRevertBlock(e.detail)}
              hunks={wholeFileView
                ? wholeFileColumns
                : renderedHunks.map((view) => ({
                    header: view.hunk.header,
                    index: view.hunk.index,
                    // Highlighted here, as the unified view does at render time.
                    lines: view.lines.map((line) => ({
                      type: line.type,
                      html: memoHighlightLine(line.text, lineLanguage),
                    })),
                  }))}
              currentChange={markedHunk}
              on:viewscroll={trackScroll}
            />
          {:else if wholeFileView}
            <!-- The whole file, virtualised: every line is in the list but only
                 the visible ones are in the DOM, which is what makes showing a
                 3000-line file affordable. -->
            <div class="diff-lines whole-file">
              <VirtualLines
                bind:this={virtualLines}
                lines={flatLines}
                language={lineLanguage}
                blockFrom={markedBlock.from}
                blockTo={markedBlock.to}
                on:viewscroll={trackScroll}
              />
            </div>
          {:else}
            <!-- Tracked as it scrolls: by the time the tab switch tears this
                 down there is nothing left to read the position from. -->
            <div class="diff-lines" bind:this={hunkListEl} on:scroll={trackScroll}>
              {#each renderedHunks as view (view.hunk.index)}
                <div
                  class="hunk"
                  class:current-hunk={view.hunk.index === currentHunkIndex}
                  data-hunk={view.hunk.index}
                >
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
                      <!-- memoHighlightLine escapes everything it emits; the diff
                           contains whatever the repository holds, including
                           files that are themselves HTML. -->
                      <code>{@html memoHighlightLine(line.text, lineLanguage)}</code>
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
     have a comfortable grab area without shifting the layout.
     flex-basis rather than width, because on a flex item the basis is what
     decides the size — the width here was being overridden, leaving a 1px box
     that only min-width stretched. Same 11px as the history dialog's splitter,
     so the two feel alike. */
  .pane-resizer {
    flex: 0 0 11px;
    margin: 0 -5px;
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

  /* Sits beside revert and looks like it: same size, same quiet treatment
     until hovered. Green rather than amber, because this one changes nothing. */
  .open-file:hover:not(:disabled) {
    color: #4ade80 !important;
    border-color: rgba(74, 222, 128, 0.4) !important;
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
  /* Sits above the panes, full width, so it never covers the code being
     searched. Styled like the note's find bar so the two read as one feature. */
  .diff-find {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 10px;
    background: #14141f;
    border-bottom: 1px solid rgba(255, 255, 255, 0.08);
    flex-shrink: 0;
  }

  .diff-find input {
    flex: 1;
    min-width: 0;
    padding: 5px 9px;
    background: rgba(0, 0, 0, 0.3);
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 6px;
    color: #e5e7eb;
    font-size: 13px;
    font-family: inherit;
  }

  .diff-find input:focus {
    outline: none;
    border-color: rgba(var(--accent-rgb), 0.6);
  }

  .diff-find .find-count {
    font-size: 12px;
    color: #6b7280;
    font-variant-numeric: tabular-nums;
    min-width: 52px;
    text-align: center;
  }

  .diff-find button {
    padding: 4px 9px;
    background: transparent;
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 5px;
    color: #9ca3af;
    font-size: 13px;
    line-height: 1;
    cursor: pointer;
  }

  .diff-find button:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.07);
    color: #e5e7eb;
  }

  .diff-find button:disabled {
    opacity: 0.4;
    cursor: default;
  }

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
  .selected-nav {
    display: flex;
    gap: 4px;
    flex-shrink: 0;
    margin-left: 8px;
  }
  .nav-btn {
    padding: 2px 7px;
    font-size: 11px;
    line-height: 1.4;
    cursor: pointer;
    border-radius: 4px;
    border: 1px solid rgba(255, 255, 255, 0.18);
    background: rgba(255, 255, 255, 0.06);
    color: inherit;
  }
  .nav-btn.wide { padding: 2px 10px; }

  .nav-btn:hover { background: rgba(255, 255, 255, 0.14); }
  .nav-btn.active {
    border-color: rgba(var(--accent-rgb), 0.5);
    background: rgba(var(--accent-rgb), 0.16);
  }
  /* The virtual viewport owns the scrolling in this mode; the wrapper must not
     also scroll, or the two fight and the jump-to-change lands short. */
  .diff-lines.whole-file {
    height: 100%;
    overflow: hidden;
  }
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

  /* Holds the scroller and the two hints, which are positioned against it. */
  .diff-viewport {
    position: relative;
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }

  /* Where the walk is about to take you, at the edge it will take you towards:
     the next file at the bottom, the previous at the top. Floating over the
     code rather than beside the buttons, because that is where the eye is when
     it reaches the end of a file. */
  .file-hint {
    position: absolute;
    right: 18px;
    z-index: 5;
    display: flex;
    align-items: center;
    gap: 5px;
    max-width: 60%;
    padding: 3px 10px;
    font-size: 11px;
    font-family: inherit;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    cursor: pointer;
    border-radius: 5px;
    border: 1px solid rgba(var(--accent-rgb), 0.35);
    /* Opaque, not tinted: it sits over code, and a translucent label with text
       behind it is unreadable. */
    background: #1f2430;
    color: #c9d1d9;
    opacity: 0.9;
  }
  .file-hint:hover { opacity: 1; border-color: rgba(var(--accent-rgb), 0.6); }
  .file-hint.top { top: 8px; }
  .file-hint.bottom { bottom: 8px; }

  .diff-content {
    flex: 1;
    overflow: auto;
    min-width: 0;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 13px;
    user-select: text;
    -webkit-user-select: text;
  }

  .diff-content.columns {
    overflow: hidden;
    display: flex;
    flex-direction: column;
    min-height: 0;
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

  /* The tint carries added/removed, not the text colour: syntax highlighting
     colours the code itself, and a line-wide colour would either fight it or
     win and undo it. The background is also the more legible signal — it marks
     the whole line, including its indentation. */
  /* The tint alone carries added/removed. The text keeps its syntax colours —
     or the ordinary foreground where there are none — so a line reads the same
     here as it does in the editor, and the eye is not asked to tell green code
     from green-because-added. */
  .diff-line.add {
    background: rgba(34, 197, 94, 0.08);
  }
  .diff-line.remove {
    background: rgba(239, 68, 68, 0.08);
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

  /* The change the arrows are on. Only the changed lines, not the context
     around them: a hunk's context is most of what it contains, and marking all
     of it says "this whole region" where the question is "these lines".
     A bar down the left edge rather than a tint, since the add/remove tints
     already carry meaning here. */
  .hunk.current-hunk .diff-line.add,
  .hunk.current-hunk .diff-line.remove {
    box-shadow: inset 3px 0 0 var(--accent, #61afef);
  }

  .hunk-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }



</style>
