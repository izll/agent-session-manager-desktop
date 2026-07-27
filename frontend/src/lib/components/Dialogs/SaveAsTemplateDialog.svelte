<script lang="ts">
  import { autoFocusDialog } from '../../utils/dialogActions';
  // Rendered into <body>: this dialog is opened from the sidebar's session
  // context menu, and the session list carries transform/contain for a
  // WebKitGTK compositing workaround. Either makes an ancestor the containing
  // block for position:fixed, laying the overlay out inside the sidebar.
  import { portal } from '../../utils/portal';
  import { createEventDispatcher } from 'svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import AgentIcon from '../common/AgentIcon.svelte';
  import type { Session } from '../../stores/sessions';
  import { t } from '../../i18n';

  export let show = false;
  export let session: Session | null = null;

  const dispatch = createEventDispatcher();

  let name = '';
  let keepPath = false;
  let saving = false;
  let saved = false;
  let error = '';

  // One guard block with the tracking variable assigned inside it: splitting
  // this in two would let the "opened just now" test race against its own
  // write when show and session change together.
  let lastInitKey = '';
  $: {
    const key = show && session ? `${session.id}` : '';
    if (key && key !== lastInitKey) {
      lastInitKey = key;
      name = session!.name;
      // Reusable by default: an arrangement worth saving is usually worth
      // using in the next project too, and the directory is asked for at
      // creation time anyway.
      keepPath = false;
      saving = false;
      saved = false;
      error = '';
    } else if (!show) {
      lastInitKey = '';
    }
  }

  $: tabs = session?.followedWindows || [];
  $: sessionPath = session?.path || '';
  $: canSave = name.trim().length > 0 && !saving;

  async function save() {
    if (!session || !canSave) return;
    saving = true;
    error = '';
    try {
      await App.SaveSessionAsTemplate(session.id, name.trim(), keepPath);
      saved = true;
      close();
    } catch (e) {
      error = String(e);
    } finally {
      saving = false;
    }
  }

  function close() {
    show = false;
    dispatch('close', { saved });
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.stopPropagation();
      close();
    }
  }
</script>

{#if show && session}
  <div
    class="dialog-overlay"
    use:portal
    use:autoFocusDialog
    role="dialog"
    aria-modal="true"
    on:click|self={close}
    on:keydown={handleKeydown}
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2>{$t('templates.saveAsTitle')}</h2>
        <button class="close-btn" on:click={close}>×</button>
      </div>

      <div class="dialog-body">
        {#if error}<div class="error-line">{error}</div>{/if}

        <label class="field">
          <span class="field-label">{$t('templates.fieldName')}</span>
          <input bind:value={name} placeholder={$t('templates.namePlaceholder')} />
        </label>

        <div class="summary">
          <span class="summary-label">{$t('templates.captures')}</span>
          <span class="summary-chip">
            <AgentIcon agent={session.agent} size="xs" />
            {session.agent}
          </span>
          {#each tabs as tab (tab.index)}
            <span class="summary-chip">
              <AgentIcon agent={tab.agent} size="xs" />
              {tab.name || tab.agent}
            </span>
          {/each}
        </div>

        <label class="toggle-row">
          <input type="checkbox" bind:checked={keepPath} />
          <span class="toggle-main">
            <span class="toggle-label">{$t('templates.keepPath')}</span>
            <span class="toggle-hint">
              {keepPath ? $t('templates.keepPathOn', { path: sessionPath }) : $t('templates.keepPathOff')}
            </span>
          </span>
        </label>
      </div>

      <div class="dialog-footer">
        <button class="btn-secondary" on:click={close}>{$t('common.cancel')}</button>
        <button class="btn-primary" disabled={!canSave} on:click={save}>{$t('common.save')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .dialog-content {
    width: min(460px, 94vw);
    display: flex;
    flex-direction: column;
  }
  .dialog-header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 14px 18px; border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  }
  .dialog-header h2 { margin: 0; font-size: 15px; color: #e4e4e7; }
  .close-btn { border: 0; background: none; color: #71717a; font-size: 20px; cursor: pointer; }
  .close-btn:hover { color: #e4e4e7; }

  .dialog-body { padding: 14px 18px; display: flex; flex-direction: column; gap: 12px; }
  .error-line { color: #fb7185; font-size: 13px; }

  .field { display: flex; flex-direction: column; gap: 3px; }
  .field-label { font-size: 12px; color: #a1a1aa; }
  .field input {
    padding: 7px 10px; border-radius: 7px; font-size: 13px;
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(0, 0, 0, 0.25); color: #e4e4e7; font-family: inherit;
  }
  .field input:focus { outline: none; border-color: rgba(var(--accent-rgb), 0.6); }

  .summary {
    display: flex; align-items: center; gap: 6px; flex-wrap: wrap;
    padding: 8px 10px; border-radius: 7px; background: rgba(255, 255, 255, 0.03);
  }
  .summary-label { font-size: 11px; color: #6b7280; }
  .summary-chip {
    display: inline-flex; align-items: center; gap: 4px;
    font-size: 12px; color: #d4d4d8;
    background: rgba(255, 255, 255, 0.06); border-radius: 999px; padding: 2px 9px;
  }

  .toggle-row { display: flex; align-items: flex-start; gap: 9px; cursor: pointer; }
  .toggle-row input { margin-top: 2px; accent-color: var(--accent); }
  .toggle-main { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
  .toggle-label { font-size: 13px; color: #e4e4e7; }
  .toggle-hint {
    font-size: 11px; color: #6b7280; line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .dialog-footer {
    display: flex; justify-content: flex-end; gap: 8px;
    padding: 12px 18px; border-top: 1px solid rgba(255, 255, 255, 0.06);
  }
  .btn-primary, .btn-secondary {
    padding: 7px 16px; border-radius: 7px; font-size: 13px; font-weight: 600; cursor: pointer;
  }
  .btn-secondary {
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(255, 255, 255, 0.05); color: #a1a1aa;
  }
  .btn-primary {
    border: 1px solid var(--accent);
    background: linear-gradient(135deg, var(--accent-dark), var(--accent)); color: var(--accent-ink);
  }
  .btn-primary:disabled { opacity: 0.45; cursor: default; }
</style>
