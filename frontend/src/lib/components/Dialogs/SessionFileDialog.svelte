<script lang="ts">
  import { autoFocusDialog } from '../../utils/dialogActions';
  import * as App from '../../../../wailsjs/go/main/App';
  import type { main } from '../../../../wailsjs/go/models';
  import { loadSessions } from '../../stores/sessions';
  import { t } from '../../i18n';

  export let show = false;

  let file: main.PortableFileInfo | null = null;
  let selected = new Set<string>();
  let loading = false;
  let error = '';
  let importedCount = 0;

  // Opening the picker is the first thing that happens, so the dialog only
  // appears once there is something to show.
  let lastShow = false;
  $: {
    if (show && !lastShow) void pickFile();
    lastShow = show;
  }

  $: missingCount = (file?.sessions || []).filter(s => selected.has(s.name) && !s.pathExists).length;
  $: allSelected = !!file && selected.size === file.sessions.length && file.sessions.length > 0;

  async function pickFile() {
    reset();
    loading = true;
    try {
      const info = await App.ReadSessionFile();
      if (!info) {
        // Cancelled in the file picker — nothing to show.
        show = false;
        return;
      }
      file = info;
      // Everything is preselected: the user already chose this file, and the
      // usual case is importing all of it.
      selected = new Set(info.sessions.map(s => s.name));
    } catch (e) {
      error = String(e);
    } finally {
      loading = false;
    }
  }

  function toggle(name: string) {
    const next = new Set(selected);
    if (next.has(name)) next.delete(name); else next.add(name);
    selected = next;
  }

  function toggleAll() {
    selected = allSelected ? new Set() : new Set((file?.sessions || []).map(s => s.name));
  }

  async function doImport() {
    if (!file || selected.size === 0) return;
    loading = true;
    error = '';
    try {
      importedCount = await App.ImportSessionFile(file.path, [...selected]);
      await loadSessions();
    } catch (e) {
      error = String(e);
    } finally {
      loading = false;
    }
  }

  function reset() {
    file = null;
    selected = new Set();
    error = '';
    importedCount = 0;
  }

  function close() {
    show = false;
    reset();
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') close();
  }
</script>

