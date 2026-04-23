<script lang="ts">
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { createEventDispatcher } from 'svelte';
  import { t } from '../../i18n';

  export let show = false;
  export let title = 'Confirm';
  export let message = 'Are you sure?';
  export let confirmText = 'Confirm';
  export let cancelText = 'Cancel';
  export let variant: 'danger' | 'warning' | 'info' = 'danger';

  const dispatch = createEventDispatcher<{
    confirm: void;
    cancel: void;
  }>();

  function handleConfirm() {
    show = false;
    dispatch('confirm');
  }

  function handleCancel() {
    show = false;
    dispatch('cancel');
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      handleCancel();
    } else if (e.key === 'Enter') {
      handleConfirm();
    }
  }

  $: variantColors = {
    danger: {
      icon: '#f87171',
      iconBg: 'rgba(239, 68, 68, 0.15)',
      btnBg: 'linear-gradient(135deg, #ef4444 0%, #dc2626 100%)',
      btnHover: '0 6px 20px rgba(239, 68, 68, 0.4)',
    },
    warning: {
      icon: '#fbbf24',
      iconBg: 'rgba(251, 191, 36, 0.15)',
      btnBg: 'linear-gradient(135deg, #f59e0b 0%, #d97706 100%)',
      btnHover: '0 6px 20px rgba(251, 191, 36, 0.4)',
    },
    info: {
      icon: '#60a5fa',
      iconBg: 'rgba(96, 165, 250, 0.15)',
      btnBg: 'linear-gradient(135deg, #3b82f6 0%, #2563eb 100%)',
      btnHover: '0 6px 20px rgba(96, 165, 250, 0.4)',
    },
  }[variant];
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
      <div class="dialog-icon" style="background: {variantColors.iconBg}">
        {#if variant === 'danger'}
          <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke={variantColors.icon} stroke-width="2">
            <path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"/>
            <line x1="10" y1="11" x2="10" y2="17"/>
            <line x1="14" y1="11" x2="14" y2="17"/>
          </svg>
        {:else if variant === 'warning'}
          <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke={variantColors.icon} stroke-width="2">
            <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
            <line x1="12" y1="9" x2="12" y2="13"/>
            <circle cx="12" cy="17" r="0.5" fill={variantColors.icon}/>
          </svg>
        {:else}
          <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke={variantColors.icon} stroke-width="2">
            <circle cx="12" cy="12" r="10"/>
            <line x1="12" y1="16" x2="12" y2="12"/>
            <circle cx="12" cy="8" r="0.5" fill={variantColors.icon}/>
          </svg>
        {/if}
      </div>

      <h2 class="dialog-title">{title}</h2>
      <p class="dialog-message">{message}</p>

      <div class="dialog-actions">
        <button class="btn-cancel" on:click={handleCancel}>
          {cancelText}
        </button>
        <button
          class="btn-confirm"
          style="background: {variantColors.btnBg}"
          on:click={handleConfirm}
        >
          {confirmText}
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
    max-width: 380px;
  }

  .dialog-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 56px;
    height: 56px;
    border-radius: 50%;
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
    justify-content: center;
    gap: 12px;
  }

  .btn-confirm {
    padding: 10px 24px;
    border: none;
    border-radius: 10px;
    font-size: 14px;
    font-weight: 600;
    color: white;
    cursor: pointer;
    transition: all 0.2s ease;
    box-shadow: 0 4px 15px rgba(0, 0, 0, 0.3);
  }

  .btn-confirm:hover {
    transform: translateY(-1px);
    box-shadow: 0 6px 20px rgba(0, 0, 0, 0.4);
  }
</style>
