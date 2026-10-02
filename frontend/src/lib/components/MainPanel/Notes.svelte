<script lang="ts">
  import { onMount, onDestroy, createEventDispatcher, tick } from 'svelte';
  import { diffTakesFocusFrom } from '../../utils/diffFocus';
  import { selectedSessionId, selectedWindowIdx, sessions } from '../../stores/sessions';
  import { notePresence } from '../../utils/noteScope';
  import { get } from 'svelte/store';
  import * as App from '../../../../wailsjs/go/main/App';
  import { createFieldDictation } from '../../utils/dictationField';
  import { t } from '../../i18n';
  import { registerUnsavedGuard } from '../../stores/unsavedChanges';
  import ConfirmDialog from '../Dialogs/ConfirmDialog.svelte';
  import { activeProjectId } from '../../stores/projects';
  import { settings } from '../../stores/settings';
  import { pendingNoteJump, clearNoteJump, type NoteJump } from '../../stores/noteJump';
  import { PROJECT_TASKS_SCOPE } from '../../utils/projectScope';
  import { portal } from '../../utils/portal';
  import { menuPosition } from '../../utils/menuPosition';
  import { claimMenu, releaseMenu } from '../../utils/openMenu';
  import { matchesShortcut, effectiveBindings, formatBinding } from '../../stores/shortcuts';
  import {
    type NotePage, editablePages, pagesKey, notePagesText, resolveActivePage, pageIndex,
    setPageText, renamePage, addPage, deletePage, movePage, dropIndex, stepPage, pageLabel,
  } from '../../utils/notePages';
  import { notesColorVars, notesColorStyle } from '../../utils/notesColors';

  export let active = false;

  // The colours chosen in Settings, as variables on the container. Every
  // setting is passed in: a call that reads the store itself would never run
  // again. Empty for the default look, which is then the stylesheet's own.
  $: notesColors = notesColorVars($settings.notesBackground, $settings.notesBackgroundColor,
                                  $settings.notesText, $settings.notesTextColor);
  $: notesColorsChosen = Object.keys(notesColors).length > 0;

  /**
   * The project's own note instead of a session's or a tab's. It is addressed
   * as one more target — session PROJECT_TASKS_SCOPE — so drafts, the save
   * queue and the unsaved-changes guard treat it like any other note; only
   * reading and writing go to the project's endpoints. Fixed for the life of
   * the view.
   */
  export let project = false;
  const isProject = project;
  /**
   * Whether a note opened from the global search shows here. The panel's
   * view takes the session notes; of the project's views, only the one in
   * the project window — where the search opens the project's note — so the
   * dashboard's copy behind it does not take the request first.
   */
  export let followsSearch = true;

  // Which note is open: this tab's, or the session's — the one every tab of
  // the session shares. The session's is addressed as its own target, window
  // SESSION_NOTES, so drafts, save queues and the unsaved-changes guard keep
  // working unchanged: to them it is just another note.
  const SESSION_NOTES = -1;
  const SCOPE_KEY = 'asmgr.notesScope';
  type NotesScope = 'tab' | 'session';
  // Remembered across openings and restarts, per viewer: which of the two a
  // person reaches for is a habit, not a property of any session. Unless the
  // settings fix a default, which then applies each time the view is opened;
  // the switch still changes it for as long as the view stays open.
  let scope: NotesScope = readScope();

  function applyDefaultScope() {
    const fixed = get(settings)?.notesDefaultScope;
    if (fixed === 'tab' || fixed === 'session') scope = fixed;
  }

  function readScope(): NotesScope {
    try {
      return localStorage.getItem(SCOPE_KEY) === 'session' ? 'session' : 'tab';
    } catch {
      return 'tab';
    }
  }

  function setScope(next: NotesScope) {
    if (next === scope) return;
    scope = next;
    try {
      localStorage.setItem(SCOPE_KEY, next);
    } catch {
      // Storage unavailable: the choice holds until the view is closed.
    }
  }

  // Which of the two notes have something in them, for the dots on the
  // switch: seeing an empty note is only worth a click if the other is not
  // empty too. Recomputed as the text, the target and the session list change.
  $: notesSession = $sessions.find(s => s.id === $selectedSessionId);
  // Any page with text counts: the texts are joined the way the session list
  // carries them, so the open note and the stored one are read alike.
  $: presence = notePresence({
    open: scope,
    openText: notePagesText(pages),
    otherDraft: draftText(draftsByTarget.get(noteKey(
      $activeProjectId, $selectedSessionId ?? '',
      scope === 'tab' ? SESSION_NOTES : $selectedWindowIdx,
    ))),
    storedTab: (notesSession?.followedWindows?.find(w => w.index === $selectedWindowIdx)?.notes
      ?? notesSession?.mainTabNotes) || '',
    storedSession: notesSession?.notes || '',
  });

  function targetWindowIdx(): number {
    return isProject || scope === 'session' ? SESSION_NOTES : get(selectedWindowIdx);
  }

  function targetSessionId(): string | null {
    return isProject ? PROJECT_TASKS_SCOPE : get(selectedSessionId);
  }

  const dispatch = createEventDispatcher();

  /**
   * A note is a list of titled pages, and it is saved whole: drafts, the save
   * queue and the unsaved-changes guard all deal in a note's full list of
   * pages, so switching from page A to page B is not a change of target and
   * cannot lose what was typed on A. `notes` is the open page's text — what
   * the textarea, find, undo and dictation work on — and is written back into
   * `pages` on every edit.
   */
  let pages: NotePage[] = editablePages([]);
  let activePageId = pages[0].id;
  let notes = '';
  let lastSessionId: string | null = null;
  let lastWindowIdx: number = 0;
  let lastProjectId = '';
  let saveTimeout: ReturnType<typeof setTimeout> | null = null;
  let loadGeneration = 0;
  let saving = false;
  /** The note as last saved (or loaded), to tell an unsaved change by. */
  let savedPages: NotePage[] = pages;
  let textareaEl: HTMLTextAreaElement;
  const saveQueues = new Map<string, Promise<void>>();
  type NoteDraft = {
    pages: NotePage[];
    activePageId: string;
    saved: NotePage[];
    saveError: string;
    loadError: string;
  };
  const draftsByTarget = new Map<string, NoteDraft>();
  let savesInFlight = 0;
  let activationGeneration = 0;
  let loadingNotes = false;
  let saveError = '';
  let loadError = '';
  let unregisterUnsavedGuard: (() => void) | null = null;
  let pendingDiscard: (() => void) | null = null;
  let pendingDiscardCancel: (() => void) | null = null;

  function draftText(draft: NoteDraft | undefined): string | undefined {
    return draft && notePagesText(draft.pages);
  }

  function draftIsDirty(draft: NoteDraft): boolean {
    return pagesKey(draft.pages) !== pagesKey(draft.saved) || !!draft.saveError;
  }

  /** Whether the open note differs from what was last saved. */
  function openNoteChanged(): boolean {
    return pagesKey(pages) !== pagesKey(savedPages);
  }

  function hasUnsavedDrafts(): boolean {
    return [...draftsByTarget.values()].some(draftIsDirty) || openNoteChanged() || !!saveError;
  }

  function unsavedRevision(): string {
    return JSON.stringify([
      pagesKey(pages), pagesKey(savedPages), saveError,
      [...draftsByTarget.entries()].map(([key, draft]) =>
        [key, pagesKey(draft.pages), pagesKey(draft.saved), draft.saveError]),
    ]);
  }

  async function confirmDiscardNotes() {
    const continuation = pendingDiscard;
    pendingDiscard = null;
    pendingDiscardCancel = null;
    // A queued write belongs to the current backend project. Stop a debounced
    // write and let already-started writes settle before a destructive action
    // (especially project switching) can change that backend identity.
    if (saveTimeout) {
      clearTimeout(saveTimeout);
      saveTimeout = null;
    }
    await Promise.all([...saveQueues.values()].map((save) => save.catch(() => undefined)));
    for (const [key, draft] of draftsByTarget) {
      draftsByTarget.set(key, {
        ...draft,
        pages: draft.saved,
        activePageId: resolveActivePage(draft.saved, draft.activePageId),
        saveError: '',
      });
    }
    pages = savedPages;
    activePageId = resolveActivePage(pages, activePageId);
    notes = activeText();
    saveError = '';
    resetHistory();
    if (continuation) continuation();
  }

  function cancelDiscardNotes() {
    const cancel = pendingDiscardCancel;
    pendingDiscard = null;
    pendingDiscardCancel = null;
    if (cancel) cancel();
  }

  function noteKey(projectId: string, sessionId: string, windowIdx: number): string {
    return `${projectId}:${sessionId}:${windowIdx}`;
  }

  function currentNoteKey(): string | null {
    return lastSessionId ? noteKey(lastProjectId, lastSessionId, lastWindowIdx) : null;
  }

  function activeText(): string {
    return pages[pageIndex(pages, activePageId)]?.text ?? '';
  }

  function rememberCurrentDraft() {
    const key = currentNoteKey();
    if (!key) return;
    draftsByTarget.set(key, {
      pages,
      activePageId,
      saved: savedPages,
      saveError,
      loadError,
    });
  }

  function showDraft(draft: NoteDraft) {
    pages = draft.pages;
    activePageId = resolveActivePage(pages, draft.activePageId);
    notes = activeText();
    savedPages = draft.saved;
    saveError = draft.saveError;
    loadError = draft.loadError;
  }

  /**
   * Which page each note was last open on, per viewer and across restarts, so
   * coming back to a note opens the page left — like the scope above, a
   * matter of where this person was, not a property of the note.
   */
  const ACTIVE_PAGE_KEY = 'asmgr.notesActivePage';
  const ACTIVE_PAGE_LIMIT = 300;

  function readActivePages(): Record<string, string> {
    try {
      const parsed = JSON.parse(localStorage.getItem(ACTIVE_PAGE_KEY) || '{}');
      return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {};
    } catch {
      return {};
    }
  }

  function rememberedActivePage(key: string): string | undefined {
    const id = readActivePages()[key];
    return typeof id === 'string' ? id : undefined;
  }

  function rememberActivePage(key: string | null, id: string) {
    if (!key) return;
    try {
      const all = readActivePages();
      delete all[key];
      all[key] = id;
      const keys = Object.keys(all);
      // Oldest first, as inserted: a note not opened for a long time
      // forgets its page before one opened today.
      for (const old of keys.slice(0, Math.max(0, keys.length - ACTIVE_PAGE_LIMIT))) delete all[old];
      localStorage.setItem(ACTIVE_PAGE_KEY, JSON.stringify(all));
    } catch {
      // Storage unavailable: the page holds until the view is closed.
    }
  }

  /**
   * Find within the note.
   *
   * A textarea cannot highlight a range of its own text — the browser gives no
   * way to paint inside it — so a match is shown by selecting it and scrolling
   * it into view. That is what every plain-text find does here, and it has the
   * advantage that the match is then ready to be typed over.
   */
  let showFind = false;
  let findQuery = '';
  let findInputEl: HTMLInputElement | undefined;

  // Positions of every match, recomputed as the query or the text changes so a
  // count never describes a note that has since been edited.
  $: matches = (() => {
    if (!showFind || !findQuery) return [] as number[];
    const haystack = notes.toLowerCase();
    const needle = findQuery.toLowerCase();
    const found: number[] = [];
    let at = haystack.indexOf(needle);
    while (at !== -1) {
      found.push(at);
      at = haystack.indexOf(needle, at + needle.length);
    }
    return found;
  })();

  let matchIndex = 0;
  // A shorter list must not leave the cursor pointing past its end.
  $: if (matchIndex >= matches.length) matchIndex = 0;

  function goToMatch(index: number) {
    if (!matches.length || !textareaEl) return;
    matchIndex = (index + matches.length) % matches.length;
    const start = matches[matchIndex];

    textareaEl.focus();
    textareaEl.setSelectionRange(start, start + findQuery.length);

    // Scrolling is by line, because a textarea has no way to ask where a
    // character sits. Close enough to put the match on screen, which is all
    // that is needed.
    const lineHeight = parseFloat(getComputedStyle(textareaEl).lineHeight) || 20;
    const line = notes.slice(0, start).split('\n').length - 1;
    textareaEl.scrollTop = Math.max(0, (line * lineHeight) - (textareaEl.clientHeight / 2));
  }

  function openFind() {
    showFind = true;
    // Pre-fill from the selection, as editors do: having just highlighted the
    // word you want to find, retyping it is pure ceremony.
    const selected = textareaEl?.value.slice(textareaEl.selectionStart, textareaEl.selectionEnd);
    if (selected && !selected.includes('\n')) findQuery = selected;
    tick().then(() => { findInputEl?.focus(); findInputEl?.select(); });
  }

  function closeFind() {
    showFind = false;
    findQuery = '';
    textareaEl?.focus();
  }

  function handleFindKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.preventDefault();
      closeFind();
      return;
    }
    // Enter, F3 and Ctrl+G all step, so whichever one the user reaches for
    // works from the box as well as from the note.
    if (event.key === 'Enter' || event.key === 'F3' ||
        ((event.ctrlKey || event.metaKey) && event.key === 'g')) {
      event.preventDefault();
      goToMatch(event.shiftKey ? matchIndex - 1 : matchIndex + 1);
    }
  }

  /**
   * Undo history for the note.
   *
   * The textarea has its own, but it is emptied whenever the value is assigned
   * from code — which happens on every tab switch, and happened on every
   * dictated word until that was fixed. Keeping a history here means Ctrl+Z
   * still reaches text typed before the last such assignment, and gives Ctrl+Y
   * a redo, which a textarea only offers as Ctrl+Shift+Z.
   *
   * Entries are whole snapshots. A note is a few kilobytes at most, and diffing
   * to save memory would cost more code than the memory is worth.
   */
  type Snapshot = { text: string; caret: number };
  let history: Snapshot[] = [];
  let historyAt = -1;
  /** Set while undo/redo is writing, so the change is not recorded as an edit. */
  let restoring = false;

  const HISTORY_LIMIT = 200;
  /** Typing is grouped into one entry until this much time passes. */
  const COALESCE_MS = 600;
  let lastRecordedAt = 0;

  /**
   * Start the history over for a freshly loaded note.
   *
   * Without this, undo would walk back into the previous tab's text and write
   * it into this one — the worst possible outcome for a key people press
   * without looking.
   */
  function resetHistory() {
    pageHistories.clear();
    startHistory();
  }

  function startHistory() {
    history = [{ text: notes, caret: 0 }];
    historyAt = 0;
    lastRecordedAt = 0;
  }

  /**
   * The histories of the open note's other pages, by page ID, so switching
   * pages and back still undoes what was typed there. The history above is
   * the open page's; it is put here when another page is opened and taken
   * back out when its page is.
   *
   * Only the open note's pages are kept — every reset above (another note,
   * a fresh load of this one, a discard) drops them all — and each is capped
   * at HISTORY_LIMIT entries like the open one.
   */
  type PageHistory = { entries: Snapshot[]; at: number };
  const pageHistories = new Map<string, PageHistory>();

  function swapPageHistory(fromId: string, toId: string) {
    // A deleted page's history has no page to go back to.
    if (pageIndex(pages, fromId) !== -1) pageHistories.set(fromId, { entries: history, at: historyAt });
    const kept = pageHistories.get(toId);
    pageHistories.delete(toId);
    // Taken up only if it ends at the page's text as it is now: a history
    // that does not would make the first undo jump to text the page no
    // longer has.
    if (kept && kept.entries[kept.at]?.text === notes) {
      history = kept.entries;
      historyAt = kept.at;
      // The first keystroke after coming back starts an entry of its own.
      lastRecordedAt = 0;
    } else {
      startHistory();
    }
  }

  function recordHistory(force = false) {
    if (restoring) return;

    const snapshot: Snapshot = { text: notes, caret: textareaEl?.selectionStart ?? notes.length };
    const current = history[historyAt];
    if (current && current.text === snapshot.text) return;

    const now = Date.now();
    // Successive keystrokes replace the last entry rather than adding one, so
    // Ctrl+Z steps back by a word or a pause, not by a character.
    const coalesce = !force && historyAt >= 0 && now - lastRecordedAt < COALESCE_MS;
    lastRecordedAt = now;

    if (coalesce) {
      history[historyAt] = snapshot;
      return;
    }

    // A new edit after undoing discards what was undone, as every editor does.
    history = history.slice(0, historyAt + 1);
    history.push(snapshot);
    if (history.length > HISTORY_LIMIT) history.shift();
    historyAt = history.length - 1;
  }

  function applySnapshot(snapshot: Snapshot) {
    restoring = true;
    notes = snapshot.text;
    tick().then(() => {
      restoring = false;
      if (!textareaEl) return;
      textareaEl.focus();
      const at = Math.min(snapshot.caret, snapshot.text.length);
      textareaEl.setSelectionRange(at, at);
    });
    handleInput(); // the restored text still has to be saved
  }

  function undoNote() {
    if (historyAt <= 0) return;
    historyAt -= 1;
    applySnapshot(history[historyAt]);
  }

  function redoNote() {
    if (historyAt >= history.length - 1) return;
    historyAt += 1;
    applySnapshot(history[historyAt]);
  }

  /** Ctrl+F anywhere in the note opens the bar. */
  function handleContainerKeydown(event: KeyboardEvent) {
    // F3 takes no modifier, so it is checked before the others.
    if (event.key === 'F3' && showFind) {
      event.preventDefault();
      goToMatch(event.shiftKey ? matchIndex - 1 : matchIndex + 1);
      return;
    }

    if (handlePageStepKey(event)) return;

    const mod = event.ctrlKey || event.metaKey;
    if (!mod) return;

    if (event.key === 'f') {
      event.preventDefault();
      openFind();
      return;
    }
    // Step through matches from the note itself, not only from the find box.
    // goToMatch puts the focus back in the textarea so the match shows as a
    // selection, which means Enter there types a newline rather than advancing
    // — without these, stepping meant clicking back into the box every time.
    if (event.key === 'g' && showFind) {
      event.preventDefault();
      goToMatch(event.shiftKey ? matchIndex - 1 : matchIndex + 1);
      return;
    }

    // Handled here rather than left to the browser: its own history is empty
    // after a tab switch, so Ctrl+Z would appear to do nothing at all.
    if (event.key === 'z' && !event.shiftKey) {
      event.preventDefault();
      undoNote();
      return;
    }
    // Both spellings of redo, since the note takes Ctrl+Y as well.
    if (event.key === 'y' || (event.key === 'z' && event.shiftKey)) {
      event.preventDefault();
      redoNote();
    }
  }

  // Dictation support
  const dictation = createFieldDictation(
    () => textareaEl,
    () => handleInput() // trigger autosave after dictation inserts text
  );
  const dictationListening = dictation.listening;

  onMount(() => {
    unregisterUnsavedGuard = registerUnsavedGuard({
      isDirty: hasUnsavedDrafts,
      revision: unsavedRevision,
      requestDiscard: (continueAfterDiscard, cancelDiscard) => {
        pendingDiscard = continueAfterDiscard;
        pendingDiscardCancel = cancelDiscard ?? null;
      },
    });
    loadNotes();
  });

  onDestroy(() => {
    unregisterUnsavedGuard?.();
    unregisterUnsavedGuard = null;
    pendingDiscard = null;
    pendingDiscardCancel = null;
    closePageMenu();
    rememberCurrentDraft();
    // Save any pending changes
    if (saveTimeout) {
      clearTimeout(saveTimeout);
      void saveNow(lastProjectId, lastSessionId, lastWindowIdx, pages);
    }
    dictation.destroy();
  });

  // Load notes when session or window changes
  async function loadNotes(force = false) {
    const projectId = get(activeProjectId);
    const sessionId = targetSessionId();
    const windowIdx = targetWindowIdx();

    if (!sessionId) {
      loadGeneration++;
      loadingNotes = false;
      lastProjectId = projectId;
      lastSessionId = null;
      pages = editablePages([]);
      activePageId = pages[0].id;
      notes = '';
      savedPages = pages;
      saveError = '';
      loadError = '';
      resetHistory();
      return;
    }

    // Only reload if session or window changed (unless forced)
    if (!force && projectId === lastProjectId && sessionId === lastSessionId && windowIdx === lastWindowIdx) {
      return;
    }

    const sameTarget = projectId === lastProjectId && sessionId === lastSessionId && windowIdx === lastWindowIdx;
    lastProjectId = projectId;
    lastSessionId = sessionId;
    lastWindowIdx = windowIdx;
    const generation = ++loadGeneration;
    const targetKey = noteKey(projectId, sessionId, windowIdx);
    const empty = editablePages([]);
    const remembered = draftsByTarget.get(targetKey) ?? {
      pages: empty,
      // Reloading the note already open stays on its page; a note opened
      // afresh goes back to the page it was left on.
      activePageId: sameTarget ? activePageId : rememberedActivePage(targetKey) ?? empty[0].id,
      saved: empty, saveError: '', loadError: '',
    };
    showDraft({ ...remembered, loadError: '' });
    // A note seen for the first time shows nothing until it arrives; the
    // page to open is kept for when it has.
    const wantedPageId = remembered.activePageId;
    loadingNotes = true;

    try {
      // A previous visit to this tab may still be flushing its last edit.
      // Read only after that tab's queue has drained; otherwise a fast
      // A → B → A switch can fetch the pre-save value and put stale text
      // back into the editor even though the later write succeeds.
      const pendingSave = saveQueues.get(targetKey);
      if (pendingSave) await pendingSave;
      if (generation !== loadGeneration || projectId !== lastProjectId || projectId !== get(activeProjectId) || sessionId !== lastSessionId || windowIdx !== lastWindowIdx) return;
      // A failed or not-yet-saved draft is the newest copy. Reading the backend
      // here would turn a failed save into apparent success and discard the only
      // copy of the user's text on a fast A -> B -> A switch.
      const latestDraft = draftsByTarget.get(targetKey);
      if (latestDraft && draftIsDirty(latestDraft)) {
        showDraft(latestDraft);
        resetHistory();
        return;
      }
      const content = isProject ? await App.GetProjectNotePages() : await App.GetTabNotePages(sessionId, windowIdx);
      if (generation !== loadGeneration || projectId !== lastProjectId || projectId !== get(activeProjectId) || sessionId !== lastSessionId || windowIdx !== lastWindowIdx) return;
      const loaded = editablePages(content);
      const draft: NoteDraft = {
        pages: loaded,
        activePageId: resolveActivePage(loaded, wantedPageId),
        saved: loaded,
        saveError: '',
        loadError: '',
      };
      showDraft(draft);
      draftsByTarget.set(targetKey, draft);
      resetHistory();
    } catch (e) {
      if (generation !== loadGeneration || projectId !== lastProjectId || projectId !== get(activeProjectId) || sessionId !== lastSessionId || windowIdx !== lastWindowIdx) return;
      console.error('Failed to load notes:', e);
      // Keep the per-target draft (including a clean cached copy) and make the
      // failed load explicit. An empty editable textarea is indistinguishable
      // from a genuinely empty note and lets the next keystroke overwrite it.
      const draft = draftsByTarget.get(targetKey) ?? remembered;
      showDraft({ ...draft, loadError: String(e) });
      draftsByTarget.set(targetKey, { ...draft, loadError: String(e) });
      resetHistory();
    } finally {
      // A stale request must not re-enable the textarea while the replacement
      // target is still loading. Keeping the old note read-only in this short
      // interval also prevents keystrokes from being saved under the new tab.
      if (generation === loadGeneration && projectId === lastProjectId && projectId === get(activeProjectId) && sessionId === lastSessionId && windowIdx === lastWindowIdx) {
        loadingNotes = false;
      }
    }
  }

  // Reload when tab becomes active
  let wasActive = false;
  // Set while opening the view reloads the note, so a find opened from the
  // global search waits for the text it is to find things in.
  let activating = false;
  $: if (active && !wasActive) {
    wasActive = true;
    applyDefaultScope();
    void activateNotes();
  } else if (!active) {
    wasActive = false;
    activating = false;
    activationGeneration++;
  }

  async function activateNotes() {
    const generation = ++activationGeneration;
    activating = true;
    try {
      await flushPendingSave();
      if (generation !== activationGeneration || !active) return;
      // A keystroke made while the flush was in flight owns the textarea. A
      // forced read here would replace it with the previous disk snapshot.
      if (saveTimeout || openNoteChanged()) return;
      await loadNotes(true);
    } finally {
      if (generation === activationGeneration) {
        activating = false;
        void focusNotesOnShow(generation);
      }
    }
  }

  /**
   * Opening the notes hands them the keyboard, once the note is loaded — the
   * textarea is disabled until then. The focus stayed where it was, mostly in
   * the terminal, so the page keys and undo did nothing until a click. Taken
   * only from the terminal, from nowhere, or from inside the notes' own
   * window: never from a field being typed in, such as the find bar a search
   * jump opens, or from another dialog.
   */
  async function focusNotesOnShow(generation: number) {
    await tick();
    await new Promise((resolve) => requestAnimationFrame(resolve));
    if (generation !== activationGeneration || !active || !textareaEl || textareaEl.disabled) return;
    if (!diffTakesFocusFrom(document.activeElement, textareaEl)) return;
    textareaEl.focus({ preventScroll: true });
  }

  /**
   * A note opened from the global search: show the note and the page it
   * names, with the query in the find bar and the first match selected.
   *
   * After the activation block above, so the note asked for wins over a
   * default scope fixed in the settings; before the target watch below, so
   * the scope set here is loaded in the same update. The scope is not written
   * to localStorage: following a search result is not choosing which note to
   * reach for next time.
   */
  let revealJump = false;
  let jumpPageId: string | undefined;
  // A jump names either a session's note or the project's; each view leaves
  // the other kind for the view that shows it.
  $: if (active && followsSearch && $pendingNoteJump &&
      ($pendingNoteJump.scope === 'project') === isProject) takeNoteJump($pendingNoteJump);

  function takeNoteJump(jump: NoteJump) {
    clearNoteJump();
    if (jump.projectId !== get(activeProjectId)) return;
    if (jump.scope !== 'project') {
      // The search selected this session a moment ago; anything else means
      // the selection has moved on, and the request is no longer the user's
      // wish.
      if (jump.sessionId !== get(selectedSessionId)) return;
      scope = jump.scope;
    }
    jumpPageId = jump.pageId;
    if (jump.query) {
      showFind = true;
      findQuery = jump.query;
    }
    revealJump = !!jump.query || !!jump.pageId;
  }

  async function flushPendingSave() {
    if (saveTimeout) {
      clearTimeout(saveTimeout);
      saveTimeout = null;
    }
    if (!lastSessionId || !openNoteChanged()) return;
    await saveNow(lastProjectId, lastSessionId, lastWindowIdx, pages);
  }

  // Watch for session/window changes, and for a switch between the tab's note
  // and the session's, which is a change of target like any other.
  $: wantedSessionId = isProject ? PROJECT_TASKS_SCOPE : $selectedSessionId;
  $: wantedWindowIdx = isProject || scope === 'session' ? SESSION_NOTES : $selectedWindowIdx;
  $: if ($activeProjectId !== lastProjectId || wantedSessionId !== lastSessionId || wantedWindowIdx !== lastWindowIdx) {
    rememberCurrentDraft();
    cancelRename();
    closePageMenu();
    // Save current notes before loading new ones
    if (saveTimeout) {
      clearTimeout(saveTimeout);
      saveTimeout = null;
      void saveNow(lastProjectId, lastSessionId, lastWindowIdx, pages);
    }
    void loadNotes();
  }

  // Waits until the note the search pointed at is loaded and editable: the
  // selection goToMatch makes is lost if the textarea is disabled afterwards
  // for a reload. A note that matched only loosely has nothing to select, so
  // the find bar is closed again rather than left saying "no matches".
  $: if (revealJump && !loadingNotes && !activating && !loadError &&
      lastSessionId === wantedSessionId && lastWindowIdx === wantedWindowIdx) {
    revealJump = false;
    // The page first, then — after a tick, when matches has been recomputed
    // for its text — the match. In a function, too: read here, matches would
    // depend on this block's own assignment to showFind.
    if (jumpPageId) selectPage(jumpPageId);
    jumpPageId = undefined;
    void tick().then(revealJumpMatch);
  }

  function revealJumpMatch() {
    if (!showFind) return;
    if (matches.length) {
      goToMatch(0);
    } else {
      showFind = false;
      findQuery = '';
    }
  }

  // Debounced save
  function handleInput() {
    if (loadingNotes || loadError) return;
    recordHistory();
    pages = setPageText(pages, activePageId, notes);
    scheduleSave();
  }

  /**
   * Queue the open note — all of its pages — to be saved shortly, and record
   * it as this target's draft until then.
   */
  function scheduleSave() {
    if (saveTimeout) {
      clearTimeout(saveTimeout);
    }
    const projectId = lastProjectId;
    const sessionId = lastSessionId;
    const windowIdx = lastWindowIdx;
    const snapshot = pages;
    if (sessionId) {
      draftsByTarget.set(noteKey(projectId, sessionId, windowIdx), {
        pages: snapshot,
        activePageId,
        saved: savedPages,
        saveError: '',
        loadError: '',
      });
      saveError = '';
    }
    const timeout = setTimeout(() => {
      if (saveTimeout === timeout) saveTimeout = null;
      void saveNow(projectId, sessionId, windowIdx, snapshot);
    }, 500);
    saveTimeout = timeout;
  }

  async function saveNow(projectId: string, sessionId: string | null, windowIdx: number, snapshot: NotePage[]) {
    // The project ID is not tested for truth: the default project's is "",
    // and refusing it left every note there unsaved.
    if (!sessionId || (projectId === lastProjectId && sessionId === lastSessionId && windowIdx === lastWindowIdx && pagesKey(snapshot) === pagesKey(savedPages))) return;

    const key = noteKey(projectId, sessionId, windowIdx);
    const previous = saveQueues.get(key) ?? Promise.resolve();
    const queued = previous.catch(() => undefined).then(async () => {
      savesInFlight++;
      saving = true;
      try {
        if (isProject) await App.SetProjectNotePages(snapshot, projectId);
        else await App.SetTabNotePages(sessionId, windowIdx, snapshot, projectId);
        const draft = draftsByTarget.get(key) ?? { pages: snapshot, activePageId: snapshot[0]?.id ?? '', saved: savedPages, saveError: '', loadError: '' };
        draftsByTarget.set(key, { ...draft, saved: snapshot, saveError: '', loadError: '' });
        if (projectId === lastProjectId && projectId === get(activeProjectId) && sessionId === lastSessionId && windowIdx === lastWindowIdx && pagesKey(pages) === pagesKey(snapshot)) {
          savedPages = snapshot;
          saveError = '';
          loadError = '';
        }
        // Notify parent to update status bar preview
        dispatch('notesChange', { sessionId, windowIdx, notes: notePagesText(snapshot) });
      } catch (e) {
        console.error('Failed to save notes:', e);
        const message = String(e);
        const draft = draftsByTarget.get(key) ?? { pages: snapshot, activePageId: snapshot[0]?.id ?? '', saved: editablePages([]), saveError: '', loadError: '' };
        draftsByTarget.set(key, { ...draft, saveError: message });
        if (projectId === lastProjectId && projectId === get(activeProjectId) && sessionId === lastSessionId && windowIdx === lastWindowIdx) saveError = message;
      } finally {
        savesInFlight--;
        saving = savesInFlight > 0;
      }
    });
    saveQueues.set(key, queued);
    await queued;
    if (saveQueues.get(key) === queued) saveQueues.delete(key);
  }

  async function retryNotes() {
    if (!lastSessionId) return;
    if (openNoteChanged() || saveError) {
      await saveNow(lastProjectId, lastSessionId, lastWindowIdx, pages);
      if (saveError) return;
    }
    await loadNotes(true);
  }

  /*
   * Pages. Every change to the list — a new page, a title, a deletion, a
   * new order — is an edit of the note like typing is: it goes into the
   * draft and is saved with the rest, so the guards and the retry that cover
   * text cover it too.
   */
  // A function, not a reactive value: it is read from inside other reactive
  // blocks, which can run before a `$:` declared further down is updated.
  function pagesLocked(): boolean {
    return loadingNotes || !!loadError;
  }

  function showPage(id: string) {
    const previousId = activePageId;
    activePageId = resolveActivePage(pages, id);
    notes = activeText();
    // Each page keeps its own undo history: one history across pages would
    // write one page's text into the next.
    if (activePageId !== previousId) swapPageHistory(previousId, activePageId);
    rememberActivePage(currentNoteKey(), activePageId);
    // Remembered with the draft as well, so coming back to this note in the
    // same sitting opens this page even before the note is saved.
    const key = currentNoteKey();
    const draft = key ? draftsByTarget.get(key) : undefined;
    if (key && draft) draftsByTarget.set(key, { ...draft, activePageId });
  }

  function selectPage(id: string) {
    if (pagesLocked() || id === activePageId || pageIndex(pages, id) === -1) return;
    showPage(id);
  }

  function changePages(next: NotePage[], nextActiveId = activePageId) {
    if (pagesLocked()) return;
    pages = next;
    if (nextActiveId !== activePageId || pageIndex(pages, activePageId) === -1) showPage(nextActiveId);
    scheduleSave();
  }

  /**
   * The page shortcuts (Alt+PgUp / Alt+PgDn unless rebound) step through the
   * pages. Ctrl+PgUp / Ctrl+PgDn — the usual pair — already switch the
   * session's tabs everywhere, the notes included, and taking them here
   * would strand the user in the note.
   */
  function handlePageStepKey(event: KeyboardEvent): boolean {
    const next = matchesShortcut(event, 'notes.nextPage');
    if (!next && !matchesShortcut(event, 'notes.prevPage')) return false;
    // One page: nothing to step, and the keys are the tabs' too — let them go.
    if (pages.length < 2) return false;
    event.preventDefault();
    event.stopPropagation();
    if (pages.length > 1) selectPage(stepPage(pages, activePageId, next ? 1 : -1));
    return true;
  }

  // The keys in the page tabs' tooltip follow a rebinding, as the help does;
  // switched off, they are left out rather than named.
  const isMac = typeof navigator !== 'undefined' && /Mac/i.test(navigator.platform);
  $: pageStepKeys = ['notes.prevPage', 'notes.nextPage']
    .flatMap((id) => $effectiveBindings.get(id) ?? [])
    .map((binding) => formatBinding(binding, isMac))
    .join(' / ');

  let pageStripEl: HTMLElement | undefined;

  // Renaming happens in place: the tab becomes a text field.
  let renamingPageId: string | null = null;
  let renameValue = '';
  let renameInputEl: HTMLInputElement | undefined;

  function startRename(id: string) {
    if (pagesLocked()) return;
    closePageMenu();
    const page = pages[pageIndex(pages, id)];
    if (!page) return;
    renamingPageId = id;
    renameValue = page.title;
    void tick().then(() => { renameInputEl?.focus(); renameInputEl?.select(); });
  }

  function commitRename() {
    const id = renamingPageId;
    if (!id) return;
    renamingPageId = null;
    if (pageIndex(pages, id) !== -1) changePages(renamePage(pages, id, renameValue));
    void tick().then(() => textareaEl?.focus());
  }

  function cancelRename() {
    renamingPageId = null;
  }

  function handleRenameKeydown(event: KeyboardEvent) {
    // The field is inside the notes view, whose own keys must not see these.
    event.stopPropagation();
    if (event.key === 'Enter') {
      event.preventDefault();
      commitRename();
    } else if (event.key === 'Escape') {
      event.preventDefault();
      cancelRename();
      void tick().then(() => textareaEl?.focus());
    }
  }

  /** "+": a new empty page at the end, opened, with its title being typed. */
  function addNewPage() {
    if (pagesLocked()) return;
    const added = addPage(pages, null);
    changePages(added.pages, added.id);
    startRename(added.id);
  }

  let pendingDeletePageId: string | null = null;

  /** Deleting a page with nothing in it needs no confirmation. */
  function requestDeletePage(id: string) {
    closePageMenu();
    const page = pages[pageIndex(pages, id)];
    if (!page || pages.length <= 1 || pagesLocked()) return;
    if (page.text.trim() === '') {
      removePage(id);
    } else {
      pendingDeletePageId = id;
    }
  }

  function removePage(id: string) {
    const result = deletePage(pages, id, activePageId);
    changePages(result.pages, result.activeId);
    if (pageIndex(pages, id) === -1) pageHistories.delete(id);
  }

  function confirmDeletePage() {
    const id = pendingDeletePageId;
    pendingDeletePageId = null;
    if (id && pageIndex(pages, id) !== -1) removePage(id);
  }

  function shiftPage(id: string, delta: number) {
    closePageMenu();
    const at = pageIndex(pages, id);
    if (at === -1) return;
    changePages(movePage(pages, id, at + delta));
  }

  // The page context menu: one menu open at a time across the app.
  let pageMenu: { id: string; x: number; y: number } | null = null;

  function openPageMenu(event: MouseEvent, id: string) {
    event.preventDefault();
    if (pagesLocked()) return;
    pageMenu = { id, x: event.clientX, y: event.clientY };
    claimMenu(closePageMenu);
  }

  function closePageMenu() {
    pageMenu = null;
    releaseMenu(closePageMenu);
  }

  function handleWindowKeydown(event: KeyboardEvent) {
    if (pageMenu && event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      closePageMenu();
    }
  }

  // Dragging a page along the strip reorders it; the marker shows which side
  // of the page under the cursor it would land on.
  let draggedPageId: string | null = null;
  let dropMarker: { id: string; after: boolean } | null = null;

  function handlePageDragStart(event: DragEvent, id: string) {
    if (pagesLocked() || renamingPageId) {
      event.preventDefault();
      return;
    }
    draggedPageId = id;
    if (event.dataTransfer) {
      event.dataTransfer.effectAllowed = 'move';
      // A type of its own: as text/plain, dropping the page into the note
      // would type its ID there.
      event.dataTransfer.setData('application/x-asmgr-note-page', id);
    }
  }

  function handlePageDragOver(event: DragEvent, id: string) {
    if (!draggedPageId) return;
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'move';
    const box = (event.currentTarget as HTMLElement).getBoundingClientRect();
    dropMarker = { id, after: event.clientX > box.left + box.width / 2 };
  }

  function handlePageDragLeave(event: DragEvent) {
    // dragleave fires on entering a child of the page too; only leaving the
    // page itself clears the marker.
    const tab = event.currentTarget as HTMLElement;
    if (event.relatedTarget instanceof Node && tab.contains(event.relatedTarget)) return;
    dropMarker = null;
  }

  function handlePageDrop(event: DragEvent, id: string) {
    event.preventDefault();
    const dragged = draggedPageId;
    const after = dropMarker?.id === id ? dropMarker.after : false;
    handlePageDragEnd();
    if (!dragged || dragged === id) return;
    changePages(movePage(pages, dragged, dropIndex(pages, dragged, id, after)));
  }

  function handlePageDragEnd() {
    draggedPageId = null;
    dropMarker = null;
  }

  function handlePageTabKeydown(event: KeyboardEvent, id: string) {
    if (handlePageStepKey(event)) return;
    if (event.key === 'F2') {
      event.preventDefault();
      startRename(id);
    } else if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
      // Arrow keys move along the strip, as in any row of tabs.
      event.preventDefault();
      const next = stepPage(pages, id, event.key === 'ArrowRight' ? 1 : -1);
      selectPage(next);
      void tick().then(() => (pageStripEl?.querySelector(`[data-page-id="${CSS.escape(next)}"]`) as HTMLElement | null)?.focus());
    }
  }
