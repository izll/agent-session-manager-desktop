<script lang="ts">
  import * as App from '../../../../wailsjs/go/main/App';
  import { settings, saveSettings } from '../../stores/settings';
  import { nextCustomId, TERMINAL_THEMES, type CustomPalette } from '../../utils/terminalThemes';
  import PalettePicker from '../common/PalettePicker.svelte';
  import { t } from '../../i18n';
  import { autoFocusDialog } from '../../utils/dialogActions';

  export let show = false;

  interface Imported {
    name: string;
    source: string;
    colors: Record<string, string>;
  }

  type Tab = 'local' | 'online' | 'file';
  let tab: Tab = 'local';

  let loading = false;
  let error = '';
  let found: Imported[] = [];
  let selected = new Set<string>();
  let onlineList: Array<{ name: string; file: string }> = [];
  let onlineFilter = '';
  let onlineSelected = new Set<string>();
  let requestGeneration = 0;

  $: customPalettes = ((($settings as any).customTerminalThemes || []) as CustomPalette[]);
  $: filteredOnline = onlineFilter.trim()
    ? onlineList.filter(o => o.name.toLowerCase().includes(onlineFilter.trim().toLowerCase()))
    : onlineList;

  // Preview cards for whatever we've parsed, so a scheme can be judged before
  // it's added.
  $: previewPalettes = found.map(f => ({
    id: f.name,
    name: f.name,
    theme: { ...TERMINAL_THEMES[0].theme, ...f.colors } as any,
  }));

  // Names already taken, so an import can warn instead of silently creating a
  // second "Dracula".
  $: takenNames = new Set(customPalettes.map(c => c.name.trim().toLowerCase()));
  $: duplicateCount = found.filter(
    f => selected.has(f.name) && takenNames.has(f.name.trim().toLowerCase())
  ).length;

  $: allFoundSelected = found.length > 0 && selected.size === found.length;

  // Scan once per opening of the local tab, not every time `found` empties —
  // otherwise adding or clearing the list would kick off a fresh scan.
  let scannedLocal = false;
  $: if (show && tab === 'local' && !scannedLocal && !loading) {
    scannedLocal = true;
    void scanLocal();
  }

  async function scanLocal() {
    const generation = ++requestGeneration;
    const targetTab = tab;
    loading = true; error = ''; found = []; selected = new Set();
    try {
      const result = ((await App.DiscoverLocalSchemes()) || []) as Imported[];
      if (!show || generation !== requestGeneration || tab !== targetTab) return;
      found = result;
      if (result.length === 0) error = $t('schemeImport.noneFound');
      // Nothing is preselected: importing 19 schemes by accident is worse
      // than one extra click, and the preview strip stays readable.
    } catch (e) {
      if (!show || generation !== requestGeneration || tab !== targetTab) return;
      error = String(e);
    } finally {
      if (show && generation === requestGeneration && tab === targetTab) loading = false;
    }
  }

  async function pickFiles() {
    const generation = ++requestGeneration;
    const targetTab = tab;
    loading = true; error = '';
    try {
      const res = ((await App.ImportSchemeFiles()) || []) as Imported[];
      if (!show || generation !== requestGeneration || tab !== targetTab) return;
      if (res.length) {
        found = res;
        selected = new Set(res.map(r => r.name));
      }
      // A cancelled picker leaves whatever was there; the tab still offers
      // the button to try again.
    } catch (e) {
      if (!show || generation !== requestGeneration || tab !== targetTab) return;
      error = String(e);
    } finally {
      if (show && generation === requestGeneration && tab === targetTab) loading = false;
    }
  }

  function toggleAll() {
    selected = allFoundSelected ? new Set() : new Set(found.map(f => f.name));
  }

  async function loadOnline() {
    if (onlineList.length) return;
    const generation = ++requestGeneration;
    const targetTab = tab;
    loading = true; error = '';
    try {
      const result = ((await App.ListOnlineSchemes()) || []) as Array<{ name: string; file: string }>;
      if (!show || generation !== requestGeneration || tab !== targetTab) return;
      onlineList = result;
    } catch (e) {
      if (!show || generation !== requestGeneration || tab !== targetTab) return;
      error = String(e);
    } finally {
      if (show && generation === requestGeneration && tab === targetTab) loading = false;
    }
  }

  async function previewOnline() {
    if (onlineSelected.size === 0) return;
    const generation = ++requestGeneration;
    const targetTab = tab;
    const files = [...onlineSelected];
    loading = true; error = '';
    try {
      const result = ((await App.FetchOnlineSchemes(files)) || []) as Imported[];
      if (!show || generation !== requestGeneration || tab !== targetTab) return;
      found = result;
      selected = new Set(result.map(f => f.name));
    } catch (e) {
      if (!show || generation !== requestGeneration || tab !== targetTab) return;
      error = String(e);
    } finally {
      if (show && generation === requestGeneration && tab === targetTab) loading = false;
    }
  }

  function toggle(name: string) {
    const next = new Set(selected);
    if (next.has(name)) next.delete(name); else next.add(name);
    selected = next;
  }

  function toggleOnline(file: string) {
    const next = new Set(onlineSelected);
    if (next.has(file)) next.delete(file); else next.add(file);
    onlineSelected = next;
  }

  // "Dracula" imported twice becomes "Dracula (2)" rather than two entries
  // that can't be told apart in the picker.
  function uniqueName(base: string, taken: Set<string>): string {
    let name = base.trim() || 'Imported';
    if (!taken.has(name.toLowerCase())) return name;
    for (let n = 2; ; n++) {
      const candidate = `${name} (${n})`;
      if (!taken.has(candidate.toLowerCase())) return candidate;
    }
  }

  function addSelected() {
    const list = [...customPalettes];
    const taken = new Set(list.map(c => c.name.trim().toLowerCase()));
    let id = nextCustomId(list);
    for (const f of found) {
      if (!selected.has(f.name)) continue;
      const name = uniqueName(f.name, taken);
      taken.add(name.toLowerCase());
      list.push({ id, name, colors: f.colors });
      id = nextCustomId(list);
    }
    saveSettings({ customTerminalThemes: list } as any);
    close();
  }

  function close() {
    requestGeneration++;
    loading = false;
    show = false;
    found = []; selected = new Set(); onlineSelected = new Set();
    error = ''; onlineFilter = '';
    scannedLocal = false;
    tab = 'local';
  }

  function switchTab(next: Tab) {
    if (next === tab) return;
    requestGeneration++;
    loading = false;
    tab = next;
    error = '';
    found = []; selected = new Set();
    if (next === 'online') void loadOnline();
    if (next === 'local') { scannedLocal = true; void scanLocal(); }
  }

  // This dialog opens on top of Settings and lives inside its DOM subtree, so
  // an un-stopped Escape would bubble into Settings' handler and close that
  // too. Handling it on the overlay (not window) keeps stopPropagation useful.
  function handleKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape') return;
    e.stopPropagation();
    close();
  }

