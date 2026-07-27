<script lang="ts">
  import { onMount, onDestroy, createEventDispatcher } from 'svelte';
  import { selectedSessionId } from '../../stores/sessions';
  import { get } from 'svelte/store';
  import * as App from '../../../../wailsjs/go/main/App';
  import type { session } from '../../../../wailsjs/go/models';
  import { t } from '../../i18n';
  import { flattenTree, type DirNode, type TreeRow } from '../../utils/fileTree';
  import { detectLanguage, highlightLines } from '../../utils/highlight';
  import { fileTypeOf } from '../../utils/fileTypes';
  import ConfirmDialog from '../Dialogs/ConfirmDialog.svelte';
  import FileQuickOpen from './FileQuickOpen.svelte';

  export let active = false;

  const dispatch = createEventDispatcher();

  // One loaded directory, keyed by its path relative to the session root ("" is
  // the root itself). Directories are fetched only when the user opens them —
  // see loadDir.
  interface LoadedDir {
    entries: session.BrowseEntry[];
    truncated: boolean;
    totalEntries: number;
  }

  let dirs: Record<string, LoadedDir> = {};
  let expanded = new Set<string>(['']);
  let loadingDirs = new Set<string>();
  let dirErrors: Record<string, string> = {};

  let rootAbsPath = '';
  let selectedPath: string | null = null;
  let selectedFile: session.BrowseFile | null = null;
  let fileLoading = false;
  let fileError = '';
  let rootError = '';
  let rootLoading = false;

  let loadedSessionId: string | null = null;
  let treeGeneration = 0;
  let fileGeneration = 0;
  let destroyed = false;

  // --- Editing --------------------------------------------------------------

  // The editor holds ONLY the text. Everything the textarea cannot represent —
  // the BOM, the line-ending convention, whether the file ends in a newline —
  // lives in `openedFile.shape` and is handed straight back to the save
  // untouched. Nothing the user can do in the textarea reaches it, which is
  // what makes an unmodified save byte-identical rather than merely usually
  // byte-identical.
  let editing = false;
  let openedFile: session.EditableFile | null = null;
  let editText = '';
  // The text as it came from disk, for the modified check. Compared rather than
  // tracked with a dirty flag so undoing every change correctly clears it.
  let savedText = '';
  let saving = false;
  let saveError = '';
  let savedFlash = false;
  let savedFlashTimer: ReturnType<typeof setTimeout> | null = null;
  let textareaEl: HTMLTextAreaElement | null = null;
  let lineNumbersEl: HTMLDivElement | null = null;
  // "modified" or "deleted" while a conflict is waiting on the user; "" when
  // there is none. The user's text is never discarded to show it.
  let conflict: '' | 'modified' | 'deleted' = '';
  // A pending navigation held back by unsaved changes: the action to run once
  // the user confirms they are willing to lose them.
  let pendingLeave: (() => void) | null = null;

  $: modified = editing && editText !== savedText;
  $: canEdit = !!openedFile && openedFile.editable;

  // Soft wrap is OFF in edit mode, deliberately. The gutter is a separate
  // element scrolled in lockstep with the textarea, and a wrapped line occupies
  // more rows in the textarea than it has numbers — there is no way to keep the
  // two aligned without measuring every wrap. Horizontal scrolling is the
  // honest trade, and it is what a code editor does anyway.
  //
  // Built as a dense array of the numbers themselves rather than iterating
  // Array(n): a sparse array is not reliably enumerated by {#each}, and the
  // numbers have to be a plain list for the rows to key correctly. Capped for
  // the same reason the read view is — a 1 MiB minified file is tens of
  // thousands of lines, and the gutter's DOM is pure overhead past the point
  // where the numbers stop being useful. The TEXT is never capped: the
  // textarea always holds the whole file, so a save can never write a partial
  // buffer.
  const MAX_GUTTER_LINES = 20000;

  $: editLineCount = editing ? Math.min(countLines(editText), MAX_GUTTER_LINES) : 0;
  $: editLineNumbers = buildLineNumbers(editLineCount);

  function buildLineNumbers(count: number): number[] {
    const out: number[] = new Array(count);
    for (let i = 0; i < count; i++) out[i] = i + 1;
    return out;
  }

  // Rendering one <div> per line with no virtualisation is what the diff view
  // does, and a file long enough to hurt is not readable inline anyway. The
  // backend already caps the transfer at 1 MiB; this caps the DOM.
  const MAX_RENDER_LINES = 3000;
  // Above this the file is not rendered until the user asks — parsing and
  // laying out tens of thousands of lines locks the main thread.
  const LARGE_FILE_LINES = 4000;
  const LARGE_FILE_BYTES = 400 * 1024;

  // --- Resizable pane (same mechanics as the diff view) ---------------------

  const FILE_PANE_MIN = 160;
  const FILE_PANE_MAX = 640;
  const FILE_PANE_DEFAULT = 280;
  // Its own key: the browser's tree is deeper than the diff's file list, so the
  // comfortable width differs and sharing one value would fight the user.
  const FILE_PANE_STORAGE_KEY = 'asmgr.browser.filePaneWidth';

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
    // Measure from the pane container, not the window: the splitter sits inside
    // the main panel, so clientX alone would be offset by the sidebar.
    const left = panesEl.getBoundingClientRect().left;
    filePaneWidth = Math.min(FILE_PANE_MAX, Math.max(FILE_PANE_MIN, e.clientX - left));
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

  function resetPaneWidth() {
    filePaneWidth = FILE_PANE_DEFAULT;
    try {
      localStorage.setItem(FILE_PANE_STORAGE_KEY, String(FILE_PANE_DEFAULT));
    } catch {
      // Ignored, as above.
    }
  }

  // --- Quick open -----------------------------------------------------------

  // Ctrl+Shift+O rather than a plain Ctrl+key: everything in this app is
  // Ctrl+Shift so the terminal and tmux never see it, and O is the only free
  // letter left in App.svelte's handler (Ctrl+P is the saved-command picker,
  // Ctrl+K/Ctrl+Shift+P the palette, Ctrl+Shift+F the global history search).
  // Handled here rather than in App.svelte because the shortcut only means
  // anything while the browser is the visible view.
  let showQuickOpen = false;

  function handleWindowKeydown(e: KeyboardEvent) {
    if (!active || !(e.ctrlKey || e.metaKey) || !e.shiftKey || e.altKey) return;
    if (e.key.toLowerCase() !== 'o') return;
    // Another dialog owning the keyboard must keep it — the overlay opening
    // behind a confirm prompt would leave two things listening for Escape.
    if (document.querySelector('.dialog-overlay')) return;
    e.preventDefault();
    e.stopPropagation();
    showQuickOpen = true;
  }

  /**
   * Open a file picked in the quick-open and reveal it in the tree.
   *
   * Revealing means loading every directory on the way down, which the lazy
   * tree has usually never opened. The loads are sequential on purpose: each
   * one is what proves the next path segment is a directory the user may
   * expand, and they are single-digit milliseconds on a local filesystem.
   */
  function openFromQuickOpen(path: string) {
    // Selecting a file discards the edit buffer, so it goes through the same
    // guard as every other way of leaving it.
    guardUnsaved(() => {
      if (editing) leaveEditModeQuietly();
      selectedPath = path;
      void loadFile(path);
      void revealInTree(path);
    });
  }

  async function revealInTree(path: string) {
    const segments = path.split('/');
    // The last segment is the file itself; only its ancestors are directories.
    segments.pop();
    const next = new Set(expanded);
    let prefix = '';
    for (const segment of segments) {
      prefix = prefix ? `${prefix}/${segment}` : segment;
      next.add(prefix);
      // Awaited one level at a time: buildBrowseTree only descends into
      // directories it has a listing for, so a parent must be loaded before
      // its child's row can exist.
      await loadDir(prefix);
      if (destroyed) return;
    }
    // Reassigned rather than mutated — Svelte 3 doesn't track Set mutations.
    expanded = next;
  }

  onMount(() => {
    // Capture phase, matching App.svelte: xterm would otherwise swallow the
    // combo before it reaches us.
    window.addEventListener('keydown', handleWindowKeydown, true);
  });

  onDestroy(() => {
    destroyed = true;
    treeGeneration++;
    fileGeneration++;
    if (savedFlashTimer) clearTimeout(savedFlashTimer);
    window.removeEventListener('keydown', handleWindowKeydown, true);
    // Leaving the tab mid-drag would otherwise strand the document listeners.
    stopPaneResize();
  });

  // --- Loading --------------------------------------------------------------

  // Directories load ON DEMAND rather than walking the tree up front. A session
  // often sits in a repository with node_modules or a build output directory;
  // an eager walk would read hundreds of thousands of entries and freeze the UI
  // before the pane ever appeared — the same failure the diff view hit with a
  // huge repo. Lazy loading costs one round trip per folder the user actually
  // opens, which is imperceptible for a local filesystem.
  async function loadDir(path: string, force = false) {
    if (!force && dirs[path]) return;
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;
    const generation = treeGeneration;

    loadingDirs = new Set(loadingDirs).add(path);
    if (path === '') rootLoading = true;
    try {
      const listing = await App.ListSessionDirectory(sessionId, path);
      if (destroyed || generation !== treeGeneration) return;
      dirs = {
        ...dirs,
        [path]: {
          entries: listing.entries || [],
          truncated: listing.truncated,
          totalEntries: listing.totalEntries,
        },
      };
      if (path === '') {
        rootAbsPath = listing.absPath;
        rootError = '';
      }
      const { [path]: _dropped, ...rest } = dirErrors;
      dirErrors = rest;
    } catch (e) {
      if (destroyed || generation !== treeGeneration) return;
      // One unreadable folder must not blank the whole tree — the message is
      // shown on that row and everything else stays browsable.
      dirErrors = { ...dirErrors, [path]: String(e) };
      if (path === '') rootError = String(e);
    }
    if (destroyed || generation !== treeGeneration) return;
    const next = new Set(loadingDirs);
    next.delete(path);
    loadingDirs = next;
    if (path === '') rootLoading = false;
  }

  async function loadFile(path: string) {
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;
    const generation = ++fileGeneration;
    fileLoading = true;
    fileError = '';
    selectedFile = null;
    try {
      const file = await App.ReadSessionDirectoryFile(sessionId, path);
      if (destroyed || generation !== fileGeneration) return;
      selectedFile = file;
    } catch (e) {
      if (destroyed || generation !== fileGeneration) return;
      // The Go side reports permission problems and missing files verbatim;
      // showing that beats a generic "could not load".
      fileError = String(e);
    }
    if (!destroyed && generation === fileGeneration) fileLoading = false;
  }

  // --- Edit mode ------------------------------------------------------------

  async function enterEditMode() {
    if (!selectedPath) return;
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;
    const generation = fileGeneration;
    saveError = '';
    conflict = '';
    try {
      // Opened through its own call rather than reusing the browse result: the
      // version that guards the save has to describe the exact bytes the text
      // was decoded from, and a version taken from a separate read could
      // already be stale.
      const file = await App.OpenSessionFileForEdit(sessionId, selectedPath);
      if (destroyed || generation !== fileGeneration) return;
      openedFile = file;
      editText = file.text;
      savedText = file.text;
      editing = true;
    } catch (e) {
      if (destroyed || generation !== fileGeneration) return;
      saveError = String(e);
    }
  }

  /** Leave edit mode, discarding the buffer. Callers check `modified` first. */
  function leaveEditMode() {
    editing = false;
    openedFile = null;
    editText = '';
    savedText = '';
    saveError = '';
    conflict = '';
    // The read view is showing a snapshot from before the edits; re-read it so
    // it does not contradict what was just saved.
    if (selectedPath) void loadFile(selectedPath);
  }

  /** Run `action`, first asking about unsaved changes if there are any. */
  function guardUnsaved(action: () => void) {
    if (modified) {
      pendingLeave = action;
      return;
    }
    action();
  }

  function confirmDiscard() {
    const action = pendingLeave;
    pendingLeave = null;
    if (action) action();
  }

  function cancelDiscard() {
    pendingLeave = null;
  }

  async function save(overwrite = false) {
    if (!openedFile || !selectedPath || saving) return;
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;
    saving = true;
    saveError = '';
    // The text is captured before the await: the user keeps typing during the
    // round trip, and the version returned by the save describes THESE bytes.
    const submitted = editText;
    try {
      const result = await App.SaveSessionFileEdit(
        sessionId,
        selectedPath,
        submitted,
        openedFile.shape,
        openedFile.version,
        overwrite,
      );
      if (destroyed) return;
      if (result.conflict) {
        // The buffer is deliberately left alone — the whole point of refusing
        // is that the user's work survives to be re-offered.
        conflict = result.conflict === 'deleted' ? 'deleted' : 'modified';
        return;
      }
      conflict = '';
      if (result.file) openedFile = result.file;
      savedText = submitted;
      flashSaved();
    } catch (e) {
      if (destroyed) return;
      saveError = String(e);
    } finally {
      if (!destroyed) saving = false;
    }
  }

  function flashSaved() {
    savedFlash = true;
    if (savedFlashTimer) clearTimeout(savedFlashTimer);
    savedFlashTimer = setTimeout(() => {
      savedFlash = false;
      savedFlashTimer = null;
    }, 2000);
  }

  /** Conflict resolution: take the disk version and lose the local edits. */
  async function reloadFromDisk() {
    conflict = '';
    await enterEditMode();
  }

  /** Conflict resolution: keep editing, leaving the conflict unresolved. */
  function dismissConflict() {
    conflict = '';
    textareaEl?.focus();
  }

  function handleEditorKeydown(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key === 's') {
      e.preventDefault();
      void save();
      return;
    }
    if (e.key === 'Escape') {
      e.preventDefault();
      guardUnsaved(leaveEditMode);
      return;
    }
    if (e.key === 'Tab') {
      // A textarea in a code editor has to insert a tab; moving focus out
      // mid-line is never what the user meant. Shift+Tab is left alone so the
      // editor can still be escaped by keyboard.
      if (e.shiftKey) return;
      e.preventDefault();
      insertTab();
    }
  }

  function insertTab() {
    const el = textareaEl;
    if (!el) return;
    const start = el.selectionStart;
    const end = el.selectionEnd;
    // Written through the DOM value rather than by reassigning editText alone:
    // setting the bound value re-renders and would drop the caret to the end.
    el.value = el.value.slice(0, start) + '\t' + el.value.slice(end);
    el.selectionStart = el.selectionEnd = start + 1;
    editText = el.value;
  }

  // The gutter is a sibling element, so it only stays aligned if it is scrolled
  // with the textarea. Wrapping is off (see editLineCount) so one line is
  // always exactly one row in both.
  function syncGutterScroll() {
    if (lineNumbersEl && textareaEl) lineNumbersEl.scrollTop = textareaEl.scrollTop;
  }

  function resetForSession() {
    treeGeneration++;
    fileGeneration++;
    editing = false;
    openedFile = null;
    editText = '';
    savedText = '';
    saveError = '';
    conflict = '';
    pendingLeave = null;
    dirs = {};
    dirErrors = {};
    loadingDirs = new Set<string>();
    expanded = new Set<string>(['']);
    selectedPath = null;
    selectedFile = null;
    fileError = '';
    rootError = '';
    rootAbsPath = '';
  }

  // The tree is keyed by paths inside ONE session's directory, so it all has to
  // go when the session changes. The tracking variable is assigned INSIDE the
  // block: Svelte 3 orders reactive statements by dependency, not by source
  // position, so a guard updated elsewhere could swallow the change.
  //
  // Gated on `active` for the same reason the diff view is: reading a directory
  // for a tab nobody is looking at is work the user never asked for.
  $: if (active && $selectedSessionId !== loadedSessionId) {
    loadedSessionId = $selectedSessionId;
    resetForSession();
    if ($selectedSessionId) void loadDir('');
  }

  // Navigating away from the Files tab keeps this component mounted but hidden,
  // so unsaved edits would sit there invisibly until the session changed and
  // dropped them. Surfacing the warning on deactivation is the last point at
  // which the user can still act on it.
  //
  // wasActive is assigned INSIDE the block: Svelte 3 orders reactive statements
  // by dependency, not by source position, so a tracking variable updated in a
  // separate statement could be written before this one ever runs.
  let wasActive = false;
  $: if (active !== wasActive) {
    const leftTheView = wasActive && !active;
    wasActive = active;
    if (leftTheView && editing && editText !== savedText) {
      // Only the prompt is raised; nothing is discarded. Confirming leaves edit
      // mode, cancelling simply leaves the buffer where it is for when the user
      // comes back.
      pendingLeave = leaveEditMode;
    }
  }

  function refresh() {
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;
    // A refresh re-reads the open file, which would silently replace the
    // buffer, so it is guarded like any other way of losing the edits.
    guardUnsaved(() => {
      if (editing) leaveEditModeQuietly();
      // The quick-open index is a snapshot of the same tree, so a refresh has
      // to drop it too or the picker keeps offering files that are gone.
      void App.InvalidateSessionFileIndex(sessionId);
      // Re-fetch everything the user had open, so a refresh shows the current
      // state of the tree rather than silently keeping stale listings.
      const open = Array.from(expanded);
      treeGeneration++;
      dirs = {};
      dirErrors = {};
      loadingDirs = new Set<string>();
      for (const path of open) void loadDir(path, true);
      if (selectedPath) void loadFile(selectedPath);
    });
  }

  // --- Tree assembly --------------------------------------------------------

  // The rows are produced by fileTree.ts's flattenTree, exactly as the diff view
  // does — only the DirNode is assembled from the lazily loaded listings here
  // instead of from a flat list of paths, because a lazy browser knows its
  // directories before it knows their contents.
  //
  // flattenTree's `collapsed` set means "do not descend", which for a lazy tree
  // is every directory the user has not opened.
  $: treeRoot = buildBrowseTree(dirs);
  $: notExpanded = collectUnexpanded(treeRoot, expanded);
  $: treeRows = flattenTree<BrowseTreeFile>(treeRoot, notExpanded);

  /** What flattenTree hands back for a file row, plus the entry behind it. */
  interface BrowseTreeFile {
    path: string;
    added: number;
    removed: number;
    entry: session.BrowseEntry;
  }

  function buildBrowseTree(loaded: Record<string, LoadedDir>): DirNode {
    return buildNode('', '', loaded);
  }

  function buildNode(path: string, label: string, loaded: Record<string, LoadedDir>): DirNode {
    const node: DirNode = { path, label, dirs: [], files: [], added: 0, removed: 0, fileCount: 0 };
    const listing = loaded[path];
    if (!listing) return node;

    for (const entry of listing.entries) {
      if (entry.isDir) {
        node.dirs.push(buildNode(entry.path, entry.name, loaded));
      } else {
        // Typed as BrowseTreeFile before the push: DirNode.files is
        // TreeFileInput[], and an object literal with the extra `entry` field
        // would be rejected by excess-property checking pushed inline.
        // added/removed are zero because a browser has no diff stats — the
        // fields exist only because flattenTree's row model carries them.
        const file: BrowseTreeFile = { path: entry.path, added: 0, removed: 0, entry };
        node.files.push(file);
      }
    }
    // fileCount is what the row badge shows. For an unopened directory we
    // genuinely do not know it yet, so it stays 0 and the badge is hidden
    // rather than showing a number that is a guess.
    node.fileCount = node.files.length;
    for (const dir of node.dirs) node.fileCount += dir.fileCount;
    return node;
  }

  /** Every directory in the tree the user has NOT opened. */
  function collectUnexpanded(node: DirNode, open: Set<string>): Set<string> {
    const closed = new Set<string>();
    const visit = (n: DirNode) => {
      for (const dir of n.dirs) {
        if (!open.has(dir.path)) closed.add(dir.path);
        visit(dir);
      }
    };
    visit(node);
    return closed;
  }

  function toggleDir(path: string) {
    // Reassign rather than mutate — Svelte 3 doesn't track Set mutations.
    const next = new Set(expanded);
    if (next.has(path)) {
      next.delete(path);
    } else {
      next.add(path);
      void loadDir(path);
    }
    expanded = next;
  }

  function selectFile(path: string) {
    if (path === selectedPath && editing) return;
    // Switching files throws the buffer away, so it goes through the same
    // guard as leaving the view.
    guardUnsaved(() => {
      if (editing) leaveEditModeQuietly();
      selectedPath = path;
      void loadFile(path);
    });
  }

  /** Leave edit mode without re-reading — the caller is loading a file anyway. */
  function leaveEditModeQuietly() {
    editing = false;
    openedFile = null;
    editText = '';
    savedText = '';
    saveError = '';
    conflict = '';
  }

  // Hoisted out of the markup: Svelte's template can't narrow the row union,
  // and casts aren't allowed there.
  function rowFile(row: TreeRow<BrowseTreeFile>): BrowseTreeFile {
    return (row as { kind: 'file'; file: BrowseTreeFile }).file;
  }

  // Indent step in px, matching the diff view's tree.
  const TREE_INDENT = 12;

  // --- Rendering ------------------------------------------------------------

  function splitPath(path: string): { dir: string; name: string } {
    const idx = path.lastIndexOf('/');
    if (idx < 0) return { dir: '', name: path };
    return { dir: path.slice(0, idx + 1), name: path.slice(idx + 1) };
  }

  function formatSize(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  }

  // Cheap line count: counts newlines without allocating a big array (split()).
  function countLines(content: string): number {
    let n = 1;
    for (let i = 0; i < content.length; i++) {
      if (content.charCodeAt(i) === 10) n++;
    }
    return n;
  }

  $: selectedParts = splitPath(selectedPath || '');
  $: fileContent = selectedFile && !selectedFile.binary ? selectedFile.content : '';
  $: isLargeFile =
    fileContent.length > LARGE_FILE_BYTES ||
    (fileContent ? countLines(fileContent) : 0) > LARGE_FILE_LINES;
  $: forceShow = !!selectedPath && !!forceShowPaths[selectedPath];
  $: shouldRender = active && !!selectedFile && !selectedFile.binary && (!isLargeFile || forceShow);

  // Splitting a big string is itself expensive, so it only happens once the
  // file is actually going to be rendered.
  $: renderedLines = shouldRender ? fileContent.split('\n').slice(0, MAX_RENDER_LINES) : [];
  $: hiddenLineCount = shouldRender
    ? Math.max(0, countLines(fileContent) - renderedLines.length)
    : 0;
  $: emptyFile = !!selectedFile && !selectedFile.binary && fileContent === '';

  // --- Syntax highlighting --------------------------------------------------

  // Hangs off `renderedLines`, so it inherits every guard already in place: it
  // runs only when `shouldRender` is true, and only over the MAX_RENDER_LINES
  // the pane actually draws — the cost is bounded by what is on screen, not by
  // the file's real size. Tokenising 3000 lines is a few milliseconds, well
  // under the cost of creating the DOM nodes for them, so it adds no stall the
  // existing render didn't already have.
  //
  // detectLanguage returns null for anything we have no scanner for, and
  // highlightLines passes that through as null, which the markup renders as
  // plain text.
  $: fileLanguage = shouldRender ? detectLanguage(selectedPath || '') : null;
  $: highlighted = fileLanguage ? highlightLines(renderedLines, fileLanguage) : null;

  // Which files the user explicitly opted into rendering despite their size.
  let forceShowPaths: Record<string, boolean> = {};

  function showSelectedAnyway() {
    if (!selectedPath) return;
    forceShowPaths = { ...forceShowPaths, [selectedPath]: true };
  }

  // The opt-ins are keyed by path, so they would silently apply to a different
  // repository's files after a session switch. Reset with the session; the
  // tracking variable is assigned INSIDE the block for the dependency-ordering
  // reason above.
  let forceShowSessionId: string | null = null;
  $: if ($selectedSessionId !== forceShowSessionId) {
    forceShowSessionId = $selectedSessionId;
    forceShowPaths = {};
  }

  $: rootListing = dirs[''] || null;
  $: rootEmpty = !!rootListing && rootListing.entries.length === 0;
