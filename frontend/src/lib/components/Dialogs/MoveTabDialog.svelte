<script lang="ts">
  import { createEventDispatcher, tick } from 'svelte';
  import { get } from 'svelte/store';
  import * as App from '../../../../wailsjs/go/main/App';
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import { autoFocusField, dialogEnterBelongsToControl } from '../../utils/dialogActions';
  import { sessions } from '../../stores/sessions';
  import { activeProjectId } from '../../stores/projects';
  import { t } from '../../i18n';
  import { describeBackendError } from '../../utils/backendError';
  import { moveTargets } from '../../utils/tabMoveRules';
  import { mergeSessionWithNotice, moveTabWithNotice } from '../../utils/tabMove';
  import DialogCloseButton from '../common/DialogCloseButton.svelte';
  import StatusIndicator from '../common/StatusIndicator.svelte';

  /**
   * Picks the session a tab — or, with mode "merge", a whole session — goes to.
   *
   * Every other session of the project is listed; the ones that would refuse
   * say why and cannot be chosen, the same answer a drag gets over them.
   */
  export let show = false;
  export let mode: 'tab' | 'merge' = 'tab';
  export let sourceId = '';
  export let windowIdx = 0;
  /** The tab's name, or for a merge the session's. */
  export let name = '';

  const dispatch = createEventDispatcher();

  let filter = '';
  let chosenId = '';
  let refusals: Record<string, string> = {};
  let busy = false;
  let loadError = '';
  let lastShow = false;
  let generation = 0;
  let dialogProjectId = '';
  let listEl: HTMLElement | undefined;

  $: if (show && !lastShow) {
    lastShow = true;
    open();
  } else if (!show && lastShow) {
    lastShow = false;
  }

  // Chosen for one project, never carried out in another.
  $: if (show && dialogProjectId && $activeProjectId !== dialogProjectId) close();

  $: targets = moveTargets($sessions, sourceId, filter);
  $: chosenAvailable = canChoose(chosenId, targets, refusals);
  $: title = mode === 'merge'
    ? $t('tabMove.pickTitleMerge', { name })
    : $t('tabMove.pickTitleTab', { name });

  async function open() {
    const current = ++generation;
    dialogProjectId = get(activeProjectId);
    filter = '';
    chosenId = '';
    refusals = {};
    busy = false;
    loadError = '';
    try {
      const answer = mode === 'merge'
        ? await App.SessionMergeRefusals(sourceId)
        : await App.TabMoveRefusals(sourceId, windowIdx);
      if (current === generation) refusals = answer ?? {};
    } catch (e) {
      if (current === generation) loadError = describeBackendError(e);
    }
  }

  function close() {
    generation++;
    busy = false;
    show = false;
    dialogProjectId = '';
    dispatch('close');
  }

  // A function of its inputs rather than read from chosenAvailable alone: a
  // handler that has just set chosenId runs before the reactive value is
  // recomputed, and would confirm (or refuse) the previous choice.
  function canChoose(id: string, list: typeof targets, refused: Record<string, string>): boolean {
    return !!id && list.some(s => s.id === id) && !refused[id];
  }

  function choose(id: string) {
    if (!refusals[id]) chosenId = id;
  }

  async function confirm() {
    if (!canChoose(chosenId, targets, refusals) || busy) return;
    const current = generation;
    busy = true;
    const ok = mode === 'merge'
      ? await mergeSessionWithNotice(sourceId, chosenId, name)
      : await moveTabWithNotice(sourceId, windowIdx, chosenId, name);
    if (current !== generation) return;
    busy = false;
    if (ok) close();
  }

  async function step(delta: number) {
    const open = targets.filter(s => !refusals[s.id]);
    if (open.length === 0) return;
    const at = open.findIndex(s => s.id === chosenId);
    const next = at < 0 ? (delta > 0 ? 0 : open.length - 1) : (at + delta + open.length) % open.length;
    chosenId = open[next].id;
    await tick();
    listEl?.querySelector('.move-target.chosen')?.scrollIntoView({ block: 'nearest' });
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      claimKeyForDialog();
      e.stopPropagation();
      close();
    } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      void step(e.key === 'ArrowDown' ? 1 : -1);
    } else if (e.key === 'Enter' && !dialogEnterBelongsToControl(e)) {
      e.preventDefault();
      void confirm();
    }
  }

  function handleFilterKeydown(e: KeyboardEvent) {
    // Enter in the filter means "the one I narrowed it down to".
    if (e.key === 'Enter') {
      e.preventDefault();
      e.stopPropagation();
      if (!canChoose(chosenId, targets, refusals)) {
        const first = targets.find(s => !refusals[s.id]);
        if (first) chosenId = first.id;
      }
      void confirm();
    }
  }
