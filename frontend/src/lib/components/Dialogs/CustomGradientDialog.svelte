<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import { autoFocusDialog, dialogEnterBelongsToControl } from '../../utils/dialogActions';
  // Into <body>, like the colour dialog it opens from: that one is portalled
  // too, and a dialog rendered inside it would share its keydown handler, so
  // Escape here would close both.
  import { portal } from '../../utils/portal';
  import { t } from '../../i18n';
  import {
    customGradient,
    getGradientCSS,
    gradientStops,
    isHexColor,
    MIN_GRADIENT_STOPS,
    MAX_GRADIENT_STOPS,
  } from '../../utils/rowColors';

  export let show = false;
  /** What the colour dialog has selected now; seeds the stops. */
  export let initial = '';
  /** The name the gradient is for, shown in the preview. */
  export let name = '';

  const dispatch = createEventDispatcher<{ apply: string; close: void }>();

  const DEFAULT_STOPS = ['#FF6B6B', '#6C9EFF'];
  let stops: string[] = [...DEFAULT_STOPS];
  // What each stop's hex field says, kept apart from the stop itself so a
  // half-typed value can sit in its field while it is being typed.
  //
  // It used to be dropped silently instead: the field showed "#12" while the
  // stop kept its old colour, and Apply applied that old colour — something
  // the dialog was not showing. A field that does not hold a colour is now
  // marked, and Apply waits until every field does, so what is applied is
  // always what the fields say.
  let texts: string[] = [...stops];
  let seeded = false;

  // Seeded once per opening. A preset or a custom gradient is taken over as
  // the starting point, and a plain colour becomes the first stop, so there is
  // always something to adjust rather than a blank to fill.
  $: if (show && !seeded) {
    const current = gradientStops(initial);
    if (current) {
      stops = current.slice(0, MAX_GRADIENT_STOPS).map(stop => stop.toUpperCase());
    } else if (isHexColor(initial)) {
      stops = [initial.toUpperCase(), DEFAULT_STOPS[1]];
    } else {
      stops = [...DEFAULT_STOPS];
    }
    texts = [...stops];
    seeded = true;
  } else if (!show) {
    seeded = false;
  }

  $: value = customGradient(stops);
  $: css = getGradientCSS(value);
  $: invalid = texts.map(text => stopFromText(text) === null);
  $: anyInvalid = invalid.some(Boolean);

  /** The colour a hex field names, or null while it names none. */
  function stopFromText(input: string): string | null {
    let hex = input.trim();
    if (!hex.startsWith('#')) hex = '#' + hex;
    return isHexColor(hex) ? hex.toUpperCase() : null;
  }

  function typeStop(index: number, input: string) {
    texts[index] = input;
    texts = texts;
    // A half-typed value stays in its field, marked, and changes nothing yet.
    const hex = stopFromText(input);
    if (hex === null) return;
    stops[index] = hex;
    stops = stops;
  }

  function pickStop(index: number, input: string) {
    const hex = stopFromText(input);
    if (hex === null) return;
    stops[index] = hex;
    texts[index] = hex;
    stops = stops;
    texts = texts;
  }

  function addStop() {
    if (stops.length >= MAX_GRADIENT_STOPS) return;
    const last = stops[stops.length - 1];
    stops = [...stops, last];
    texts = [...texts, last];
  }

  function removeStop(index: number) {
    if (stops.length <= MIN_GRADIENT_STOPS) return;
    stops = stops.filter((_, at) => at !== index);
    texts = texts.filter((_, at) => at !== index);
  }

  function close() {
    show = false;
    dispatch('close');
  }

  function apply() {
    if (!value || anyInvalid) return;
    dispatch('apply', value);
    close();
  }

  function handleKeydown(e: KeyboardEvent) {
    // Kept from the colour dialog underneath: it is a separate overlay, but a
    // window-level listener would still see the key.
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
    class="dialog-overlay custom-gradient-overlay" use:portal use:autoFocusDialog
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
    tabindex="-1"
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2>{$t('color.customGradient')}</h2>
        <button class="close-btn" on:click={close} aria-label={$t('color.cancel')}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/>
            <line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>

      <div class="dialog-body">
        <div class="preview">
          <!-- The same two layers the sidebar uses: clip to the letters on an
               inline span of its own. -->
          <span class="preview-name" style="background-image: {css}; -webkit-background-clip: text; background-clip: text; -webkit-text-fill-color: transparent;">{name}</span>
        </div>

        <div class="bar" style="background-image: {css};"></div>

        <div class="stops">
          {#each stops as stop, index}
            <div class="stop">
              <input
                type="color"
                value={stop.toLowerCase()}
                aria-label={$t('color.customGradientStop', { n: index + 1 })}
                on:input={(e) => pickStop(index, e.currentTarget.value)}
              />
              <input
                type="text"
                class="hex"
                class:invalid={invalid[index]}
                value={texts[index]}
                maxlength="7"
                spellcheck="false"
                aria-label={$t('color.customGradientStop', { n: index + 1 })}
                aria-invalid={invalid[index]}
                title={invalid[index] ? $t('color.customGradientInvalid') : undefined}
                on:input={(e) => typeStop(index, e.currentTarget.value)}
              />
              {#if stops.length > MIN_GRADIENT_STOPS}
                <button
                  class="remove"
                  title={$t('color.customGradientRemove')}
                  aria-label={$t('color.customGradientRemove')}
                  on:click={() => removeStop(index)}
                >×</button>
              {/if}
            </div>
          {/each}
          {#if stops.length < MAX_GRADIENT_STOPS}
            <button class="add" on:click={addStop}>+ {$t('color.customGradientAdd')}</button>
          {/if}
        </div>
      </div>

      <div class="dialog-actions">
        <button class="btn-cancel" on:click={close}>{$t('color.cancel')}</button>
        <button class="btn-primary" on:click={apply} disabled={!value || anyInvalid}>{$t('color.apply')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .dialog-content {
    width: min(520px, 94vw);
    max-width: min(520px, 94vw);
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
  }

  .bar {
    height: 16px;
    margin: 16px 0;
    border-radius: 8px;
  }

  .stops {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }

  .stop {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 3px;
    border-radius: 8px;
    background: rgba(0, 0, 0, 0.2);
  }

  .stop input[type='color'] {
    width: 28px;
    height: 24px;
    padding: 0;
    border: none;
    border-radius: 5px;
    background: none;
    cursor: pointer;
  }

  .hex {
    width: 72px;
    padding: 3px 6px;
    border-radius: 5px;
    border: 1px solid rgba(255, 255, 255, 0.08);
    background: rgba(0, 0, 0, 0.25);
    color: #e4e4e7;
    font-family: monospace;
    font-size: 12px;
  }

  .hex:focus {
    outline: none;
    border-color: rgba(var(--accent-rgb), 0.5);
  }

  /* After :focus, so the field being typed into still shows it is wrong. */
  .hex.invalid {
    border-color: rgba(239, 68, 68, 0.7);
    color: #fca5a5;
  }

  .remove {
    width: 20px;
    height: 20px;
    border: none;
    border-radius: 4px;
    background: none;
    color: #9ca3af;
    cursor: pointer;
    line-height: 1;
  }

  .remove:hover {
    color: #fca5a5;
    background: rgba(239, 68, 68, 0.12);
  }

  .add {
    padding: 5px 10px;
    border-radius: 6px;
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(255, 255, 255, 0.04);
    color: #d4d4d8;
    font-size: 12px;
    cursor: pointer;
  }

  .add:hover {
    background: rgba(255, 255, 255, 0.08);
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
