<script lang="ts">
  /**
   * The "…undo" bar, as Gmail has it.
   *
   * Floats above the interface rather than pushing it: an action that happened
   * should not reflow the page it happened on.
   */
  import { fly } from 'svelte/transition';
  import { undoState, runUndo, dismissUndo } from '../../stores/undo';
  import { t } from '../../i18n';

  let busy = false;

  async function undo() {
    if (busy) return;
    busy = true;
    try {
      await runUndo();
    } finally {
      busy = false;
    }
  }
</script>

{#if $undoState.action}
  <div class="undo-toast" transition:fly={{ y: 20, duration: 150 }} role="status" aria-live="polite">
    <span class="message">{$undoState.action.message}</span>

    <button class="undo-btn" on:click={undo} disabled={busy}>
      {$t('undo.action')}
      <!-- The countdown is shown, not just felt: a bar that vanishes without
           warning teaches people to click it immediately, every time. -->
      <span class="count">{$undoState.remaining}</span>
    </button>

    <button class="close" on:click={dismissUndo} aria-label={$t('common.close')}>×</button>
  </div>
{/if}

<style>
  .undo-toast {
    position: fixed;
    left: 50%;
    transform: translateX(-50%);
    bottom: 24px;
    z-index: 9000;
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 10px 12px 10px 16px;
    border-radius: 8px;
    background: #1f2937;
    border: 1px solid rgba(255, 255, 255, 0.1);
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.45);
    font-size: 13px;
    color: #e5e7eb;
    max-width: min(560px, 90vw);
  }

  .message {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .undo-btn {
    display: flex;
    align-items: center;
    gap: 7px;
    flex-shrink: 0;
    padding: 5px 11px;
    border-radius: 6px;
    background: transparent;
    border: 1px solid rgba(var(--accent-rgb), 0.45);
    color: var(--accent-light);
    font-size: 13px;
    font-weight: 500;
    font-family: inherit;
    cursor: pointer;
  }

  .undo-btn:hover:not(:disabled) {
    background: rgba(var(--accent-rgb), 0.15);
  }

  .undo-btn:disabled {
    opacity: 0.6;
    cursor: default;
  }

  .count {
    font-variant-numeric: tabular-nums;
    font-size: 11px;
    color: #9ca3af;
    /* Fixed width, so the bar does not twitch as the number changes. */
    min-width: 14px;
    text-align: right;
  }

  .close {
    flex-shrink: 0;
    padding: 2px 6px;
    background: transparent;
    border: none;
    color: #6b7280;
    font-size: 17px;
    line-height: 1;
    cursor: pointer;
    border-radius: 4px;
  }

  .close:hover {
    background: rgba(255, 255, 255, 0.08);
    color: #d1d5db;
  }
</style>
