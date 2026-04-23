<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { createGroup } from '../../stores/sessions';
  import { t } from '../../i18n';

  export let show = false;

  const dispatch = createEventDispatcher();

  let groupName = '';
  let loading = false;
  let error = '';
  let lastShow = false;

  // Reset form only when dialog transitions from hidden to shown
  $: if (show && !lastShow) {
    groupName = '';
    error = '';
  }
  $: lastShow = show;

  function close() {
    show = false;
    dispatch('close');
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      close();
    } else if (e.key === 'Enter' && groupName.trim()) {
      handleCreate();
    }
  }

  async function handleCreate() {
    if (!groupName.trim()) return;

    loading = true;
    error = '';

    try {
      await createGroup(groupName.trim());
      close();
    } catch (e) {
      error = String(e);
    }

    loading = false;
  }
</script>

{#if show}
  <div
    class="dialog-overlay"
    on:click|self={close}
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2>{$t('newGroup.title')}</h2>
        <button class="close-btn" on:click={close}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/>
            <line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
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
