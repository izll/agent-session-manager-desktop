<script lang="ts">
  import { createEventDispatcher, onMount } from 'svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import { t } from '../../i18n';

  interface HistoryEntry {
    agent: string;
    content: string;
    sessionFile: string;
    sessionId: string;
    score: number;
  }

  export let show = false;

  const dispatch = createEventDispatcher();

  let query = '';
  let results: HistoryEntry[] = [];
  let loading = false;
  let error = '';
  let searchTimeout: ReturnType<typeof setTimeout> | null = null;
  let selectedEntry: HistoryEntry | null = null;
  let preview = '';
  let previewLoading = false;
  let searchInput: HTMLInputElement;
  let previewContainer: HTMLElement;
  let matchCount = 0;
  let currentMatchIndex = 0;
  let isFullscreen = false;

  $: if (show && searchInput) {
    setTimeout(() => searchInput?.focus(), 100);
  }

  function close() {
    show = false;
    query = '';
    results = [];
    error = '';
    selectedEntry = null;
    preview = '';
    dispatch('close');
  }

  function handleQueryChange() {
    if (searchTimeout) {
      clearTimeout(searchTimeout);
    }
    selectedEntry = null;
    preview = '';

    if (!query.trim()) {
      results = [];
      return;
    }

    searchTimeout = setTimeout(doSearch, 300);
  }

  async function doSearch() {
    if (!query.trim()) return;

    loading = true;
    error = '';

    try {
      results = await App.GlobalSearch(query);
    } catch (e) {
      error = String(e);
      results = [];
    }
    loading = false;
  }

  async function selectEntry(entry: HistoryEntry) {
    selectedEntry = entry;
    previewLoading = true;
    preview = '';

    try {
      preview = await App.GetHistoryPreview(entry);
    } catch (e) {
      preview = `Error loading preview: ${e}`;
    }
    previewLoading = false;
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      if (isFullscreen) {
        isFullscreen = false;
      } else if (selectedEntry) {
        selectedEntry = null;
        preview = '';
      } else {
        close();
      }
    }
    // F11 for fullscreen toggle
    if (e.key === 'F11') {
      e.preventDefault();
      isFullscreen = !isFullscreen;
    }
    // F3 / Shift+F3 or Ctrl+G / Ctrl+Shift+G for match navigation
    if (e.key === 'F3' || (e.ctrlKey && e.key === 'g')) {
      e.preventDefault();
      if (e.shiftKey) {
        prevMatch();
      } else {
        nextMatch();
      }
    }
    // Enter in preview to go to next match
    if (e.key === 'Enter' && selectedEntry && matchCount > 0) {
      e.preventDefault();
      if (e.shiftKey) {
        prevMatch();
      } else {
        nextMatch();
      }
    }
  }

  function getAgentIcon(agent: string): string {
    const icons: Record<string, string> = {
      claude: '🤖',
      gemini: '💎',
      aider: '🔧',
      codex: '📦',
      amazonq: '🦜',
      opencode: '💻',
      terminal: '🖥️',
    };
    return icons[agent] || '❓';
  }

  function getAgentColor(agent: string): string {
    const colors: Record<string, string> = {
      claude: '#a78bfa',
      gemini: '#60a5fa',
      aider: '#4ade80',
      codex: '#fbbf24',
      amazonq: '#f87171',
      opencode: '#22d3ee',
      terminal: '#9ca3af',
    };
    return colors[agent] || '#9ca3af';
  }

  function truncate(text: string, length: number): string {
    if (text.length <= length) return text;
    return text.substring(0, length) + '...';
  }

  function highlightAndScroll() {
    if (!previewContainer || !query || !preview) return;

    // Wait for DOM update
    setTimeout(() => {
      const marks = previewContainer.querySelectorAll('mark');
      matchCount = marks.length;
      currentMatchIndex = 0;

      // Update active state and scroll to first
      updateActiveMatch();
    }, 100);
  }

  function updateActiveMatch() {
    if (!previewContainer) return;

    const marks = previewContainer.querySelectorAll('mark');
    marks.forEach((mark, i) => {
      mark.classList.toggle('active', i === currentMatchIndex);
    });

    const activeMark = marks[currentMatchIndex];
    if (activeMark) {
      activeMark.scrollIntoView({ behavior: 'smooth', block: 'center' });
    }
  }

  function nextMatch() {
    if (matchCount === 0) return;
    currentMatchIndex = (currentMatchIndex + 1) % matchCount;
    updateActiveMatch();
  }

  function prevMatch() {
    if (matchCount === 0) return;
    currentMatchIndex = (currentMatchIndex - 1 + matchCount) % matchCount;
    updateActiveMatch();
  }

  function highlightText(text: string, searchQuery: string): string {
    if (!searchQuery.trim()) return escapeHtml(text);

    const escaped = escapeHtml(text);
    const searchTerms = searchQuery.toLowerCase().split(/\s+/).filter(t => t.length > 0);

    let result = escaped;
    for (const term of searchTerms) {
      const regex = new RegExp(`(${escapeRegex(term)})`, 'gi');
      result = result.replace(regex, '<mark>$1</mark>');
    }
    return result;
  }

  function escapeHtml(text: string): string {
    return text
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#039;');
  }

  function escapeRegex(text: string): string {
    return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  }

  $: if (preview && query && previewContainer) {
    highlightAndScroll();
  }
