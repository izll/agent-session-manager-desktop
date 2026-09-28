<!--
  The ✕ in a dialog's header, one for every dialog.
  Each dialog used to write its own, and they drifted: some an svg, some a
  bare "×" glyph at whatever font size the dialog chose, some restyled to a
  borderless 20px character — which is why the background-agents, servers and
  saved-commands dialogs had a smaller, different close button from Settings.
  The look lives in the global .close-btn (style.css); this is only the markup.
-->
<script lang="ts">
  import { t } from '../../i18n';

  /** Accessible name. Defaults to "Close"; a dialog whose ✕ discards an edit
      passes its own ("Cancel"). */
  export let label: string | undefined = undefined;
  export let disabled = false;
</script>

<button
  type="button"
  class="close-btn"
  {disabled}
  aria-label={label ?? $t('common.close')}
  title={label ?? $t('common.close')}
  on:click
>
  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
    <line x1="18" y1="6" x2="6" y2="18"/>
    <line x1="6" y1="6" x2="18" y2="18"/>
  </svg>
</button>
