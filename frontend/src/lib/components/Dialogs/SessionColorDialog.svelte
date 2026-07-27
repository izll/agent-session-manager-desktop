<script lang="ts">
  import { autoFocusDialog } from '../../utils/dialogActions';
  // Rendered into <body>: this dialog is opened from the sidebar, whose scroll
  // container carries transform/contain for a WebKitGTK compositing workaround.
  // Either of those makes an ancestor the containing block for position:fixed,
  // so the overlay was laid out inside the sidebar instead of the window.
  import { portal } from '../../utils/portal';
  import { createEventDispatcher } from 'svelte';
  import { setSessionColor, setGroupColor, type Group, type Session } from '../../stores/sessions';
  import { t } from '../../i18n';
  import {
    colorOptions,
    gradientOptions,
    gradients,
    getContrastColor,
    isGradient,
  } from '../../utils/rowColors';

  export let show = false;
  export let session: Session | null = null;
  /** Groups carry the same three colour fields, so they reuse this dialog. */
  export let group: Group | null = null;

  const dispatch = createEventDispatcher();

  // Whichever of the two is set is the thing being recoloured.
  $: target = session || group;

  let selectedColor = '';
  let selectedBgColor = '';
  let fullRowColor = false;
  let colorMode: 'text' | 'bg' = 'text'; // Which color we're editing
  let lastInitKey = '';

  // Initialize fields only when dialog opens for a (new) target, not on every target update
  $: {
    const key = show && target ? `${show}|${target.id}` : '';
    if (key && key !== lastInitKey) {
      selectedColor = target!.color || '';
      selectedBgColor = target!.bgColor || '';
      fullRowColor = target!.fullRowColor || false;
      colorMode = 'text';
      lastInitKey = key;
    } else if (!show) {
      lastInitKey = '';
    }
  }

  // Get filtered options based on mode (gradients only for text)
  $: filteredOptions = colorMode === 'text'
    ? [...colorOptions, ...gradientOptions]
    : colorOptions.filter(c => c.name !== 'auto'); // No auto for background

  $: currentValue = colorMode === 'text' ? selectedColor : selectedBgColor;

  function close() {
    show = false;
    dispatch('close');
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      close();
    } else if (e.key === 'Tab') {
      e.preventDefault();
      colorMode = colorMode === 'text' ? 'bg' : 'text';
    } else if (e.key === 'f') {
      fullRowColor = !fullRowColor;
    }
  }

  async function applyColor() {
    if (session) {
      await setSessionColor(session.id, selectedColor, selectedBgColor, fullRowColor);
    } else if (group) {
      await setGroupColor(group.id, selectedColor, selectedBgColor, fullRowColor);
    } else {
      return;
    }
    close();
  }

  function selectColor(color: string) {
    if (colorMode === 'text') {
      selectedColor = color;
    } else {
      selectedBgColor = color;
    }
  }

  // Get preview style for session name
  function getPreviewStyle(): string {
    let style = '';
    const fg = selectedColor;
    const bg = selectedBgColor;

    if (bg && bg !== 'auto' && !isGradient(bg)) {
      style += `background-color: ${bg};`;
    }

    if (fg && fg !== 'auto' && !isGradient(fg)) {
      style += `color: ${fg};`;
    } else if (fg === 'auto' && bg && !isGradient(bg)) {
      style += `color: ${getContrastColor(bg)};`;
    } else if (!fg && bg && !isGradient(bg)) {
      style += `color: ${getContrastColor(bg)};`;
    }

    return style;
  }

  // Create gradient CSS for preview
  function getGradientStyle(gradientName: string): string {
    const colors = gradients[gradientName];
    if (!colors) return '';
    return `background: linear-gradient(90deg, ${colors.join(', ')});`;
  }
</script>

