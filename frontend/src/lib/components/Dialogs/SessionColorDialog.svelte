<script lang="ts">
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import { autoFocusDialog } from '../../utils/dialogActions';
  // Rendered into <body>: this dialog is opened from the sidebar, whose scroll
  // container carries transform/contain for a WebKitGTK compositing workaround.
  // Either of those makes an ancestor the containing block for position:fixed,
  // so the overlay was laid out inside the sidebar instead of the window.
  import { portal } from '../../utils/portal';
  import { createEventDispatcher } from 'svelte';
  import { get } from 'svelte/store';
  import { setSessionColor, setGroupColor, type Group, type Session } from '../../stores/sessions';
  import { t } from '../../i18n';
  import { activeProjectId } from '../../stores/projects';
  import {
    colorOptions,
    gradientOptions,
    gradients,
    getContrastColor,
    getGradientCSS,
    isGradient,
    isCustomGradient,
    gradientTextStyle,
  } from '../../utils/rowColors';
  import CustomGradientDialog from './CustomGradientDialog.svelte';

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

  // The custom gradient editor, opened from the last swatch of the grid.
  let showCustom = false;
  let overlayEl: HTMLDivElement;
  let targetProjectId = '';
  let targetId = '';
  let targetKind: 'session' | 'group' | '' = '';
  let targetCaptured = false;

  // Initialize once per open cycle. If a keyed sidebar row is reused for a
  // same-id object in another project, close instead of silently rebinding the
  // already-edited swatches to that replacement object.
  $: {
    const kind = session ? 'session' : group ? 'group' : '';
    if (show && target && !targetCaptured) {
      selectedColor = target!.color || '';
      selectedBgColor = target!.bgColor || '';
      fullRowColor = target!.fullRowColor || false;
      colorMode = 'text';
      // Each opening starts with the editor shut. See the reset below.
      showCustom = false;
      targetProjectId = $activeProjectId;
      targetId = target!.id;
      targetKind = kind;
      targetCaptured = true;
    } else if (show && targetCaptured &&
        (targetProjectId !== $activeProjectId || targetId !== target?.id || targetKind !== kind)) {
      close();
    } else if (!show) {
      // The editor lives inside this dialog's {#if}, so closing the dialog
      // takes it off screen without ever closing it: showCustom stayed true,
      // and the editor popped up on its own the next time a colour dialog
      // opened, for whichever row that was.
      showCustom = false;
      targetCaptured = false;
      targetProjectId = '';
      targetId = '';
      targetKind = '';
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
      claimKeyForDialog();
      e.stopPropagation();
      close();
    } else if (e.key === 'Tab') {
      e.preventDefault();
      colorMode = colorMode === 'text' ? 'bg' : 'text';
    } else if (e.key === 'f') {
      fullRowColor = !fullRowColor;
    }
  }

  async function applyColor() {
    const projectId = targetProjectId;
    const id = targetId;
    const kind = targetKind;
    if (projectId !== get(activeProjectId) || !id) return close();
    if (kind === 'session') {
      await setSessionColor(id, selectedColor, selectedBgColor, fullRowColor);
    } else if (kind === 'group') {
      await setGroupColor(id, selectedColor, selectedBgColor, fullRowColor);
    } else {
      return;
    }
    if (projectId === get(activeProjectId)) close();
  }

  function applyCustomGradient(e: CustomEvent<string>) {
    selectedColor = e.detail;
  }

  // Back to this dialog's overlay, or its own keys (Escape, Tab, F) would do
  // nothing until it was clicked: the editor took focus and then went away.
  function customClosed() {
    overlayEl?.focus();
  }

  function selectColor(color: string) {
    if (colorMode === 'text') {
      selectedColor = color;
    } else {
      selectedBgColor = color;
    }
  }

  // Get preview style for session name
  //
  // The colours are parameters, not read from the component, on purpose. A
  // call with no arguments in the template is compiled untracked — Svelte 5's
  // legacy mode keeps Svelte 4's rule that a template only depends on what it
  // names — so picking a background (or a plain text colour) never updated the
  // preview. Only "full row" did, because that styles the row from
  // selectedBgColor directly.
  function getPreviewStyle(fg: string, bg: string): string {
    let style = '';

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

  /**
   * A gradient as a plain background, for the swatches in the grid.
   *
   * Separate from the text version below: a swatch is an empty span, and
   * clipping a gradient to text it does not have paints nothing at all.
   */
  function getGradientSwatchStyle(gradientName: string): string {
    const css = getGradientCSS(gradientName);
    if (css === gradientName) return '';
    return `background-image: ${css};`;
  }

  /**
   * The preview's gradient, from the same helper the sidebar renders with — so
   * what is previewed is what the list will show.
   *
   * It had its own copy, which differed in the case that matters: for a name it
   * did not recognise it returned an empty string, leaving the text with
   * `-webkit-text-fill-color: transparent` and no background to clip against.
   * The result was an invisible name rather than a wrong colour.
   */
  function getGradientTextStyle(gradientName: string): string {
    const css = getGradientCSS(gradientName);
    // Not a gradient at all: nothing to clip. (An unreadable gradient comes
    // back as a grey one, which is safe to paint.)
    if (css === gradientName) return '';
    // The shared style (see gradientTextStyle for why it is background-image
    // and not the shorthand). inline-block for the reason the .gradient-text
    // rule below gives, repeated inline so it holds whatever the stylesheet
    // does.
    return gradientTextStyle(gradientName, 'display: inline-block;');
  }
</script>

{#if show && target}
  <div
    class="dialog-overlay" use:portal use:autoFocusDialog
    bind:this={overlayEl}
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
    tabindex="-1"
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
            <!-- Any gradient, including one that cannot be read: that one is
                 painted grey (see getGradientCSS), so the name always has
                 something to be clipped against. -->
            {#if isGradient(selectedColor)}
              <!-- No whitespace around the name: on an inline-block the
                   surrounding newlines become spaces inside the clipped box,
                   and the gradient shows through them as bars either side. -->
              <!-- The background chip goes around the gradient, not on it:
                   the text clip would take the chip with it. -->
              <span class="preview-name" style={getPreviewStyle(selectedColor, selectedBgColor)}><span class="gradient-text" style={getGradientTextStyle(selectedColor)}>{target.name}</span></span>
            {:else}
              <span class="preview-name" style={getPreviewStyle(selectedColor, selectedBgColor)}>
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
              {$t('color.textLabel', { color: isCustomGradient(selectedColor) ? $t('color.customGradient') : (selectedColor || $t('color.none')) })}
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
                  <span class="color-swatch gradient-swatch" style={getGradientSwatchStyle(option.color)}></span>
                {:else}
                  <span class="color-swatch" style="background: {option.color}; box-shadow: 0 0 8px {option.color}40;"></span>
                {/if}
                <span class="color-name">{option.name}</span>
              </button>
            {/each}
            <!-- Last in the grid, for the text colour only: a gradient
                 background is never drawn (the chip and the row tint are flat
                 colours), so offering one there would do nothing. Shows the
                 custom gradient itself once one is chosen. -->
            {#if colorMode === 'text'}
              <button
                class="color-btn custom-btn"
                class:selected={isCustomGradient(selectedColor)}
                class:gradient={isCustomGradient(selectedColor)}
                on:click={() => (showCustom = true)}
                title={$t('color.customGradient')}
              >
                {#if isCustomGradient(selectedColor)}
                  <span class="color-swatch gradient-swatch" style={getGradientSwatchStyle(selectedColor)}></span>
                {:else}
                  <span class="color-swatch custom-swatch">+</span>
                {/if}
                <span class="color-name">{$t('color.custom')}</span>
              </button>
            {/if}
          </div>
        </div>
      </div>

      <div class="dialog-footer">
        <button class="btn-cancel" on:click={close}>{$t('color.cancel')}</button>
        <button class="btn-primary" on:click={applyColor}>{$t('color.apply')}</button>
      </div>
    </div>
  </div>

  <CustomGradientDialog
    bind:show={showCustom}
    initial={selectedColor}
    name={target.name}
    on:apply={applyCustomGradient}
    on:close={customClosed}
  />
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
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #6b7280;
    margin-bottom: 10px;
  }

  .preview-section {
    margin-bottom: 16px;
  }

  /* The "custom" swatch before a gradient is chosen: an invitation, not a
     colour. */
  .custom-swatch {
    display: flex;
    align-items: center;
    justify-content: center;
    border: 1px dashed rgba(255, 255, 255, 0.35);
    color: #d4d4d8;
    font-size: 14px;
    line-height: 1;
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

  .gradient-text {
    /* inline-block, because background-clip:text clips to the TEXT only on an
       inline box. As a flex item this span is blockified, and the clip then
       takes the whole box — which painted the gradient as a solid bar the width
       of the element, with the name invisible inside it.
       The sidebar never hit this: there the gradient span sits inside another
       span, so it stays inline. */
    display: inline-block;
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
    font-size: 12px;
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
    font-size: 12px;
    color: #9ca3af;
    cursor: pointer;
  }

  .full-row-toggle input {
    accent-color: var(--accent);
  }

  .hint {
    font-size: 11px;
    color: #4b5563;
    margin-left: auto;
  }

  .color-section {
    margin-bottom: 12px;
  }

  .color-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
    gap: 6px;
    padding-right: 4px;
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
    font-size: 11px;
    color: #9ca3af;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
