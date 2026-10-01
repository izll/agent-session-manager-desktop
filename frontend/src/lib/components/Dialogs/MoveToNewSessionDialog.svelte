<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { get } from 'svelte/store';
  import * as App from '../../../../wailsjs/go/main/App';
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import { autoFocusField, dialogEnterBelongsToControl } from '../../utils/dialogActions';
  import { sessions } from '../../stores/sessions';
  import { activeProjectId } from '../../stores/projects';
  import { t } from '../../i18n';
  import { describeBackendError } from '../../utils/backendError';
  import { uniqueSessionName } from '../../utils/tabMoveRules';
  import { splitTabWithNotice } from '../../utils/tabMove';
  import DialogCloseButton from '../common/DialogCloseButton.svelte';

  /**
   * Asks the name of the session a tab is to become, then makes it one.
   *
   * Offered the name it would get unasked — the tab's, numbered when a
   * session already has it — so Enter alone keeps the old behaviour. A tab
   * that cannot be a session of its own says why and cannot be confirmed.
   */
  export let show = false;
  export let sourceId = '';
  export let windowIdx = 0;
  export let tabName = '';

  const dispatch = createEventDispatcher();

  let name = '';
  let refusal = '';
  let busy = false;
  let lastShow = false;
  let generation = 0;
  let dialogProjectId = '';

  $: if (show && !lastShow) {
    lastShow = true;
    open();
  } else if (!show && lastShow) {
    lastShow = false;
  }

  // Asked for one project, never carried out in another.
  $: if (show && dialogProjectId && $activeProjectId !== dialogProjectId) close();

  $: canConfirm = ready(name, refusal, busy);

  function ready(value: string, refused: string, working: boolean): boolean {
    return !!value.trim() && !refused && !working;
  }

  async function open() {
    const current = ++generation;
    dialogProjectId = get(activeProjectId);
    name = uniqueSessionName(tabName, get(sessions));
    refusal = '';
    busy = false;
    try {
      const answer = await App.TabSplitRefusal(sourceId, windowIdx);
      if (current === generation) refusal = answer ?? '';
    } catch {
      // Unanswered, the split itself says no.
    }
  }

  function close() {
    generation++;
    busy = false;
    show = false;
    dialogProjectId = '';
    dispatch('close');
  }

  async function confirm() {
    if (!ready(name, refusal, busy)) return;
    const current = generation;
    busy = true;
    const ok = await splitTabWithNotice(sourceId, windowIdx, tabName, name.trim());
    if (current !== generation) return;
    busy = false;
    if (ok) close();
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      claimKeyForDialog();
      e.stopPropagation();
      close();
    } else if (e.key === 'Enter' && !dialogEnterBelongsToControl(e)) {
      e.preventDefault();
      void confirm();
    }
  }
</script>

{#if show}
  <div class="dialog-overlay" use:autoFocusField on:keydown={handleKeydown} role="dialog" aria-modal="true" tabindex="-1">
    <div class="dialog-content move-new-session-dialog">
      <div class="dialog-header">
        <h2>{$t('tabMove.newSessionTitle', { name: tabName })}</h2>
        <DialogCloseButton on:click={close} disabled={busy} />
      </div>

      <div class="dialog-body">
        <div class="form-group">
          <label for="move-new-session-name">{$t('tabMove.newSessionName')}</label>
          <input
            id="move-new-session-name"
            class="text-input move-new-session-name"
            type="text"
            bind:value={name}
          />
        </div>
        {#if refusal}
          <div class="error-message">{describeBackendError(refusal)}</div>
        {/if}
      </div>

      <div class="dialog-footer">
        <button class="btn-cancel" on:click={close} disabled={busy}>{$t('common.cancel')}</button>
        <button class="btn-primary" on:click={confirm} disabled={!canConfirm}>{$t('tabMove.move')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .move-new-session-dialog {
    width: min(420px, calc(100vw - 32px));
  }

  .move-new-session-name {
    width: 100%;
  }
</style>