</script>

<svelte:window on:click={() => { if (pageMenu) closePageMenu(); }} on:keydown={handleWindowKeydown} />

<div
  class="notes-container"
  class:custom-colors={notesColorsChosen}
  style={notesColorStyle(notesColors)}
  data-note-pages={pages.length}
>
  <div class="notes-header">
    <span class="notes-title">{isProject ? $t('projectTasks.notesTitle') : $t('notes.title')}</span>
    {#if !isProject}
    <div class="scope-switch" role="group" aria-label={$t('notes.title')}>
      <button
        type="button"
        class:selected={scope === 'tab'}
        aria-pressed={scope === 'tab'}
        title={$t('notes.scopeTabHint')}
        on:click={() => setScope('tab')}
      >{$t('notes.scopeTab')}{#if presence.tab}<span class="scope-dot" aria-label={$t('notes.hasNote')}></span>{/if}</button>
      <button
        type="button"
        class:selected={scope === 'session'}
        aria-pressed={scope === 'session'}
        title={$t('notes.scopeSessionHint')}
        on:click={() => setScope('session')}
      >{$t('notes.scopeSession')}{#if presence.session}<span class="scope-dot" aria-label={$t('notes.hasNote')}></span>{/if}</button>
    </div>
    {/if}
    <div class="header-actions">
      {#if saving}
        <span class="save-indicator">{$t('notes.saving')}</span>
      {:else if pagesKey(pages) !== pagesKey(savedPages)}
        <span class="save-indicator unsaved">{$t('notes.unsaved')}</span>
      {/if}
      <!-- Ctrl+F opens the same bar; the button is here for the people who
           never learn the shortcut, which is most of them. -->
      <button
        class="mic-btn find-toggle"
        class:active={showFind}
        on:click={() => (showFind ? closeFind() : openFind())}
        title="{$t('notes.findPlaceholder')} (Ctrl+F)"
        aria-label={$t('notes.findPlaceholder')}
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="11" cy="11" r="7"/>
          <path d="M21 21l-4.35-4.35"/>
        </svg>
      </button>
      <button
        class="mic-btn"
        class:active={$dictationListening}
        on:click={() => dictation.toggle()}
        disabled={loadingNotes}
        title={$dictationListening ? $t('tabBar.stopDictation') : $t('tabBar.startDictation')}
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z"/>
          <path d="M19 10v2a7 7 0 0 1-14 0v-2"/>
          <line x1="12" y1="19" x2="12" y2="23"/>
          <line x1="8" y1="23" x2="16" y2="23"/>
        </svg>
      </button>
    </div>
  </div>
  <!-- The note's pages. Click to open one, double-click (or F2) to rename,
       right-click for the rest; drag to reorder. -->
  <div class="page-strip" role="tablist" aria-label={$t('notes.pages')} bind:this={pageStripEl}>
    {#each pages as page, i (page.id)}
      {#if renamingPageId === page.id}
        <input
          class="page-title-input"
          type="text"
          maxlength="200"
          bind:this={renameInputEl}
          bind:value={renameValue}
          on:keydown={handleRenameKeydown}
          on:blur={commitRename}
          placeholder={pageLabel('', i, pages.length, $t)}
          aria-label={$t('notes.pageTitle')}
        />
      {:else}
        <button
          type="button"
          role="tab"
          class="page-tab"
          class:active={page.id === activePageId}
          class:drop-before={dropMarker?.id === page.id && !dropMarker.after}
          class:drop-after={dropMarker?.id === page.id && dropMarker.after}
          class:untitled={!page.title}
          aria-selected={page.id === activePageId}
          tabindex={page.id === activePageId ? 0 : -1}
          data-page-id={page.id}
          draggable={!loadingNotes && !loadError}
          disabled={loadingNotes || !!loadError}
          title="{pageLabel(page.title, i, pages.length, $t)} — {$t('notes.pageHint')}{pageStepKeys ? ` ${$t('notes.pageStepHint', { keys: pageStepKeys })}` : ''}"
          on:click={() => selectPage(page.id)}
          on:dblclick={() => startRename(page.id)}
          on:contextmenu={(e) => openPageMenu(e, page.id)}
          on:keydown={(e) => handlePageTabKeydown(e, page.id)}
          on:dragstart={(e) => handlePageDragStart(e, page.id)}
          on:dragover={(e) => handlePageDragOver(e, page.id)}
          on:dragleave={handlePageDragLeave}
          on:drop={(e) => handlePageDrop(e, page.id)}
          on:dragend={handlePageDragEnd}
        >{pageLabel(page.title, i, pages.length, $t)}</button>
      {/if}
    {/each}
    <button
      type="button"
      class="page-add"
      on:click={addNewPage}
      disabled={loadingNotes || !!loadError}
      title={$t('notes.addPage')}
      aria-label={$t('notes.addPage')}
    >+</button>
  </div>
  {#if showFind}
    <div class="find-bar">
      <input
        type="text"
        bind:this={findInputEl}
        bind:value={findQuery}
        on:keydown={handleFindKeydown}
        placeholder={$t('notes.findPlaceholder')}
        title="{$t('notes.nextMatch')}: Enter · F3 · Ctrl+G — {$t('notes.previousMatch')}: Shift+Enter"
      />
      <span class="find-count">
        {matches.length ? `${matchIndex + 1}/${matches.length}` : (findQuery ? $t('notes.noMatches') : '')}
      </span>
      <!-- The shortcut is named in the tooltip, not only bound: a key nobody
           is told about is a key nobody presses. -->
      <button
        on:click={() => goToMatch(matchIndex - 1)}
        disabled={!matches.length}
        title="{$t('notes.previousMatch')} (Shift+Enter · Shift+F3)"
        aria-label={$t('notes.previousMatch')}
      >↑</button>
      <button
        on:click={() => goToMatch(matchIndex + 1)}
        disabled={!matches.length}
        title="{$t('notes.nextMatch')} (Enter · F3 · Ctrl+G)"
        aria-label={$t('notes.nextMatch')}
      >↓</button>
      <button
        on:click={closeFind}
        title="{$t('common.close')} (Esc)"
        aria-label={$t('common.close')}
      >×</button>
    </div>
  {/if}
  {#if saveError || loadError}
    <div class="notes-error" role="alert">
      <span>{saveError || loadError}</span>
      <button on:click={retryNotes} disabled={loadingNotes || saving}>{$t('common.refresh')}</button>
    </div>
  {/if}
  <div class="notes-content">
    <textarea
      class="notes-textarea"
      class:dictating={$dictationListening}
      placeholder={isProject ? $t('projectTasks.notesPlaceholder') : $t('notes.placeholder')}
      bind:value={notes}
      bind:this={textareaEl}
      on:input={handleInput}
      on:keydown={handleContainerKeydown}
      disabled={loadingNotes || !!loadError}
    ></textarea>
  </div>
</div>

{#if pageMenu}
  {@const menuPageId = pageMenu.id}
  {@const menuAt = pageIndex(pages, pageMenu.id)}
  <!-- At body level, like every context menu: inside the project tasks
       window a fixed menu would be placed against the dialog. The click
       handler only keeps a click inside from closing it; Escape closes it
       from the keyboard. -->
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <div
    class="page-menu"
    role="menu"
    tabindex="-1"
    use:portal
    use:menuPosition={{ x: pageMenu.x, y: pageMenu.y }}
    on:click|stopPropagation
  >
    <button role="menuitem" on:click={() => startRename(menuPageId)}>{$t('notes.renamePage')}</button>
    <button role="menuitem" disabled={menuAt <= 0} on:click={() => shiftPage(menuPageId, -1)}>{$t('notes.movePageLeft')}</button>
    <button role="menuitem" disabled={menuAt === -1 || menuAt >= pages.length - 1} on:click={() => shiftPage(menuPageId, 1)}>{$t('notes.movePageRight')}</button>
    <div class="menu-divider"></div>
    <button role="menuitem" class="danger" disabled={pages.length <= 1} on:click={() => requestDeletePage(menuPageId)}>{$t('notes.deletePage')}</button>
  </div>
{/if}

{#if pendingDeletePageId}
  {@const deleteAt = pageIndex(pages, pendingDeletePageId)}
  <ConfirmDialog
    show={true}
    variant="danger"
    title={$t('notes.deletePageTitle')}
    message={$t('notes.deletePageMessage', { title: pageLabel(pages[deleteAt]?.title ?? '', deleteAt, pages.length, $t) })}
    confirmText={$t('notes.deletePage')}
    cancelText={$t('common.cancel')}
    on:confirm={confirmDeletePage}
    on:cancel={() => (pendingDeletePageId = null)}
  />
{/if}

{#if pendingDiscard}
  <ConfirmDialog
    show={true}
    variant="warning"
    title={$t('notes.unsavedQuitTitle')}
    message={$t('notes.unsavedQuitMessage')}
    confirmText={$t('browser.discardChanges')}
    cancelText={$t('browser.keepEditing')}
    on:confirm={confirmDiscardNotes}
    on:cancel={cancelDiscardNotes}
  />
{/if}

<style>
  /* The note's pages, as a row of small tabs under the header. */
  .page-strip {
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 4px 12px 0;
    overflow-x: auto;
    scrollbar-width: thin;
    border-bottom: 1px solid rgba(255, 255, 255, 0.06);
    background: rgba(0, 0, 0, 0.15);
  }
  .page-tab,
  .page-title-input {
    flex-shrink: 0;
    max-width: 220px;
    padding: 5px 12px;
    font-size: 12px;
    border: 1px solid transparent;
    border-bottom: none;
    border-radius: 6px 6px 0 0;
  }
  .page-tab {
    position: relative;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    background: transparent;
    color: #9ca3af;
    cursor: pointer;
  }
  .page-tab.untitled {
    font-style: italic;
  }
  .page-tab:hover:not(:disabled) {
    color: #e4e4e7;
    background: rgba(255, 255, 255, 0.04);
  }
  .page-tab.active {
    color: var(--accent-pale, #e4e4e7);
    background: rgba(var(--accent-rgb), 0.16);
    border-color: rgba(var(--accent-rgb), 0.3);
  }
  .page-tab:disabled {
    cursor: default;
    opacity: 0.6;
  }
  .page-tab:focus-visible {
    outline: 1px solid rgba(var(--accent-rgb), 0.6);
    outline-offset: -1px;
  }
  /* Where a dragged page would land: a line on the near side. */
  .page-tab.drop-before {
    box-shadow: inset 2px 0 0 var(--accent);
  }
  .page-tab.drop-after {
    box-shadow: inset -2px 0 0 var(--accent);
  }
  .page-title-input {
    width: 160px;
    background: rgba(0, 0, 0, 0.35);
    border-color: rgba(var(--accent-rgb), 0.5);
    color: #e5e7eb;
    font-family: inherit;
    outline: none;
  }
  .page-add {
    flex-shrink: 0;
    width: 24px;
    height: 24px;
    margin-left: 4px;
    border: none;
    border-radius: 5px;
    background: transparent;
    color: #6b7280;
    font-size: 16px;
    line-height: 1;
    cursor: pointer;
  }
  .page-add:hover:not(:disabled) {
    color: #e4e4e7;
    background: rgba(255, 255, 255, 0.06);
  }
  .page-add:disabled {
    opacity: 0.4;
    cursor: default;
  }

  .page-menu {
    position: fixed;
    background: var(--bg-raised);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 8px;
    padding: 6px 0;
    min-width: 160px;
    z-index: 1000;
    box-shadow: 0 10px 40px rgba(0, 0, 0, 0.5);
  }
  .page-menu button {
    display: block;
    width: 100%;
    background: transparent;
    border: none;
    color: #d1d5db;
    padding: 8px 16px;
    font-size: 13px;
    text-align: left;
    cursor: pointer;
  }
  .page-menu button:hover:not(:disabled) {
    background: rgba(var(--accent-rgb), 0.1);
  }
  .page-menu button:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .page-menu button.danger {
    color: #ef4444;
  }
  .menu-divider {
    height: 1px;
    background: rgba(255, 255, 255, 0.1);
    margin: 4px 0;
  }

  /* Two states of one choice, drawn as one control so it reads as "which
     note" rather than as two unrelated buttons. */
  .scope-switch {
    display: inline-flex;
    /* Keeps its size in a narrow panel; squeezed, "Session" was cut to
       "Sess" and the choice stopped reading. */
    flex-shrink: 0;
    margin-left: 10px;
    /* Beside the title, not spread to the middle by the header's
       space-between. */
    margin-right: auto;
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 6px;
    overflow: hidden;
  }
  .scope-switch button {
    padding: 4px 12px;
    border: none;
    background: transparent;
    color: #9ca3af;
    font-size: 12px;
    cursor: pointer;
    white-space: nowrap;
  }
  .scope-switch button + button {
    border-left: 1px solid rgba(255, 255, 255, 0.1);
  }
  .scope-switch button:hover {
    color: #e4e4e7;
  }
  /* The same dot the Notes view tab carries, on whichever of the two notes
     has something in it. */
  .scope-dot {
    display: inline-block;
    width: 6px;
    height: 6px;
    margin-left: 6px;
    vertical-align: middle;
    border-radius: 50%;
    background: var(--accent-light);
    opacity: 0.7;
  }
  .scope-switch button.selected {
    background: rgba(var(--accent-rgb), 0.18);
    color: var(--accent-pale, #e4e4e7);
  }

  .find-bar {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 10px;
    background: var(--bg-raised);
    border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  }

  .find-bar input {
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

  .find-bar input:focus {
    outline: none;
    border-color: rgba(var(--accent-rgb), 0.6);
  }

  .find-count {
    font-size: 12px;
    color: #6b7280;
    font-variant-numeric: tabular-nums;
    /* Fixed width so the buttons do not shift as the count changes. */
    min-width: 52px;
    text-align: center;
  }

  .find-bar button {
    padding: 4px 9px;
    background: transparent;
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 5px;
    color: #9ca3af;
    font-size: 13px;
    line-height: 1;
    cursor: pointer;
  }

  .find-bar button:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.07);
    color: #e5e7eb;
  }

  .find-bar button:disabled {
    opacity: 0.4;
    cursor: default;
  }

  .notes-container {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: var(--bg-surface);
  }

  .notes-error {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 7px 12px;
    border-bottom: 1px solid rgba(248, 113, 113, 0.25);
    background: rgba(127, 29, 29, 0.25);
    color: #fca5a5;
    font-size: 12px;
  }

  .notes-error span {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .notes-error button {
    flex: 0 0 auto;
    padding: 3px 8px;
    border: 1px solid rgba(248, 113, 113, 0.35);
    border-radius: 5px;
    background: rgba(255, 255, 255, 0.05);
    color: #fecaca;
    cursor: pointer;
  }

  .notes-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    /* A narrow panel wraps the actions onto a second line rather than
       pushing them out of the header. */
    flex-wrap: wrap;
    padding: 10px 16px;
    background: rgba(0, 0, 0, 0.3);
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  }

  .header-actions {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .notes-title {
    font-size: 13px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #6b7280;
  }

  .save-indicator {
    font-size: 12px;
    color: #4ade80;
    white-space: nowrap;
  }

  .save-indicator.unsaved {
    color: #fbbf24;
  }

  .notes-content {
    flex: 1;
    padding: 12px;
    overflow: hidden;
  }

  /* The colours come from the Settings (notesColors.ts) as variables on the
     container; each fallback is the look the notes had before, which is what
     an unset setting shows. */
  .notes-textarea {
    width: 100%;
    height: 100%;
    background: var(--notes-bg, rgba(0, 0, 0, 0.2));
    border: 1px solid var(--notes-border, rgba(255, 255, 255, 0.08));
    border-radius: 12px;
    padding: 16px;
    font-size: 14px;
    font-family: inherit;
    color: var(--notes-fg, white);
    caret-color: var(--notes-fg, auto);
    resize: none;
    transition: all 0.2s ease;
    line-height: 1.6;
  }

  .notes-textarea:focus {
    outline: none;
    border-color: rgba(var(--accent-rgb), 0.4);
    box-shadow: 0 0 0 3px rgba(var(--accent-rgb), 0.1);
  }

  .notes-textarea::placeholder {
    color: var(--notes-placeholder, #4b5563);
  }

  /* With colours of its own the editor no longer leaves the selection — and
     with it the find bar's match — to the platform: a system highlight that
     suits white text on black can swallow dark text on a pale page. The
     accent shows through, the text keeps its colour. */
  .custom-colors .notes-textarea::selection {
    background: rgba(var(--accent-rgb), var(--notes-selection-alpha, 0.4));
    color: var(--notes-fg);
  }

  /* Drawn in the text colour, so it shows on a light page as on a dark one. */
  .custom-colors .notes-textarea::-webkit-scrollbar-thumb {
    background: var(--notes-scrollbar);
    background-clip: padding-box;
  }

  .custom-colors .notes-textarea::-webkit-scrollbar-thumb:hover {
    background: var(--notes-scrollbar-hover);
    background-clip: padding-box;
  }




  .notes-textarea.dictating {
    border-color: rgba(var(--accent-rgb), 0.5);
    box-shadow: 0 0 0 3px rgba(var(--accent-rgb), 0.15);
  }

  .mic-btn {
    background: none;
    border: none;
    cursor: pointer;
    color: #6b7280;
    padding: 4px;
    border-radius: 4px;
    display: flex;
    align-items: center;
    transition: color 0.2s;
  }

  .mic-btn:hover {
    color: #9ca3af;
  }

  .mic-btn.active {
    color: var(--accent);
    animation: mic-pulse 1.5s ease-in-out infinite;
  }

  @keyframes mic-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.5; }
  }
</style>
