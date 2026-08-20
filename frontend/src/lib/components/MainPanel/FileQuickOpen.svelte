<script lang="ts">
  import { createEventDispatcher, onDestroy } from 'svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import type { session } from '../../../../wailsjs/go/models';
  import { t } from '../../i18n';
  import { rankFiles, highlightSegments, type FileMatch, type HighlightSegment } from '../../utils/fileMatch';

  // An overlay rather than a search box in the browser's header. The header
  // already carries the title, the root path and two buttons, and the body is
  // two panes with a draggable splitter between them — a permanent input would
  // take space from a tree that is already narrow. An overlay costs nothing
  // when closed, and it is the interaction the user already knows from the
  // saved-command picker (Ctrl+P), down to the arrow/Enter/Escape keys.

  export let show = false;
  export let sessionId = '';
  /**
   * Which tab's directory to search.
   *
   * Passed in rather than read from the store, like sessionId beside it. A tab
   * can be opened in a directory of its own, and without this the quick-open
   * searched the session's tree while the file tree next to it showed the
   * tab's — the same screen answering one question two ways.
   */
  export let windowIdx = 0;
  /** Canonical root currently owned by the surrounding FileBrowser tree. */
  export let root = '';

  const dispatch = createEventDispatcher<{ pick: { path: string }; close: void }>();

  // How many rows are ranked and rendered. The list scrolls, and nobody reads
  // past the first screen of a fuzzy match — ranking the whole index but
  // rendering only this many keeps the keystroke cost flat in repository size.
  const MAX_ROWS = 200;

  let query = '';
  let selectedIdx = 0;
  // The overlay often opens under the pointer. Without this the row that
  // happens to be beneath it would fire and steal the selection, so Enter
  // opens a file the user never chose. mousemove, not mouseenter: the mouse
  // only takes over once it has actually moved. Same fix as
  // CommandPickerDialog.
  let mouseActive = false;
  let listEl: HTMLElement | null = null;
  let inputEl: HTMLInputElement | null = null;

  let index: session.FileIndex | null = null;
  let loading = false;
  let error = '';
  // Whether the walk was asked to include node_modules and friends. Kept
  // across opens on purpose: a user who needed dependency sources once usually
  // needs them for the next few searches too.
  let includeAll = false;
  let destroyed = false;
  // Guards a stale walk finishing after a newer one was started.
  let loadGeneration = 0;

  // --- Content search -------------------------------------------------------

  // Two modes over the same index: the filename ranking runs locally on every
  // keystroke, the grep is a backend round trip and so is only run on demand.
  let mode: 'files' | 'content' = 'files';
  let contentResult: session.ContentSearchResult | null = null;
  let contentLoading = false;
  let contentError = '';
  let searchedQuery = '';
  let searchGeneration = 0;

  onDestroy(() => {
    destroyed = true;
    loadGeneration++;
    searchGeneration++;
  });

  // The index is fetched when the overlay opens, and again whenever its full
  // session/tab/root target or the include-everything setting changes.
  //
  // Collapsed to ONE tracking string rather than three separate guards. Svelte 3
  // orders reactive statements by dependency, not by source position, so the
  // tracking variable must be assigned inside this same block — and a block that
  // both reads and writes several guards re-enters once per guard, racing with
  // itself. With a single key the block re-enters at most once, and that pass
  // sees an equal key and does nothing.
  let lastKey = '';
  $: {
    const key = `${show}|${includeAll}|${sessionId}|${windowIdx}|${root}`;
    if (key !== lastKey) {
      const wasOpen = lastKey.startsWith('true|');
      lastKey = key;
      // A response for the previous tab/root must not populate the new picker.
      loadGeneration++;
      searchGeneration++;
      index = null;
      contentResult = null;
      selectedIdx = 0;
      loading = false;
      contentLoading = false;
      error = '';
      contentError = '';
      searchedQuery = '';
      if (show) void load();
      else if (wasOpen) reset();
    }
  }

  function reset() {
    query = '';
    selectedIdx = 0;
    mouseActive = false;
    error = '';
    mode = 'files';
    contentResult = null;
    contentError = '';
    searchedQuery = '';
  }

  async function load() {
    const targetSessionId = sessionId;
    const targetWindowIdx = windowIdx;
    const targetRoot = root;
    if (!targetSessionId || !targetRoot) return;
    const generation = ++loadGeneration;
    loading = true;
    error = '';
    try {
      const result = await App.SearchSessionFileIndex(targetSessionId, includeAll, targetWindowIdx, targetRoot);
      if (destroyed || generation !== loadGeneration || targetSessionId !== sessionId ||
          targetWindowIdx !== windowIdx || targetRoot !== root) return;
      index = result;
    } catch (e) {
      if (destroyed || generation !== loadGeneration || targetSessionId !== sessionId ||
          targetWindowIdx !== windowIdx || targetRoot !== root) return;
      // The Go side names the directory it could not read; showing that beats
      // a generic failure the user cannot act on.
      error = String(e);
      index = null;
    }
    if (!destroyed && generation === loadGeneration && targetSessionId === sessionId &&
        targetWindowIdx === windowIdx && targetRoot === root) loading = false;
  }

  /** Re-walk the tree, for a user who just created the file they are looking for. */
  function reload() {
    void App.InvalidateSessionFileIndex(sessionId);
    void load();
  }

  // --- Filename ranking -----------------------------------------------------

  $: candidates = index ? index.files || [] : [];
  $: matches = mode === 'files' ? rankFiles(candidates, query, MAX_ROWS) : [];

  // Highlights are built only for the rows on screen, not for the whole ranking
  // pass — the segments are display detail and the ranking is the hot path.
  $: segmented = matches.map((m: FileMatch) => ({
    path: m.path,
    segments: highlightSegments(m.path, m.positions),
  }));

  // --- Content search -------------------------------------------------------

  $: contentMatches = contentResult ? contentResult.matches || [] : [];
  // One flat list for the keyboard, whichever mode is up, so ↑/↓ and Enter mean
  // the same thing in both.
  $: rowCount = mode === 'files' ? matches.length : contentMatches.length;

  // Keep the cursor inside the list as the query narrows it.
  $: if (selectedIdx >= rowCount) selectedIdx = Math.max(0, rowCount - 1);

  async function runContentSearch() {
    const q = query.trim();
    const targetSessionId = sessionId;
    const targetWindowIdx = windowIdx;
    const targetRoot = root;
    if (!q || !targetSessionId || !targetRoot) return;
    const generation = ++searchGeneration;
    contentLoading = true;
    contentError = '';
    searchedQuery = q;
    try {
      const result = await App.SearchSessionFileContents(
        targetSessionId,
        q,
        false,
        includeAll,
        targetWindowIdx,
        targetRoot,
      );
      if (destroyed || generation !== searchGeneration || targetSessionId !== sessionId ||
          targetWindowIdx !== windowIdx || targetRoot !== root) return;
      contentResult = result;
      selectedIdx = 0;
    } catch (e) {
      if (destroyed || generation !== searchGeneration || targetSessionId !== sessionId ||
          targetWindowIdx !== windowIdx || targetRoot !== root) return;
      contentError = String(e);
      contentResult = null;
    }
    if (!destroyed && generation === searchGeneration && targetSessionId === sessionId &&
        targetWindowIdx === windowIdx && targetRoot === root) contentLoading = false;
  }

  function switchMode(next: 'files' | 'content') {
    if (mode === next) return;
    mode = next;
    selectedIdx = 0;
    mouseActive = false;
    if (next === 'content') {
      // The grep is a round trip over every indexed file, so it is never run
      // per keystroke — only when the user asks for it.
      if (query.trim() && query.trim() !== searchedQuery) void runContentSearch();
    }
    inputEl?.focus();
  }

  // --- Keyboard -------------------------------------------------------------

  function scrollSelectedIntoView() {
    if (!listEl) return;
    const row = listEl.querySelector<HTMLElement>(`[data-idx="${selectedIdx}"]`);
    row?.scrollIntoView({ block: 'nearest' });
  }

  $: if (selectedIdx >= 0 && listEl && !mouseActive) {
    // Keyboard navigation has to keep the selection visible. Skipped while the
    // mouse is driving: the row is already under the pointer and scrolling to
    // it would fight the user's own scrolling.
    void selectedIdx;
    requestAnimationFrame(scrollSelectedIntoView);
  }

  function move(delta: number) {
    if (rowCount === 0) return;
    mouseActive = false;
    selectedIdx = (selectedIdx + delta + rowCount) % rowCount;
  }

  function pickCurrent() {
    if (mode === 'files') {
      const match = matches[selectedIdx];
      if (match) pick(match.path);
      return;
    }
    const hit = contentMatches[selectedIdx];
    if (hit) pick(hit.path);
  }

  function pick(path: string) {
    dispatch('pick', { path });
    show = false;
  }

  function close() {
    show = false;
    dispatch('close');
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      close();
      return;
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      move(1);
      return;
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault();
      move(-1);
      return;
    }
    if (e.key === 'Tab') {
      // Tab toggles the two modes rather than moving focus: there is nothing
      // else in the overlay worth tabbing to, and the modes are the only thing
      // the user switches between while typing.
      e.preventDefault();
      switchMode(mode === 'files' ? 'content' : 'files');
      return;
    }
    if (e.key === 'Enter') {
      e.preventDefault();
      if (mode === 'content' && query.trim() !== searchedQuery) {
        void runContentSearch();
        return;
      }
      pickCurrent();
    }
  }

  function handleInput() {
    mouseActive = false;
    selectedIdx = 0;
  }

  function autoFocus(node: HTMLInputElement) {
    inputEl = node;
    requestAnimationFrame(() => node.focus());
    return {
      destroy() {
        if (inputEl === node) inputEl = null;
      },
    };
  }

  // Hoisted out of the markup: Svelte's template cannot hold a cast, and the
  // segment list is a plain value the {#each} needs by name.
  function segmentsOf(row: { segments: HighlightSegment[] }): HighlightSegment[] {
    return row.segments;
  }

  /** The matched slice of a grep hit's line, for the highlight. */
  function contentParts(hit: session.ContentMatch): HighlightSegment[] {
    const start = Math.max(0, Math.min(hit.col, hit.text.length));
    const end = Math.max(start, Math.min(hit.col + hit.length, hit.text.length));
    const out: HighlightSegment[] = [];
    if (start > 0) out.push({ text: hit.text.slice(0, start), matched: false });
    out.push({ text: hit.text.slice(start, end), matched: true });
    if (end < hit.text.length) out.push({ text: hit.text.slice(end), matched: false });
    return out;
  }
