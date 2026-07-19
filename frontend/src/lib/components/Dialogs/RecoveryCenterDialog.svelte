<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import { loadSessions, selectSession, selectWindow } from '../../stores/sessions';
  import { loadSettings } from '../../stores/settings';
  import { t } from '../../i18n';
  import ConfirmDialog from './ConfirmDialog.svelte';

  export let show = false;
  const dispatch = createEventDispatcher();

  interface TrashItem {
    id: string;
    kind: 'session' | 'tab';
    name: string;
    parentSessionId: string;
    parentSessionName: string;
    deletedAt: string;
  }

  interface BackupItem {
    id: string;
    createdAt: string;
    size: number;
  }

  let activeTab: 'trash' | 'backups' = 'trash';
  let trash: TrashItem[] = [];
  let backups: BackupItem[] = [];
  let loading = false;
  let error = '';
  let loadedForOpen = false;
  let showConfirm = false;
  let confirmTitle = '';
  let confirmMessage = '';
  let pendingAction: (() => Promise<void>) | null = null;

  $: if (show && !loadedForOpen) {
    loadedForOpen = true;
    void loadRecoveryData();
  } else if (!show) {
    loadedForOpen = false;
  }

  async function loadRecoveryData() {
    loading = true;
    error = '';
    try {
      const [trashResult, backupResult] = await Promise.all([
        App.GetTrashItems(),
        App.GetBackups()
      ]);
      trash = (trashResult || []) as TrashItem[];
      backups = (backupResult || []) as BackupItem[];
    } catch (e) {
      error = String(e);
    } finally {
      loading = false;
    }
  }

  function close() {
    show = false;
    dispatch('close');
  }

  async function restoreTrash(item: TrashItem) {
    error = '';
    try {
      const result = await App.RestoreTrashItem(item.id);
      await loadSessions();
      if (result?.sessionId) {
        selectSession(result.sessionId);
        selectWindow(result.windowIdx || 0);
      }
      await loadRecoveryData();
      dispatch('restored');
    } catch (e) {
      error = String(e);
    }
  }

  function requestPermanentDelete(item: TrashItem) {
    confirmTitle = $t('recovery.permanentDeleteTitle');
    confirmMessage = $t('recovery.permanentDeleteMessage', { name: item.name });
    pendingAction = async () => {
      await App.PermanentlyDeleteTrashItem(item.id);
      await loadRecoveryData();
    };
    showConfirm = true;
  }

  function requestEmptyTrash() {
    confirmTitle = $t('recovery.emptyTrashTitle');
    confirmMessage = $t('recovery.emptyTrashMessage');
    pendingAction = async () => {
      await App.EmptyTrash();
      await loadRecoveryData();
    };
    showConfirm = true;
  }

  async function createBackup() {
    error = '';
    try {
      await App.CreateBackup();
      await loadRecoveryData();
    } catch (e) {
      error = String(e);
    }
  }

  function requestRestoreBackup(item: BackupItem) {
    confirmTitle = $t('recovery.restoreBackupTitle');
    confirmMessage = $t('recovery.restoreBackupMessage', { time: formatDate(item.createdAt) });
    pendingAction = async () => {
      await App.RestoreBackup(item.id);
      selectSession(null);
      await Promise.all([loadSessions(), loadSettings()]);
      await loadRecoveryData();
      dispatch('restored');
    };
    showConfirm = true;
  }

  async function runConfirmedAction() {
    const action = pendingAction;
    pendingAction = null;
    if (!action) return;
    error = '';
    try {
      await action();
    } catch (e) {
      error = String(e);
    }
  }

  function formatDate(value: string): string {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
  }

  function formatSize(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
    return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && !showConfirm) close();
  }
</script>

