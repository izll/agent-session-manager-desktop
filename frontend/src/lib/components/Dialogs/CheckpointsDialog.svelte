<script lang="ts">
  /**
   * Checkpoints: save the working tree before letting an agent loose on it,
   * and put it back in one click if the agent went the wrong way.
   *
   * The Go side does the work (git_checkpoints.go); this lists what is saved,
   * takes new ones, and asks before anything is overwritten or deleted.
   */
  import { createEventDispatcher, tick } from 'svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import type { main } from '../../../../wailsjs/go/models';
  import { t } from '../../i18n';
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import { autoFocusField, dialogEnterBelongsToControl } from '../../utils/dialogActions';
  import { portal } from '../../utils/portal';
  import { checkpointName, checkpointDifference } from '../../utils/checkpoints';
  import ConfirmDialog from './ConfirmDialog.svelte';

  export let show = false;
  export let projectId = '';
  export let sessionId = '';
  export let windowIdx = 0;
  /** The tab's working directory, as the panel showed it. The backend refuses
   *  to act if the tab has moved elsewhere since. */
  export let root = '';
  /** Whether an agent in this session looks busy, for the restore warning. */
  export let agentBusy = false;

  const dispatch = createEventDispatcher<{ close: void }>();

  let checkpoints: main.Checkpoint[] = [];
  let repositoryRoot = '';
  let label = '';
  let loading = false;
  /** One write at a time: a second click while a restore runs would restore
   *  over a tree that is still being written. */
  let working = false;
  let error = '';
  let notice = '';

  let pendingRestore: main.Checkpoint | null = null;
  let pendingDelete: main.Checkpoint | null = null;
  let showRestoreConfirm = false;
  let showDeleteConfirm = false;

  // Any answer arriving for an earlier opening, or for another tab, is
  // dropped: it describes a repository the dialog is no longer showing.
  let generation = 0;
  let loadedKey = '';
  $: targetKey = `${projectId}\x1f${sessionId}\x1f${windowIdx}\x1f${root}`;

  $: if (show && targetKey !== loadedKey) {
    loadedKey = targetKey;
    label = '';
    error = '';
    notice = '';
    checkpoints = [];
    repositoryRoot = '';
    void refresh();
  } else if (!show && loadedKey) {
    loadedKey = '';
    generation++;
  }

  async function refresh() {
    const mine = ++generation;
    loading = true;
    try {
      const list = await App.ListCheckpoints(sessionId, windowIdx, root);
      if (mine !== generation) return;
      checkpoints = list?.checkpoints ?? [];
      repositoryRoot = list?.root ?? '';
    } catch (e: any) {
      if (mine !== generation) return;
      error = String(e?.message ?? e);
    } finally {
      if (mine === generation) loading = false;
    }
  }

  // A reactive value rather than a helper reading $t inside: markup calling
  // such a helper would not re-render when the language changes.
  $: nameTexts = {
    untitled: $t('checkpoints.untitled'),
    beforeRestore: $t('checkpoints.beforeRestore'),
  };

  /** Dates are formatted here rather than in Go: the interface knows the
   *  user's locale and the backend does not. */
  function when(iso: string): string {
    if (!iso) return '';
    const date = new Date(iso);
    return Number.isNaN(date.getTime()) ? iso : date.toLocaleString();
  }

  async function run(action: () => Promise<string>) {
    if (working) return;
    const mine = generation;
    working = true;
    error = '';
    notice = '';
    try {
      const message = await action();
      if (mine !== generation) return;
      notice = message;
    } catch (e: any) {
      if (mine !== generation) return;
      error = String(e?.message ?? e);
    } finally {
      working = false;
    }
    if (mine === generation) await refresh();
  }

  function create() {
    void run(async () => {
      await App.CreateCheckpoint(sessionId, windowIdx, root, label.trim(), projectId);
      label = '';
      return $t('checkpoints.created');
    });
  }

  let overlayEl: HTMLDivElement | null = null;

  /**
   * Back into this dialog once a confirmation closes. The confirmation took
   * focus into an overlay of its own, and when that goes focus would fall to
   * the page — where Escape no longer reaches this dialog's handler.
   */
  async function refocus() {
    await tick();
    if (!show || !overlayEl) return;
    const field = overlayEl.querySelector<HTMLInputElement>('.label-input');
    (field && !field.disabled ? field : overlayEl).focus();
  }

  function askRestore(checkpoint: main.Checkpoint) {
    pendingRestore = checkpoint;
    showRestoreConfirm = true;
  }

  function askDelete(checkpoint: main.Checkpoint) {
    pendingDelete = checkpoint;
    showDeleteConfirm = true;
  }

  function confirmRestore() {
    const target = pendingRestore;
    pendingRestore = null;
    void refocus();
    if (!target) return;
    void run(async () => {
      const result = await App.RestoreCheckpoint(sessionId, windowIdx, root, target.id, projectId);
      return $t('checkpoints.restored', {
        name: checkpointName(target, nameTexts),
        written: result?.written ?? 0,
        removed: result?.removed ?? 0,
      });
    });
  }

  function confirmDelete() {
    const target = pendingDelete;
    pendingDelete = null;
    void refocus();
    if (!target) return;
    void run(async () => {
      await App.DeleteCheckpoint(sessionId, windowIdx, root, target.id, projectId);
      return $t('checkpoints.deleted');
    });
  }

  $: restoreMessage = pendingRestore
    ? [
        $t('checkpoints.restoreMessage', { name: checkpointName(pendingRestore, nameTexts) }),
        agentBusy ? $t('checkpoints.restoreAgentBusy') : $t('checkpoints.restoreStopAgent'),
      ].join('\n\n')
    : '';

  function close() {
    show = false;
    dispatch('close');
  }

  function handleKeydown(e: KeyboardEvent) {
    // The confirmations are overlays of their own, portalled beside this one,
    // so their keys never arrive here; this handles only the dialog itself.
    e.stopPropagation();
    if (e.key === 'Escape') {
      claimKeyForDialog();
      close();
    } else if (e.key === 'Enter' && !dialogEnterBelongsToControl(e)) {
      // Enter in the label field takes the checkpoint: typing a label and
      // pressing Enter is the whole of the common case.
      if ((e.target as HTMLElement | null)?.classList?.contains('label-input')) {
        claimKeyForDialog();
        e.preventDefault();
        create();
      }
    }
  }
