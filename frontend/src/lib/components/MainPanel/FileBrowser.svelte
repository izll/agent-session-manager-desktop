<script lang="ts">
  import { onMount, onDestroy, createEventDispatcher, tick } from 'svelte';
  import { pendingFileJump, clearFileJump } from '../../stores/fileJump';
  import { registerUnsavedGuard } from '../../stores/unsavedChanges';
  import { selectedSessionId, selectedWindowIdx, selectSession, selectWindow } from '../../stores/sessions';
  import { activeProjectId } from '../../stores/projects';
  import { get } from 'svelte/store';
  import * as App from '../../../../wailsjs/go/main/App';
  import type { session } from '../../../../wailsjs/go/models';
  import { t } from '../../i18n';
  import { flattenTree, type DirNode, type TreeRow } from '../../utils/fileTree';
  import { fileTypeOf } from '../../utils/fileTypes';
  import { EditorState, Compartment } from '@codemirror/state';
  import { EditorView, keymap } from '@codemirror/view';
  import { search, searchKeymap, highlightSelectionMatches, openSearchPanel } from '@codemirror/search';
  import { history, historyKeymap, defaultKeymap } from '@codemirror/commands';
  import { baseExtensions, insertTabKeymap, loadLanguage, cachedLanguage } from '../../utils/codemirror';
  import ConfirmDialog from '../Dialogs/ConfirmDialog.svelte';
  import FileQuickOpen from './FileQuickOpen.svelte';

  export let active = false;

  const dispatch = createEventDispatcher();

  async function openRequestedFile(path: string, line?: number) {
    // Same route as opening from quick-open, guard included: selecting a file
    // discards the edit buffer, and a jump from the diff must not throw away
    // unsaved work any more than a click in the tree would.
    guardUnsaved(async () => {
      if (editing) leaveEditModeQuietly();
      selectedPath = path;
      // The request has reached the initialized target. From here a failed read
      // is a normal file error, not a jump that should retry in another tab.
      clearFileJump();
      const loaded = await loadFile(path);
      if (!loaded) return;
      void revealInTree(path);

      // After the document is in the editor, not before: CodeMirror has no
      // document to position within until loadFile has built the view.
      if (line && line > 1) {
        await tick();
        scrollToLine(line);
      }
    }, clearFileJump);
  }

  /**
   * Put a 1-based line at the top of the editor.
   *
   * Converted to a document offset because that is what CodeMirror positions
   * by; a line beyond the end is clamped rather than throwing.
   */
  /**
   * Open the find panel from outside the editor.
   *
   * The read view is not editable, and a non-editable CodeMirror does not take
   * focus — so Ctrl+F pressed while looking at a file never reached the search
   * keymap at all. Caught on the surrounding element instead and forwarded
   * here, which works in both views.
   */
  function handleBrowserKeydown(event: KeyboardEvent) {
    if (!(event.ctrlKey || event.metaKey) || event.key !== 'f') return;
    if (!view) return;
    event.preventDefault();
    openSearchPanel(view);
  }

  function scrollToLine(line: number) {
    if (!view) return;
    const target = Math.min(Math.max(line, 1), view.state.doc.lines);
    const at = view.state.doc.line(target).from;
    view.dispatch({ effects: EditorView.scrollIntoView(at, { y: 'start' }) });
  }

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
  let loadedWindowIdx = 0;
  /** Which session:tab the tree currently belongs to. */
  let loadedBrowseKey = '';
  let treeGeneration = 0;
  let fileGeneration = 0;
  let destroyed = false;
  let unregisterUnsavedGuard: (() => void) | null = null;

  // --- Editing --------------------------------------------------------------

  // The editor holds ONLY the text. Everything the editor cannot represent —
  // the BOM, the line-ending convention, whether the file ends in a newline —
  // lives in `openedFile.shape` and is handed straight back to the save
  // untouched. Nothing the user can do in the editor reaches it, which is
  // what makes an unmodified save byte-identical rather than merely usually
  // byte-identical.
  //
  // CodeMirror is pinned to an LF line separator for the same reason — see
  // codemirror.ts's lineSeparatorLF. Without it a mixed-ending file, which the
  // Go side passes through with its CRs embedded precisely so they can be
  // restored, would be silently normalised on open.
  let editing = false;
  let openedFile: session.EditableFile | null = null;
  // Absolute root the editable snapshot came from. A running tab can `cd`
  // without changing its session/window identity; saves must still target the
  // directory whose bytes were opened, never whatever cwd the pane has later.
  let openedRoot = '';
  let editText = '';
  // The text as it came from disk, for the modified check. Compared rather than
  // tracked with a dirty flag so undoing every change correctly clears it.
  let savedText = '';
  let saving = false;
  let saveError = '';
  let savedFlash = false;
  let savedFlashTimer: ReturnType<typeof setTimeout> | null = null;
  // "modified" or "deleted" while a conflict is waiting on the user; "" when
  // there is none. The user's text is never discarded to show it.
  let conflict: '' | 'modified' | 'deleted' = '';
  // A pending navigation held back by unsaved changes: the action to run once
  // the user confirms they are willing to lose them.
  let pendingLeave: (() => void) | null = null;
  let pendingLeaveCancel: (() => void) | null = null;

  $: modified = editing && editText !== savedText;
  // Tied to the file currently selected, not merely to whatever was last
  // opened for editing. Selecting a different file leaves `editing` and
  // `openedFile` describing the previous one for a moment; without this check
  // both views claimed to be showing, the edit view won, and its element —
  // gated on canEdit for the OLD file — never rendered. The result was a black
  // pane with nothing mounted into it.
  $: canEdit = !!openedFile && openedFile.editable && samePath(openedFile.path, selectedPath);

  /**
   * Whether two of this browser's paths name the same file.
   *
   * The Go side returns paths through filepath.Clean, while selectedPath is
   * whatever the tree handed over, so comparing them raw would call a file
   * different from itself over a leading "./" — and the editor would silently
   * refuse to open.
   */
  function samePath(a: string | null, b: string | null): boolean {
    if (!a || !b) return false;
    const norm = (p: string) => p.replace(/^\.\//, '').replace(/\/+/g, '/').replace(/\/$/, '');
    return norm(a) === norm(b);
  }

  // Soft wrap is OFF in both views, deliberately, and it is CodeMirror's default
  // — `EditorView.lineWrapping` is simply never added. Horizontal scrolling is
  // the honest trade for a code file, and it is what the textarea did.
  //
  // The line numbers come from CodeMirror's own `lineNumbers()` gutter, which
  // renders only the rows in view, so the 20 000-row cap the hand-built gutter
  // needed is gone: a 100 000-line file now costs the same DOM as a short one.

  // The total, for the footer. Read off the document rather than counted over
  // the string: CodeMirror already maintains it, and on a large file recounting
  // per keystroke was the expensive part.
  let editTotalLines = 0;

  // There is no render cap any more. CodeMirror draws only the lines in the
  // viewport, so the DOM cost no longer scales with the file, and the read view
  // shows the whole file rather than its first 3000 lines.
  //
  // The large-file gate below stays: the cost that remains is PARSING, and
  // handing a multi-megabyte minified bundle to a Lezer grammar still locks the
  // main thread. The gate is now about that, not about the DOM.
  //
  // Above this the file is not rendered until the user asks.
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
    unregisterUnsavedGuard = registerUnsavedGuard({
      isDirty: () => modified,
      requestDiscard: (continueAfterDiscard, cancelDiscard) => {
        guardUnsaved(() => {
          if (editing) leaveEditMode();
          continueAfterDiscard();
        }, cancelDiscard ?? null);
      },
    });
  });

  onDestroy(() => {
    destroyed = true;
    treeGeneration++;
    fileGeneration++;
    if (savedFlashTimer) clearTimeout(savedFlashTimer);
    // A CodeMirror view holds DOM listeners and a measure loop; leaving one
    // behind when the tab unmounts is a real leak.
    destroyView();
    window.removeEventListener('keydown', handleWindowKeydown, true);
    unregisterUnsavedGuard?.();
    unregisterUnsavedGuard = null;
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
    const expectedRoot = path === '' ? '' : rootAbsPath;
    if (path !== '' && !expectedRoot) return;

    loadingDirs = new Set(loadingDirs).add(path);
    if (path === '') rootLoading = true;
    try {
      const listing = await App.ListSessionDirectory(sessionId, path, get(selectedWindowIdx) ?? 0, expectedRoot);
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

  async function loadFile(path: string): Promise<boolean> {
    const sessionId = get(selectedSessionId);
    if (!sessionId) return false;
    const windowIdx = get(selectedWindowIdx) ?? 0;
    const targetKey = `${sessionId}:${windowIdx}`;
    const generation = ++fileGeneration;
    const expectedRoot = rootAbsPath;
    if (!expectedRoot) return false;
    fileLoading = true;
    fileError = '';
    selectedFile = null;
    try {
      const file = await App.ReadSessionDirectoryFile(sessionId, path, windowIdx, expectedRoot);
      if (destroyed || generation !== fileGeneration || targetKey !== browseKey || expectedRoot !== rootAbsPath) return false;
      selectedFile = file;
    } catch (e) {
      if (destroyed || generation !== fileGeneration || targetKey !== browseKey) return false;
      // The Go side reports permission problems and missing files verbatim;
      // showing that beats a generic "could not load".
      fileError = String(e);
    }
    if (!destroyed && generation === fileGeneration && targetKey === browseKey) fileLoading = false;
    return !destroyed && generation === fileGeneration && targetKey === browseKey && !!selectedFile;
  }

  // --- Edit mode ------------------------------------------------------------

  async function enterEditMode() {
    if (!selectedPath) return;
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;
    const generation = fileGeneration;
    const targetKey = browseKey;
    const expectedRoot = rootAbsPath;
    if (!expectedRoot) return;
    saveError = '';
    conflict = '';
    try {
      // Opened through its own call rather than reusing the browse result: the
      // version that guards the save has to describe the exact bytes the text
      // was decoded from, and a version taken from a separate read could
      // already be stale.
      const file = await App.OpenSessionFileForEdit(sessionId, selectedPath, get(selectedWindowIdx) ?? 0, expectedRoot) as session.EditableFile & { root?: string };
      if (destroyed || generation !== fileGeneration || targetKey !== browseKey || expectedRoot !== rootAbsPath) return;
      if (!file.root) throw new Error('editable file response did not include its canonical root');
      openedFile = file;
      openedRoot = file.root;
      editText = file.text;
      savedText = file.text;
      editing = true;
      // Forces the view to rebuild around the new buffer even when the path and
      // mode are unchanged — which is exactly the reload-from-disk case.
      editGeneration++;
    } catch (e) {
      if (destroyed || generation !== fileGeneration) return;
      saveError = String(e);
    }
  }

  /** Leave edit mode, discarding the buffer. Callers check `modified` first. */
  function leaveEditMode() {
    editing = false;
    openedFile = null;
    openedRoot = '';
    editText = '';
    savedText = '';
    saveError = '';
    conflict = '';
    // The read view is showing a snapshot from before the edits; re-read it so
    // it does not contradict what was just saved.
    if (selectedPath) void loadFile(selectedPath);
  }

  /** Run `action`, first asking about unsaved changes if there are any. */
  function guardUnsaved(action: () => void, onCancel: (() => void) | null = null) {
    if (modified) {
      pendingLeave = action;
      pendingLeaveCancel = onCancel;
      return;
    }
    action();
  }

  function confirmDiscard() {
    const action = pendingLeave;
    pendingLeave = null;
    pendingLeaveCancel = null;
    if (action) action();
  }

  function cancelDiscard() {
    const cancel = pendingLeaveCancel;
    pendingLeave = null;
    pendingLeaveCancel = null;
    if (cancel) cancel();
  }

  async function save(overwrite = false) {
    if (!openedFile || !openedRoot || !selectedPath || saving) return;
    const sessionId = loadedSessionId;
    if (!sessionId) return;
    const windowIdx = loadedWindowIdx;
    const targetKey = loadedBrowseKey;
    const path = selectedPath;
    saving = true;
    saveError = '';
    // The text is captured before the await: the user keeps typing during the
    // round trip, and the version returned by the save describes THESE bytes.
    const submitted = editText;
    try {
      const result = await App.SaveSessionFileEdit(
        sessionId,
        path,
        submitted,
        openedFile.shape,
        openedFile.version,
        overwrite,
        windowIdx,
        openedRoot,
      );
      if (destroyed || targetKey !== loadedBrowseKey || path !== selectedPath) return;
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
      if (destroyed || targetKey !== loadedBrowseKey || path !== selectedPath) return;
      saveError = String(e);
    } finally {
      if (!destroyed && targetKey === loadedBrowseKey) saving = false;
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
    view?.focus();
  }

  // --- CodeMirror ------------------------------------------------------------

  // Where the caret is, for the editor's footer. Read from state.selection on
  // every update rather than recomputed over the text: CodeMirror already knows
  // which line an offset falls on, so this is O(log n) instead of a scan.
  let caretLine = 1;
  let caretColumn = 1;
  /** Non-zero while a selection is active, so the footer can report its size. */
  let selectionLength = 0;

  /** The live editor, for whichever view is mounted. Null when neither is. */
  let view: EditorView | null = null;
  /** The element the view attaches to, bound by the markup. */
  // Two hosts, not one shared reference. With a single bind:this on both the
  // read and the edit element, switching modes had Svelte null it as the old
  // element unmounted — and that could land AFTER the new element bound
  // itself, leaving the reference null and no editor mounted at all.
  let readHost: HTMLDivElement | null = null;
  let editHost: HTMLDivElement | null = null;
  $: editorHost = showEditEditor ? editHost : readHost;
  /** Swapped when a grammar finishes loading, so no rebuild is needed. */
  let langCompartment = new Compartment();
  /**
   * What the mounted view was built for. Compared against the wanted state to
   * decide whether to recreate — see the reactive block below.
   */
  let mountedFor = '';

  /**
   * Ctrl+S and Escape, bound inside CodeMirror rather than on the container.
   *
   * CodeMirror stops propagation of keys it handles, so a listener on an
   * ancestor would see neither reliably. Bound highest-precedence so the
   * editor's own bindings cannot claim them first.
   */
  const appKeymap = keymap.of([
    {
      key: 'Mod-s',
      run: () => {
        void save();
        return true;
      },
    },
    {
      key: 'Escape',
      run: (target) => {
        // The find panel takes Escape first. appKeymap is pushed ahead of the
        // search keymap so Ctrl+S wins, which means this binding would
        // otherwise close the whole editor while the user was only trying to
        // dismiss the search box.
        if (target.dom.querySelector('.cm-search')) return false;
        guardUnsaved(leaveEditMode);
        return true;
      },
    },
  ]);

  /**
   * Mirror the document and the selection back into the Svelte state.
   *
   * `editText` is the single source of truth for `modified` and for what a save
   * submits, so it has to track every document change — including undo, paste
   * and drag, which no keydown handler would see.
   */
  const syncFromView = EditorView.updateListener.of((update) => {
    if (update.docChanged) {
      editText = update.state.doc.toString();
      editTotalLines = update.state.doc.lines;
    }
    if (update.docChanged || update.selectionSet) {
      const range = update.state.selection.main;
      const line = update.state.doc.lineAt(range.head);
      caretLine = line.number;
      // Columns are 1-based and counted in characters, matching what the
      // textarea's footer reported.
      caretColumn = range.head - line.from + 1;
      selectionLength = Math.abs(range.to - range.from);
    }
    // Tracked continuously rather than read when a switch happens: by the time
    // a tab or session change reaches this component, Svelte has already torn
    // the host element down and destroyed the view, so there was nothing left
    // to ask — every save recorded zero.
    // viewportChanged, not geometryChanged: the latter fires when the editor
    // is resized, not when it is scrolled, so plain scrolling recorded nothing
    // and returning landed on whatever the last click had saved.
    if (update.viewportChanged || update.geometryChanged ||
        update.docChanged || update.selectionSet) {
      recordPlaceFromUpdate(update.view);
    }
  });

  /** Latest known spot in the file on screen, kept current by the listener. */
  function recordPlaceFromUpdate(v: EditorView) {
    if (!selectedPath) return;
    const offset = showEditEditor
      ? v.state.selection.main.head
      : firstVisibleOffset(v);
    if (offset > 0) placeByFile.set(placeKey(selectedPath), offset);
  }

  /** Tear the editor down. Safe to call when nothing is mounted. */
  function destroyView() {
    view?.destroy();
    view = null;
    mountedFor = '';
  }

  /**
   * Build the editor for the current file and mode.
   *
   * `readOnly` is the ONLY difference between the two views: same theme, same
   * gutter, same grammar, so reading and editing are pixel-identical rather
   * than two renderers kept in sync by hand.
   */
  /**
   * Where the user is in the current file, as a document offset.
   *
   * The caret in the edit view, but the TOP OF THE VIEWPORT when reading —
   * a read-only view never moves its caret, so it sits at zero however far
   * down the file you have scrolled, and remembering it recorded nothing.
   */
  /**
   * Offset of the first line fully in view.
   *
   * Probed a little way below the top edge, not at it: a pane scrolled to a
   * fraction of a line has its topmost row partly cut off, and measuring at
   * the very edge returns THAT row. Restoring then aligned it flush, leaving
   * the file a line higher than it was. Sampling past the partial row picks
   * the first whole one, which is what the restore puts back.
   */
  function firstVisibleOffset(v: EditorView): number {
    const top = v.scrollDOM.getBoundingClientRect().top;
    // Roughly one line down — enough to clear a partial row at any font size
    // this editor uses.
    const probe = top + v.defaultLineHeight;
    return v.posAtCoords({ x: 0, y: probe }, false) ?? 0;
  }

  function currentPlaceOffset(): number {
    if (!view) return 0;
    if (showEditEditor) return view.state.selection.main.head;
    // The first line CodeMirror considers visible. Asked of the view rather
    // than measured off scrollDOM.scrollTop, which read zero however far the
    // pane had been scrolled — the scroller CodeMirror owns is not the element
    // the browser was actually scrolling.
    return firstVisibleOffset(view);
  }

  /**
   * Put the caret back where this session was left, if anything is pending.
   *
   * Clamped, because the file may have shrunk while away, and scrolled to the
   * middle — a caret outside the viewport is no more use than none. Consumes
   * the pending offset, so it applies once and not to the next file opened.
   */
  function applyRestoreOffset() {
    if (!view || restoreOffset <= 0) return;
    const at = Math.min(restoreOffset, view.state.doc.length);
    restoreOffset = 0;
    if (showEditEditor) {
      view.dispatch({
        selection: { anchor: at },
        effects: EditorView.scrollIntoView(at, { y: 'center' }),
      });
    } else {
      // Scroll only. The read view is read-only, where moving the caret is
      // both meaningless and refused, and the remembered offset is the top of
      // the viewport rather than a cursor — so it belongs at the top, not
      // centred.
      view.dispatch({ effects: EditorView.scrollIntoView(at, { y: 'start' }) });
    }
  }

  function createView(host: HTMLDivElement, doc: string, readOnly: boolean, path: string) {
    // Mounts unconditionally. An earlier attempt deferred to the next frame
    // when the host measured 0x0, guarded by `viewKey !== mountedFor` — but
    // that state can settle before the frame runs, so the guard rejected the
    // very mount it was waiting for and the editor never appeared at all.
    // CodeMirror handles being mounted into a box that gains its size later,
    // so there is nothing to wait for.
    destroyView();
    langCompartment = new Compartment();

    // A grammar already fetched is applied synchronously, so reopening a file of
    // a known type never flashes uncoloured.
    const ready = cachedLanguage(path);

    const extensions = [
      ...baseExtensions(),
      langCompartment.of(ready ? [ready] : []),
      syncFromView,
      // viewportChanged only fires when CodeMirror renders new rows, so a
      // short scroll within the rendered range recorded nothing. The DOM
      // event fires for every scroll.
      EditorView.domEventHandlers({
        scroll: (_e, v) => {
          recordPlaceFromUpdate(v);
          return false;
        },
      }),
      EditorState.readOnly.of(readOnly),
      EditorView.editable.of(!readOnly),
      // Find, in both the read and edit views: looking for something in a file
      // is at least as common as changing it, and the read view is where most
      // files are opened from the diff.
      //
      // highlightSelectionMatches shows the other occurrences of whatever is
      // selected, which is what makes the panel's own count meaningful.
      search({ top: true }),
      highlightSelectionMatches(),
      keymap.of(searchKeymap),
    ];
    if (!readOnly) {
      // Order matters: appKeymap first so Ctrl+S and Escape win, then the tab
      // override, then the defaults — whose Tab binding would otherwise reindent
      // rather than insert a character.
      extensions.push(appKeymap, insertTabKeymap, history(), keymap.of([...historyKeymap, ...defaultKeymap]));
    }

    view = new EditorView({ doc, extensions, parent: host });
    mountedFor = viewKey;

    // The edit view's host is created by an {#if}, so bind:this fires before
    // the browser has laid it out and CodeMirror sizes itself against a 0x0
    // box. Asking it to measure again once layout has settled is CodeMirror's
    // own remedy for exactly this, and costs nothing when the box was already
    // correct — which it is for the read view.
    const mounted = view;
    requestAnimationFrame(() => {
      if (mounted === view) mounted.requestMeasure();
    });

    // Where this file was last left, if it has been open before. Selecting a
    // NEW file has no entry, so it starts at the top — which is what picking a
    // file from the tree should do.
    if (restoreOffset <= 0) restoreOffset = placeByFile.get(placeKey(path)) || 0;
    applyRestoreOffset();

    if (!readOnly) {
      editTotalLines = view.state.doc.lines;
      caretLine = 1;
      caretColumn = 1;
      selectionLength = 0;
      view.focus();
    }

    if (!ready) void applyLanguage(path);
  }

  /**
   * Fetch the grammar for `path` and swap it in.
   *
   * The view is checked again after the await: the user can switch files while
   * a chunk is in flight, and applying a stale grammar would colour the new file
   * as the old one's language.
   */
  async function applyLanguage(path: string) {
    const wanted = mountedFor;
    const support = await loadLanguage(path);
    if (!support || !view || destroyed || mountedFor !== wanted) return;
    view.dispatch({ effects: langCompartment.reconfigure([support]) });
  }

  /**
   * Identity of the editor that SHOULD be mounted right now.
   *
   * Recreating is keyed off this string rather than off the document, because
   * the document changes on every keystroke and rebuilding then would destroy
   * the view under the user's cursor. It changes only when the file, the mode or
   * the reason to show an editor at all changes.
   *
   * `editGeneration` is what makes a reload-from-disk recreate the view: the
   * path and mode are unchanged, but the buffer must be replaced wholesale.
   */
  let editGeneration = 0;
  $: showReadEditor = shouldRender && !editing && !!selectedFile && !selectedFile.binary;
  $: showEditEditor = editing && canEdit;
  // The tab index is part of the identity even though the tabs of one session
  // share a directory. Switching tabs tears the host element down and back up,
  // and with a key built only from the path the read view's key was unchanged
  // across that — so nothing remounted into the new element and the pane stayed
  // black. The edit view happened to survive because editGeneration made its
  // key differ anyway.
  $: viewKey = showEditEditor
    ? `edit:${editGeneration}:${selectedPath || ''}`
    : showReadEditor
      ? `read:${$selectedWindowIdx ?? 0}:${selectedPath || ''}`
      : '';

  // Mount, remount or tear down to match `viewKey`.
  //
  // mountedFor is assigned INSIDE this block (via createView/destroyView):
  // Svelte 3 orders reactive statements by dependency rather than by source
  // position, so a tracking variable updated in a separate statement could be
  // written before this one ever ran, and the editor would not rebuild.
  $: if (editorHost && viewKey !== mountedFor) {
    if (!viewKey) {
      destroyView();
    } else {
      // The read view shows what was loaded for browsing; the edit view shows
      // the separately-opened buffer whose version guards the save.
      createView(editorHost, showEditEditor ? editText : fileContent, !showEditEditor, selectedPath || '');
    }
  }

  // The read view's text arrives AFTER the view is mounted — the file is
  // fetched asynchronously, so viewKey settles on the new path while
  // fileContent is still the old file's (or empty). Without this the pane
  // showed nothing until something else forced a remount.
  //
  // A document swap rather than a rebuild: rebuilding on every content change
  // would also fire on each keystroke in the edit view and drop the caret. The
  // guard reads the view's own text, so it is a no-op once they agree.
  $: if (view && !showEditEditor && mountedFor === viewKey &&
         view.state.doc.toString() !== fileContent) {
    view.dispatch({
      changes: { from: 0, to: view.state.doc.length, insert: fileContent },
    });
    // The caret is restored HERE rather than at mount: the read view's text
    // arrives after the view exists, so at mount time the document is still
    // empty and any offset would clamp to zero.
    applyRestoreOffset();
  }

  // The host element goes away with the {#if} that owns it, and a view left
  // pointing at a detached node is a leak — this browser opens many files in a
  // session.
  //
  // destroyView clears mountedFor, which is what lets the block above mount
  // again into the replacement element. Without that the two would agree while
  // no view existed, and the pane stayed empty until something else changed the
  // key — the black pane seen after switching tabs.
  $: if (!editorHost && view) destroyView();

  function resetForSession() {
    treeGeneration++;
    fileGeneration++;
    saving = false;
    fileLoading = false;
    rootLoading = false;
    savedFlash = false;
    if (savedFlashTimer) {
      clearTimeout(savedFlashTimer);
      savedFlashTimer = null;
    }
    editing = false;
    openedFile = null;
    openedRoot = '';
    editText = '';
    savedText = '';
    saveError = '';
    conflict = '';
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
  // Which file was open in each session tab, and where the caret was in it, so
  // switching away and back returns you to the spot rather than to an empty
  // pane or the top of the file. In memory only: it records where you are in
  // this sitting, not a preference worth persisting.
  const lastFileByTab = new Map<string, string>();

  /**
   * Where the user was in each file, keyed by session, tab, and path.
   *
   * Per FILE rather than per session: switching tabs, sessions or away to the
   * terminal and back should land where you were, but picking a different file
   * should start at its top — and it does, because a file never opened has no
   * entry here.
   */
  const placeByFile = new Map<string, number>();
  /** Set just before a mount that should restore rather than start at the top. */
  let restoreOffset = 0;

  function placeKey(path: string | null): string {
    return `${loadedBrowseKey}|${path || ''}`;
  }

  /** Record where we are, so the next mount of this same file can return to it. */
  function rememberPlace() {
    // Usually a no-op: the update listener has already recorded the spot. Kept
    // for the paths that change selectedPath before any update fires.
    if (!view || !selectedPath) return;
    const off = currentPlaceOffset();
    if (off > 0) placeByFile.set(placeKey(selectedPath), off);
  }

  // Keyed on the TAB, not the session. A tab can be opened in a directory of
  // its own, so switching between tabs of one session changes which tree
  // belongs on screen — and this block used to compare session ids alone, which
  // are identical across those tabs.
  // Project identity is part of the tab. Project A and B may both contain
  // session "1" / window 0; sharing their remembered path would open A's last
  // file automatically in B and reuse its scroll position.
  $: browseKey = `${$activeProjectId}:${$selectedSessionId ?? ''}:${$selectedWindowIdx ?? 0}`;

  $: if (active && browseKey !== loadedBrowseKey) {
    requestBrowseTarget(browseKey, $selectedSessionId, $selectedWindowIdx ?? 0);
  }

  function requestBrowseTarget(targetKey: string, sessionId: string | null, windowIdx: number) {
    const previousSessionId = loadedSessionId;
    const previousWindowIdx = loadedWindowIdx;
    if (modified) {
      guardUnsaved(
        () => applyBrowseTarget(targetKey, sessionId, windowIdx),
        () => {
          if (!previousSessionId) return;
          selectSession(previousSessionId);
          selectWindow(previousWindowIdx);
        },
      );
      return;
    }
    applyBrowseTarget(targetKey, sessionId, windowIdx);
  }

  function applyBrowseTarget(targetKey: string, sessionId: string | null, windowIdx: number) {
    rememberPlace();
    if (loadedBrowseKey && selectedPath) lastFileByTab.set(loadedBrowseKey, selectedPath);
    const returningTo = sessionId ? lastFileByTab.get(targetKey) : undefined;

    loadedSessionId = sessionId;
    loadedWindowIdx = windowIdx;
    loadedBrowseKey = targetKey;
    pendingLeave = null;
    pendingLeaveCancel = null;
    resetForSession();
    if (!sessionId) return;
    void loadDir('').then(() => {
      if (returningTo && !selectedPath && loadedBrowseKey === targetKey) selectFile(returningTo);
    });
  }

  /** Honour jumps only after the target tab's tree owns the component. */
  $: if (active && loadedBrowseKey === browseKey && $pendingFileJump) {
    const jump = $pendingFileJump;
    void openRequestedFile(jump.path, jump.line);
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
    const cameBack = !wasActive && active;
    wasActive = active;
    // Leaving for the terminal is a switch like any other: record the spot so
    // coming back does not start at the top.
    if (leftTheView) rememberPlace();

    if (leftTheView && editing && editText !== savedText) {
      // Only the prompt is raised; nothing is discarded. Confirming leaves edit
      // mode, cancelling simply leaves the buffer where it is for when the user
      // comes back.
      pendingLeave = leaveEditMode;
    }
    // Returning to the browser with an editor still open — from another tab,
    // say — showed a black pane: the edit host is behind {#if canEdit}, and
    // once the file behind it has moved on there is nothing to mount into.
    // An unmodified buffer has nothing to lose, so drop it and show the file.
    // Coming back used to force a way out of edit mode, because a stale editor
    // rendered as a black pane. That is now handled at the root — canEdit
    // requires openedFile to be the file actually selected — and dropping the
    // buffer here would have thrown away edits the user came back to finish.
    void cameBack;
  }

  function refresh() {
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;
    // A refresh re-reads the open file, which would silently replace the
    // buffer, so it is guarded like any other way of losing the edits.
    guardUnsaved(() => { void refreshCurrentTree(sessionId); });
  }

  async function refreshCurrentTree(sessionId: string) {
    if (sessionId !== get(selectedSessionId) || loadedBrowseKey !== browseKey) return;
    const targetKey = loadedBrowseKey;
    try {
      if (editing) leaveEditModeQuietly();
      // The quick-open index is a snapshot of the same tree, so a refresh has
      // to drop it too or the picker keeps offering files that are gone.
      await App.InvalidateSessionFileIndex(sessionId);
      if (destroyed || targetKey !== loadedBrowseKey || sessionId !== get(selectedSessionId)) return;
      // Re-fetch everything the user had open, so a refresh shows the current
      // state of the tree rather than silently keeping stale listings.
      const open = Array.from(expanded);
      treeGeneration++;
      const generation = treeGeneration;
      dirs = {};
      dirErrors = {};
      loadingDirs = new Set<string>();
      rootAbsPath = '';
      // Bootstrap the canonical root first. Subdirectory calls are fail-closed
      // against that root; firing them beside the root request would send the
      // previous cwd and could combine old subtrees with the new root listing.
      await loadDir('', true);
      if (destroyed || generation !== treeGeneration || targetKey !== loadedBrowseKey) return;
      if (!rootAbsPath) return;
      for (const path of open) {
        if (path) void loadDir(path, true);
      }
      if (selectedPath) void loadFile(selectedPath);
    } catch (e) {
      if (!destroyed && targetKey === loadedBrowseKey) rootError = String(e);
    }
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
      // The file being left keeps its spot, so coming back to it later lands
      // where you were rather than at the top.
      rememberPlace();
      if (editing) leaveEditModeQuietly();
      selectedPath = path;
      void loadFile(path);
    });
  }

  /** Leave edit mode without re-reading — the caller is loading a file anyway. */
  function leaveEditModeQuietly() {
    editing = false;
    openedFile = null;
    openedRoot = '';
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

  $: emptyFile = !!selectedFile && !selectedFile.binary && fileContent === '';

  // Syntax highlighting is CodeMirror's now — the grammar is fetched lazily by
  // createView and swapped in when it arrives, so there is nothing to compute
  // here. The read view renders only the lines on screen, so the whole file is
  // handed to it rather than a truncated slice: scrolling to the end of a long
  // file now shows the end of it.

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

<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
<div class="browser-container" role="region" tabindex="-1" on:keydown={handleBrowserKeydown}>
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
            <!-- CodeMirror mounts itself into this element; the gutter and the
                 line numbers are its own, so there is nothing to keep aligned
                 by hand any more. -->
            <div
              class="editor"
              bind:this={editHost}
              role="textbox"
              tabindex="-1"
              aria-multiline="true"
              aria-label={$t('browser.editorLabel')}
            ></div>
            <div class="editor-caret">
              <span>{$t('browser.caretPosition', { line: caretLine, col: caretColumn })}</span>
              {#if selectionLength > 0}
                <span class="caret-selection">{$t('browser.caretSelection', { n: selectionLength })}</span>
              {/if}
              <span class="caret-total">{$t('browser.caretTotal', { n: editTotalLines })}</span>
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
                {$t('browser.showAnywayAll')}
              </button>
            </div>
          {:else}
            {#if selectedFile.truncated}
              <div class="file-notice">{$t('browser.fileTruncated', { size: formatSize(selectedFile.size) })}</div>
            {/if}
            <!-- The same CodeMirror as the edit view, read-only, so the two are
                 identical rather than merely similar. -->
            <div class="editor read" bind:this={readHost}></div>
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
  windowIdx={$selectedWindowIdx ?? 0}
  root={rootAbsPath}
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
     have a comfortable grab area without shifting the layout.
     flex-basis rather than width, because on a flex item the basis is what
     decides the size — the width here was being overridden, leaving a 1px box
     that only min-width stretched. 11px matches the other splitters. */
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

  /* The host CodeMirror mounts into. It owns its own gutter and scroller, so
     this only has to give it a box to fill — the type is set in the CM6 theme
     (codemirror.ts) so the read and edit views cannot drift apart. */
  .editor {
    flex: 1;
    min-height: 0;
    min-width: 0;
    overflow: hidden;
    /* A flex child in its own right, so the CodeMirror view it hosts can take
       the height. The theme asks for height:100%, which resolves against this
       box — on a plain `flex: 1` div with no resolved height of its own that
       percentage computes to zero and the editor renders as an empty black
       rectangle. */
    display: flex;
    flex-direction: column;
  }

  /* CodeMirror mounts a child div rather than replacing the host, so the host's
     height has to be passed down explicitly. :global because CM6 creates these
     elements itself and Svelte's scoping never sees them. */
  .editor :global(.cm-editor) {
    flex: 1;
    min-height: 0;
  }

  /* Sits below the editor, not inside it: .editor is a horizontal flex row of
     gutter and text, so a child would land beside the textarea. */
  .editor-caret {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-shrink: 0;
    padding: 4px 12px;
    border-top: 1px solid rgba(255, 255, 255, 0.05);
    background: rgba(0, 0, 0, 0.2);
    font-size: 11px;
    font-variant-numeric: tabular-nums;
    color: #6b7280;
  }

  .caret-total {
    margin-left: auto;
  }

  .caret-selection {
    color: var(--accent-light);
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

  /* A column so the notice banner can sit above a CodeMirror that fills the
     rest. Scrolling belongs to CodeMirror's own scroller, not to this box —
     two nested scrollers would fight. */
  .file-content {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    min-width: 0;
    min-height: 0;
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


  /* CodeMirror's own scroller, which also reaches the window edge. :global
     because the element is created by the editor, not by this template. */



</style>
