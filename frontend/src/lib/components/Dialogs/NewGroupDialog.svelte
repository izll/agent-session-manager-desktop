<script lang="ts">
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import { autoFocusDialog, dialogEnterBelongsToControl } from '../../utils/dialogActions';
  import { createEventDispatcher } from 'svelte';
  import { get } from 'svelte/store';
  import { createGroup } from '../../stores/sessions';
  import { activeProjectId } from '../../stores/projects';
  import { t } from '../../i18n';
  import DialogCloseButton from '../common/DialogCloseButton.svelte';

  export let show = false;

  const dispatch = createEventDispatcher();

  let groupName = '';
  let loading = false;
  let error = '';
  let lastShow = false;
  let operationGeneration = 0;
  let dialogProjectId = '';
  let hasDialogProject = false;

  // Reset form only when dialog transitions from hidden to shown
  // Assign lastShow inside the same block: a separate `$: lastShow = show`
  // is ordered BEFORE this guard, so the fields were never reset on open.
  $: {
    if (show && !lastShow) {
      operationGeneration++;
      dialogProjectId = get(activeProjectId);
      hasDialogProject = true;
      groupName = '';
      error = '';
      loading = false;
    }
    lastShow = show;
  }

  // A group name drafted for project A must never turn into a group in B.
  $: if (show && hasDialogProject && $activeProjectId !== dialogProjectId) close();

  function close() {
    operationGeneration++;
    loading = false;
    show = false;
    dispatch('close');
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      claimKeyForDialog();
      e.stopPropagation();
      close();
    } else if (e.key === 'Enter' && groupName.trim() && !dialogEnterBelongsToControl(e)) {
      handleCreate();
    }
  }

  async function handleCreate() {
    if (!groupName.trim() || loading) return;

    const generation = operationGeneration;
    const targetProjectId = dialogProjectId;
    const submittedName = groupName.trim();
    loading = true;
    error = '';

    try {
      await createGroup(submittedName);
      if (!show || generation !== operationGeneration || targetProjectId !== get(activeProjectId)) return;
      close();
    } catch (e) {
      if (!show || generation !== operationGeneration) return;
      error = String(e);
    }

    if (generation === operationGeneration) loading = false;
  }
</script>

{#if show}
  <div
    class="dialog-overlay" use:autoFocusDialog
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2>{$t('newGroup.title')}</h2>
        <DialogCloseButton on:click={close} />
      </div>

      <div class="dialog-body">
        <div class="form-group">
          <label for="group-name">{$t('newGroup.nameLabel')}</label>
          <input
            id="group-name"
            type="text"
            bind:value={groupName}
            placeholder={$t('newGroup.namePlaceholder')}
            class="text-input"
            autofocus
          />
        </div>

        {#if error}
          <div class="error-message">{error}</div>
        {/if}
      </div>

      <div class="dialog-footer">
        <button class="btn-cancel" on:click={close}>{$t('common.cancel')}</button>
        <button
          class="btn-primary"
          on:click={handleCreate}
          disabled={!groupName.trim() || loading}
        >
          {loading ? $t('newGroup.creating') : $t('newGroup.create')}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  /* Global styles are defined in style.css */
</style>
