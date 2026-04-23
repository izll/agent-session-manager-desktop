<script lang="ts">
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { createEventDispatcher } from 'svelte';
  import type { Session } from '../../stores/sessions';
  import { t } from '../../i18n';

  export let show = false;
  export let session: Session | null = null;
  export let hasTabs = false;

  const dispatch = createEventDispatcher<{
    newSession: void;
    continueExisting: void;
    restartWithTabs: void;
    cancel: void;
  }>();

  $: maxCursor = hasTabs ? 2 : 1;

  let cursor = 0;
  $: if (show) cursor = 0;

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      handleCancel();
    } else if (e.key === 'ArrowUp' || e.key === 'k') {
      e.preventDefault();
      cursor = Math.max(0, cursor - 1);
    } else if (e.key === 'ArrowDown' || e.key === 'j') {
      e.preventDefault();
      cursor = Math.min(maxCursor, cursor + 1);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      handleSelect();
    } else if (e.key === '1') {
      e.preventDefault();
      cursor = 0;
      handleSelect();
    } else if (e.key === '2') {
      e.preventDefault();
      cursor = 1;
      handleSelect();
    } else if (e.key === '3' && hasTabs) {
      e.preventDefault();
      cursor = 2;
      handleSelect();
    }
  }

  function handleSelect() {
    if (hasTabs) {
      if (cursor === 0) handleRestartWithTabs();
      else if (cursor === 1) handleNewSession();
      else if (cursor === 2) handleContinueExisting();
    } else {
      if (cursor === 0) handleNewSession();
      else if (cursor === 1) handleContinueExisting();
    }
  }

  function handleNewSession() {
    show = false;
    dispatch('newSession');
  }

  function handleContinueExisting() {
    show = false;
    dispatch('continueExisting');
  }

  function handleRestartWithTabs() {
    show = false;
    dispatch('restartWithTabs');
  }

  function handleCancel() {
    show = false;
    dispatch('cancel');
  }
</script>

{#if show}
  <div
    class="dialog-overlay" use:autoFocusDialog
    on:click|self={handleCancel}
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
    tabindex="0"
  >
    <div class="dialog-content">
      <div class="dialog-icon">
        <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="#3b82f6" stroke-width="2">
          <circle cx="12" cy="12" r="10"/>
          <path d="M12 6v6l4 2"/>
        </svg>
      </div>

      <h2 class="dialog-title">{$t('resume.title')}</h2>
      <p class="dialog-message">
        {$t('resume.message', { name: session?.name || '' })}
      </p>

      <div class="dialog-actions">
        {#if hasTabs}
          <button
            class="btn-option tabs {cursor === 0 ? 'active' : ''}"
            on:click={handleRestartWithTabs}
            on:mouseenter={() => cursor = 0}
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="3" y="3" width="18" height="18" rx="2" ry="2"/>
              <line x1="9" y1="3" x2="9" y2="21"/>
            </svg>
            <div class="option-content">
              <span class="option-title">{$t('resume.restartWithTabs')}</span>
              <span class="option-desc">{$t('resume.restartWithTabsDesc')} ({$t('resume.tabs', { count: 1 + (session?.followedWindows?.length ?? 0) })})</span>
            </div>
            <span class="option-shortcut">1</span>
          </button>

          <button
            class="btn-option new {cursor === 1 ? 'active' : ''}"
            on:click={handleNewSession}
            on:mouseenter={() => cursor = 1}
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M12 5v14M5 12h14"/>
            </svg>
            <div class="option-content">
              <span class="option-title">{$t('resume.newSession')}</span>
              <span class="option-desc">{$t('resume.newSessionDesc')}</span>
            </div>
            <span class="option-shortcut">2</span>
          </button>

          <button
            class="btn-option continue {cursor === 2 ? 'active' : ''}"
            on:click={handleContinueExisting}
            on:mouseenter={() => cursor = 2}
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/>
            </svg>
            <div class="option-content">
              <span class="option-title">{$t('resume.continueExisting')}</span>
              <span class="option-desc">{$t('resume.continueExistingDesc')}</span>
            </div>
            <span class="option-shortcut">3</span>
          </button>
        {:else}
          <button
            class="btn-option new {cursor === 0 ? 'active' : ''}"
            on:click={handleNewSession}
            on:mouseenter={() => cursor = 0}
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M12 5v14M5 12h14"/>
            </svg>
            <div class="option-content">
              <span class="option-title">{$t('resume.newSession')}</span>
              <span class="option-desc">{$t('resume.newSessionDesc')}</span>
            </div>
            <span class="option-shortcut">1</span>
          </button>

          <button
            class="btn-option continue {cursor === 1 ? 'active' : ''}"
            on:click={handleContinueExisting}
            on:mouseenter={() => cursor = 1}
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/>
            </svg>
            <div class="option-content">
              <span class="option-title">{$t('resume.continueExisting')}</span>
              <span class="option-desc">{$t('resume.continueExistingDesc')}</span>
            </div>
            <span class="option-shortcut">2</span>
          </button>
        {/if}

        <button class="btn-cancel" on:click={handleCancel}>
          {$t('resume.cancel')}
        </button>
      </div>

      <div class="dialog-hint">
        {hasTabs ? $t('resume.navHint3') : $t('resume.navHint2')}
      </div>
    </div>
  </div>
{/if}

<style>
  .dialog-content {
    padding: 32px;
    text-align: center;
    max-width: 480px;
  }

  .dialog-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 56px;
    height: 56px;
    border-radius: 50%;
    background: rgba(59, 130, 246, 0.15);
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
    gap: 12px;
  }

  .btn-option {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 16px 20px;
    background: rgba(255, 255, 255, 0.03);
    border: 2px solid rgba(255, 255, 255, 0.1);
    border-radius: 12px;
    color: #e4e4e7;
    cursor: pointer;
    transition: all 0.2s ease;
    text-align: left;
    position: relative;
  }

  .btn-option:hover,
  .btn-option.active {
    background: rgba(255, 255, 255, 0.08);
    border-color: rgba(255, 255, 255, 0.3);
    transform: translateY(-1px);
  }

  .btn-option.active {
    border-color: #3b82f6;
    box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.1);
  }

  .btn-option svg {
    flex-shrink: 0;
  }

  .option-content {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .option-title {
    font-size: 14px;
    font-weight: 600;
    color: #e4e4e7;
  }

  .option-desc {
    font-size: 12px;
    color: #9ca3af;
  }

  .option-shortcut {
    position: absolute;
    top: 12px;
    right: 16px;
    font-size: 11px;
    font-weight: 600;
    color: #6b7280;
    background: rgba(255, 255, 255, 0.05);
    padding: 2px 8px;
    border-radius: 6px;
  }

  .btn-cancel {
    margin-top: 8px;
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

  .dialog-hint {
    margin-top: 20px;
    font-size: 12px;
    color: #6b7280;
  }
</style>
