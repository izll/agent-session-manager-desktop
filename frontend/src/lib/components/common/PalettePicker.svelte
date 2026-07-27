<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import type { TerminalTheme } from '../../utils/terminalThemes';

  /** Palettes to choose from (built-ins + the user's own). */
  export let palettes: TerminalTheme[] = [];
  export let value = '';
  /** Adds an "inherit" choice with this label when set. */
  export let inheritLabel = '';
  export let compact = false;

  const dispatch = createEventDispatcher();

  // The swatch strip: the eight ANSI colours a user actually recognises a
  // scheme by, drawn over the palette's own background.
  const SWATCH_KEYS = ['red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'foreground', 'brightBlack'];

  function swatches(t: TerminalTheme): string[] {
    const th = t.theme as Record<string, string>;
    return SWATCH_KEYS.map(k => th[k] || '#888');
  }

  function pick(id: string) {
    value = id;
    dispatch('change', id);
  }
</script>

<div class="palette-picker" class:compact>
  {#if inheritLabel}
    <button
      type="button"
      class="palette-card inherit"
      class:selected={!value}
      on:click={() => pick('')}
    >
      <span class="palette-name">{inheritLabel}</span>
    </button>
  {/if}

  {#each palettes as p (p.id)}
    <button
      type="button"
      class="palette-card"
      class:selected={value === p.id}
      on:click={() => pick(p.id)}
      title={p.name}
    >
      <!-- Mini preview: a scaled-down terminal in this scheme's colours. -->
      <span class="preview" style="background: {p.theme.background}; color: {p.theme.foreground}">
        <span class="preview-line">
          <span style="color: {p.theme.green}">❯</span>
          <span style="color: {p.theme.foreground}">git</span>
          <span style="color: {p.theme.cyan}">status</span>
        </span>
        <span class="preview-line">
          <span style="color: {p.theme.red}">M</span>
          <span style="color: {p.theme.brightBlack}">src/app.go</span>
        </span>
        <span class="swatches">
          {#each swatches(p) as c}
            <span class="swatch" style="background: {c}"></span>
          {/each}
        </span>
      </span>
      <span class="palette-name">{p.name}</span>
    </button>
  {/each}
</div>

<style>
  .palette-picker {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(108px, 1fr));
    gap: 8px;
  }
  .palette-picker.compact {
    grid-template-columns: repeat(auto-fill, minmax(92px, 1fr));
  }

  .palette-card {
    display: flex;
    flex-direction: column;
    gap: 5px;
    padding: 5px;
    border-radius: 8px;
    border: 1px solid rgba(255, 255, 255, 0.08);
    background: rgba(255, 255, 255, 0.03);
    cursor: pointer;
    transition: border-color 0.15s ease, background 0.15s ease;
  }
  .palette-card:hover { border-color: rgba(255, 255, 255, 0.2); }
  .palette-card.selected {
    border-color: rgba(var(--accent-rgb), 0.7);
    background: rgba(var(--accent-rgb), 0.12);
  }

  .preview {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 6px 7px;
    border-radius: 5px;
    font-family: 'JetBrains Mono', Menlo, monospace;
    font-size: 9px;
    line-height: 1.35;
    text-align: left;
    overflow: hidden;
  }
  .compact .preview { font-size: 7px; padding: 4px 5px; }
  .preview-line {
    display: flex;
    gap: 4px;
    white-space: nowrap;
  }
  .swatches { display: flex; gap: 2px; margin-top: 3px; }
  .swatch { width: 7px; height: 7px; border-radius: 2px; flex-shrink: 0; }

  .palette-name {
    font-size: 11px;
    color: #d4d4d8;
    text-align: center;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .palette-card.selected .palette-name { color: var(--accent-pale); }

  .palette-card.inherit {
    justify-content: center;
    min-height: 76px;
    border-style: dashed;
  }
</style>
