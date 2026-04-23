<script lang="ts">
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { onMount } from 'svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import { t } from '../../i18n';

  export let show = false;

  interface UpdateInfo {
    available: boolean;
    currentVersion: string;
    latestVersion: string;
  }

  let updateInfo: UpdateInfo | null = null;
  let isChecking = false;
  let isUpdating = false;
  let error = '';
  let success = '';

  let lastShow = false;
  $: if (show && !lastShow) {
    checkForUpdate();
  }
  $: lastShow = show;

  async function checkForUpdate() {
    isChecking = true;
    error = '';
    success = '';
    try {
      updateInfo = await App.CheckForUpdate();
    } catch (e) {
      error = String(e);
    } finally {
      isChecking = false;
    }
  }

  async function performUpdate() {
    if (!updateInfo?.latestVersion) return;

    isUpdating = true;
    error = '';
    try {
      await App.PerformUpdate(updateInfo.latestVersion);
      success = $t('update.success');
    } catch (e) {
      error = String(e);
    } finally {
      isUpdating = false;
    }
  }

  function close() {
    show = false;
    updateInfo = null;
    error = '';
    success = '';
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      close();
    }
  }
</script>

{#if show}
  <div
    class="dialog-overlay" use:autoFocusDialog
    on:click|self={close}
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2>{$t('update.title')}</h2>
        <button class="close-btn" on:click={close}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/>
            <line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>

      <div class="dialog-body">
        {#if isChecking}
          <div class="status-message">
            <div class="spinner"></div>
            <span>{$t('update.checking')}</span>
          </div>
        {:else if error}
          <div class="error-message">
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="12" cy="12" r="10"/>
              <line x1="15" y1="9" x2="9" y2="15"/>
              <line x1="9" y1="9" x2="15" y2="15"/>
            </svg>
            <span>{error}</span>
          </div>
        {:else if success}
          <div class="success-message">
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/>
              <polyline points="22 4 12 14.01 9 11.01"/>
            </svg>
            <span>{success}</span>
          </div>
        {:else if updateInfo}
          <div class="version-info">
            <div class="version-row">
              <span class="label">{$t('update.currentVersion')}</span>
              <span class="value">v{updateInfo.currentVersion}</span>
            </div>
            {#if updateInfo.available}
              <div class="version-row highlight">
                <span class="label">{$t('update.latestVersion')}</span>
                <span class="value new">{updateInfo.latestVersion}</span>
              </div>
              <div class="update-available">
                <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
                  <polyline points="7 10 12 15 17 10"/>
                  <line x1="12" y1="15" x2="12" y2="3"/>
                </svg>
                <span>{$t('update.available')}</span>
              </div>
            {:else}
              <div class="up-to-date">
                <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/>
                  <polyline points="22 4 12 14.01 9 11.01"/>
                </svg>
                <span>{$t('update.upToDate')}</span>
              </div>
            {/if}
          </div>
        {/if}
      </div>

      <div class="dialog-footer">
        {#if updateInfo?.available && !success}
          <button
            class="btn btn-primary"
            on:click={performUpdate}
            disabled={isUpdating}
          >
            {#if isUpdating}
              <div class="spinner small"></div>
              {$t('update.updateNow')}...
            {:else}
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
                <polyline points="7 10 12 15 17 10"/>
                <line x1="12" y1="15" x2="12" y2="3"/>
              </svg>
              {$t('update.updateNow')}
            {/if}
          </button>
        {/if}
        <button class="btn btn-secondary" on:click={close}>
          {success ? $t('update.close') : $t('update.cancel')}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  /* Component-specific styles */
  .status-message {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 12px;
    padding: 20px;
    color: #9ca3af;
  }

  .success-message {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 16px;
    background: rgba(34, 197, 94, 0.1);
    border: 1px solid rgba(34, 197, 94, 0.2);
    border-radius: 8px;
    color: #4ade80;
  }

  .version-info {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .version-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 12px 16px;
    background: rgba(255, 255, 255, 0.02);
    border-radius: 8px;
  }

  .version-row.highlight {
    background: rgba(139, 92, 246, 0.1);
    border: 1px solid rgba(139, 92, 246, 0.2);
  }

  .label {
    color: #9ca3af;
    font-size: 14px;
  }

  .value {
    font-weight: 600;
    color: #e4e4e7;
  }

  .value.new {
    color: #a78bfa;
  }

  .update-available, .up-to-date {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 12px;
    padding: 20px;
    margin-top: 8px;
    border-radius: 8px;
  }

  .update-available {
    background: rgba(139, 92, 246, 0.1);
    color: #a78bfa;
  }

  .up-to-date {
    background: rgba(34, 197, 94, 0.1);
    color: #4ade80;
  }

  /* Button base and secondary styles (not in global CSS) */
  .btn {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 10px 20px;
    font-size: 14px;
    font-weight: 500;
    border-radius: 8px;
    border: none;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .btn:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .btn-secondary {
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.1);
    color: #9ca3af;
  }

  .btn-secondary:hover {
    background: rgba(255, 255, 255, 0.1);
    color: white;
  }

  /* Spinner animation */
  .spinner {
    width: 24px;
    height: 24px;
    border: 2px solid rgba(139, 92, 246, 0.2);
    border-top-color: #8b5cf6;
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  .spinner.small {
    width: 16px;
    height: 16px;
    border-width: 2px;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }
</style>
