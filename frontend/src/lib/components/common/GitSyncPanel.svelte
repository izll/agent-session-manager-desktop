<script lang="ts">
  import { createEventDispatcher, onMount, tick } from 'svelte';
  import { t } from '../../i18n';
  import Select from './Select.svelte';
  import {
    currentGitTarget,
    getGitSyncPreview,
    runGitPush,
    runGitPull,
    cancelGitSync,
    revalidateGitBranch,
    type GitRepositoryTarget,
    type GitSyncPreview,
    type GitSyncResult
  } from '../../stores/gitBranch';
  import { canRunSync, chosenRemote, outcomeKey, type GitSyncDirection } from '../../utils/gitSync';
  import { describeBackendError } from '../../utils/backendError';
  import { portal } from '../../utils/portal';

  /** Push lists what is only here; pull lists what is only upstream. */
  export let direction: GitSyncDirection;
  /** The count pill the panel hangs from. */
  export let anchor: HTMLElement | null = null;

  const dispatch = createEventDispatcher<{ close: void }>();

  let panelRef: HTMLDivElement;
  /** Captured once: the panel acts on the tab it was opened for, even if the
   *  badge moves on while a push is running. */
  let target: GitRepositoryTarget | null = null;
  let preview: GitSyncPreview | null = null;
  let loading = true;
  let loadError = '';
  let pickedRemote = '';
  /** Bound by the badge, which keeps a running push's panel open even when
   *  the tab under it changes — otherwise its result would never be seen. */
  export let busy = false;
  let result: GitSyncResult | null = null;
  let runError = '';

  $: remote = chosenRemote(preview, pickedRemote);
  $: runnable = canRunSync(preview, pickedRemote, busy);
  $: remoteOptions = (preview?.remotes || []).map((name) => ({ value: name, label: name }));
  $: title = preview
    ? $t(direction === 'push' ? 'gitSync.pushTitle' : 'gitSync.pullTitle', { branch: preview.branch })
    : $t(direction === 'push' ? 'gitSync.push' : 'gitSync.pull');
  $: actionLabel = direction === 'pull'
    ? $t('gitSync.pull')
    : preview?.setUpstream ? $t('gitSync.pushSetUpstream') : $t('gitSync.push');

  async function load() {
    loading = true;
    loadError = '';
    result = null;
    runError = '';
    try {
      target = currentGitTarget();
      if (!target) throw new Error('no repository');
      preview = await getGitSyncPreview(target, direction);
      pickedRemote = '';
    } catch (e) {
      preview = null;
      loadError = describeBackendError(e);
    } finally {
      loading = false;
    }
  }

  // Only on an explicit click: a push publishes work and a pull rewrites the
  // working tree, so neither may ever follow from opening the panel.
  async function run() {
    if (!target || !preview || !runnable) return;
    busy = true;
    result = null;
    runError = '';
    try {
      result = direction === 'push'
        ? await runGitPush(target, preview, remote)
        : await runGitPull(target, preview);
    } catch (e) {
      runError = describeBackendError(e);
    } finally {
      busy = false;
    }
    // Refreshed whatever happened: a failed push may still have fetched, and
    // a rejected one means the ↓ count is about to matter.
    void revalidateGitBranch();
  }

  function close() {
    // A running push is not abandoned by looking away; it is cancelled
    // explicitly or left to finish.
    if (busy) return;
    dispatch('close');
  }

  function position() {
    // After a push the ↑ pill disappears; the panel stays where it was to
    // show the result instead of jumping to the corner of the window.
    if (!anchor || !anchor.isConnected || !panelRef) return;
    const rect = anchor.getBoundingClientRect();
    const gap = 6;
    const height = panelRef.offsetHeight;
    const openUpwards = rect.bottom + gap + height > window.innerHeight;
    panelRef.style.top = openUpwards
      ? `${Math.max(gap, rect.top - gap - height)}px`
      : `${rect.bottom + gap}px`;
    const left = Math.min(rect.left, window.innerWidth - panelRef.offsetWidth - gap);
    panelRef.style.left = `${Math.max(gap, left)}px`;
  }

  // Re-measured whenever the content changes size, so a panel opening upwards
  // stays attached to the pill rather than to its loading placeholder.
  let lastSignature = '';
  $: {
    const signature = `${loading}|${preview?.commits?.length}|${busy}|${!!result}|${runError}|${loadError}|${remote}`;
    if (signature !== lastSignature) {
      lastSignature = signature;
      tick().then(position);
    }
  }

  function handleWindowClick(e: MouseEvent) {
    const node = e.target as Node;
    if (panelRef?.contains(node) || anchor?.contains(node)) return;
    // The remote picker's list is portaled to the body, outside the panel.
    if ((node as Element)?.closest?.('.select-dropdown')) return;
    close();
  }

  function handleWindowKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape') return;
    // An open remote list takes Escape for itself.
    if (document.querySelector('.select-dropdown')) return;
    e.stopPropagation();
    close();
  }

  onMount(() => {
    void load();
  });