{#if show}
  <div
    class="dialog-overlay" use:autoFocusDialog
    on:click|self={close}
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
  >
    <div class="dialog-content file-import">
      <div class="dialog-header">
        <h2>{$t('sessionFile.importTitle')}</h2>
        <button class="close-btn" on:click={close}>×</button>
      </div>

      <div class="dialog-body">
        {#if loading}
          <div class="state">{$t('sessionFile.working')}</div>
        {:else if importedCount > 0}
          <div class="done">{$t('sessionFile.imported', { n: importedCount })}</div>
        {:else if error}
          <div class="error-line">{error}</div>
        {:else if file}
          <div class="file-meta">
            <span class="file-path" title={file.path}>{file.path}</span>
            {#if file.exportedAt}
              <span class="file-date">{$t('sessionFile.exportedAt', { date: file.exportedAt })}</span>
            {/if}
          </div>

          <div class="list-head">
            <button class="link-btn" on:click={toggleAll}>
              {allSelected ? $t('sessionFile.selectNone') : $t('sessionFile.selectAll')}
            </button>
            <span class="count">{$t('sessionFile.selectedCount', { n: selected.size })}</span>
          </div>

          <div class="session-list">
            {#each file.sessions as s (s.name)}
              <label class="session-row" class:checked={selected.has(s.name)} class:missing={!s.pathExists}>
                <input type="checkbox" checked={selected.has(s.name)} on:change={() => toggle(s.name)} />
                <span class="session-main">
                  <span class="session-name">
                    {s.name}
                    {#if s.tabs > 0}<span class="tab-count">{$t('sessionFile.tabs', { n: s.tabs })}</span>{/if}
                  </span>
                  <span class="session-path">{s.path}</span>
                  {#if !s.pathExists}
                    <span class="path-warning">{$t('sessionFile.pathMissing')}</span>
                  {/if}
                </span>
                <span class="session-agent">{s.agent}</span>
              </label>
            {/each}
          </div>

          {#if missingCount > 0}
            <p class="missing-note">{$t('sessionFile.missingNote', { n: missingCount })}</p>
          {/if}
        {/if}
      </div>

      <div class="dialog-footer">
        {#if importedCount > 0}
          <button class="btn-primary" on:click={close}>{$t('sessionFile.close')}</button>
        {:else}
          <button class="btn-secondary" on:click={close}>{$t('sessionFile.cancel')}</button>
          <button class="btn-primary" disabled={!file || selected.size === 0 || loading} on:click={doImport}>
            {$t('sessionFile.importSelected', { n: selected.size })}
          </button>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .file-import {
    width: min(640px, 94vw);
    max-width: min(640px, 94vw);
    max-height: 84vh;
    display: flex;
    flex-direction: column;
  }
  .dialog-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 18px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  }
  .dialog-header h2 { margin: 0; font-size: 15px; color: #e4e4e7; }
  .close-btn { border: 0; background: none; color: #71717a; font-size: 20px; cursor: pointer; }
  .close-btn:hover { color: #e4e4e7; }

  .dialog-body { padding: 16px 18px; overflow-y: auto; flex: 1; min-height: 0; }
  .state, .done { padding: 26px 0; text-align: center; font-size: 13px; }
  .state { color: #71717a; }
  .done { color: #4ade80; }
  .error-line { color: #fb7185; font-size: 12px; }

  .file-meta { display: flex; flex-direction: column; gap: 2px; margin-bottom: 12px; }
  .file-path {
    font-family: 'JetBrains Mono', monospace; font-size: 11px; color: #a1a1aa;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .file-date { font-size: 11px; color: #6b7280; }

  .list-head { display: flex; align-items: center; gap: 10px; margin-bottom: 6px; }
  .link-btn {
    border: 0; background: none; padding: 0; cursor: pointer;
    font-size: 11px; color: var(--accent-light); text-decoration: underline;
  }
  .link-btn:hover { color: var(--accent-pale); }
  .count { font-size: 11px; color: #6b7280; }

  .session-list { display: flex; flex-direction: column; gap: 3px; }
  .session-row {
    display: flex; align-items: flex-start; gap: 9px; padding: 7px 9px;
    border-radius: 7px; cursor: pointer; border: 1px solid transparent;
  }
  .session-row:hover { background: rgba(255, 255, 255, 0.04); }
  .session-row.checked { background: rgba(var(--accent-rgb), 0.1); }
  /* A session whose directory is gone can still be imported — it just won't
     start until the path is fixed, so warn rather than block. */
  .session-row.missing { border-color: rgba(251, 191, 36, 0.35); }
  .session-row input { margin-top: 2px; accent-color: var(--accent); }

  .session-main { display: flex; flex-direction: column; gap: 1px; flex: 1; min-width: 0; }
  .session-name { font-size: 13px; color: #e4e4e7; display: flex; align-items: center; gap: 6px; }
  .tab-count {
    font-size: 10px; color: #a1a1aa; padding: 0 5px;
    border-radius: 999px; background: rgba(255, 255, 255, 0.07);
  }
  .session-path {
    font-family: 'JetBrains Mono', monospace; font-size: 10px; color: #6b7280;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .path-warning { font-size: 10px; color: #fbbf24; }
  .session-agent { font-size: 10px; color: #71717a; flex-shrink: 0; }
  .missing-note { margin: 10px 0 0; font-size: 11px; color: #fbbf24; }

  .dialog-footer {
    display: flex; justify-content: flex-end; gap: 8px;
    padding: 12px 18px; border-top: 1px solid rgba(255, 255, 255, 0.06);
  }
  .btn-primary, .btn-secondary {
    padding: 7px 16px; border-radius: 7px; font-size: 12px; font-weight: 600; cursor: pointer;
  }
  .btn-secondary {
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(255, 255, 255, 0.05); color: #a1a1aa;
  }
  .btn-primary {
    border: 1px solid var(--accent);
    background: linear-gradient(135deg, var(--accent-dark), var(--accent)); color: var(--accent-ink);
  }
  .btn-primary:disabled { opacity: 0.45; cursor: default; }
</style>
