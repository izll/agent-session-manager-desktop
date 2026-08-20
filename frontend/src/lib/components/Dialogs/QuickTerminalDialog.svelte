<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { get } from 'svelte/store';
  import { selectedSessionId, selectWindow, loadSessions } from '../../stores/sessions';
  import * as App from '../../../../wailsjs/go/main/App';
  import { t } from '../../i18n';

  /**
   * Open a terminal tab in one keystroke.
   *
   * The full new-tab dialog asks which agent, with what arguments, in which
   * directory — worth answering when starting an agent, and all of it in the
   * way when what you want is a shell. This asks the one thing that differs
   * between two terminals: what to call it.
   *
   * The name arrives filled in and selected, so Enter accepts it and typing
   * replaces it. Neither costs a keystroke more than the other.
   */
  export let show = false;
  export let sessionId = '';

  const dispatch = createEventDispatcher();

  const DEFAULT_NAME = 'Terminal';

  let name = DEFAULT_NAME;
  let inputEl: HTMLInputElement | null = null;
  let isSubmitting = false;
  let error = '';

  // Fresh every time it opens: a name left over from last time is not a
  // starting point, it is something to delete first.
  $: if (show) {
    name = DEFAULT_NAME;
    error = '';
    focusAndSelect();
  }

  /**
   * Focus the field with the whole name selected.
   *
   * Not the shared autoFocusField action: that deliberately puts the cursor at
   * the end and selects nothing, because the dialogs using it open on text the
   * user came to amend, where select-all means one keystroke wipes it. Here the
   * text is a suggestion — keeping it costs Enter, replacing it costs typing,
   * and a selection is what makes both true at once.
   */
  function focusAndSelect() {
    // Two frames: one for Svelte to create the input, one for it to be in the
    // document and focusable.
    requestAnimationFrame(() => requestAnimationFrame(() => {
      inputEl?.focus();
      inputEl?.select();
    }));
  }

  function close() {
    show = false;
    isSubmitting = false;
    dispatch('close');
  }

  async function handleSubmit() {
    const trimmed = name.trim();
    if (!trimmed) {
      error = $t('newTab.nameRequired');
      return;
    }

    const targetSessionId = sessionId;
    if (!targetSessionId) {
      error = $t('newTab.noSession');
      return;
    }

    isSubmitting = true;
    error = '';

    try {
      // A terminal, with no agent, no extra arguments and the session's own
      // working directory — the defaults the full dialog would have offered.
      const newIdx = await App.CreateTab(targetSessionId, false, 'terminal', trimmed, '', '');
      await loadSessions();
      close();
      // Switch to it and put the keyboard in it, so the tab is ready to type
      // in rather than merely created.
      if (get(selectedSessionId) === targetSessionId && typeof newIdx === 'number' && newIdx >= 0) {
        selectWindow(newIdx);
        requestAnimationFrame(() =>
          window.dispatchEvent(new CustomEvent('terminal:focus')));
      }
      dispatch('created', { name: trimmed });
    } catch (e) {
      error = String(e);
      isSubmitting = false;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.stopPropagation();
      close();
    } else if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSubmit();
    }
  }
</script>

{#if show}
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
  <div
    class="dialog-overlay"
    on:click|self={close}
    on:keydown={handleKeydown}
    role="dialog"
    tabindex="-1"
  >
    <div class="dialog-content quick-terminal">
      <h2>{$t('quickTerminal.title')}</h2>

      <input
        bind:this={inputEl}
        bind:value={name}
        placeholder={DEFAULT_NAME}
        disabled={isSubmitting}
        on:keydown={handleKeydown}
      />

      {#if error}
        <p class="error">{error}</p>
      {/if}

      <!-- Enter does this, and the hint says so — but a dialog with no button
           reads as unfinished, and someone will go looking for one before
           trusting the keyboard. -->
      <div class="dialog-actions">
        <div class="hint">{$t('quickTerminal.hint')}</div>
        <button type="button" class="btn-cancel" on:click={close}>
          {$t('common.cancel')}
        </button>
        <button type="button" class="btn-primary" disabled={isSubmitting} on:click={handleSubmit}>
          {$t('quickTerminal.create')}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  /* Narrower than the shared default: one field is all there is, and a wide
     box around it makes it look like something is missing.
     The padding is the dialog's own — .dialog-content sets none, leaving each
     dialog to say what it wants, and without it the title and the hint sat
     against the edges with their tops and left ends clipped by the corner
     radius. 24px is what the full new-tab dialog beside it uses. */
  .quick-terminal {
    width: min(420px, calc(100vw - 32px));
    max-width: min(420px, calc(100vw - 32px));
    padding: 24px;
  }

  h2 {
    margin: 0 0 12px;
    font-size: 15px;
    font-weight: 600;
    color: #e6e6e6;
  }

  input {
    width: 100%;
    padding: 9px 11px;
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 7px;
    background: rgba(0, 0, 0, 0.25);
    color: #e6e6e6;
    font-size: 14px;
    font-family: inherit;
    box-sizing: border-box;
  }

  input:focus {
    outline: none;
    border-color: rgba(var(--accent-rgb), 0.6);
  }

  .error {
    margin: 8px 0 0;
    color: #e06c75;
    font-size: 12px;
  }

  /* The hint sits on the same row as the buttons, pushed left by the gap it
     leaves — it says what they do, so putting it under them would repeat the
     row rather than label it. */
  .dialog-actions {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 14px;
  }

  .hint {
    margin-right: auto;
    color: #6b7280;
    font-size: 11px;
  }

  .btn-primary {
    padding: 8px 18px;
    box-shadow: 0 4px 15px rgba(var(--accent-rgb), 0.3);
  }

  .btn-primary:hover:not(:disabled) {
    box-shadow: 0 6px 20px rgba(var(--accent-rgb), 0.4);
  }
</style>
