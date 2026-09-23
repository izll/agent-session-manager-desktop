<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import { autoFocusDialog, dialogEnterBelongsToControl } from '../../utils/dialogActions';
  // Into <body>, like the colour dialog it opens from: rendered inside it, it
  // would share that dialog's keydown handler and Escape would close both.
  import { portal } from '../../utils/portal';
  import { t } from '../../i18n';
  import { getNameStyle, isHexColor } from '../../utils/rowColors';

  export let show = false;
  /** The background the colour dialog has selected now; seeds the picker. */
  export let initial = '';
  /** The text colour, so the preview shows the name as the list will. */
  export let textColor = '';
  /** The name the colour is for, shown in the preview. */
  export let name = '';

  const dispatch = createEventDispatcher<{ apply: string; close: void }>();

  const DEFAULT_COLOR = '#6C9EFF';
  // What the hex field says, kept apart from the colour so a half-typed value
  // can sit in the field. Apply waits until it names a colour, so what is
  // applied is always what the field shows.
  let text = DEFAULT_COLOR;
  let seeded = false;

  $: if (show && !seeded) {
    text = isHexColor(initial) ? initial.toUpperCase() : DEFAULT_COLOR;
    seeded = true;
  } else if (!show) {
    seeded = false;
  }

  $: color = colorFromText(text);
  $: previewStyle = color ? getNameStyle(textColor, color, false) : '';

  /** The colour the field names, or null while it names none. */
  function colorFromText(input: string): string | null {
    let hex = input.trim();
    if (!hex.startsWith('#')) hex = '#' + hex;
    return isHexColor(hex) ? hex.toUpperCase() : null;
  }

  function close() {
    show = false;
    dispatch('close');
  }

  function apply() {
    if (!color) return;
    dispatch('apply', color);
    close();
  }

  function handleKeydown(e: KeyboardEvent) {
    // A separate overlay from the colour dialog, but a window-level listener
    // would still see the key.
    e.stopPropagation();
    if (e.key === 'Escape') {
      claimKeyForDialog();
      close();
    } else if (e.key === 'Enter' && !dialogEnterBelongsToControl(e)) {
      claimKeyForDialog();
      apply();
    }
  }
</script>

{#if show}
  <div
    class="dialog-overlay" use:portal use:autoFocusDialog
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
    tabindex="-1"
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2>{$t('color.customColor')}</h2>
        <button class="close-btn" on:click={close} aria-label={$t('color.cancel')}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/>
            <line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>

      <div class="dialog-body">
        <div class="preview">
          <span class="preview-name" style={previewStyle}>{name}</span>
        </div>

        <div class="picker">
          <input
            type="color"
            value={(color ?? DEFAULT_COLOR).toLowerCase()}
            aria-label={$t('color.customColor')}
            on:input={(e) => (text = e.currentTarget.value.toUpperCase())}
          />
          <input
            type="text"
            class="hex"
            class:invalid={!color}
            value={text}
            maxlength="7"
            spellcheck="false"
            aria-label={$t('color.customColor')}
            aria-invalid={!color}
            title={color ? undefined : $t('color.customGradientInvalid')}
            on:input={(e) => (text = e.currentTarget.value)}
          />
        </div>
      </div>

      <div class="dialog-actions">
        <button class="btn-cancel" on:click={close}>{$t('color.cancel')}</button>
        <button class="btn-primary" on:click={apply} disabled={!color}>{$t('color.apply')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .dialog-content {
    width: min(420px, 94vw);
    max-width: min(420px, 94vw);
  }

  .dialog-body {
    padding: 20px 24px;
  }

  .preview {
    padding: 12px 14px;
    border-radius: 8px;
    background: rgba(0, 0, 0, 0.25);
  }

  .preview-name {
    font-size: 15px;
    font-weight: 600;
    color: #e4e4e7;
  }

  .picker {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 16px;
  }

  .picker input[type='color'] {
    width: 40px;
    height: 30px;
    padding: 0;
    border: none;
    border-radius: 6px;
    background: none;
    cursor: pointer;
  }

  .hex {
    width: 96px;
    padding: 5px 8px;
    border-radius: 6px;
    border: 1px solid rgba(255, 255, 255, 0.08);
    background: rgba(0, 0, 0, 0.25);
    color: #e4e4e7;
    font-family: monospace;
    font-size: 13px;
  }

  .hex:focus {
    outline: none;
    border-color: rgba(var(--accent-rgb), 0.5);
  }

  .hex.invalid {
    border-color: rgba(239, 68, 68, 0.7);
  }

  .dialog-actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
  }

  .dialog-actions button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