</script>

<div class="browser-container">
  <div class="browser-header">
    <div class="header-left">
      <span class="browser-title">{$t('browser.title')}</span>
      {#if rootAbsPath}
        <span class="browser-root" title={rootAbsPath}>{rootAbsPath}</span>
      {/if}
    </div>
    <div class="header-right">
      <!-- A button as well as the shortcut: a keyboard-only feature in a view
           reached by mouse is a feature most users never find. -->
      <button class="refresh-btn" on:click={() => (showQuickOpen = true)} title={$t('browser.quickOpenTooltip')}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="11" cy="11" r="7"/>
          <path d="M21 21l-4.35-4.35"/>
        </svg>
      </button>
      <button class="refresh-btn" on:click={refresh} disabled={rootLoading} title={$t('browser.refresh')}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class:spinning={rootLoading}>
          <path d="M23 4v6h-6M1 20v-6h6"/>
          <path d="M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15"/>
        </svg>
      </button>
      <!-- The browser's own way back to the terminal. It matters when the view
           bar is hidden for this tab, where this is the only exit. -->
      <button class="refresh-btn" on:click={() => dispatch('close')} title={$t('browser.close')}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M18 6L6 18M6 6l12 12"/>
        </svg>
      </button>
    </div>
  </div>

  {#if rootLoading && !rootListing}
    <div class="browser-state loading">{$t('browser.loading')}</div>
  {:else if rootError}
    <div class="browser-state error">{rootError}</div>
  {:else}
    <div class="browser-body" bind:this={panesEl} class:resizing={isResizingPane}>
      <div class="file-pane" style="width: {filePaneWidth}px">
        <div class="file-pane-header">
          <span>{$t('browser.filesLabel')}</span>
        </div>
        <div class="file-list">
          {#if rootEmpty}
            <div class="tree-note">{$t('browser.emptyDirectory')}</div>
          {/if}
          {#each treeRows as row (row.kind + ':' + row.path)}
            {#if row.kind === 'dir'}
              <div
                class="tree-dir"
                style="padding-left: {10 + row.depth * TREE_INDENT}px"
                role="button"
                tabindex="0"
                aria-expanded={expanded.has(row.path)}
                title={row.path}
                on:click={() => toggleDir(row.path)}
                on:keydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggleDir(row.path); } }}
              >
                <div class="file-main">
                  <svg
                    class="tree-caret"
                    class:collapsed={!expanded.has(row.path)}
                    width="12" height="12" viewBox="0 0 24 24"
                    fill="none" stroke="currentColor" stroke-width="2"
                  >
                    <polyline points="6 9 12 15 18 9"/>
                  </svg>
                  <span class="tree-dir-name">{row.label}</span>
                </div>
                {#if loadingDirs.has(row.path)}
                  <span class="tree-hint">{$t('browser.loadingShort')}</span>
                {:else if dirErrors[row.path]}
                  <span class="tree-hint error" title={dirErrors[row.path]}>{$t('browser.unreadable')}</span>
                {:else if expanded.has(row.path) && row.fileCount > 0}
                  <span class="tree-dir-count">{row.fileCount}</span>
                {/if}
              </div>
              {#if expanded.has(row.path) && dirs[row.path] && dirs[row.path].entries.length === 0}
                <div class="tree-note" style="padding-left: {22 + row.depth * TREE_INDENT}px">
                  {$t('browser.emptyDirectory')}
                </div>
              {/if}
              {#if expanded.has(row.path) && dirs[row.path] && dirs[row.path].truncated}
                <div class="tree-note" style="padding-left: {22 + row.depth * TREE_INDENT}px">
                  {$t('browser.listTruncated', { shown: dirs[row.path].entries.length, total: dirs[row.path].totalEntries })}
                </div>
              {/if}
            {:else}
              {@const file = rowFile(row)}
              {@const type = fileTypeOf(file.entry.name)}
              <div
                class="file-row tree-file"
                class:selected={file.path === selectedPath}
                class:unreadable={file.entry.unreadable}
                style="padding-left: {10 + row.depth * TREE_INDENT}px"
                role="button"
                tabindex="0"
                title={file.entry.unreadable ? $t('browser.unreadable') : file.path}
                on:click={() => selectFile(file.path)}
                on:keydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); selectFile(file.path); } }}
              >
                <div class="file-main">
                  <!-- The page icon stays: it is what separates a file row from
                       a directory row, which has a caret in the same slot. The
                       type only recolours it, so the signal costs no width. -->
                  <svg
                    class="file-icon"
                    style="color: {type.colour}"
                    width="12" height="12" viewBox="0 0 24 24"
                    fill="none" stroke="currentColor" stroke-width="2"
                  >
                    <title>{$t('browser.fileType')}: {type.label}</title>
                    <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
                    <polyline points="14 2 14 8 20 8"/>
                  </svg>
                  <span class="file-path tree-file-path">
                    <span class="file-name">{file.entry.name}</span>
                  </span>
                </div>
                <div class="file-meta">
                  <span class="file-size">{formatSize(file.entry.size)}</span>
                </div>
              </div>
            {/if}
          {/each}
          {#if rootListing && rootListing.truncated}
            <div class="tree-note">
              {$t('browser.listTruncated', { shown: rootListing.entries.length, total: rootListing.totalEntries })}
            </div>
          {/if}
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

      <div class="content-pane">
        <!-- The list shows only the basename, so the full path belongs here —
             otherwise two same-named files in different directories are
             indistinguishable once selected. -->
        {#if selectedPath}
          <div class="selected-path" title={selectedPath}>
            <span class="selected-text"><span class="selected-dir">{selectedParts.dir}</span><span class="selected-name">{selectedParts.name}</span></span>
            {#if modified}
              <span class="modified-dot" title={$t('browser.unsavedChanges')}>●</span>
            {/if}
            {#if savedFlash}
              <span class="saved-flash">{$t('browser.saved')}</span>
            {/if}
            {#if selectedFile}
              <span class="selected-size">{formatSize(selectedFile.size)}</span>
            {/if}
            {#if editing}
              <button class="edit-btn primary" on:click={() => save()} disabled={saving || !modified}>
                {saving ? $t('browser.saving') : $t('browser.save')}
              </button>
              <button class="edit-btn" on:click={() => guardUnsaved(leaveEditMode)} disabled={saving}>
                {$t('browser.stopEditing')}
              </button>
            {:else if selectedFile && !selectedFile.binary && !selectedFile.truncated}
              <button class="edit-btn" on:click={enterEditMode}>{$t('browser.edit')}</button>
            {/if}
          </div>
        {/if}

        {#if editing && conflict}
          <!-- Deliberately a banner over the editor rather than a modal: the
               user's text stays visible and editable behind it, which is the
               whole reason the save was refused instead of applied. -->
          <div class="conflict-banner" class:deleted={conflict === 'deleted'}>
            <span class="conflict-text">
              {conflict === 'deleted' ? $t('browser.conflictDeleted') : $t('browser.conflictModified')}
            </span>
            <div class="conflict-actions">
              <button on:click={() => save(true)} disabled={saving}>{$t('browser.conflictOverwrite')}</button>
              {#if conflict !== 'deleted'}
                <button on:click={reloadFromDisk} disabled={saving}>{$t('browser.conflictReload')}</button>
              {/if}
              <button on:click={dismissConflict}>{$t('browser.conflictKeepEditing')}</button>
            </div>
          </div>
        {/if}
        {#if editing && saveError}
          <div class="file-notice error">{saveError}</div>
        {/if}

        {#if editing}
          {#if canEdit}
            <div class="editor">
              <!-- Soft wrap is off, so one text line is exactly one gutter row
                   and the two stay aligned by construction. -->
              <div class="editor-gutter" bind:this={lineNumbersEl} aria-hidden="true">
                {#each editLineNumbers as n (n)}
                  <div class="editor-line-no">{n}</div>
                {/each}
              </div>
              <textarea
                class="editor-textarea"
                bind:this={textareaEl}
                bind:value={editText}
                on:keydown={handleEditorKeydown}
                on:scroll={syncGutterScroll}
                spellcheck="false"
                autocomplete="off"
                autocapitalize="off"
                autocorrect="off"
                wrap="off"
                aria-label={$t('browser.editorLabel')}
              ></textarea>
            </div>
          {:else}
            <div class="browser-state error">
              <span>
                {#if openedFile && openedFile.notEditableReason === 'truncated'}
                  {$t('browser.cannotEditTruncated')}
                {:else if openedFile && openedFile.notEditableReason === 'binary'}
                  {$t('browser.cannotEditBinary')}
                {:else}
                  {$t('browser.cannotEdit')}
                {/if}
              </span>
            </div>
          {/if}
        {:else}
        <div class="file-content">
          {#if !selectedPath}
            <div class="browser-state">
              <span>{$t('browser.selectFile')}</span>
            </div>
          {:else if fileLoading}
            <div class="browser-state">{$t('browser.loading')}</div>
          {:else if fileError}
            <div class="browser-state error">{fileError}</div>
          {:else if !selectedFile}
            <div class="browser-state">{$t('browser.loading')}</div>
          {:else if selectedFile.binary}
            <div class="browser-state">
              <span>{$t('browser.binaryFile')}</span>
              <span class="state-hint">{$t('browser.binaryFileHint', { size: formatSize(selectedFile.size) })}</span>
            </div>
          {:else if emptyFile}
            <div class="browser-state">
              <span>{$t('browser.emptyFile')}</span>
            </div>
          {:else if isLargeFile && !forceShow}
            <div class="large-file-warning">
              <svg width="44" height="44" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
                <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
                <line x1="12" y1="9" x2="12" y2="13"/>
                <line x1="12" y1="17" x2="12.01" y2="17"/>
              </svg>
              <span class="large-file-title">{$t('browser.largeFileTitle', { size: formatSize(selectedFile.size) })}</span>
              <span class="large-file-hint">{$t('browser.largeFileHint')}</span>
              <button class="large-file-show" on:click={showSelectedAnyway}>
                {$t('browser.showAnyway', { count: MAX_RENDER_LINES })}
              </button>
            </div>
          {:else}
            {#if selectedFile.truncated}
              <div class="file-notice">{$t('browser.fileTruncated', { size: formatSize(selectedFile.size) })}</div>
            {/if}
            <div class="file-lines">
              {#each renderedLines as line, idx}
                <div class="file-line">
                  <span class="line-no">{idx + 1}</span><code
                    >{#if highlighted}{#each highlighted[idx] as token}<span
                          class={token.kind ? 'tk-' + token.kind : ''}>{token.text}</span
                        >{/each}{:else}{line}{/if}</code
                  >
                </div>
              {/each}
              {#if hiddenLineCount > 0}
                <div class="file-line truncated">
                  <span class="line-no"></span><code>{$t('browser.linesHidden', { count: hiddenLineCount })}</code>
                </div>
              {/if}
            </div>
          {/if}
        </div>
        {/if}
      </div>
    </div>
  {/if}
</div>

<FileQuickOpen
  bind:show={showQuickOpen}
  sessionId={$selectedSessionId || ''}
  on:pick={(e) => openFromQuickOpen(e.detail.path)}
/>

{#if pendingLeave}
  <ConfirmDialog
    show={true}
    variant="warning"
    title={$t('browser.unsavedTitle')}
    message={$t('browser.unsavedMessage')}
    confirmText={$t('browser.discardChanges')}
    cancelText={$t('browser.keepEditing')}
    on:confirm={confirmDiscard}
    on:cancel={cancelDiscard}
  />
{/if}

<style>
  .browser-container {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: #0a0a0f;
  }

  .browser-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 10px 16px;
    background: rgba(0, 0, 0, 0.3);
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  }

  .header-left {
    display: flex;
    align-items: baseline;
    gap: 12px;
    min-width: 0;
  }

  .header-right {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .browser-title {
    flex-shrink: 0;
    font-size: 13px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #6b7280;
  }

  /* The root path can be long; it may shrink away entirely before the title. */
  .browser-root {
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 12px;
    color: #6b7280;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    direction: rtl;
    text-align: left;
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

  /* Two-pane layout: tree left, file contents right. */
  .browser-body {
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
  .browser-body.resizing {
    cursor: col-resize;
    user-select: none;
  }
  .browser-body.resizing .pane-resizer {
    background: rgba(var(--accent-rgb), 0.5);
  }

  .file-pane-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 6px 8px 6px 12px;
    font-size: 12px;
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

  /* Directory rows read as structure, not as content: dimmer than the file
     names underneath them. */
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

  .tree-hint {
    flex-shrink: 0;
    font-size: 11px;
    color: #6b7280;
  }
  .tree-hint.error {
    color: #f87171;
  }

  /* Notices that belong to a directory rather than to a row of its own. */
  .tree-note {
    padding: 4px 10px 6px;
    font-size: 11px;
    font-style: italic;
    color: #4b5563;
  }

  .file-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 5px 10px;
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
  /* An entry whose metadata could not be read is still listed, but dimmed so it
     is obviously not a normal file. */
  .file-row.unreadable .file-name,
  .file-row.unreadable .file-size {
    color: #4b5563;
    font-style: italic;
  }
  /* The type colour is set inline, so only opacity can dim it here — and the
     row saying "unreadable" matters more than what type it would have been. */
  .file-row.unreadable .file-icon {
    opacity: 0.4;
  }

  .file-main {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  /* The colour comes from the file type, set inline per row. */
  .file-icon {
    flex-shrink: 0;
    opacity: 0.9;
  }
  .file-row:hover .file-icon,
  .file-row.selected .file-icon {
    opacity: 1;
  }

  .file-path {
    font-size: 13px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  /* Inside the tree the directory is already the row above, so the file row
     shows only the basename — and it must not be reversed. */
  .tree-file-path {
    direction: ltr;
  }

  .file-name {
    color: #e4e4e7;
  }

  .file-meta {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
  }

  .file-size {
    font-size: 11px;
    font-family: monospace;
    color: #6b7280;
  }

  /* Wraps the path header + the scrolling contents, so only the contents scroll
     and the path stays put while reading a long file. */
  .content-pane {
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
  .selected-size {
    flex-shrink: 0;
    color: #6b7280;
  }

  /* --- Editing ------------------------------------------------------------ */

  .edit-btn {
    flex-shrink: 0;
    padding: 3px 10px;
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 5px;
    background: rgba(255, 255, 255, 0.05);
    color: #d4d4d8;
    font-family: inherit;
    font-size: 12px;
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .edit-btn:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.1);
    color: white;
  }
  .edit-btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .edit-btn.primary:not(:disabled) {
    background: rgba(var(--accent-rgb), 0.2);
    border-color: rgba(var(--accent-rgb), 0.5);
    color: var(--accent);
  }
  .edit-btn.primary:hover:not(:disabled) {
    background: rgba(var(--accent-rgb), 0.32);
  }

  /* The unsaved marker sits next to the file name, where the eye already is
     when deciding whether to leave. */
  .modified-dot {
    flex-shrink: 0;
    color: #fbbf24;
    font-size: 14px;
    line-height: 1;
  }

  .saved-flash {
    flex-shrink: 0;
    color: #4ade80;
    font-size: 12px;
  }

  /* Gutter + textarea as siblings, scrolled in lockstep. */
  .editor {
    flex: 1;
    display: flex;
    min-height: 0;
    min-width: 0;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 13px;
    /* line-height must match between the two columns exactly or the numbers
       drift by a fraction of a row per line and are visibly wrong by the
       bottom of a long file. */
    line-height: 1.6;
  }

  .editor-gutter {
    flex-shrink: 0;
    width: 56px;
    padding: 12px 8px 12px 0;
    overflow: hidden;
    text-align: right;
    color: #3f3f46;
    background: rgba(0, 0, 0, 0.2);
    user-select: none;
    -webkit-user-select: none;
  }

  .editor-line-no {
    /* Explicit, not inherited: a bare div would take the browser's normal
       line-height and fall out of step with the textarea. */
    line-height: 1.6;
    height: 1.6em;
  }

  .editor-textarea {
    flex: 1;
    min-width: 0;
    padding: 12px 16px;
    border: none;
    outline: none;
    resize: none;
    background: transparent;
    color: #d4d4d8;
    font-family: inherit;
    font-size: inherit;
    line-height: inherit;
    /* Off on purpose: with soft wrap a text line can occupy several rows in
       the textarea while the gutter still gives it one number, and there is no
       way to realign them without measuring every wrap. */
    white-space: pre;
    overflow: auto;
    tab-size: 4;
    -moz-tab-size: 4;
  }

  .editor-textarea::-webkit-scrollbar {
    width: 6px;
    height: 6px;
  }
  .editor-textarea::-webkit-scrollbar-track {
    background: transparent;
  }
  .editor-textarea::-webkit-scrollbar-thumb {
    background: rgba(var(--accent-rgb), 0.3);
    border-radius: 3px;
  }

  .conflict-banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    flex-shrink: 0;
    padding: 8px 14px;
    background: rgba(251, 191, 36, 0.12);
    border-bottom: 1px solid rgba(251, 191, 36, 0.35);
    color: #fbbf24;
    font-size: 13px;
  }
  /* A deletion is a harder case than a concurrent edit — saving recreates a
     file someone removed — so it reads as a warning rather than a notice. */
  .conflict-banner.deleted {
    background: rgba(239, 68, 68, 0.12);
    border-bottom-color: rgba(239, 68, 68, 0.35);
    color: #f87171;
  }

  .conflict-text {
    flex: 1;
    min-width: 200px;
    line-height: 1.5;
  }

  .conflict-actions {
    display: flex;
    gap: 8px;
    flex-shrink: 0;
  }
  .conflict-actions button {
    padding: 4px 10px;
    border: 1px solid currentColor;
    border-radius: 5px;
    background: transparent;
    color: inherit;
    font-family: inherit;
    font-size: 12px;
    cursor: pointer;
    transition: background 0.15s ease;
  }
  .conflict-actions button:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.1);
  }
  .conflict-actions button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .file-content {
    flex: 1;
    overflow: auto;
    min-width: 0;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 13px;
    user-select: text;
    -webkit-user-select: text;
  }

  .file-notice {
    padding: 6px 16px;
    font-size: 12px;
    color: #fbbf24;
    background: rgba(251, 191, 36, 0.1);
    border-bottom: 1px solid rgba(251, 191, 36, 0.25);
    flex-shrink: 0;
  }
  .file-notice.error {
    color: #f87171;
    background: rgba(239, 68, 68, 0.1);
    border-bottom-color: rgba(239, 68, 68, 0.25);
  }

  .file-lines {
    padding: 12px 0;
  }

  .file-line {
    display: flex;
    gap: 12px;
    padding: 0 16px;
    line-height: 1.6;
    white-space: pre;
  }

  /* Fixed-width gutter so the code column doesn't shift as the numbers grow,
     and unselectable so copying the file doesn't drag the numbers along. */
  .line-no {
    flex-shrink: 0;
    width: 44px;
    text-align: right;
    color: #3f3f46;
    user-select: none;
    -webkit-user-select: none;
  }

  .file-line code {
    font-family: inherit;
    font-size: inherit;
    color: #d4d4d8;
  }

  .file-line.truncated code {
    color: var(--accent);
    font-weight: 600;
  }

  /* Syntax colours.

     SEMANTIC and fixed — deliberately not derived from --accent. The accent is
     user-chosen and can be any hue (uiThemes.ts: 8 presets plus custom), so a
     palette that tracked it would collide with it on some settings and lose the
     meaning of the colours on others. Fixed hues sit next to any accent.

     Contrast measured against the pane background (#0a0a0f), per WCAG:

       comment  #6b7f8f   4.76:1     function  #82aaff   8.60:1
       string   #9ece6a  10.81:1     property  #73daca  11.86:1
       number   #ff9e64   9.71:1     tag       #f7768e   7.46:1
       keyword  #c792ea   8.21:1     attr      #e0af68   9.88:1
       type     #7dcfff  11.51:1     heading   #7aa2f7   7.84:1
       literal  #ffc777  12.87:1     punct     #89ddff  13.03:1

     All clear AA (4.5:1). Comments are the dimmest on purpose — they should
     recede — but still pass. */
  .file-line code :global(.tk-comment) { color: #6b7f8f; font-style: italic; }
  .file-line code :global(.tk-string) { color: #9ece6a; }
  .file-line code :global(.tk-number) { color: #ff9e64; }
  .file-line code :global(.tk-keyword) { color: #c792ea; }
  .file-line code :global(.tk-type) { color: #7dcfff; }
  .file-line code :global(.tk-literal) { color: #ffc777; }
  .file-line code :global(.tk-function) { color: #82aaff; }
  .file-line code :global(.tk-property) { color: #73daca; }
  .file-line code :global(.tk-tag) { color: #f7768e; }
  .file-line code :global(.tk-attr) { color: #e0af68; }
  .file-line code :global(.tk-heading) { color: #7aa2f7; font-weight: 600; }
  .file-line code :global(.tk-punct) { color: #89ddff; }

  .browser-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    gap: 10px;
    padding: 24px;
    text-align: center;
    color: #4b5563;
  }
  .browser-state.error {
    color: #f87171;
  }
  .state-hint {
    font-size: 13px;
    opacity: 0.7;
  }

  .large-file-warning {
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
  .large-file-title {
    font-size: 15px;
    font-weight: 600;
  }
  .large-file-hint {
    font-size: 13px;
    color: #a1a1aa;
    max-width: 360px;
    line-height: 1.5;
  }
  .large-file-show {
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
  .large-file-show:hover {
    background: rgba(251, 191, 36, 0.25);
  }

  .file-content::-webkit-scrollbar,
  .file-list::-webkit-scrollbar {
    width: 6px;
  }
  .file-content::-webkit-scrollbar-track,
  .file-list::-webkit-scrollbar-track {
    background: transparent;
  }
  .file-content::-webkit-scrollbar-thumb,
  .file-list::-webkit-scrollbar-thumb {
    background: rgba(var(--accent-rgb), 0.3);
    border-radius: 3px;
  }
</style>
