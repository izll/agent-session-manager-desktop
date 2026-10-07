<script lang="ts">
  /**
   * Asked after a view is dropped in a new place in the view bar: whether
   * that is the order of every tab or of this one alone. Both exist, and a
   * drag that silently picked one left the other looking unchanged.
   *
   * The answer can be kept, so the question stops; Settings changes it.
   */
  import { createEventDispatcher } from 'svelte';
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { portal } from '../../utils/portal';
  import { t } from '../../i18n';
  import DialogCloseButton from '../common/DialogCloseButton.svelte';

  /** Whether the tab has an order of its own, which "every tab" replaces. */
  export let hasOwn = false;

  const dispatch = createEventDispatcher<{
    choose: { target: 'global' | 'tab'; remember: boolean };
    cancel: void;
  }>();

  let remember = false;

  function choose(target: 'global' | 'tab') {
    dispatch('choose', { target, remember });
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      claimKeyForDialog();
      e.stopPropagation();
      dispatch('cancel');
    }
  }
</script>

<div
  class="dialog-overlay"
  use:portal
  use:autoFocusDialog
  on:keydown={handleKeydown}
  role="dialog"
  aria-modal="true"
  tabindex="-1"
  data-dialog="view-order-drop"
>
  <div class="dialog-content view-order-drop">
    <div class="dialog-header">
      <h2>{$t('viewOrder.dropTitle')}</h2>
      <DialogCloseButton on:click={() => dispatch('cancel')} />
    </div>
    <div class="dialog-body">
      <div class="choices">
        <button class="choice" data-target="global" on:click={() => choose('global')}>
          <span class="choice-label">{$t('viewOrder.dropGlobal')}</span>
          <span class="choice-desc">
            {hasOwn ? $t('viewOrder.dropGlobalDescOwn') : $t('viewOrder.dropGlobalDesc')}
          </span>
        </button>
        <button class="choice" data-target="tab" on:click={() => choose('tab')}>
          <span class="choice-label">{$t('viewOrder.dropTab')}</span>
          <span class="choice-desc">{$t('viewOrder.dropTabDesc')}</span>
        </button>
      </div>
      <label class="remember">
        <input type="checkbox" bind:checked={remember} data-remember />
        <span>{$t('viewOrder.dropRemember')}</span>
      </label>
    </div>
    <div class="dialog-footer">
      <button class="btn-secondary" on:click={() => dispatch('cancel')}>{$t('common.cancel')}</button>
    </div>
  </div>
</div>

<style>
  .view-order-drop {
    width: min(440px, 94vw);
    max-width: min(440px, 94vw);
  }
  .choices {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .choice {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 3px;
    padding: 10px 12px;
    border-radius: 8px;
    border: 1px solid rgba(255, 255, 255, 0.1);
    background: rgba(255, 255, 255, 0.03);
    color: inherit;
    font-family: inherit;
    text-align: left;
    cursor: pointer;
  }
  .choice:hover,
  .choice:focus-visible {
    border-color: rgba(var(--accent-rgb), 0.5);
    background: rgba(var(--accent-rgb), 0.1);
  }
  .choice-label { font-size: 13px; font-weight: 600; color: #e4e4e7; }
  .choice-desc { font-size: 12px; color: #9ca3af; }
  .remember {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 14px;
    font-size: 12px;
    color: #9ca3af;
    cursor: pointer;
  }
</style>