</script>

{#if show}
  <div
    class="dialog-overlay"
    class:fullscreen={isFullscreen}
    on:click|self={close}
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
  >
    <div class="dialog-content" class:has-preview={selectedEntry} class:fullscreen={isFullscreen}>
      <!-- Search Header -->
      <div class="search-header">
        <div class="search-input-wrapper">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="11" cy="11" r="8"/>
            <path d="M21 21l-4.35-4.35"/>
          </svg>
          <input
            type="text"
            placeholder={$t('search.placeholder')}
            bind:value={query}
            bind:this={searchInput}
            on:input={handleQueryChange}
            class="search-input"
          />
          {#if query}
            <button class="clear-btn" on:click={() => { query = ''; results = []; }}>
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <line x1="18" y1="6" x2="6" y2="18"/>
                <line x1="6" y1="6" x2="18" y2="18"/>
              </svg>
            </button>
          {/if}
        </div>
        <button class="fullscreen-btn" on:click={() => isFullscreen = !isFullscreen} title={isFullscreen ? $t('search.exitFullscreen') : $t('search.fullscreen')}>
          {#if isFullscreen}
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M8 3v3a2 2 0 0 1-2 2H3m18 0h-3a2 2 0 0 1-2-2V3m0 18v-3a2 2 0 0 1 2-2h3M3 16h3a2 2 0 0 1 2 2v3"/>
            </svg>
          {:else}
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M8 3H5a2 2 0 0 0-2 2v3m18 0V5a2 2 0 0 0-2-2h-3m0 18h3a2 2 0 0 0 2-2v-3M3 16v3a2 2 0 0 0 2 2h3"/>
            </svg>
          {/if}
        </button>
        <button class="close-btn" on:click={close}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/>
            <line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>

      <!-- Content -->
      <div class="dialog-body">
        <!-- Results List -->
        <div class="results-panel" class:narrow={selectedEntry}>
          {#if loading}
            <div class="loading-state">
              <svg class="spinner" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83"/>
              </svg>
              <span>{$t('search.searching')}</span>
            </div>
          {:else if error}
            <div class="error-state">
              <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <circle cx="12" cy="12" r="10"/>
                <line x1="12" y1="8" x2="12" y2="12"/>
                <line x1="12" y1="16" x2="12.01" y2="16"/>
              </svg>
              <span>{error}</span>
            </div>
          {:else if !query}
            <div class="empty-state">
              <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
                <circle cx="11" cy="11" r="8"/>
                <path d="M21 21l-4.35-4.35"/>
              </svg>
              <span>{$t('search.noQuery')}</span>
              <span class="hint">{$t('search.noQueryHint')}</span>
            </div>
          {:else if results.length === 0}
            <div class="empty-state">
              <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
                <circle cx="12" cy="12" r="10"/>
                <path d="M16 16s-1.5-2-4-2-4 2-4 2"/>
                <line x1="9" y1="9" x2="9.01" y2="9"/>
                <line x1="15" y1="9" x2="15.01" y2="9"/>
              </svg>
              <span>{$t('search.noResults')}</span>
              <span class="hint">{$t('search.noResultsHint')}</span>
            </div>
          {:else}
            <div class="results-list">
              {#each results as entry}
                <button
                  class="result-item"
                  class:selected={selectedEntry === entry}
                  on:click={() => selectEntry(entry)}
                >
                  <span class="agent-icon" style="color: {getAgentColor(entry.agent)}">
                    {getAgentIcon(entry.agent)}
                  </span>
                  <div class="result-content">
                    <span class="result-text">{truncate(entry.content, 100)}</span>
                    <span class="result-meta">
                      <span class="agent-name">{entry.agent}</span>
                      <span class="score">{$t('search.score', { score: entry.score })}</span>
                    </span>
                  </div>
                </button>
              {/each}
            </div>
          {/if}
        </div>

        <!-- Preview Panel -->
        {#if selectedEntry}
          <div class="preview-panel">
            <div class="preview-header">
              <span class="preview-title">
                <span style="color: {getAgentColor(selectedEntry.agent)}">
                  {getAgentIcon(selectedEntry.agent)}
                </span>
                {$t('search.conversationPreview')}
              </span>
              <div class="preview-nav">
                {#if matchCount > 0}
                  <span class="match-counter">{currentMatchIndex + 1} / {matchCount}</span>
                  <button class="nav-btn" on:click={prevMatch} title={$t('search.prevMatch')}>
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <polyline points="18 15 12 9 6 15"/>
                    </svg>
                  </button>
                  <button class="nav-btn" on:click={nextMatch} title={$t('search.nextMatch')}>
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <polyline points="6 9 12 15 18 9"/>
                    </svg>
                  </button>
                {/if}
                <button class="close-preview" on:click={() => { selectedEntry = null; preview = ''; }}>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <line x1="18" y1="6" x2="6" y2="18"/>
                    <line x1="6" y1="6" x2="18" y2="18"/>
                  </svg>
                </button>
              </div>
            </div>
            <div class="preview-content" bind:this={previewContainer}>
              {#if previewLoading}
                <div class="loading-state">
                  <svg class="spinner" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83"/>
                  </svg>
                  <span>{$t('search.loadingPreview')}</span>
                </div>
              {:else}
                <pre class="preview-text">{@html highlightText(preview, query)}</pre>
              {/if}
            </div>
          </div>
        {/if}
      </div>

      <!-- Footer -->
      <div class="dialog-footer">
        <span class="result-count">
          {#if results.length > 0}
            {results.length === 1 ? $t('search.resultCount', { count: results.length }) : $t('search.resultCountPlural', { count: results.length })}
          {:else}
            {$t('search.pressToSearch')}
          {/if}
        </span>
        <span class="keyboard-hint">
          {#if selectedEntry && matchCount > 0}
            <kbd>F3</kbd> next · <kbd>⇧F3</kbd> prev ·
          {/if}
          <kbd>F11</kbd> fullscreen · <kbd>ESC</kbd> close
        </span>
      </div>
    </div>
  </div>
{/if}

<style>
  /* Override global dialog-overlay for search dialog positioning */
  .dialog-overlay {
    align-items: flex-start;
    padding-top: 10vh;
    transition: padding 0.2s ease;
  }

  .dialog-overlay.fullscreen {
    padding: 20px;
    align-items: center;
  }

  /* Override global dialog-content for search dialog sizing */
  .dialog-content {
    max-width: 600px;
    max-height: 70vh;
    display: flex;
    flex-direction: column;
    transition: all 0.2s ease;
  }

  .dialog-content.has-preview {
    max-width: 1000px;
  }

  .dialog-content.fullscreen {
    max-width: none;
    max-height: none;
    width: calc(100vw - 40px);
    height: calc(100vh - 40px);
    border-radius: 12px;
  }

  .fullscreen-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    background: rgba(255, 255, 255, 0.05);
    border: none;
    border-radius: 8px;
    color: #6b7280;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .fullscreen-btn:hover {
    background: rgba(139, 92, 246, 0.2);
    color: #a78bfa;
  }

  .search-header {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 16px 20px;
    background: linear-gradient(180deg, rgba(139, 92, 246, 0.1) 0%, transparent 100%);
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  }

  .search-input-wrapper {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 16px;
    background: rgba(0, 0, 0, 0.3);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 10px;
    color: #6b7280;
  }

  .search-input-wrapper:focus-within {
    border-color: rgba(139, 92, 246, 0.5);
    box-shadow: 0 0 0 3px rgba(139, 92, 246, 0.1);
  }

  .search-input {
    flex: 1;
    background: transparent;
    border: none;
    font-size: 15px;
    color: white;
    outline: none;
  }

  .search-input::placeholder {
    color: #4b5563;
  }

  .clear-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: none;
    color: #6b7280;
    cursor: pointer;
    padding: 4px;
    border-radius: 4px;
    transition: all 0.2s ease;
  }

  .clear-btn:hover {
    color: white;
    background: rgba(255, 255, 255, 0.1);
  }

  /* Override global dialog-body for search dialog layout */
  .dialog-body {
    flex: 1;
    display: flex;
    overflow: hidden;
    padding: 0;
  }

  .results-panel {
    flex: 1;
    overflow-y: auto;
    min-width: 300px;
    transition: flex 0.3s ease;
  }

  .results-panel.narrow {
    flex: 0 0 350px;
    border-right: 1px solid rgba(255, 255, 255, 0.05);
  }

  .preview-panel {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .preview-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 16px;
    background: rgba(0, 0, 0, 0.2);
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  }

  .preview-title {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    font-weight: 600;
    color: #9ca3af;
  }

  .close-preview {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    background: transparent;
    border: none;
    color: #6b7280;
    cursor: pointer;
    border-radius: 4px;
    transition: all 0.2s ease;
  }

  .close-preview:hover {
    color: white;
    background: rgba(255, 255, 255, 0.1);
  }

  .preview-content {
    flex: 1;
    overflow: auto;
    padding: 16px;
  }

  .preview-text {
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 12px;
    line-height: 1.6;
    color: #e4e4e7;
    white-space: pre-wrap;
    word-wrap: break-word;
    margin: 0;
  }

  .preview-text :global(mark) {
    background: rgba(251, 191, 36, 0.3);
    color: #fbbf24;
    padding: 1px 2px;
    border-radius: 2px;
    transition: all 0.15s ease;
  }

  .preview-text :global(mark.active) {
    background: rgba(251, 191, 36, 0.7);
    color: #000;
    box-shadow: 0 0 0 2px rgba(251, 191, 36, 0.5);
  }

  .preview-nav {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .match-counter {
    font-size: 12px;
    color: #9ca3af;
    padding: 0 8px;
    font-variant-numeric: tabular-nums;
  }

  .nav-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 4px;
    color: #9ca3af;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .nav-btn:hover {
    background: rgba(139, 92, 246, 0.2);
    border-color: rgba(139, 92, 246, 0.3);
    color: #a78bfa;
  }

  .loading-state, .error-state, .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 48px 24px;
    color: #6b7280;
    gap: 12px;
    text-align: center;
  }

  .error-state {
    color: #f87171;
  }

  .hint {
    font-size: 12px;
    opacity: 0.7;
  }

  .spinner {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }

  .results-list {
    padding: 8px;
  }

  .result-item {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    width: 100%;
    padding: 12px;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 10px;
    text-align: left;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .result-item:hover {
    background: rgba(255, 255, 255, 0.05);
    border-color: rgba(255, 255, 255, 0.1);
  }

  .result-item.selected {
    background: linear-gradient(135deg, rgba(139, 92, 246, 0.2) 0%, rgba(99, 102, 241, 0.15) 100%);
    border-color: rgba(139, 92, 246, 0.3);
  }

  .agent-icon {
    font-size: 20px;
    flex-shrink: 0;
  }

  .result-content {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .result-text {
    font-size: 13px;
    color: #e4e4e7;
    line-height: 1.4;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .result-meta {
    display: flex;
    align-items: center;
    gap: 12px;
    font-size: 11px;
  }

  .agent-name {
    color: #6b7280;
    text-transform: capitalize;
  }

  .score {
    color: #4b5563;
  }

  /* Override global dialog-footer for search dialog layout */
  .dialog-footer {
    justify-content: space-between;
    padding: 10px 20px;
    font-size: 11px;
    color: #4b5563;
  }

  .result-count {
    color: #6b7280;
  }

  .keyboard-hint {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .keyboard-hint kbd {
    padding: 2px 6px;
    background: rgba(255, 255, 255, 0.1);
    border: 1px solid rgba(255, 255, 255, 0.15);
    border-radius: 4px;
    font-family: inherit;
    font-size: 10px;
    color: #9ca3af;
  }

  /* Scrollbar styles */
  .results-panel::-webkit-scrollbar,
  .preview-content::-webkit-scrollbar {
    width: 6px;
  }

  .results-panel::-webkit-scrollbar-track,
  .preview-content::-webkit-scrollbar-track {
    background: transparent;
  }

  .results-panel::-webkit-scrollbar-thumb,
  .preview-content::-webkit-scrollbar-thumb {
    background: rgba(139, 92, 246, 0.3);
    border-radius: 3px;
  }
</style>
