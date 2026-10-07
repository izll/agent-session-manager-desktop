<script lang="ts">
  /**
   * Reorders the view bar's views: by dragging a row, or with its arrows for
   * the keyboard. Every change is reported at once — the owner decides where
   * it is stored, Settings for every tab or the tab's own.
   */
  import { createEventDispatcher } from 'svelte';
  import { t } from '../../i18n';
  import { moveView, VIEW_LABEL_KEYS, type ViewId } from '../../utils/viewOrder';

  export let order: ViewId[] = [];

  const dispatch = createEventDispatcher<{ change: ViewId[] }>();

  let dragFrom = -1;
  let dropAt = -1;

  function move(from: number, to: number) {
    const next = moveView(order, from, to);
    if (next.every((view, i) => view === order[i])) return;
    order = next;
    dispatch('change', next);
  }

  function onDragStart(e: DragEvent, index: number) {
    dragFrom = index;
    e.dataTransfer?.setData('text/plain', order[index]);
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
  }

  function onDragOver(e: DragEvent, index: number) {
    if (dragFrom < 0) return;
    e.preventDefault();
    dropAt = index;
  }

  function onDrop(e: DragEvent, index: number) {
    e.preventDefault();
    if (dragFrom >= 0) move(dragFrom, index);
    dragFrom = -1;
    dropAt = -1;
  }

  function onDragEnd() {
    dragFrom = -1;
    dropAt = -1;
  }
</script>

<ol class="view-order" data-view-order>
  {#each order as view, i (view)}
    <li
      class="view-order-row"
      class:dragging={dragFrom === i}
      class:drop-target={dropAt === i && dragFrom !== i}
      data-view={view}
      draggable="true"
      on:dragstart={(e) => onDragStart(e, i)}
      on:dragover={(e) => onDragOver(e, i)}
      on:drop={(e) => onDrop(e, i)}
      on:dragend={onDragEnd}
    >
      <span class="grip" aria-hidden="true">⠿</span>
      <span class="view-name">{$t(VIEW_LABEL_KEYS[view])}</span>
      <button
        type="button"
        class="move-btn"
        data-move="up"
        disabled={i === 0}
        title={$t('viewOrder.moveUp')}
        aria-label="{$t('viewOrder.moveUp')}: {$t(VIEW_LABEL_KEYS[view])}"
        on:click={() => move(i, i - 1)}
      >↑</button>
      <button
        type="button"
        class="move-btn"
        data-move="down"
        disabled={i === order.length - 1}
        title={$t('viewOrder.moveDown')}
        aria-label="{$t('viewOrder.moveDown')}: {$t(VIEW_LABEL_KEYS[view])}"
        on:click={() => move(i, i + 1)}
      >↓</button>
    </li>
  {/each}
</ol>

<style>
  .view-order {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .view-order-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 8px;
    border-radius: 6px;
    border: 1px solid rgba(255, 255, 255, 0.08);
    background: rgba(255, 255, 255, 0.03);
    cursor: grab;
    user-select: none;
  }
  .view-order-row.dragging { opacity: 0.45; }
  .view-order-row.drop-target { border-color: rgba(var(--accent-rgb), 0.6); background: rgba(var(--accent-rgb), 0.1); }

  .grip { color: #6b7280; font-size: 13px; }
  .view-name { flex: 1; font-size: 13px; color: #e4e4e7; }

  .move-btn {
    padding: 2px 8px;
    border-radius: 5px;
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: transparent;
    color: #9ca3af;
    font-size: 12px;
    line-height: 1.2;
    cursor: pointer;
  }
  .move-btn:hover:not(:disabled) { background: rgba(255, 255, 255, 0.07); color: #e5e7eb; }
  .move-btn:disabled { opacity: 0.3; cursor: default; }
</style>
