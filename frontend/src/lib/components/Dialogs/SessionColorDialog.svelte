<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { setSessionColor, type Session } from '../../stores/sessions';
  import { t } from '../../i18n';

  export let show = false;
  export let session: Session | null = null;

  const dispatch = createEventDispatcher();

  // Gradients from TUI
  const gradients: Record<string, string[]> = {
    'gradient-rainbow':  ['#FF0000', '#FF7F00', '#FFFF00', '#00FF00', '#00FFFF', '#0000FF', '#8B00FF'],
    'gradient-sunset':   ['#FF512F', '#F09819', '#FF8C00', '#DD2476', '#FF416C'],
    'gradient-ocean':    ['#00D2FF', '#3A7BD5', '#00D2D3', '#54A0FF', '#2E86DE'],
    'gradient-forest':   ['#134E5E', '#11998E', '#38EF7D', '#A8E063', '#56AB2F'],
    'gradient-fire':     ['#FF0000', '#FF4500', '#FF6347', '#FF8C00', '#FFD700'],
    'gradient-ice':      ['#E0FFFF', '#B0E0E6', '#87CEEB', '#00CED1', '#4682B4'],
    'gradient-neon':     ['#FF00FF', '#00FFFF', '#39FF14', '#FF6600', '#BF00FF'],
    'gradient-galaxy':   ['#0F0C29', '#302B63', '#8E2DE2', '#4A00E0', '#24243E'],
    'gradient-pastel':   ['#FFB6C1', '#FFDAB9', '#FFFACD', '#98FB98', '#ADD8E6', '#E6E6FA'],
    'gradient-pink':     ['#FF69B4', '#FF1493', '#DB7093', '#FF69B4'],
    'gradient-blue':     ['#00BFFF', '#1E90FF', '#4169E1', '#0000FF', '#4169E1', '#1E90FF'],
    'gradient-green':    ['#00FF00', '#32CD32', '#228B22', '#006400', '#228B22', '#32CD32'],
    'gradient-gold':     ['#FFD700', '#FFA500', '#FF8C00', '#FFA500', '#FFD700'],
    'gradient-purple':   ['#9400D3', '#8A2BE2', '#9932CC', '#BA55D3', '#9932CC', '#8A2BE2'],
    'gradient-cyber':    ['#00FF00', '#00FFFF', '#FF00FF', '#00FFFF', '#00FF00'],
  };

  // Color options from TUI
  const colorOptions = [
    { name: 'none', color: '' },
    { name: 'auto', color: 'auto' },
    { name: 'black', color: '#000000' },
    { name: 'white', color: '#FFFFFF' },
    { name: 'red', color: '#FF6B6B' },
    { name: 'orange', color: '#FFA500' },
    { name: 'yellow', color: '#FFD93D' },
    { name: 'lime', color: '#ADFF2F' },
    { name: 'green', color: '#6BCB77' },
    { name: 'teal', color: '#20B2AA' },
    { name: 'cyan', color: '#4DD0E1' },
    { name: 'sky', color: '#87CEEB' },
    { name: 'blue', color: '#6C9EFF' },
    { name: 'indigo', color: '#7B68EE' },
    { name: 'purple', color: '#B388FF' },
    { name: 'magenta', color: '#FF00FF' },
    { name: 'pink', color: '#FF8FAB' },
    { name: 'rose', color: '#FF69B4' },
    { name: 'coral', color: '#FF7F50' },
    { name: 'gold', color: '#FFD700' },
    { name: 'silver', color: '#C0C0C0' },
    { name: 'gray', color: '#888888' },
    { name: 'dark-red', color: '#8B0000' },
    { name: 'dark-green', color: '#006400' },
    { name: 'dark-blue', color: '#00008B' },
    { name: 'dark-purple', color: '#4B0082' },
  ];

  // Gradient options
  const gradientOptions = Object.keys(gradients).map(name => ({ name, color: name }));

  let selectedColor = '';
  let selectedBgColor = '';
  let fullRowColor = false;
  let colorMode: 'text' | 'bg' = 'text'; // Which color we're editing
  let lastInitKey = '';

  // Initialize fields only when dialog opens for a (new) session, not on every session update
  $: {
    const key = show && session ? `${show}|${session.id}` : '';
    if (key && key !== lastInitKey) {
      selectedColor = session!.color || '';
      selectedBgColor = session!.bgColor || '';
      fullRowColor = session!.fullRowColor || false;
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

  function isGradient(color: string): boolean {
    return color.startsWith('gradient-');
  }

  function getContrastColor(bgColor: string): string {
    if (!bgColor || bgColor === 'auto') return '#FFFFFF';
    const hex = bgColor.replace('#', '');
    if (hex.length !== 6) return '#FFFFFF';
    const r = parseInt(hex.slice(0, 2), 16);
    const g = parseInt(hex.slice(2, 4), 16);
    const b = parseInt(hex.slice(4, 6), 16);
    const luminance = (0.299 * r + 0.587 * g + 0.114 * b) / 255;
    return luminance > 0.5 ? '#000000' : '#FFFFFF';
  }

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
    if (!session) return;
    await setSessionColor(session.id, selectedColor, selectedBgColor, fullRowColor);
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

{#if show && session}
  <div
    class="dialog-overlay"
    on:click|self={close}
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2>{$t('color.title')}</h2>
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
                {session.name}
              </span>
            {:else}
              <span class="preview-name" style={getPreviewStyle()}>
                {session.name}
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
  /* Component-specific: wider dialog for color grid */
  .dialog-content {
    max-width: 480px;
    max-height: 80vh;
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
    background: rgba(139, 92, 246, 0.2);
    border-color: rgba(139, 92, 246, 0.4);
    color: #a78bfa;
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
    accent-color: #8b5cf6;
  }

  .hint {
    font-size: 10px;
    color: #4b5563;
    margin-left: auto;
  }

  .color-section {
    margin-bottom: 12px;
  }

  .color-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 6px;
    max-height: 300px;
    overflow-y: auto;
    padding-right: 4px;
  }

  .color-grid::-webkit-scrollbar {
    width: 4px;
  }

  .color-grid::-webkit-scrollbar-track {
    background: transparent;
  }

  .color-grid::-webkit-scrollbar-thumb {
    background: rgba(139, 92, 246, 0.3);
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
    background: rgba(139, 92, 246, 0.15);
    border-color: rgba(139, 92, 246, 0.4);
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
    background: linear-gradient(135deg, #fbbf24, #a78bfa);
    color: white;
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
