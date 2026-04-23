<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { t } from '../../i18n';

  export let show = false;
  export let sessionName = '';
  export let hasFollowedWindows = false;

  const dispatch = createEventDispatcher<{
    startSession: void;
    startTab: void;
    cancel: void;
  }>();

  function handleStartSession() {
    show = false;
    dispatch('startSession');
  }

  function handleStartTab() {
    show = false;
    dispatch('startTab');
  }

  function handleCancel() {
    show = false;
    dispatch('cancel');
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      handleCancel();
    } else if (e.key === 'Enter') {
      handleStartSession();
    }
  }
</script>

{#if show}
  <div
    class="dialog-overlay"
    on:click|self={handleCancel}
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
  >
    <div class="dialog-content">
      <div class="dialog-icon">
        <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="#22c55e" stroke-width="2">
          <polygon points="5 3 19 12 5 21 5 3"/>
        </svg>
      </div>

      <h2 class="dialog-title">{$t('start.title')}</h2>
      <p class="dialog-message">
        {$t('start.message', { name: sessionName })}
      </p>

      <div class="dialog-actions">
        <button class="btn-option session" on:click={handleStartSession}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <rect x="3" y="3" width="18" height="18" rx="2"/>
            <line x1="9" y1="3" x2="9" y2="21"/>
            <line x1="15" y1="3" x2="15" y2="21"/>
          </svg>
          <span>{hasFollowedWindows ? $t('start.entireSessionTabs') : $t('start.entireSession')}</span>
        </button>
        {#if hasFollowedWindows}
          <button class="btn-option tab" on:click={handleStartTab}>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="3" y="3" width="18" height="18" rx="2"/>
              <polygon points="10 8 16 12 10 16 10 8"/>
            </svg>
            <span>{$t('start.currentTab')}</span>
          </button>
        {/if}
        <button class="btn-cancel" on:click={handleCancel}>
          {$t('start.cancel')}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
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
    background: rgba(34, 197, 94, 0.15);
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
    background: linear-gradient(135deg, #22c55e 0%, #16a34a 100%);
    box-shadow: 0 4px 15px rgba(34, 197, 94, 0.3);
  }

  .btn-option.session:hover {
    transform: translateY(-1px);
    box-shadow: 0 6px 20px rgba(34, 197, 94, 0.4);
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