{#if show && target}
  <div
    class="dialog-overlay" use:portal use:autoFocusDialog
    on:click|self={close}
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2>{group ? $t('color.groupTitle') : $t('color.title')}</h2>
        <button class="close-btn" on:click={close}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/>
            <line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>

      <div class="dialog-body">
        <!-- Preview -->
        <div class="preview-section">
          <span class="label">{$t('color.preview')}</span>
          <div
            class="session-preview"
            class:full-row={fullRowColor && selectedBgColor}
            style={selectedBgColor && fullRowColor && !isGradient(selectedBgColor) ? `background: ${selectedBgColor}20` : ''}
          >
            <span class="preview-dot"></span>
            {#if isGradient(selectedColor)}
              <span class="preview-name gradient-text" style={getGradientStyle(selectedColor)}>
                {target.name}
              </span>
            {:else}
              <span class="preview-name" style={getPreviewStyle()}>
                {target.name}
              </span>
            {/if}
          </div>
        </div>

        <!-- Mode Toggle -->
        <div class="mode-section">
          <div class="mode-toggle">
            <button
              class="mode-btn"
              class:active={colorMode === 'text'}
              on:click={() => colorMode = 'text'}
            >
              {$t('color.textLabel', { color: selectedColor || $t('color.none') })}
            </button>
            <button
              class="mode-btn"
              class:active={colorMode === 'bg'}
              on:click={() => colorMode = 'bg'}
            >
              {$t('color.bgLabel', { color: selectedBgColor || $t('color.none') })}
            </button>
          </div>
          <label class="full-row-toggle">
            <input type="checkbox" bind:checked={fullRowColor} />
            <span>{$t('color.fullRow')}</span>
          </label>
          <span class="hint">{$t('color.hint')}</span>
        </div>

        <!-- Color Grid -->
        <div class="color-section">
          <span class="label">{colorMode === 'text' ? $t('color.textColors') : $t('color.bgColors')}</span>
          <div class="color-grid">
            {#each filteredOptions as option}
              {@const isSelected = currentValue === option.color}
              {@const isGrad = isGradient(option.color)}
              <button
                class="color-btn"
                class:selected={isSelected}
                class:gradient={isGrad}
                on:click={() => selectColor(option.color)}
                title={option.name}
              >
                {#if option.color === ''}
                  <span class="color-swatch none-swatch">
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <line x1="18" y1="6" x2="6" y2="18"/>
                    </svg>
                  </span>
                {:else if option.color === 'auto'}
                  <span class="color-swatch auto-swatch">
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <path d="M12 3v1m0 16v1m-9-9h1m16 0h1m-2.64-6.36l-.7.7m-12.02 12.02l-.7.7m0-12.72l.7.7m12.02 12.02l.7.7"/>
                      <circle cx="12" cy="12" r="4"/>
                    </svg>
                  </span>
                {:else if isGrad}
                  <span class="color-swatch gradient-swatch" style={getGradientStyle(option.color)}></span>
                {:else}
                  <span class="color-swatch" style="background: {option.color}; box-shadow: 0 0 8px {option.color}40;"></span>
                {/if}
                <span class="color-name">{option.name}</span>
              </button>
            {/each}
          </div>
        </div>
      </div>

      <div class="dialog-footer">
        <button class="btn-cancel" on:click={close}>{$t('color.cancel')}</button>
        <button class="btn-primary" on:click={applyColor}>{$t('color.apply')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  /* Component-specific: wide enough that the colour grid fits without
     scrolling on a normal screen. */
  .dialog-content {
    width: min(760px, 94vw);
    max-width: min(760px, 94vw);
    max-height: 88vh;
    display: flex;
    flex-direction: column;
  }

  /* Component-specific: custom body padding and scroll */
  .dialog-body {
    padding: 20px 24px;
    overflow-y: auto;
    flex: 1;
  }

  .label {
    display: block;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #6b7280;
    margin-bottom: 10px;
  }

  .preview-section {
    margin-bottom: 16px;
  }

  .session-preview {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px 14px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.06);
    border-radius: 10px;
  }

  .preview-dot {
    width: 8px;
    height: 8px;
    background: #888;
    border-radius: 50%;
  }

  .preview-name {
    font-size: 13px;
    font-weight: 600;
    color: #e4e4e7;
  }

  .preview-name.gradient-text {
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
  }

  .mode-section {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 16px;
    flex-wrap: wrap;
  }

  .mode-toggle {
    display: flex;
    gap: 4px;
  }

  .mode-btn {
    padding: 6px 12px;
    font-size: 11px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 6px;
    color: #9ca3af;
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .mode-btn:hover {
    background: rgba(255, 255, 255, 0.06);
  }

  .mode-btn.active {
    background: rgba(var(--accent-rgb), 0.2);
    border-color: rgba(var(--accent-rgb), 0.4);
    color: var(--accent-light);
  }

  .full-row-toggle {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 11px;
    color: #9ca3af;
    cursor: pointer;
  }

  .full-row-toggle input {
    accent-color: var(--accent);
  }

  .hint {
    font-size: 10px;
    color: #4b5563;
    margin-left: auto;
  }

  .color-section {
    margin-bottom: 12px;
  }

  /* Fills the width instead of a fixed 4 columns, and has no inner height
     cap: a scrollbar inside the dialog's own scrollbar meant scrolling twice
     to reach the last colours. The dialog body still scrolls if it must. */
  .color-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
    gap: 6px;
    padding-right: 4px;
  }

  .color-grid::-webkit-scrollbar {
    width: 4px;
  }

  .color-grid::-webkit-scrollbar-track {
    background: transparent;
  }

  .color-grid::-webkit-scrollbar-thumb {
    background: rgba(var(--accent-rgb), 0.3);
    border-radius: 2px;
  }

  .color-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 8px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.06);
    border-radius: 6px;
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .color-btn:hover {
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(255, 255, 255, 0.1);
  }

  .color-btn.selected {
    background: rgba(var(--accent-rgb), 0.15);
    border-color: rgba(var(--accent-rgb), 0.4);
  }

  .color-swatch {
    width: 16px;
    height: 16px;
    border-radius: 4px;
    flex-shrink: 0;
  }

  .none-swatch {
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(255, 255, 255, 0.1);
    color: #6b7280;
  }

  .auto-swatch {
    display: flex;
    align-items: center;
    justify-content: center;
    background: linear-gradient(135deg, #fbbf24, var(--accent-light));
    color: var(--accent-ink);
  }

  .gradient-swatch {
    width: 16px;
    height: 16px;
  }

  .color-name {
    font-size: 10px;
    color: #9ca3af;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