</script>

{#if show}
  <div
    class="dialog-overlay" use:portal use:autoFocusField
    bind:this={overlayEl}
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
    aria-labelledby="checkpoints-title"
    tabindex="-1"
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2 id="checkpoints-title">{$t('checkpoints.title')}</h2>
        <button class="close-btn" on:click={close} aria-label={$t('common.close')} title={$t('common.close')}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/>
            <line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>

      <div class="dialog-body">
        <p class="intro">{$t('checkpoints.intro')}</p>
        {#if repositoryRoot}
          <p class="root" title={repositoryRoot}>{$t('checkpoints.repository', { path: repositoryRoot })}</p>
        {/if}

        <div class="create-row">
          <input
            class="text-input label-input"
            type="text"
            bind:value={label}
            maxlength="120"
            spellcheck="false"
            placeholder={$t('checkpoints.labelPlaceholder')}
            aria-label={$t('checkpoints.labelPlaceholder')}
            disabled={working}
          />
          <button class="btn-primary" on:click={create} disabled={working}>
            {working ? $t('checkpoints.working') : $t('checkpoints.create')}
          </button>
        </div>

        {#if error}
          <p class="message error" role="alert">{error}</p>
        {:else if notice}
          <p class="message notice" role="status">{notice}</p>
        {/if}

        <div class="list" aria-busy={loading}>
          {#if loading && checkpoints.length === 0}
            <p class="empty">{$t('common.loading')}</p>
          {:else if checkpoints.length === 0 && !error}
            <p class="empty">{$t('checkpoints.empty')}</p>
          {:else}
            {#each checkpoints as checkpoint (checkpoint.id)}
              {@const difference = checkpointDifference(checkpoint)}
              <div class="row">
                <div class="info">
                  <div class="name">
                    {#if checkpoint.kind === 'beforeRestore'}
                      <span class="kind" title={$t('checkpoints.beforeRestoreHint')}>↺</span>
                    {/if}
                    <span class="name-text" title={checkpointName(checkpoint, nameTexts)}>{checkpointName(checkpoint, nameTexts)}</span>
                  </div>
                  <div class="meta">
                    <span>{when(checkpoint.created)}</span>
                    {#if checkpoint.session}
                      <span class="sep">·</span><span class="session">{checkpoint.session}</span>
                    {/if}
                    <span class="sep">·</span>
                    {#if difference === 'same'}
                      <span class="same">{$t('checkpoints.sameAsNow')}</span>
                    {:else if difference === 'differs'}
                      <span>{$t('checkpoints.differs', { files: checkpoint.files })}</span>
                      <span class="ins">+{checkpoint.insertions}</span>
                      <span class="del">−{checkpoint.deletions}</span>
                    {:else}
                      <span>{$t('checkpoints.differenceUnknown')}</span>
                    {/if}
                  </div>
                </div>
                <div class="actions">
                  <button
                    class="row-btn"
                    on:click={() => askRestore(checkpoint)}
                    disabled={working}
                    title={$t('checkpoints.restoreHint')}
                  >{$t('checkpoints.restore')}</button>
                  <button
                    class="row-btn danger"
                    on:click={() => askDelete(checkpoint)}
                    disabled={working}
                  >{$t('common.delete')}</button>
                </div>
              </div>
            {/each}
          {/if}
        </div>
      </div>

      <div class="dialog-actions">
        <button class="btn-cancel" on:click={close}>{$t('common.close')}</button>
      </div>
    </div>
  </div>
{/if}

<ConfirmDialog
  bind:show={showRestoreConfirm}
  title={$t('checkpoints.restoreTitle')}
  message={restoreMessage}
  confirmText={$t('checkpoints.restore')}
  cancelText={$t('common.cancel')}
  variant="warning"
  on:confirm={confirmRestore}
  on:cancel={() => { pendingRestore = null; void refocus(); }}
/>

<ConfirmDialog
  bind:show={showDeleteConfirm}
  title={$t('checkpoints.deleteTitle')}
  message={pendingDelete ? $t('checkpoints.deleteMessage', { name: checkpointName(pendingDelete, nameTexts) }) : ''}
  confirmText={$t('common.delete')}
  cancelText={$t('common.cancel')}
  variant="danger"
  on:confirm={confirmDelete}
  on:cancel={() => { pendingDelete = null; void refocus(); }}
/>

<style>
  .dialog-content {
    width: min(620px, 94vw);
    max-width: min(620px, 94vw);
  }

  .dialog-body {
    padding: 18px 24px;
    gap: 12px;
  }

  .intro {
    margin: 0;
    font-size: 13px;
    line-height: 1.5;
    color: #9ca3af;
  }

  .root {
    margin: 0;
    font-family: monospace;
    font-size: 12px;
    color: #6b7280;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .create-row {
    display: flex;
    gap: 8px;
    align-items: stretch;
  }

  .create-row .text-input {
    flex: 1;
    min-width: 0;
    padding: 9px 12px;
  }

  .create-row .btn-primary {
    white-space: nowrap;
  }

  .message {
    margin: 0;
    padding: 8px 12px;
    border-radius: 8px;
    font-size: 13px;
    white-space: pre-line;
  }

  .message.error {
    background: rgba(239, 68, 68, 0.12);
    color: #fca5a5;
  }

  .message.notice {
    background: rgba(34, 197, 94, 0.1);
    color: #86efac;
  }

  .list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .empty {
    margin: 12px 0;
    text-align: center;
    font-size: 13px;
    color: #6b7280;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 12px;
    border-radius: 10px;
    background: rgba(0, 0, 0, 0.2);
    border: 1px solid rgba(255, 255, 255, 0.05);
  }

  .info {
    flex: 1;
    min-width: 0;
  }

  .name {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 14px;
    font-weight: 500;
    color: #e4e4e7;
  }

  .name-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .kind {
    color: var(--accent-light);
  }

  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-top: 2px;
    font-size: 12px;
    color: #8b8b95;
  }

  .sep {
    opacity: 0.6;
  }

  .same {
    color: #86efac;
  }

  .ins {
    color: #86efac;
  }

  .del {
    color: #fca5a5;
  }

  .actions {
    display: flex;
    gap: 6px;
    flex-shrink: 0;
  }

  .row-btn {
    padding: 5px 10px;
    border-radius: 6px;
    border: 1px solid rgba(var(--accent-rgb), 0.25);
    background: rgba(var(--accent-rgb), 0.08);
    color: #d4d4d8;
    font-size: 12px;
    cursor: pointer;
  }

  .row-btn:hover:not(:disabled) {
    background: rgba(var(--accent-rgb), 0.18);
    color: white;
  }

  .row-btn.danger {
    border-color: rgba(239, 68, 68, 0.25);
    background: rgba(239, 68, 68, 0.06);
  }

  .row-btn.danger:hover:not(:disabled) {
    background: rgba(239, 68, 68, 0.16);
    color: #fecaca;
  }

  .row-btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .dialog-actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
  }
</style>
