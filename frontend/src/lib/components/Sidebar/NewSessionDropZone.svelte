<script lang="ts">
  import { get } from 'svelte/store';
  import { activeProjectId } from '../../stores/projects';
  import { t } from '../../i18n';
  import { describeBackendError } from '../../utils/backendError';
  import { TAB_DRAG_MIME, decodeTabDrag, newSessionDropState, type TabDropState } from '../../utils/tabMoveRules';
  import { askNewSessionFor, endTabDrag, tabDrag } from '../../utils/tabMove';

  /**
   * Where a tab dragged out of the tab bar is dropped to become a session of
   * its own. There only while a tab of this project is being dragged; a drop
   * asks for the new session's name, as the tab's menu does.
   *
   * Lit like a session row under the tab: accepting, or refusing with the
   * reason as its tooltip — and then not taking the drop.
   */

  let over: TabDropState = 'none';

  $: shown = !!$tabDrag && $tabDrag.projectId === $activeProjectId;
  $: if (!$tabDrag) over = 'none';
  $: refusal = over === 'refused' && $tabDrag?.newSessionRefusal
    ? describeBackendError($tabDrag.newSessionRefusal)
    : '';

  function handleDragOver(e: DragEvent) {
    if (!e.dataTransfer?.types.includes(TAB_DRAG_MIME)) return;
    over = newSessionDropState(get(tabDrag), get(activeProjectId));
    if (over === 'ok') {
      e.preventDefault();
      e.dataTransfer.dropEffect = 'move';
    }
  }

  function handleDragLeave() {
    over = 'none';
  }

  function handleDrop(e: DragEvent) {
    if (!e.dataTransfer?.types.includes(TAB_DRAG_MIME)) return;
    e.preventDefault();
    const state = newSessionDropState(get(tabDrag), get(activeProjectId));
    over = 'none';
    const payload = decodeTabDrag(e.dataTransfer.getData(TAB_DRAG_MIME));
    // As on a row: the tab bar may never see its dragend.
    endTabDrag();
    if (state !== 'ok' || !payload || payload.projectId !== get(activeProjectId) || payload.onlyTab) return;
    askNewSessionFor(payload.sessionId, payload.windowIdx, payload.name);
  }
</script>

{#if shown}
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div
    class="new-session-drop"
    class:tab-drop-ok={over === 'ok'}
    class:tab-drop-refused={over === 'refused'}
    title={refusal || undefined}
    data-drop-target="new-session"
    on:dragover={handleDragOver}
    on:dragleave={handleDragLeave}
    on:drop={handleDrop}
  >
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
      <rect x="3" y="4" width="18" height="16" rx="2"/>
      <line x1="12" y1="9" x2="12" y2="15"/>
      <line x1="9" y1="12" x2="15" y2="12"/>
    </svg>
    <span>{$t('tabMove.newSessionDrop')}</span>
  </div>
{/if}

<style>
  .new-session-drop {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    margin: 6px 8px;
    padding: 10px 12px;
    border: 1px dashed rgba(var(--accent-rgb), 0.45);
    border-radius: 8px;
    color: #a1a1aa;
    font-size: 12px;
  }

  /* Children would end the hover with a dragleave of their own. */
  .new-session-drop > * {
    pointer-events: none;
  }

  /* As a session row under a tab: SessionItem's tab-drop-ok / -refused. */
  .new-session-drop.tab-drop-ok {
    border-style: solid;
    border-color: rgba(var(--accent-rgb), 0.7);
    background: rgba(var(--accent-rgb), 0.18);
    box-shadow: inset 0 0 0 1px rgba(var(--accent-rgb), 0.5);
    color: #e4e4e7;
  }

  .new-session-drop.tab-drop-refused {
    border-style: solid;
    border-color: rgba(239, 68, 68, 0.55);
    background: rgba(239, 68, 68, 0.08);
    cursor: not-allowed;
  }
</style>
