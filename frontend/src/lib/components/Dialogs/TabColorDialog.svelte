<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { t } from '../../i18n';
  import * as App from '../../../../wailsjs/go/main/App';

  interface TabColorTarget {
    Index: number;
    Name: string;
    TextColor?: string;
    BackgroundColor?: string;
  }

  export let show = false;
  export let sessionId = '';
  export let tab: TabColorTarget | null = null;

  const dispatch = createEventDispatcher<{
    applied: { index: number; textColor: string; backgroundColor: string };
    close: void;
  }>();

  const colors = [
    '#FFFFFF', '#C0C0C0', '#888888', '#000000',
    '#FF6B6B', '#FF7F50', '#FFA500', '#FFD93D',
    '#ADFF2F', '#6BCB77', '#20B2AA', '#4DD0E1',
    '#87CEEB', '#6C9EFF', '#7B68EE', '#B388FF',
    '#FF00FF', '#FF8FAB', '#FF69B4', '#8B0000',
    '#006400', '#00008B', '#4B0082'
  ];

  let textColor = '';
  let backgroundColor = '';
  let customTextColor = '#FFFFFF';
  let customBackgroundColor = '#1A1A2E';
  let saving = false;
  let error = '';
  let lastInitKey = '';

  $: {
    const key = show && tab ? `${sessionId}:${tab.Index}` : '';
    if (key && key !== lastInitKey) {
      textColor = tab?.TextColor || '';
      backgroundColor = tab?.BackgroundColor || '';
      if (isHex(textColor)) customTextColor = normalizeColorInput(textColor);
      if (isHex(backgroundColor)) customBackgroundColor = normalizeColorInput(backgroundColor);
      error = '';
      saving = false;
      lastInitKey = key;
    } else if (!show) {
      lastInitKey = '';
    }
  }

  function isHex(color: string): boolean {
    return /^#[0-9a-fA-F]{3,8}$/.test(color);
  }

  function normalizeColorInput(color: string): string {
    if (/^#[0-9a-fA-F]{6}$/.test(color)) return color;
    if (/^#[0-9a-fA-F]{8}$/.test(color)) return color.slice(0, 7);
    if (/^#[0-9a-fA-F]{3}$/.test(color)) {
      return `#${color[1]}${color[1]}${color[2]}${color[2]}${color[3]}${color[3]}`;
    }
    if (/^#[0-9a-fA-F]{4}$/.test(color)) {
      return `#${color[1]}${color[1]}${color[2]}${color[2]}${color[3]}${color[3]}`;
    }
    return '#FFFFFF';
  }

  function contrastColor(background: string): string {
    const hex = normalizeColorInput(background).slice(1);
    const r = parseInt(hex.slice(0, 2), 16);
    const g = parseInt(hex.slice(2, 4), 16);
    const b = parseInt(hex.slice(4, 6), 16);
    return (0.299 * r + 0.587 * g + 0.114 * b) / 255 > 0.55 ? '#111111' : '#FFFFFF';
  }

  function previewStyle(): string {
    const styles: string[] = [];
    if (backgroundColor) styles.push(`background: ${backgroundColor}`);
    if (textColor === 'auto' && backgroundColor) {
      styles.push(`color: ${contrastColor(backgroundColor)}`);
    } else if (isHex(textColor)) {
      styles.push(`color: ${textColor}`);
    }
    return styles.join('; ');
  }

  function close() {
    if (saving) return;
    show = false;
    dispatch('close');
  }

  function reset() {
    textColor = '';
    backgroundColor = '';
  }

  async function apply() {
    if (!tab || !sessionId || saving) return;
    saving = true;
    error = '';
    try {
      await App.SetTabColor(sessionId, tab.Index, textColor, backgroundColor);
      dispatch('applied', { index: tab.Index, textColor, backgroundColor });
      show = false;
    } catch (e) {
      error = String(e);
    } finally {
      saving = false;
    }
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') close();
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault();
      apply();
    }
  }
</script>

{#if show && tab}
  <div
    class="dialog-overlay"
    use:autoFocusDialog
    role="dialog"
    aria-modal="true"
    on:click|self={close}
    on:keydown={handleKeydown}
  >
    <div class="dialog-content tab-color-dialog">
      <div class="dialog-header">
        <h2>{$t('tabColor.title')}</h2>
        <button class="close-btn" on:click={close} aria-label={$t('color.cancel')}>×</button>
      </div>

      <div class="dialog-body">
        <div class="preview" style={previewStyle()}>
          <span class="preview-dot"></span>
          <span>{tab.Name}</span>
        </div>

        <section>
          <div class="section-heading">
            <span>{$t('color.textColors')}</span>
            <div class="quick-actions">
              <button class:active={textColor === ''} on:click={() => textColor = ''}>{$t('color.none')}</button>
              <button class:active={textColor === 'auto'} on:click={() => textColor = 'auto'}>{$t('color.auto')}</button>
            </div>
          </div>
          <div class="color-grid">
            {#each colors as color}
              <button
                class="color-swatch"
                class:selected={textColor.toUpperCase() === color}
                style="--swatch: {color}"
                title={color}
                aria-label={color}
                on:click={() => textColor = color}
              ></button>
            {/each}
          </div>
          <label class="custom-color">
            <span>{$t('tabColor.custom')}</span>
            <input type="color" bind:value={customTextColor} on:input={() => textColor = customTextColor} />
            <code>{textColor || '—'}</code>
          </label>
        </section>

        <section>
          <div class="section-heading">
            <span>{$t('color.bgColors')}</span>
            <div class="quick-actions">
              <button class:active={backgroundColor === ''} on:click={() => backgroundColor = ''}>{$t('color.none')}</button>
            </div>
          </div>
          <div class="color-grid">
            {#each colors as color}
              <button
                class="color-swatch"
                class:selected={backgroundColor.toUpperCase() === color}
                style="--swatch: {color}"
                title={color}
                aria-label={color}
                on:click={() => backgroundColor = color}
              ></button>
            {/each}
          </div>
          <label class="custom-color">
            <span>{$t('tabColor.custom')}</span>
            <input type="color" bind:value={customBackgroundColor} on:input={() => backgroundColor = customBackgroundColor} />
            <code>{backgroundColor || '—'}</code>
          </label>
        </section>

        {#if error}<div class="error-message">{error}</div>{/if}
      </div>

      <div class="dialog-footer">
        <button class="btn-cancel reset-btn" on:click={reset}>{$t('tabColor.reset')}</button>
        <span class="footer-spacer"></span>
        <button class="btn-cancel" on:click={close}>{$t('color.cancel')}</button>
        <button class="btn-primary" disabled={saving} on:click={apply}>
          {saving ? `${$t('common.save')}…` : $t('color.apply')}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .tab-color-dialog { max-width: 500px; }
  .dialog-body { padding: 20px 24px; display: flex; flex-direction: column; gap: 18px; }
  .preview {
    display: flex; align-items: center; gap: 9px; min-height: 38px; padding: 0 14px;
    border: 1px solid rgba(255,255,255,.1); border-radius: 8px;
    background: rgba(255,255,255,.05); color: #e4e4e7; font-size: 13px; font-weight: 600;
  }
  .preview-dot { width: 7px; height: 7px; border-radius: 50%; background: currentColor; opacity: .8; }
  section { display: flex; flex-direction: column; gap: 9px; }
  .section-heading { display: flex; align-items: center; justify-content: space-between; color: #a1a1aa; font-size: 12px; font-weight: 600; }
  .quick-actions { display: flex; gap: 5px; }
  .quick-actions button {
    padding: 3px 8px; border-radius: 5px; border: 1px solid rgba(255,255,255,.1);
    background: rgba(255,255,255,.04); color: #9ca3af; cursor: pointer; font-size: 10px;
  }
  .quick-actions button.active { border-color: #8b5cf6; color: #c4b5fd; background: rgba(139,92,246,.18); }
  .color-grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: 6px; }
  .color-swatch {
    aspect-ratio: 1; min-width: 0; border-radius: 5px; border: 2px solid transparent;
    background: var(--swatch); cursor: pointer; box-shadow: inset 0 0 0 1px rgba(255,255,255,.14);
  }
  .color-swatch:hover { transform: scale(1.12); }
  .color-swatch.selected { border-color: #fff; box-shadow: 0 0 0 2px #8b5cf6; }
  .custom-color { display: flex; align-items: center; gap: 9px; color: #71717a; font-size: 11px; }
  .custom-color input { width: 30px; height: 24px; padding: 0; border: 0; background: none; cursor: pointer; }
  .custom-color code { color: #a1a1aa; font-size: 10px; }
  .error-message { color: #f87171; font-size: 12px; }
  .dialog-footer { display: flex; align-items: center; }
  .footer-spacer { flex: 1; }
  .reset-btn { color: #fca5a5; }
  button:disabled { opacity: .55; cursor: default; }
  @media (max-width: 620px) { .color-grid { grid-template-columns: repeat(8, 1fr); } }
</style>