</script>

{#if show}
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <div
    class="dialog-overlay quickopen-overlay"
    tabindex="-1"
    role="dialog"
    aria-modal="true"
    on:click|self={close}
    on:keydown={handleKeydown}
  >
    <div class="quickopen">
      <div class="qo-head">
        <input
          class="qo-input"
          use:autoFocus
          bind:value={query}
          on:input={handleInput}
          placeholder={mode === 'files'
            ? $t('browser.quickOpenPlaceholder')
            : $t('browser.contentSearchPlaceholder')}
          aria-label={$t('browser.quickOpenTitle')}
          spellcheck="false"
          autocomplete="off"
        />
        <div class="qo-modes" role="tablist">
          <button
            class="qo-mode"
            class:active={mode === 'files'}
            role="tab"
            aria-selected={mode === 'files'}
            on:click={() => switchMode('files')}
          >
            {$t('browser.modeFiles')}
          </button>
          <button
            class="qo-mode"
            class:active={mode === 'content'}
            role="tab"
            aria-selected={mode === 'content'}
            on:click={() => switchMode('content')}
          >
            {$t('browser.modeContent')}
          </button>
        </div>
      </div>

      <div class="qo-body" bind:this={listEl}>
        {#if loading}
          <div class="qo-state">{$t('browser.indexing')}</div>
        {:else if error}
          <div class="qo-state error">{error}</div>
        {:else if mode === 'files'}
          {#if matches.length === 0}
            <div class="qo-state">{$t('browser.noMatches')}</div>
          {:else}
            {#each segmented as row, idx (row.path)}
              <div
                class="qo-row"
                class:selected={idx === selectedIdx}
                data-idx={idx}
                role="button"
                tabindex="-1"
                title={row.path}
                on:click={() => pick(row.path)}
                on:mousemove={() => { mouseActive = true; selectedIdx = idx; }}
                on:keydown={() => {}}
              >
                <span class="qo-path"
                  >{#each segmentsOf(row) as seg}<span class:hit={seg.matched}>{seg.text}</span>{/each}</span
                >
              </div>
            {/each}
          {/if}
        {:else if contentLoading}
          <div class="qo-state">{$t('browser.searching')}</div>
        {:else if contentError}
          <div class="qo-state error">{contentError}</div>
        {:else if !searchedQuery}
          <div class="qo-state">{$t('browser.contentSearchHint')}</div>
        {:else if contentMatches.length === 0}
          <div class="qo-state">{$t('browser.noMatches')}</div>
        {:else}
          {#each contentMatches as hit, idx (hit.path + ':' + hit.line + ':' + idx)}
            <div
              class="qo-row content"
              class:selected={idx === selectedIdx}
              data-idx={idx}
              role="button"
              tabindex="-1"
              title={hit.path}
              on:click={() => pick(hit.path)}
              on:mousemove={() => { mouseActive = true; selectedIdx = idx; }}
              on:keydown={() => {}}
            >
              <span class="qo-loc">{hit.path}<span class="qo-line">:{hit.line}</span></span>
              <span class="qo-text"
                >{#each contentParts(hit) as seg}<span class:hit={seg.matched}>{seg.text}</span>{/each}</span
              >
            </div>
          {/each}
        {/if}
      </div>

      <div class="qo-foot">
        <div class="qo-notes">
          {#if index && index.truncated}
            <span class="qo-warn">{$t('browser.indexTruncated', { count: candidates.length })}</span>
          {/if}
          {#if index && !index.includedAll && index.skippedDirs && index.skippedDirs.length > 0}
            <!-- Never a silent drop: the skipped directories are named, and
                 including them is one click away. -->
            <span class="qo-note" title={index.skippedDirs.join('\n')}>
              {$t('browser.indexSkipped', { count: index.skippedDirs.length })}
            </span>
            <button class="qo-link" on:click={() => (includeAll = true)}>
              {$t('browser.includeSkipped')}
            </button>
          {/if}
          {#if index && index.includedAll}
            <span class="qo-note">{$t('browser.includingAll')}</span>
            <button class="qo-link" on:click={() => (includeAll = false)}>
              {$t('browser.excludeSkipped')}
            </button>
          {/if}
          {#if mode === 'content' && contentResult && contentResult.truncated}
            <span class="qo-warn">{$t('browser.contentTruncated', { count: contentMatches.length })}</span>
          {/if}
        </div>
        <div class="qo-actions">
          <button class="qo-link" on:click={reload}>{$t('browser.reindex')}</button>
          <span class="qo-hint">{$t('browser.quickOpenHint')}</span>
        </div>
      </div>
    </div>
  </div>
{/if}

<style>
  /* Anchored near the top rather than centred: the list grows downward as the
     query narrows, and a centred box would jump as it resizes. */
  .quickopen-overlay {
    align-items: flex-start;
    padding-top: 8vh;
  }
  .quickopen-overlay:focus {
    outline: none;
  }

  .quickopen {
    width: min(720px, 92vw);
    max-height: 74vh;
    display: flex;
    flex-direction: column;
    background: #16161d;
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 10px;
    box-shadow: 0 18px 48px rgba(0, 0, 0, 0.6);
    overflow: hidden;
  }

  .qo-head {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px 14px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  }

  .qo-input {
    flex: 1;
    min-width: 0;
    padding: 8px 11px;
    border-radius: 7px;
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(0, 0, 0, 0.28);
    color: #e4e4e7;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 13px;
  }
  .qo-input:focus {
    outline: none;
    border-color: rgba(var(--accent-rgb), 0.6);
  }

  .qo-modes {
    display: flex;
    flex-shrink: 0;
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 7px;
    overflow: hidden;
  }
  .qo-mode {
    padding: 6px 11px;
    border: 0;
    background: transparent;
    color: #71717a;
    font-family: inherit;
    font-size: 12px;
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .qo-mode:hover {
    color: #d4d4d8;
  }
  .qo-mode.active {
    background: rgba(var(--accent-rgb), 0.18);
    color: var(--accent);
  }

  .qo-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 6px;
  }

  .qo-row {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 6px 9px;
    border-radius: 6px;
    border: 1px solid transparent;
    cursor: pointer;
  }
  .qo-row.selected {
    background: rgba(var(--accent-rgb), 0.14);
    border-color: rgba(var(--accent-rgb), 0.4);
  }

  .qo-path {
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 13px;
    color: #a1a1aa;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    /* The end of a path (the file name) matters more than its start, so an
       over-long path loses its leading directories rather than its name. */
    direction: rtl;
    text-align: left;
  }

  /* The matched characters are the whole point of a fuzzy list: without them
     it is not obvious why a row is there at all. */
  .qo-path :global(.hit),
  .qo-text :global(.hit) {
    color: var(--accent);
    font-weight: 600;
  }

  .qo-loc {
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 12px;
    color: #71717a;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .qo-line {
    color: var(--accent-light);
  }

  .qo-text {
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 13px;
    color: #d4d4d8;
    white-space: pre;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .qo-state {
    padding: 28px 0;
    text-align: center;
    font-size: 13px;
    color: #71717a;
  }
  .qo-state.error {
    color: #fb7185;
  }

  .qo-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    padding: 8px 14px;
    border-top: 1px solid rgba(255, 255, 255, 0.06);
  }
  .qo-notes,
  .qo-actions {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }
  .qo-note {
    font-size: 11px;
    color: #6b7280;
  }
  .qo-warn {
    font-size: 11px;
    color: #fbbf24;
  }
  .qo-hint {
    font-size: 11px;
    color: #6b7280;
    flex-shrink: 0;
  }
  .qo-link {
    border: 0;
    background: none;
    padding: 0;
    cursor: pointer;
    font-family: inherit;
    font-size: 11px;
    color: var(--accent-light);
    text-decoration: underline;
    flex-shrink: 0;
  }
  .qo-link:hover {
    color: var(--accent-pale);
  }

</style>
