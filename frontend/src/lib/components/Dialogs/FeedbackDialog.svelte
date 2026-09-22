<script lang="ts">
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { createEventDispatcher } from 'svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import { BrowserOpenURL, ClipboardSetText } from '../../../../wailsjs/runtime/runtime';
  import { t } from '../../i18n';

  export let show = false;

  const dispatch = createEventDispatcher();

  let kind: 'bug' | 'idea' = 'bug';
  let summary = '';
  let detail = '';
  // On by default because a report without it usually prompts the same first
  // question back — but asked, since it describes the user's own machine.
  let includeSystem = true;
  let copied = false;
  let error = '';

  $: canSend = summary.trim() !== '' || detail.trim() !== '';

  function close() {
    show = false;
    summary = '';
    detail = '';
    kind = 'bug';
    includeSystem = true;
    copied = false;
    error = '';
    dispatch('close');
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      claimKeyForDialog();
      e.stopPropagation();
      close();
    }
  }

  async function compose() {
    error = '';
    return App.ComposeFeedback({ kind, summary, detail, includeSystem } as any);
  }

  // Each route hands the report to something the user controls — their
  // browser, their mail client, their clipboard. Nothing is sent from the app,
  // so nothing it ships has to carry a credential.
  async function sendToGitHub() {
    try {
      const links = await compose();
      BrowserOpenURL(links.github);
      close();
    } catch (e) {
      error = String(e);
    }
  }

  async function sendByEmail() {
    try {
      const links = await compose();
      BrowserOpenURL(links.email);
      close();
    } catch (e) {
      error = String(e);
    }
  }

  // The way out for someone with neither a GitHub account nor a mail client
  // set up — and the one route that never truncates.
  async function copyToClipboard() {
    try {
      const links = await compose();
      await ClipboardSetText(links.text);
      copied = true;
      setTimeout(() => { copied = false; }, 2000);
    } catch (e) {
      error = String(e);
    }
  }
</script>

{#if show}
  <div
    class="dialog-overlay" use:autoFocusDialog
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
    tabindex="0"
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2>{$t('feedback.title')}</h2>
        <button class="close-btn" on:click={close} aria-label={$t('common.cancel')}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/>
            <line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>

      <div class="dialog-body">
        {#if error}
          <p class="feedback-error">{error}</p>
        {/if}

        <div class="form-group">
          <span class="form-label">{$t('feedback.kind')}</span>
          <div class="kind-row">
            <button
              type="button"
              class="kind-btn {kind === 'bug' ? 'selected' : ''}"
              on:click={() => kind = 'bug'}
            >{$t('feedback.kindBug')}</button>
            <button
              type="button"
              class="kind-btn {kind === 'idea' ? 'selected' : ''}"
              on:click={() => kind = 'idea'}
            >{$t('feedback.kindIdea')}</button>
          </div>
        </div>

        <div class="form-group">
          <label class="form-label" for="feedback-summary">{$t('feedback.summary')}</label>
          <input
            id="feedback-summary"
            type="text"
            class="form-input"
            bind:value={summary}
            placeholder={$t('feedback.summaryPlaceholder')}
          />
        </div>

        <div class="form-group">
          <label class="form-label" for="feedback-detail">{$t('feedback.detail')}</label>
          <textarea
            id="feedback-detail"
            class="form-input feedback-detail"
            bind:value={detail}
            rows="7"
            placeholder={$t('feedback.detailPlaceholder')}
          ></textarea>
        </div>

        <label class="checkbox-label">
          <input type="checkbox" bind:checked={includeSystem} class="checkbox-input" />
          <span class="checkbox-custom"></span>
          <span class="checkbox-text">{$t('feedback.includeSystem')}</span>
        </label>

        <p class="field-hint">{$t('feedback.privacyNote')}</p>
      </div>

      <div class="dialog-actions">
        <button class="btn-cancel" on:click={copyToClipboard} disabled={!canSend}>
          {copied ? $t('feedback.copied') : $t('feedback.copy')}
        </button>
        <button class="btn-cancel" on:click={sendByEmail} disabled={!canSend}>
          {$t('feedback.byEmail')}
        </button>
        <button class="btn-primary" on:click={sendToGitHub} disabled={!canSend}>
          {$t('feedback.onGitHub')}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .dialog-content {
    max-width: 560px;
  }

  .dialog-body {
    padding: 20px 24px;
  }

  .form-group {
    margin-bottom: 16px;
  }

  .form-label {
    display: block;
    margin-bottom: 6px;
    font-size: 12px;
    font-weight: 600;
    color: #9ca3af;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .form-input {
    width: 100%;
    padding: 10px 12px;
    background: rgba(0, 0, 0, 0.2);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 8px;
    font-size: 14px;
    color: white;
  }

  .form-input:focus {
    outline: none;
    border-color: rgba(var(--accent-rgb), 0.5);
  }

  /* Resizable vertically only: sideways it would break the dialog's width. */
  .feedback-detail {
    resize: vertical;
    min-height: 120px;
    font-family: inherit;
    line-height: 1.5;
  }

  .kind-row {
    display: flex;
    gap: 8px;
  }

  .kind-btn {
    flex: 1;
    padding: 10px;
    border-radius: 8px;
    border: 1px solid rgba(255, 255, 255, 0.1);
    background: rgba(255, 255, 255, 0.03);
    color: #d4d4d8;
    font-size: 13px;
    cursor: pointer;
  }

  .kind-btn.selected {
    border-color: rgba(var(--accent-rgb), 0.6);
    background: rgba(var(--accent-rgb), 0.12);
    color: var(--accent-pale);
  }

  .field-hint {
    margin: 12px 0 0;
    font-size: 11px;
    color: #8b8b93;
    line-height: 1.5;
  }

  .feedback-error {
    margin: 0 0 12px;
    padding: 10px 12px;
    border-radius: 8px;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.3);
    color: #fca5a5;
    font-size: 13px;
  }

  .dialog-actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
    padding-top: 8px;
    border-top: 1px solid rgba(255, 255, 255, 0.05);
  }

  .dialog-actions button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
