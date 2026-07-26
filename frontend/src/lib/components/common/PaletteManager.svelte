<script lang="ts">
  import { settings, saveSettings } from '../../stores/settings';
  import {
    CUSTOM_THEME_KEYS,
    getTerminalTheme,
    nextCustomId,
    type CustomPalette,
  } from '../../utils/terminalThemes';
  import { t } from '../../i18n';
  import SchemeImportDialog from '../Dialogs/SchemeImportDialog.svelte';

  /** Collapsed by default where it's a secondary action (e.g. Agents tab). */
  export let collapsible = false;
  let open = !collapsible;
  let showImport = false;

  $: customPalettes = ((($settings as any).customTerminalThemes || []) as CustomPalette[]);

  let editingId: string | null = null;
  $: editing = customPalettes.find(c => c.id === editingId) || null;
  $: editingColors = {
    ...(getTerminalTheme('asmgr') as Record<string, string>),
    ...((editing?.colors) || {}),
  } as Record<string, string>;

  function save(list: CustomPalette[]) {
    saveSettings({ customTerminalThemes: list } as any);
  }

  function add() {
    const id = nextCustomId(customPalettes);
    save([...customPalettes, {
      id,
      name: `Custom ${customPalettes.length + 1}`,
      colors: { ...(getTerminalTheme('asmgr') as Record<string, string>) },
    }]);
    editingId = id;
    open = true;
  }

  function rename(id: string, name: string) {
    save(customPalettes.map(c => (c.id === id ? { ...c, name } : c)));
  }

  function remove(id: string) {
    save(customPalettes.filter(c => c.id !== id));
    if (editingId === id) editingId = null;
    // Anything still pointing at the deleted palette falls back to default.
    if ($settings.terminalTheme === id) saveSettings({ terminalTheme: 'asmgr' });
    const map = { ...((($settings as any).agentTerminalThemes || {}) as Record<string, string>) };
    let touched = false;
    for (const k of Object.keys(map)) if (map[k] === id) { delete map[k]; touched = true; }
    if (touched) saveSettings({ agentTerminalThemes: map } as any);
  }

  function setColor(key: string, value: string) {
    if (!editingId) return;
    save(customPalettes.map(c =>
      c.id === editingId ? { ...c, colors: { ...(c.colors || {}), [key]: value } } : c
    ));
  }

  // Accepts #rgb / #rrggbb / #rrggbbaa and rgb()/rgba(); anything else is
  // ignored so a half-typed value can't wipe a colour.
  function setColorText(key: string, raw: string) {
    const v = raw.trim();
    if (!v) return;
    const ok = /^#([0-9a-f]{3}|[0-9a-f]{6}|[0-9a-f]{8})$/i.test(v) || /^rgba?\(/i.test(v);
    if (ok) setColor(key, v);
  }

  function swatchValue(key: string): string {
    const v = editingColors[key];
    return typeof v === 'string' && v.startsWith('#') ? v : '#000000';
  }
</script>

