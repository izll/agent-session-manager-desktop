<script lang="ts">
  // Folders linked to a tab, beside its own: their diff and their files, seen here
  // without opening another tab. Bound to the tab it was opened for; switching tabs
  // closes it, since every view inside reads the selected one.
  import { createEventDispatcher, tick } from 'svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import type { main } from '../../../../wailsjs/go/models';
  import { t } from '../../i18n';
  import { portal } from '../../utils/portal';
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import { selectedSessionId, selectedWindowIdx } from '../../stores/sessions';
  import { activeProjectId } from '../../stores/projects';
  import { describeBackendError } from '../../utils/backendError';
  import DialogCloseButton from '../common/DialogCloseButton.svelte';
  import Diff from '../MainPanel/Diff.svelte';
  import FileBrowser from '../MainPanel/FileBrowser.svelte';
  import GitHistoryDialog from './GitHistoryDialog.svelte';

  export let show = false;
  export let projectId = '';
  export let sessionId = '';
  export let windowIdx = 0;
  /** The server the tab runs on; empty for this computer. Folders on a server
   *  cannot be linked yet: their files would be read here instead. */
  export let serverId = '';
  /** Where the folder picker starts: the tab's own folder. */
  export let startPath = '';

  const dispatch = createEventDispatcher();

  let folders: main.LinkedFolder[] = [];
  let selected = '';
  let view: 'diff' | 'files' = 'diff';
  let error = '';
  let browser: FileBrowser | null = null;
  let loadedFor = '';
  // The commit history of the folder shown, in the history window over this one.
  let historyOpen = false;

  // Whole screen or not, kept for the next opening. Browser storage can be
  // missing or refuse; then it is simply not remembered.
  const MAXIMISED_KEY = 'asmgr.linkedFolders.maximised';
  let maximised = (() => {
    try { return localStorage.getItem(MAXIMISED_KEY) === '1'; } catch { return false; }
  })();
  function toggleMaximised() {
    maximised = !maximised;
    try { localStorage.setItem(MAXIMISED_KEY, maximised ? '1' : '0'); } catch { /* not kept */ }
  }

  $: current = folders.find((f) => f.path === selected) || null;
  // A folder that is not a repository has nothing to diff: its files it has.
  $: if (current && !current.isGitRepo && view === 'diff') view = 'files';

  $: if (show && `${sessionId}:${windowIdx}` !== loadedFor) void load();
  $: if (!show) loadedFor = '';
  // Every view in here reads the selected tab: another one would show its files
  // under this tab's folders. Unsaved edits here are asked about first, and kept
  // with the tab they belong to when the user stays.
  let leavingTab = false;
  $: if (show && $activeProjectId !== projectId) finishClose();
  $: if (show && !leavingTab && ($selectedSessionId !== sessionId || $selectedWindowIdx !== windowIdx)) leaveTab();

  function leaveTab() {
    leavingTab = true;
    leaveBrowser(
      () => { leavingTab = false; finishClose(); },
      () => {
        leavingTab = false;
        selectedSessionId.set(sessionId);
        selectedWindowIdx.set(windowIdx);
      },
    );
  }

  /** Runs `action` once the editor in the Files view has nothing unsaved. */
  function leaveBrowser(action: () => void, onCancel: (() => void) | null = null) {
    if (view === 'files' && browser) browser.whenLeaving(action, onCancel);
    else action();
  }

  function pick(path: string) {
    if (path === selected) return;
    leaveBrowser(() => { selected = path; });
  }

  function showView(next: 'diff' | 'files') {
    if (next === view) return;
    leaveBrowser(() => { view = next; });
  }

  async function load() {
    loadedFor = `${sessionId}:${windowIdx}`;
    error = '';
    try {
      apply((await App.GetTabLinkedFolders(sessionId, windowIdx)) || []);
    } catch (e) {
      error = describeBackendError(e);
    }
  }

  function apply(next: main.LinkedFolder[]) {
    folders = next;
    if (!folders.some((f) => f.path === selected)) {
      selected = folders.find((f) => !f.missing)?.path || folders[0]?.path || '';
      view = 'diff';
    }
  }

  async function add() {
    error = '';
    if (serverId) return;
    try {
      const picked = await App.BrowseDirectory(startPath);
      if (picked) await link(picked);
    } catch (e) {
      error = describeBackendError(e);
    }
  }

  async function link(dir: string) {
    try {
      const before = new Set(folders.map((f) => f.path));
      const next = (await App.AddTabLinkedFolder(sessionId, windowIdx, dir, projectId)) || [];
      // The new one, as the backend spelled it; one already linked is shown again.
      const added = next.find((f) => !before.has(f.path)) || next.find((f) => f.path === dir);
      const reveal = () => { apply(next); if (added) selected = added.path; };
      if (added && added.path !== selected) leaveBrowser(reveal);
      else reveal();
    } catch (e) {
      error = describeBackendError(e);
    }
  }

  async function unlink(dir: string) {
    error = '';
    try {
      apply((await App.RemoveTabLinkedFolder(sessionId, windowIdx, dir, projectId)) || []);
    } catch (e) {
      error = describeBackendError(e);
    }
  }

  async function openInFiles(e: CustomEvent<{ path: string; line?: number; folder?: boolean }>) {
    view = 'files';
    await tick();
    browser?.openPath(e.detail.path, e.detail.line, !!e.detail.folder);
  }

  function folderName(path: string): string {
    return path.replace(/[\\/]+$/, '').split(/[\\/]/).pop() || path;
  }

  function close() {
    leaveBrowser(finishClose);
  }

  function finishClose() {
    show = false;
    dispatch('close');
  }

  function handleKeydown(e: KeyboardEvent) {
    // A menu open over the diff closes first, by its own handler.
    if (e.key === 'Escape' && document.querySelector('.context-menu')) return;
    if (e.key === 'Escape' && !e.defaultPrevented) {
      e.stopPropagation();
      claimKeyForDialog();
      close();
    }
  }
