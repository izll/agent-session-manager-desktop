<script lang="ts">
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { createEventDispatcher } from 'svelte';
  import { t } from '../../i18n';

  export let show = false;
  export let sessionName = '';
  export let hasFollowedWindows = false;

  const dispatch = createEventDispatcher<{
    stopSession: void;
    stopTab: void;
    cancel: void;
  }>();

  function handleStopSession() {
    show = false;
    dispatch('stopSession');
  }

  function handleStopTab() {
    show = false;
    dispatch('stopTab');
  }

  function handleCancel() {
    show = false;
    dispatch('cancel');
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      handleCancel();
    }
  }
</script>

{#if show}
  <div
    class="dialog-overlay" use:autoFocusDialog
    on:click|self={handleCancel}
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
  >
    <div class="dialog-content">
      <div class="dialog-icon">
        <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="#f97316" stroke-width="2">
          <circle cx="12" cy="12" r="10"/>
          <line x1="15" y1="9" x2="9" y2="15"/>
          <line x1="9" y1="9" x2="15" y2="15"/>
        </svg>
      </div>

      <h2 class="dialog-title">{$t('stop.title')}</h2>
      <p class="dialog-message">
        {$t('stop.message', { name: sessionName })}
      </p>

      <div class="dialog-actions">
        {#if hasFollowedWindows}
          <button class="btn-option tab" on:click={handleStopTab}>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="3" y="3" width="18" height="18" rx="2"/>
              <line x1="15" y1="9" x2="9" y2="15"/>
              <line x1="9" y1="9" x2="15" y2="15"/>
            </svg>
            <span>{$t('stop.currentTab')}</span>
          </button>
        {/if}
        <button class="btn-option session" on:click={handleStopSession}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <rect x="3" y="3" width="18" height="18" rx="2"/>
            <line x1="9" y1="3" x2="9" y2="21"/>
            <line x1="15" y1="3" x2="15" y2="21"/>
          </svg>
          <span>{hasFollowedWindows ? $t('stop.entireSessionTabs') : $t('stop.entireSession')}</span>
        </button>
        <button class="btn-cancel" on:click={handleCancel}>
          {$t('stop.cancel')}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  /* Override global dialog-content for centered layout with padding */
  .dialog-content {
    padding: 32px;
    text-align: center;
    max-width: 420px;
  }

  .dialog-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 56px;
    height: 56px;
    border-radius: 50%;
    background: rgba(249, 115, 22, 0.15);
    margin-bottom: 20px;
  }

  .dialog-title {
    font-size: 18px;
    font-weight: 600;
    color: #e4e4e7;
    margin: 0 0 12px;
  }

  .dialog-message {
    font-size: 14px;
    color: #9ca3af;
    margin: 0 0 28px;
    line-height: 1.6;
  }

  .dialog-actions {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .btn-option {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    padding: 12px 20px;
    border: 2px solid transparent;
    border-radius: 10px;
    font-size: 14px;
    font-weight: 600;
    color: white;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .btn-option.tab {
    background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%);
    box-shadow: 0 4px 15px rgba(59, 130, 246, 0.3);
  }

  .btn-option.tab:hover {
    transform: translateY(-1px);
    box-shadow: 0 6px 20px rgba(59, 130, 246, 0.4);
  }

  .btn-option.session {
    background: linear-gradient(135deg, #f97316 0%, #ea580c 100%);
    box-shadow: 0 4px 15px rgba(249, 115, 22, 0.3);
  }

  .btn-option.session:hover {
    transform: translateY(-1px);
    box-shadow: 0 6px 20px rgba(249, 115, 22, 0.4);
  }

  .btn-cancel {
    padding: 10px 18px;
    background: transparent;
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 8px;
    font-size: 13px;
    font-weight: 500;
    color: #9ca3af;
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .btn-cancel:hover {
    background: rgba(255, 255, 255, 0.05);
    color: white;
  }
</style>