<div class="palette-manager">
  {#if collapsible}
    <button class="manager-toggle" on:click={() => open = !open}>
      {open ? '▾' : '▸'} {$t('settings.paletteCustom')}
      {#if customPalettes.length}<span class="count">{customPalettes.length}</span>{/if}
    </button>
  {/if}

  {#if open}
    {#if customPalettes.length > 0}
      <div class="custom-list">
        {#each customPalettes as cp (cp.id)}
          <div class="custom-row" class:editing={editingId === cp.id}>
            <input
              class="custom-name"
              value={cp.name}
              on:change={(e) => rename(cp.id, e.currentTarget.value)}
            />
            <button class="palette-toggle" on:click={() => editingId = editingId === cp.id ? null : cp.id}>
              {editingId === cp.id ? $t('settings.customPaletteHide') : $t('settings.customPaletteEdit')}
            </button>
            <button class="palette-delete" title={$t('common.delete')} on:click={() => remove(cp.id)}>×</button>
          </div>
          {#if editingId === cp.id}
            <div class="palette-grid">
              {#each CUSTOM_THEME_KEYS as c (c.key)}
                <div class="palette-swatch">
                  <span class="swatch-label">{$t('palette.' + c.key)}</span>
                  <div class="swatch-inputs">
                    <input
                      type="color"
                      title={$t('palette.pickColor')}
                      value={swatchValue(c.key)}
                      on:input={(e) => setColor(c.key, e.currentTarget.value)}
                    />
                    <input
                      type="text"
                      class="hex-input"
                      spellcheck="false"
                      value={editingColors[c.key] || ''}
                      placeholder="#000000"
                      on:change={(e) => setColorText(c.key, e.currentTarget.value)}
                    />
                  </div>
                </div>
              {/each}
            </div>
          {/if}
        {/each}
      </div>
    {/if}

    <div class="manager-actions">
      <button class="palette-add" on:click={add}>+ {$t('settings.customPaletteAdd')}</button>
      <button class="palette-import" on:click={() => showImport = true}>
        ↓ {$t('schemeImport.button')}
      </button>
    </div>
  {/if}
</div>

<SchemeImportDialog bind:show={showImport} />

<style>
  .palette-manager { display: flex; flex-direction: column; gap: 8px; }

  .manager-toggle {
    align-self: flex-start; display: flex; align-items: center; gap: 6px;
    padding: 4px 2px; border: 0; background: none; cursor: pointer;
    color: #a1a1aa; font-size: 12px;
  }
  .manager-toggle:hover { color: #e4e4e7; }
  .count {
    padding: 1px 6px; border-radius: 999px; font-size: 10px;
    background: rgba(var(--accent-rgb), 0.15); color: var(--accent-light);
  }

  .custom-list { display: flex; flex-direction: column; gap: 6px; }
  .custom-row { display: flex; align-items: center; gap: 6px; }
  .custom-row.editing .custom-name { border-color: rgba(var(--accent-rgb), 0.5); }
  .custom-name {
    flex: 1; min-width: 0; padding: 6px 9px; border-radius: 6px; font-size: 12px;
    border: 1px solid rgba(255, 255, 255, 0.12); background: rgba(0, 0, 0, 0.25); color: #e4e4e7;
  }
  .palette-toggle {
    padding: 6px 12px; border-radius: 7px; font-size: 12px; cursor: pointer;
    border: 1px solid rgba(255, 255, 255, 0.12); background: rgba(255, 255, 255, 0.05); color: #d4d4d8;
  }
  .palette-toggle:hover { border-color: rgba(var(--accent-rgb), 0.5); color: var(--accent-pale); }
  .palette-delete {
    border: 1px solid rgba(255, 255, 255, 0.12); background: rgba(255, 255, 255, 0.04);
    color: #a1a1aa; border-radius: 6px; width: 28px; height: 28px; cursor: pointer; font-size: 15px;
  }
  .palette-delete:hover { color: #fb7185; border-color: rgba(251, 113, 133, 0.5); }
  .palette-add {
    align-self: flex-start; padding: 6px 12px; border-radius: 7px; font-size: 12px; cursor: pointer;
    border: 1px dashed rgba(var(--accent-rgb), 0.4); background: rgba(var(--accent-rgb), 0.08); color: var(--accent-lighter);
  }
  .palette-add:hover { background: rgba(var(--accent-rgb), 0.16); }
  .manager-actions { display: flex; gap: 8px; flex-wrap: wrap; }
  .palette-import {
    padding: 6px 12px; border-radius: 7px; font-size: 12px; cursor: pointer;
    border: 1px solid rgba(255,255,255,.12); background: rgba(255,255,255,.05); color: #d4d4d8;
  }
  .palette-import:hover { border-color: rgba(var(--accent-rgb), .5); color: var(--accent-pale); }

  .palette-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(168px, 1fr)); gap: 10px; }
  .palette-swatch { display: flex; flex-direction: column; gap: 4px; }
  .swatch-label { font-size: 11px; color: #a1a1aa; }
  .swatch-inputs { display: flex; align-items: center; gap: 6px; }
  .palette-swatch input[type="color"] {
    width: 32px; height: 26px; padding: 0; border: 1px solid rgba(255, 255, 255, 0.15);
    border-radius: 5px; background: transparent; cursor: pointer; flex-shrink: 0;
  }
  .hex-input {
    flex: 1; min-width: 0; width: 100%; padding: 5px 7px; border-radius: 5px;
    border: 1px solid rgba(255, 255, 255, 0.12); background: rgba(0, 0, 0, 0.25);
    color: #e4e4e7; font-family: 'JetBrains Mono', Menlo, monospace; font-size: 11px;
  }
  .hex-input:focus { outline: none; border-color: rgba(var(--accent-rgb), 0.5); }
</style>