</script>

{#if show}
  <div
    class="dialog-overlay"
    use:portal
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
    aria-labelledby="linked-folders-title"
    tabindex="-1"
    data-linked-folders
  >
    <div class="dialog-content linked-dialog" class:maximised>
      <div class="dialog-header">
        <h2 id="linked-folders-title">{$t('linkedFolders.title')}</h2>
        <div class="dialog-header-actions">
          <button
            class="dialog-header-btn"
            data-maximise
            title={maximised ? $t('history.restore') : $t('history.maximise')}
            aria-label={maximised ? $t('history.restore') : $t('history.maximise')}
            on:click={toggleMaximised}
          >
            {#if maximised}
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
                <polyline points="4 14 10 14 10 20"/><polyline points="20 10 14 10 14 4"/>
              </svg>
            {:else}
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
                <polyline points="15 3 21 3 21 9"/><polyline points="9 21 3 21 3 15"/>
              </svg>
            {/if}
          </button>
          <DialogCloseButton on:click={close} />
        </div>
      </div>
      <div class="linked-body">
        <aside class="folder-list">
          {#each folders as folder (folder.path)}
            <div class="folder-row" class:selected={folder.path === selected} class:missing={folder.missing}>
              <button class="folder-pick" title={folder.path} on:click={() => pick(folder.path)}>
                <span class="folder-name">{folderName(folder.path)}</span>
                <!-- Cut at the start, where a long path repeats itself; <bdi> keeps
                     the path itself left to right, slashes where they belong. -->
                <span class="folder-path"><bdi>{folder.missing ? $t('linkedFolders.missing') : folder.path}</bdi></span>
              </button>
              <button class="folder-remove" title={$t('linkedFolders.remove')} aria-label={$t('linkedFolders.remove')}
                data-unlink={folder.path} on:click={() => unlink(folder.path)}>×</button>
            </div>
          {/each}
          <button class="folder-add" data-link-folder on:click={add} disabled={!!serverId}
            title={serverId ? $t('linkedFolders.serverHint') : undefined}>+ {$t('linkedFolders.add')}</button>
          {#if serverId}
            <p class="linked-note">{$t('linkedFolders.serverHint')}</p>
          {/if}
          {#if error}
            <p class="linked-error" role="alert">{error}</p>
          {/if}
        </aside>
        <section class="folder-view">
          {#if current && !current.missing}
            <div class="view-switch" role="tablist">
              <button role="tab" aria-selected={view === 'diff'} class:active={view === 'diff'}
                disabled={!current.isGitRepo}
                title={current.isGitRepo ? undefined : $t('linkedFolders.notRepository')}
                on:click={() => showView('diff')}>{$t('linkedFolders.diff')}</button>
              <button role="tab" aria-selected={view === 'files'} class:active={view === 'files'}
                on:click={() => showView('files')}>{$t('linkedFolders.files')}</button>
              <button class="history-btn" data-linked-history disabled={!current.isGitRepo}
                title={current.isGitRepo ? undefined : $t('linkedFolders.notRepository')}
                on:click={() => { historyOpen = true; }}>
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
                  <circle cx="12" cy="12" r="9"/><polyline points="12 7 12 12 15 14"/>
                </svg>
                {$t('linkedFolders.history')}
              </button>
            </div>
            <div class="view-host">
              {#if view === 'diff'}
                <Diff active={show} initialMode="full" fixedRoot={current.path} on:openinfiles={openInFiles} />
              {:else}
                <FileBrowser bind:this={browser} active={show} fixedRoot={current.path} />
              {/if}
            </div>
          {:else}
            <p class="linked-empty">{folders.length ? $t('linkedFolders.missingHint') : $t('linkedFolders.empty')}</p>
          {/if}
        </section>
      </div>
    </div>
  </div>
{/if}

{#if historyOpen && current}
  <!-- Moved to the end of the page like this window itself, or it would open
       underneath it: the history dialog stays where it is mounted. -->
  <div use:portal>
    <GitHistoryDialog bind:show={historyOpen} {projectId} {sessionId} {windowIdx} path={current.path} />
  </div>
{/if}

<style>
  .linked-dialog {
    width: min(1280px, 96vw);
    max-width: min(1280px, 96vw);
    height: min(860px, 90vh);
    display: flex;
    flex-direction: column;
  }
  .linked-dialog.maximised {
    width: 100vw;
    max-width: 100vw;
    height: 100vh;
    max-height: 100vh;
    border-radius: 0;
  }
  .linked-body {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: 12px;
    padding: 12px 16px 16px;
  }
  .folder-list {
    width: 240px;
    flex-shrink: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
    overflow-y: auto;
  }
  .folder-row {
    display: flex;
    align-items: stretch;
    border-radius: 6px;
    border: 1px solid transparent;
  }
  .folder-row.selected {
    border-color: rgba(var(--accent-rgb), 0.45);
    background: rgba(var(--accent-rgb), 0.12);
  }
  .folder-pick {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 2px;
    padding: 6px 8px;
    background: transparent;
    border: 0;
    color: #e4e4e7;
    cursor: pointer;
    text-align: left;
  }
  .folder-name { font-size: 13px; font-weight: 600; }
  .folder-path {
    font-size: 11px;
    color: #a1a1aa;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    direction: rtl;
  }
  .folder-row.missing .folder-name { color: #a1a1aa; text-decoration: line-through; }
  .folder-row.missing .folder-path { color: #fca5a5; direction: ltr; }
  .folder-remove {
    border: 0;
    background: transparent;
    color: #71717a;
    cursor: pointer;
    font-size: 16px;
    padding: 0 8px;
  }
  .folder-remove:hover { color: #fca5a5; }
  .folder-add {
    margin-top: 4px;
    padding: 6px 8px;
    border-radius: 6px;
    border: 1px dashed rgba(255, 255, 255, 0.2);
    background: transparent;
    color: #d4d4d8;
    cursor: pointer;
    text-align: left;
    font-size: 13px;
  }
  .folder-add:hover:not(:disabled) { border-color: rgba(var(--accent-rgb), 0.6); }
  .folder-add:disabled { opacity: 0.45; cursor: not-allowed; }
  .linked-note { color: #a1a1aa; font-size: 12px; line-height: 1.4; margin: 6px 0 0; }
  .linked-error { color: #fca5a5; font-size: 12px; margin: 6px 0 0; }
  .folder-view {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 8px;
    overflow: hidden;
  }
  .view-switch {
    display: flex;
    gap: 2px;
    padding: 6px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  }
  .view-switch button {
    padding: 4px 12px;
    border-radius: 6px;
    border: 0;
    background: transparent;
    color: #a1a1aa;
    cursor: pointer;
    font-size: 12px;
  }
  .view-switch button.active { background: rgba(var(--accent-rgb), 0.18); color: #e4e4e7; }
  .view-switch button:disabled { opacity: 0.4; cursor: default; }
  .view-switch .history-btn {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    line-height: 1;
  }
  /* Centred on the lowercase letters, which sit below the line's middle: centred
     on the line itself the clock looks raised beside them. */
  .view-switch .history-btn svg {
    display: block;
    flex-shrink: 0;
    transform: translateY(0.5px);
  }
  .view-host { flex: 1; min-height: 0; position: relative; display: flex; flex-direction: column; }
  .linked-empty {
    margin: auto;
    max-width: 420px;
    text-align: center;
    color: #a1a1aa;
    font-size: 13px;
    line-height: 1.5;
  }
</style>