</script>

{#if show}
  <div class="dialog-overlay" use:autoFocusField on:keydown={handleKeydown} role="dialog" aria-modal="true" tabindex="-1">
    <div class="dialog-content move-tab-dialog">
      <div class="dialog-header">
        <h2>{title}</h2>
        <DialogCloseButton on:click={close} disabled={busy} />
      </div>

      <div class="dialog-body">
        {#if mode === 'merge'}
          <p class="move-note">{$t('tabMove.mergeNote', { name })}</p>
        {/if}
        <input
          class="text-input move-filter"
          type="text"
          bind:value={filter}
          placeholder={$t('tabMove.filter')}
          on:keydown={handleFilterKeydown}
        />
        {#if loadError}
          <div class="error-message">{loadError}</div>
        {/if}
        <div class="move-targets" bind:this={listEl} role="listbox" aria-label={title}>
          {#each targets as target (target.id)}
            <button
              type="button"
              class="move-target"
              class:chosen={chosenId === target.id}
              class:refused={!!refusals[target.id]}
              role="option"
              aria-selected={chosenId === target.id}
              aria-disabled={!!refusals[target.id]}
              title={refusals[target.id] ? describeBackendError(refusals[target.id]) : target.path}
              data-session-id={target.id}
              on:click={() => choose(target.id)}
              on:dblclick={() => { choose(target.id); void confirm(); }}
            >
              <StatusIndicator status={target.status === 'running' ? 'running' : 'stopped'} size="sm" />
              <span class="move-target-name">{target.name}</span>
              {#if refusals[target.id]}
                <span class="move-target-reason">{describeBackendError(refusals[target.id])}</span>
              {:else if target.serverName}
                <span class="move-target-server">{target.serverName}</span>
              {/if}
            </button>
          {:else}
            <p class="move-empty">{$t('tabMove.noTargets')}</p>
          {/each}
        </div>
      </div>

      <div class="dialog-footer">
        <button class="btn-cancel" on:click={close} disabled={busy}>{$t('common.cancel')}</button>
        <button class="btn-primary" on:click={confirm} disabled={!chosenAvailable || busy}>
          {mode === 'merge' ? $t('tabMove.merge') : $t('tabMove.move')}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .move-tab-dialog {
    width: min(460px, calc(100vw - 32px));
  }

  .move-note {
    margin: 0 0 10px;
    font-size: 12px;
    opacity: 0.65;
    line-height: 1.45;
  }

  .move-filter {
    width: 100%;
    margin-bottom: 8px;
  }

  .move-targets {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 320px;
    overflow-y: auto;
  }

  .move-target {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 7px 10px;
    border: 1px solid transparent;
    border-radius: 6px;
    background: transparent;
    color: inherit;
    font-size: 13px;
    text-align: left;
    cursor: pointer;
  }

  .move-target:hover {
    background: rgba(255, 255, 255, 0.07);
  }

  .move-target.chosen {
    border-color: var(--accent);
    background: rgba(var(--accent-rgb), 0.15);
  }

  .move-target.refused {
    opacity: 0.55;
    cursor: not-allowed;
  }

  .move-target-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .move-target-reason,
  .move-target-server {
    flex-shrink: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 11px;
    opacity: 0.65;
  }

  .move-empty {
    margin: 8px 0;
    font-size: 12px;
    opacity: 0.65;
  }
</style>