{#if show}
  <div class="dialog-overlay" on:click|self={close} on:keydown={handleKeydown} role="dialog" aria-modal="true">
    <div class="dialog-content recovery-dialog">
      <div class="dialog-header">
        <div>
          <h2>{$t('recovery.title')}</h2>
          <p>{$t('recovery.subtitle')}</p>
        </div>
        <button class="close-btn" on:click={close}>×</button>
      </div>

      <div class="recovery-tabs" role="tablist">
        <button class:active={activeTab === 'trash'} on:click={() => activeTab = 'trash'}>
          {$t('recovery.trash')} <span>{trash.length}</span>
        </button>
        <button class:active={activeTab === 'backups'} on:click={() => activeTab = 'backups'}>
          {$t('recovery.backups')} <span>{backups.length}</span>
        </button>
      </div>

      {#if error}<div class="error-line" role="alert">{error}</div>{/if}

      <div class="recovery-body">
        {#if loading}
          <div class="empty">{$t('common.loading')}</div>
        {:else if activeTab === 'trash'}
          <div class="toolbar">
            <span>{$t('recovery.trashHint')}</span>
            <button class="danger subtle" disabled={trash.length === 0} on:click={requestEmptyTrash}>{$t('recovery.emptyTrash')}</button>
          </div>
          {#if trash.length === 0}
            <div class="empty">{$t('recovery.trashEmpty')}</div>
          {:else}
            <div class="item-list">
              {#each trash as item (item.id)}
                <div class="recovery-item">
                  <div class="item-icon">{item.kind === 'session' ? '▣' : '▤'}</div>
                  <div class="item-info">
                    <strong>{item.name}</strong>
                    <small>
                      {item.kind === 'session' ? $t('recovery.session') : $t('recovery.tab')}
                      {#if item.parentSessionName} · {item.parentSessionName}{/if}
                      · {formatDate(item.deletedAt)}
                    </small>
                  </div>
                  <div class="item-actions">
                    <button class="primary" on:click={() => restoreTrash(item)}>{$t('recovery.restore')}</button>
                    <button class="danger" on:click={() => requestPermanentDelete(item)}>{$t('recovery.deleteForever')}</button>
                  </div>
                </div>
              {/each}
            </div>
          {/if}
        {:else}
          <div class="toolbar">
            <span>{$t('recovery.backupHint')}</span>
            <button class="primary" on:click={createBackup}>{$t('recovery.backupNow')}</button>
          </div>
          {#if backups.length === 0}
            <div class="empty">{$t('recovery.backupsEmpty')}</div>
          {:else}
            <div class="item-list">
              {#each backups as item (item.id)}
                <div class="recovery-item">
                  <div class="item-icon">↺</div>
                  <div class="item-info">
                    <strong>{formatDate(item.createdAt)}</strong>
                    <small>{formatSize(item.size)}</small>
                  </div>
                  <div class="item-actions">
                    <button class="primary" on:click={() => requestRestoreBackup(item)}>{$t('recovery.restore')}</button>
                  </div>
                </div>
              {/each}
            </div>
          {/if}
        {/if}
      </div>
    </div>
  </div>
{/if}

<ConfirmDialog
  bind:show={showConfirm}
  title={confirmTitle}
  message={confirmMessage}
  confirmText={$t('recovery.confirm')}
  cancelText={$t('common.cancel')}
  variant="danger"
  on:confirm={runConfirmedAction}
/>

<style>
  .recovery-dialog { width: min(720px, calc(100vw - 36px)); max-height: min(680px, calc(100vh - 36px)); }
  .dialog-header { display:flex; align-items:flex-start; justify-content:space-between; padding:18px 20px 14px; border-bottom:1px solid rgba(255,255,255,.07); }
  .dialog-header h2 { margin:0; font-size:17px; color:#f4f4f5; }
  .dialog-header p { margin:4px 0 0; font-size:11px; color:#71717a; }
  .close-btn { border:0; background:transparent; color:#71717a; font-size:22px; cursor:pointer; }
  .recovery-tabs { display:flex; gap:4px; padding:10px 16px 0; }
  .recovery-tabs button { border:0; border-radius:6px; padding:7px 11px; background:transparent; color:#71717a; cursor:pointer; }
  .recovery-tabs button.active { background:rgba(139,92,246,.14); color:#c4b5fd; }
  .recovery-tabs span { margin-left:4px; padding:1px 5px; border-radius:8px; background:rgba(255,255,255,.07); font-size:10px; }
  .error-line { margin:10px 16px 0; padding:8px 10px; border:1px solid rgba(248,113,113,.25); border-radius:6px; color:#fca5a5; background:rgba(127,29,29,.2); font-size:11px; }
  .recovery-body { min-height:300px; max-height:500px; overflow:auto; padding:12px 16px 18px; }
  .toolbar { display:flex; align-items:center; justify-content:space-between; gap:12px; margin-bottom:10px; color:#71717a; font-size:11px; }
  .item-list { display:flex; flex-direction:column; gap:7px; }
  .recovery-item { display:flex; align-items:center; gap:11px; padding:10px; border:1px solid rgba(255,255,255,.07); border-radius:8px; background:rgba(255,255,255,.025); }
  .item-icon { display:grid; place-items:center; width:30px; height:30px; border-radius:7px; color:#a78bfa; background:rgba(139,92,246,.12); }
  .item-info { min-width:0; flex:1; display:flex; flex-direction:column; gap:3px; }
  .item-info strong { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; color:#e4e4e7; font-size:12px; }
  .item-info small { color:#71717a; font-size:10px; }
  .item-actions { display:flex; gap:6px; }
  .item-actions button, .toolbar button { border:1px solid rgba(255,255,255,.1); border-radius:6px; padding:5px 9px; background:rgba(255,255,255,.05); color:#a1a1aa; cursor:pointer; font-size:10px; }
  button.primary { color:#c4b5fd; border-color:rgba(139,92,246,.25); background:rgba(139,92,246,.1); }
  button.danger { color:#fca5a5; border-color:rgba(248,113,113,.2); }
  button.subtle { background:transparent; }
  button:disabled { opacity:.4; cursor:not-allowed; }
  .empty { display:grid; place-items:center; min-height:230px; color:#52525b; font-size:12px; }
</style>