</script>

<svelte:window on:click={handleWindowClick} on:keydown={handleWindowKeydown} on:resize={close} />

<div class="git-sync-panel" bind:this={panelRef} use:portal role="dialog" aria-label={title}>
  <div class="git-sync-head">
    <span class="git-sync-title" title={title}>{title}</span>
    {#if preview?.target}
      <span class="git-sync-target">
        {$t(direction === 'push' ? 'gitSync.to' : 'gitSync.from', { target: preview.target })}
      </span>
    {/if}
  </div>

  {#if loading}
    <div class="git-sync-note">{$t('gitSync.loading')}</div>
  {:else if loadError}
    <div class="git-sync-note error">{loadError}</div>
  {:else if preview}
    {#if preview.commits.length === 0}
      <div class="git-sync-note">
        {$t(direction === 'push' ? 'gitSync.nothingToPush' : 'gitSync.nothingToPull')}
      </div>
    {:else}
      <ul class="git-sync-commits">
        {#each preview.commits as commit (commit.hash)}
          <li title={commit.subject}>
            <span class="git-sync-hash">{commit.shortHash}</span>
            <span class="git-sync-subject">{commit.subject}</span>
          </li>
        {/each}
      </ul>
      {#if preview.truncated}
        <div class="git-sync-note">{$t('gitSync.more', { count: preview.total - preview.commits.length })}</div>
      {/if}
    {/if}

    {#if direction === 'push' && preview.setUpstream}
      <div class="git-sync-info">{$t('gitSync.noUpstream')}</div>
      {#if preview.remotes.length > 1}
        <div class="git-sync-remote">
          <span>{$t('gitSync.remote')}</span>
          <Select
            small
            value={remote}
            options={remoteOptions}
            placeholder={$t('gitSync.chooseRemote')}
            on:change={(e) => (pickedRemote = e.detail)}
          />
        </div>
      {:else if remote}
        <div class="git-sync-remote"><span>{$t('gitSync.remote')}</span><code>{remote}</code></div>
      {/if}
    {/if}

    {#if direction === 'pull'}
      {#if preview.diverged}
        <div class="git-sync-info warn">{$t('gitSync.diverged', { target: preview.target })}</div>
      {:else}
        <div class="git-sync-info">{$t('gitSync.ffOnly')}</div>
      {/if}
    {/if}
  {/if}

  {#if result}
    <div class="git-sync-result" class:ok={result.ok} class:error={!result.ok}>
      {$t(outcomeKey(result.outcome))}
    </div>
    {#if result.message}
      <pre class="git-sync-output">{result.message}</pre>
    {/if}
  {:else if runError}
    <div class="git-sync-result error">{runError}</div>
  {/if}

  <div class="git-sync-actions">
    {#if busy}
      <span class="git-sync-progress">
        <span class="git-sync-spinner" aria-hidden="true"></span>
        {$t(direction === 'push' ? 'gitSync.pushing' : 'gitSync.pulling')}
      </span>
      <button type="button" class="git-sync-button" on:click={() => void cancelGitSync()}>
        {$t('common.cancel')}
      </button>
    {:else if result?.ok}
      <button type="button" class="git-sync-button" on:click={close}>{$t('common.close')}</button>
    {:else}
      {#if result?.outcome === 'changed'}
        <button type="button" class="git-sync-button" on:click={load}>{$t('gitSync.reload')}</button>
      {/if}
      <button type="button" class="git-sync-button" on:click={close}>{$t('common.cancel')}</button>
      {#if preview && result?.outcome !== 'changed'}
        <button type="button" class="git-sync-button primary" disabled={!runnable} on:click={run}>
          {actionLabel}
        </button>
      {/if}
    {/if}
  </div>
</div>

<style>
  /* Portaled to body, so the panel's own rules need :global — the same
     surface as the branch menu it sits beside. */
  :global(.git-sync-panel) {
    position: fixed;
    z-index: 1000;
    width: 380px;
    max-width: calc(100vw - 12px);
    max-height: min(480px, calc(100vh - 12px));
    display: flex;
    flex-direction: column;
    gap: 6px;
    background: var(--bg-raised);
    border: 1px solid rgba(var(--accent-rgb), 0.3);
    border-radius: 8px;
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.5);
    padding: 8px;
    font-size: 13px;
    color: #d4d4d8;
  }

  :global(.git-sync-panel .git-sync-head) {
    display: flex;
    align-items: baseline;
    gap: 8px;
    min-width: 0;
    padding: 2px 4px;
  }

  :global(.git-sync-panel .git-sync-title) {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  :global(.git-sync-panel .git-sync-target) {
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 11px;
    color: #6b7280;
    white-space: nowrap;
  }

  :global(.git-sync-panel .git-sync-commits) {
    list-style: none;
    margin: 0;
    padding: 0;
    overflow-y: auto;
    min-height: 0;
    flex: 1 1 auto;
  }

  :global(.git-sync-panel .git-sync-commits li) {
    display: flex;
    gap: 8px;
    padding: 3px 4px;
    user-select: text;
  }

  :global(.git-sync-panel .git-sync-hash) {
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 11px;
    color: #fcd34d;
    flex-shrink: 0;
    padding-top: 1px;
  }

  :global(.git-sync-panel .git-sync-subject) {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  :global(.git-sync-panel .git-sync-note) {
    padding: 4px;
    font-size: 12px;
    color: #6b7280;
    font-style: italic;
  }

  :global(.git-sync-panel .git-sync-note.error),
  :global(.git-sync-panel .git-sync-result.error) {
    color: #f87171;
    font-style: normal;
  }

  :global(.git-sync-panel .git-sync-info) {
    padding: 4px;
    font-size: 12px;
    color: #9ca3af;
  }

  :global(.git-sync-panel .git-sync-info.warn) {
    color: #fcd34d;
  }

  :global(.git-sync-panel .git-sync-remote) {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 4px;
    font-size: 12px;
    color: #9ca3af;
  }

  :global(.git-sync-panel .git-sync-result) {
    padding: 4px;
    font-size: 12px;
  }

  :global(.git-sync-panel .git-sync-result.ok) {
    color: #4ade80;
  }

  :global(.git-sync-panel .git-sync-output) {
    margin: 0;
    padding: 6px 8px;
    max-height: 140px;
    overflow: auto;
    background: rgba(0, 0, 0, 0.25);
    border-radius: 4px;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 11px;
    color: #9ca3af;
    white-space: pre-wrap;
    word-break: break-word;
    user-select: text;
  }

  :global(.git-sync-panel .git-sync-actions) {
    display: flex;
    justify-content: flex-end;
    align-items: center;
    gap: 6px;
    padding-top: 6px;
    border-top: 1px solid rgba(255, 255, 255, 0.08);
  }

  :global(.git-sync-panel .git-sync-progress) {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin-right: auto;
    font-size: 12px;
    color: #9ca3af;
  }

  :global(.git-sync-panel .git-sync-spinner) {
    width: 12px;
    height: 12px;
    border: 2px solid rgba(255, 255, 255, 0.15);
    border-top-color: var(--accent-light);
    border-radius: 50%;
    animation: git-sync-spin 0.8s linear infinite;
  }

  @keyframes -global-git-sync-spin {
    to { transform: rotate(360deg); }
  }

  :global(.git-sync-panel .git-sync-button) {
    padding: 5px 12px;
    border-radius: 6px;
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: transparent;
    color: inherit;
    font: inherit;
    font-size: 12px;
    cursor: pointer;
  }

  :global(.git-sync-panel .git-sync-button:hover:not(:disabled)) {
    background: rgba(255, 255, 255, 0.07);
  }

  :global(.git-sync-panel .git-sync-button.primary) {
    background: rgba(var(--accent-rgb), 0.85);
    border-color: transparent;
    color: #fff;
  }

  :global(.git-sync-panel .git-sync-button.primary:hover:not(:disabled)) {
    background: rgb(var(--accent-rgb));
  }

  :global(.git-sync-panel .git-sync-button:disabled) {
    opacity: 0.45;
    cursor: default;
  }
</style>
