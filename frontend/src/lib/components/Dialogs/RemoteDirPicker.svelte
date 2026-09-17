<script lang="ts">
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import { autoFocusDialog } from '../../utils/dialogActions';
  import * as App from '../../../../wailsjs/go/main/App';
  import type { main } from '../../../../wailsjs/go/models';
  import { createEventDispatcher } from 'svelte';
  import { t } from '../../i18n';

  export let show = false;
  export let serverId = '';
  /** Where to open. Empty starts at the server's home directory. */
  export let startPath = '';

  const dispatch = createEventDispatcher<{ chosen: string }>();

  let listing: main.RemoteDirListing | null = null;
  let loading = false;
  let error = '';
  let generation = 0;
  // What the user typed. Kept apart from the listing's own path so typing a
  // path does not fight with a listing that arrives a moment later.
  let typedPath = '';

  let lastShow = false;
  $: {
    if (show && !lastShow) {
      typedPath = startPath;
      void open(startPath);
    }
    if (!show && lastShow) {
      generation++;
      listing = null;
      error = '';
    }
    lastShow = show;
  }

  async function open(path: string) {
    if (!serverId) return;
    const current = ++generation;
    loading = true;
    error = '';
    try {
      const result = await App.ListServerDirectory(serverId, path);
      if (!show || current !== generation) return;
      listing = result;
      typedPath = result?.path || path;
    } catch (e) {
      if (!show || current !== generation) return;
      error = String(e);
    } finally {
      if (current === generation) loading = false;
    }
  }

  function choose() {
    // What the user typed wins over what was last listed: someone who typed a
    // path and pressed the button meant that path, even if the listing has not
    // caught up.
    dispatch('chosen', typedPath || listing?.path || '');
    show = false;
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      claimKeyForDialog();
      e.stopPropagation();
      show = false;
    }
  }
</script>

{#if show}
  <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
  <div
    class="dialog-overlay"
    use:autoFocusDialog
    tabindex="-1"
    role="dialog"
    aria-modal="true"
    on:keydown={handleKeydown}
  >
    <div class="dialog-content picker">
      <div class="dialog-header">
        <h2>{$t('servers.pickDirectory')}</h2>
        <button class="close-btn" on:click={() => (show = false)}>×</button>
      </div>

      <div class="dialog-body">
        {#if error}<div class="error-line">{error}</div>{/if}

        <div class="path-row">
          <input
            bind:value={typedPath}
            spellcheck="false"
            on:keydown={e => { if (e.key === 'Enter') open(typedPath); }}
          />
          <button class="btn-secondary small" on:click={() => open(typedPath)}>
            {$t('servers.goToPath')}
          </button>
        </div>

        {#if loading}
          <p class="empty">{$t('common.loading')}</p>
        {:else if listing}
          {@const current = listing}
          <ul class="entries">
            {#if current.parent}
              <li>
                <button class="entry up" on:click={() => open(current.parent)}>
                  <span class="glyph">↰</span>
                  <span>..</span>
                </button>
              </li>
            {/if}
            {#each current.entries || [] as entry (entry.name)}
              <li>
                <button
                  class="entry"
                  class:file={!entry.isDir}
                  disabled={!entry.isDir}
                  on:click={() => open(`${current.path === '/' ? '' : current.path}/${entry.name}`)}
                >
                  <span class="glyph">{entry.isDir ? '▸' : '·'}</span>
                  <span>{entry.name}</span>
                </button>
              </li>
            {/each}
            {#if (current.entries || []).length === 0}
              <li><p class="empty">{$t('servers.emptyDirectory')}</p></li>
            {/if}
          </ul>
        {/if}
      </div>

      <div class="dialog-footer">
        <button class="btn-secondary" on:click={() => (show = false)}>{$t('common.cancel')}</button>
        <button class="btn-primary" on:click={choose}>{$t('servers.useThisDirectory')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .picker {
    width: min(560px, 92vw);
    max-height: 76vh;
    display: flex;
    flex-direction: column;
  }

  .dialog-body {
    overflow-y: auto;
    padding: 12px 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .path-row {
    display: flex;
    gap: 8px;
  }

  .path-row input {
    flex: 1;
    background: rgba(0, 0, 0, 0.25);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 6px;
    padding: 7px 9px;
    color: #e4e4e7;
    font-size: 13px;
    font-family: 'JetBrains Mono', 'Menlo', monospace;
  }

  .path-row input:focus {
    outline: none;
    border-color: var(--accent);
  }

  .entries {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .entry {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 9px;
    border: none;
    border-radius: 5px;
    background: transparent;
    color: #d4d4d8;
    font-size: 13px;
    text-align: start;
    cursor: pointer;
  }

  .entry:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.06);
  }

  /* Files are listed so the directory looks like itself, but they are not a
     working directory and cannot be picked. */
  .entry.file {
    color: #71717a;
    cursor: default;
  }

  .glyph {
    width: 12px;
    flex: none;
    color: #8b8b93;
  }

  .dialog-footer {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding: 12px 16px;
    border-top: 1px solid rgba(255, 255, 255, 0.06);
  }

  .btn-primary,
  .btn-secondary {
    padding: 7px 16px;
    border-radius: 7px;
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
  }

  .btn-secondary {
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(255, 255, 255, 0.05);
    color: #a1a1aa;
  }

  .btn-secondary.small {
    padding: 5px 11px;
    font-size: 12px;
    font-weight: 500;
  }

  .btn-primary {
    border: 1px solid var(--accent);
    background: linear-gradient(135deg, var(--accent-dark), var(--accent));
    color: var(--accent-ink);
  }

  .empty {
    text-align: center;
    color: #71717a;
    font-size: 12px;
    padding: 18px 0;
    margin: 0;
  }

  .error-line {
    background: rgba(248, 113, 113, 0.1);
    border: 1px solid rgba(248, 113, 113, 0.3);
    color: #fca5a5;
    border-radius: 6px;
    padding: 8px 10px;
    font-size: 12px;
  }
</style>