</script>

{#if show}
  <div
    class="dialog-overlay scheme-overlay"
    use:autoFocusDialog
    tabindex="-1"
    role="dialog"
    aria-modal="true"
    on:keydown={handleKeydown}
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2>{$t('schemeImport.title')}</h2>
        <button class="close-btn" on:click={close}>×</button>
      </div>

      <div class="source-tabs">
        <button class:active={tab === 'local'} on:click={() => switchTab('local')}>{$t('schemeImport.local')}</button>
        <button class:active={tab === 'online'} on:click={() => switchTab('online')}>{$t('schemeImport.online')}</button>
        <button class:active={tab === 'file'} on:click={() => switchTab('file')}>{$t('schemeImport.file')}</button>
      </div>

      <p class="source-hint">
        {#if tab === 'local'}{$t('schemeImport.localHint')}
        {:else if tab === 'online'}{$t('schemeImport.onlineHint')}
        {:else}{$t('schemeImport.fileHint')}{/if}
      </p>

      {#if error}<div class="error-line">{error}</div>{/if}

      {#if loading}
        <div class="state">{$t('schemeImport.loading')}</div>
      {:else if tab === 'file' && found.length === 0}
        <div class="state file-state">
          <button class="primary" on:click={pickFiles}>{$t('schemeImport.choose')}</button>
        </div>
      {:else if tab === 'online' && found.length === 0}
        <div class="online-picker">
          <input class="filter" bind:value={onlineFilter} placeholder={$t('schemeImport.filterPlaceholder')} />
          <div class="online-list">
            {#each filteredOnline.slice(0, 300) as o (o.file)}
              <label class="online-row" class:checked={onlineSelected.has(o.file)}>
                <input type="checkbox" checked={onlineSelected.has(o.file)} on:change={() => toggleOnline(o.file)} />
                <span>{o.name}</span>
              </label>
            {/each}
          </div>
          <div class="online-actions">
            <span class="count">{$t('schemeImport.selectedCount', { n: onlineSelected.size })}</span>
            <button class="primary" disabled={onlineSelected.size === 0} on:click={previewOnline}>
              {$t('schemeImport.preview')}
            </button>
          </div>
        </div>
      {:else if found.length > 0}
        <div class="found-head">
          <button class="link-btn" on:click={toggleAll}>
            {allFoundSelected ? $t('schemeImport.selectNone') : $t('schemeImport.selectAll')}
          </button>
          <span class="count">{$t('schemeImport.selectedCount', { n: selected.size })}</span>
          {#if tab === 'online'}
            <button class="link-btn back" on:click={() => { found = []; selected = new Set(); }}>
              {$t('schemeImport.backToList')}
            </button>
          {:else if tab === 'file'}
            <button class="link-btn back" on:click={pickFiles}>{$t('schemeImport.choose')}</button>
          {/if}
        </div>

        <div class="found-list">
          {#each found as f (f.name)}
            <label class="found-row" class:checked={selected.has(f.name)}>
              <input type="checkbox" checked={selected.has(f.name)} on:change={() => toggle(f.name)} />
              <span class="found-name">{f.name}</span>
              <span class="found-source">{f.source}</span>
            </label>
          {/each}
        </div>

        <div class="preview-strip">
          <PalettePicker compact palettes={previewPalettes} value="" />
        </div>
      {/if}

      {#if duplicateCount > 0}
        <p class="dup-note">{$t('schemeImport.duplicateNote', { n: duplicateCount })}</p>
      {/if}

      <div class="dialog-actions">
        <button class="cancel" on:click={close}>{$t('common.cancel')}</button>
        <button class="primary" disabled={selected.size === 0} on:click={addSelected}>
          {$t('schemeImport.add', { n: selected.size })}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  /* Sits above the Settings overlay it's opened from (both default to 50). */
  .scheme-overlay { z-index: 60; }
  .scheme-overlay:focus { outline: none; }

  .dialog-content {
    width: min(720px, 94vw);
    max-width: min(720px, 94vw);
    max-height: 84vh;
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 18px;
    border-radius: 12px;
    border: 1px solid rgba(var(--accent-rgb), 0.25);
    background: linear-gradient(180deg, #1a1a2e 0%, #0f0f1a 100%);
  }
  .dialog-header { display: flex; align-items: center; justify-content: space-between; }
  .dialog-header h2 { margin: 0; font-size: 16px; color: #e4e4e7; }
  .close-btn { border: 0; background: none; color: #71717a; font-size: 20px; cursor: pointer; }
  .close-btn:hover { color: #e4e4e7; }

  .source-tabs { display: flex; gap: 6px; }
  .source-tabs button {
    padding: 6px 14px; border-radius: 7px; font-size: 13px; cursor: pointer;
    border: 1px solid rgba(255, 255, 255, 0.1); background: rgba(255, 255, 255, 0.04); color: #a1a1aa;
  }
  .source-tabs button.active {
    border-color: rgba(var(--accent-rgb), 0.6); background: rgba(var(--accent-rgb), 0.15); color: var(--accent-pale);
  }
  .source-hint { margin: 0; font-size: 12px; color: #71717a; }
  .error-line { color: #fb7185; font-size: 13px; }
  .state { padding: 30px 0; text-align: center; color: #71717a; font-size: 13px; }

  .filter, .online-list, .found-list { width: 100%; }
  .filter {
    padding: 7px 10px; border-radius: 7px; font-size: 13px;
    border: 1px solid rgba(255, 255, 255, 0.12); background: rgba(0, 0, 0, 0.25); color: #e4e4e7;
  }
  .online-picker { display: flex; flex-direction: column; gap: 8px; min-height: 0; }
  .online-list {
    display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 2px;
    max-height: 300px; overflow-y: auto; padding-right: 4px;
  }
  .online-row, .found-row {
    display: flex; align-items: center; gap: 8px; padding: 5px 8px;
    border-radius: 6px; font-size: 13px; color: #d4d4d8; cursor: pointer;
  }
  .online-row:hover, .found-row:hover { background: rgba(255, 255, 255, 0.05); }
  .online-row.checked, .found-row.checked { background: rgba(var(--accent-rgb), 0.12); color: var(--accent-pale); }
  .online-row input, .found-row input { accent-color: var(--accent); }
  .online-actions { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
  .count { font-size: 12px; color: #71717a; }

  .found-head { display: flex; align-items: center; gap: 10px; }
  .found-head .back { margin-left: auto; }
  .link-btn {
    border: 0; background: none; padding: 0; cursor: pointer;
    font-size: 12px; color: var(--accent-light); text-decoration: underline;
  }
  .link-btn:hover { color: var(--accent-pale); }
  .file-state { display: flex; justify-content: center; }
  .dup-note { margin: 0; font-size: 12px; color: #fbbf24; }

  .found-list { display: flex; flex-direction: column; gap: 2px; max-height: 200px; overflow-y: auto; }
  .found-name { flex: 1; }
  .found-source {
    font-size: 11px; color: #71717a; padding: 1px 6px; border-radius: 999px;
    background: rgba(255, 255, 255, 0.06);
  }
  .preview-strip { max-height: 200px; overflow-y: auto; }

  .dialog-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 2px; }
  .dialog-actions button {
    padding: 7px 16px; border-radius: 7px; font-size: 13px; font-weight: 600; cursor: pointer;
  }
  .dialog-actions .cancel {
    border: 1px solid rgba(255, 255, 255, 0.12); background: rgba(255, 255, 255, 0.04); color: #a1a1aa;
  }
  .dialog-actions .primary, .online-actions .primary {
    border: 1px solid var(--accent); background: linear-gradient(135deg, var(--accent-dark), var(--accent)); color: var(--accent-ink);
    padding: 7px 16px; border-radius: 7px; font-size: 13px; font-weight: 600; cursor: pointer;
  }
  .dialog-actions .primary:disabled, .online-actions .primary:disabled { opacity: 0.45; cursor: default; }
</style>
