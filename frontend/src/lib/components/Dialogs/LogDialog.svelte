<script lang="ts">
  import { createEventDispatcher, tick } from 'svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import { ClipboardSetText } from '../../../../wailsjs/runtime/runtime';
  import { autoFocusDialog } from '../../utils/dialogActions';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import { t } from '../../i18n';

  export let show = false;

  const dispatch = createEventDispatcher();

  /**
   * Which logs are offered. The application log carries what the app itself
   * did; dictation keeps its own, in its own format, and it is the one that
   * answers "the microphone recorded nothing" — none of it reaches the
   * application log, because dictation prints rather than logs.
   */
  const SOURCES = [
    { key: 'app', label: 'logs.app' },
    { key: 'dictation', label: 'logs.dictation' },
  ];

  let source = 'app';
  let log: { path: string; lines: string[]; truncated: boolean; missing: boolean } | null = null;
  let loading = false;
  let filter = '';
  /** On by default: the activity poll writes on every tick and accounted for
   *  96% of the lines in a real log — measured at 68,000 in an hour. */
  let hideNoise = true;
  let body: HTMLPreElement | null = null;

  const NOISE = ['[WaitDebug]', '[StatusDebug]'];

  $: lines = (() => {
    const all = log?.lines ?? [];
    const needle = filter.trim().toLowerCase();
    return all.filter((line) => {
      if (hideNoise && NOISE.some((p) => line.includes(p))) return false;
      return !needle || line.toLowerCase().includes(needle);
    });
  })();

  // Load on open, and whenever the source changes.
  let loadedFor = '';
  $: if (show && source !== loadedFor) {
    loadedFor = source;
    void load();
  }
  $: if (!show) loadedFor = '';

  async function load() {
    loading = true;
    try {
      log = await App.GetLog(source);
      await tick();
      // The end is what explains whatever went wrong.
      if (body) body.scrollTop = body.scrollHeight;
    } catch (e) {
      log = { path: '', lines: [String(e)], truncated: false, missing: false };
    } finally {
      loading = false;
    }
  }

  async function copy() {
    if (!lines.length) return;
    // What is on screen, not the whole file: filtering exists to find the
    // relevant lines, and those are the ones worth pasting into a report.
    await ClipboardSetText(lines.join('\n'));
  }

  async function openFolder() {
    try {
      await App.OpenLogFolder(source);
    } catch (e) {
      console.error('Failed to open the log folder:', e);
    }
  }

  function close() {
    show = false;
    dispatch('close');
  }

  function handleKeydown(e: KeyboardEvent) {
    // Only the outer dialog: with the confirmation open, Escape belongs to it.
    if (e.key === 'Escape' && !confirmClear) close();
  }

  let confirmClear = false;

  async function clearLog() {
    confirmClear = false;
    try {
      await App.ClearLog(source);
      await load();
    } catch (e) {
      log = { path: log?.path ?? '', lines: [String(e)], truncated: false, missing: false };
    }
  }
</script>

<svelte:window on:keydown={show ? handleKeydown : undefined} />

{#if show}
  <div class="dialog-overlay" use:autoFocusDialog on:click={close} role="presentation">
    <div class="dialog-content log-dialog" on:click|stopPropagation role="presentation">
      <div class="dialog-header">
        <h2>{$t('logs.title')}</h2>
        <button class="close-btn" on:click={close}>×</button>
      </div>

      <div class="log-sources">
        {#each SOURCES as s}
          <button
            class="source-btn"
            class:active={source === s.key}
            on:click={() => source = s.key}
          >{$t(s.label)}</button>
        {/each}
      </div>

      <div class="log-toolbar">
        <input class="log-search" type="text" bind:value={filter} placeholder={$t('settings.logFilter')} />
        <button class="action-btn" class:active={hideNoise} on:click={() => hideNoise = !hideNoise}>
          {$t('settings.logHideNoise')}
        </button>
        <button class="action-btn" on:click={load}>{$t('settings.logRefresh')}</button>
        <button class="action-btn" on:click={copy} disabled={!lines.length}>{$t('settings.logCopy')}</button>
        <button class="action-btn" on:click={openFolder}>{$t('settings.logOpenFolder')}</button>
        <button class="action-btn danger" on:click={() => confirmClear = true} disabled={!log?.lines?.length}>
          {$t('logs.clear')}
        </button>
      </div>

      <p class="log-note">
        <span class="log-path" title={log?.path}>{log?.path ?? ''}</span>
        {#if log?.lines?.length}
          · {$t('settings.logMatches', { shown: lines.length, total: log.lines.length })}
        {/if}
        {#if log?.truncated}
          · {$t('settings.logTruncated', { count: log.lines.length })}
        {/if}
      </p>

      <pre class="log-body" bind:this={body}>{loading
        ? ''
        : lines.length
          ? lines.join('\n')
          : $t('settings.logEmpty')}</pre>

      <div class="dialog-footer">
        <button class="btn-cancel" on:click={close}>{$t('logs.close')}</button>
      </div>
    </div>
  </div>

  <!-- Asked for, because clearing cannot be undone and the button sits beside
       ones that cannot lose anything. -->
  <ConfirmDialog
    bind:show={confirmClear}
    title={$t('logs.clear')}
    message={$t('logs.clearConfirm')}
    confirmText={$t('logs.clear')}
    cancelText={$t('common.cancel')}
    variant="warning"
    on:confirm={clearLog}
  />
{/if}

<style>
  /* Tall and wide: a log is read by scanning down long lines, and the inline
     panel this replaced was too small to do that in.
     max-width has to be overridden explicitly — the shared .dialog-content caps
     every dialog at 400px, which is right for a confirmation and useless here.
     Setting width alone left it at that cap. */
  .log-dialog {
    width: min(1200px, 94vw);
    max-width: min(1200px, 94vw);
    height: min(84vh, 950px);
    display: flex;
    flex-direction: column;
  }
  .log-sources {
    display: flex;
    gap: 6px;
    /* Space above as well: the source buttons sat directly under the title,
       reading as part of the header rather than as a choice below it. */
    padding: 12px 20px 10px;
  }
  .source-btn {
    padding: 4px 12px;
    font-size: 12px;
    cursor: pointer;
    border-radius: 5px;
    border: 1px solid rgba(255, 255, 255, 0.14);
    background: rgba(255, 255, 255, 0.05);
    color: #c9d1d9;
  }
  .source-btn.active {
    border-color: rgba(var(--accent-rgb), 0.5);
    background: rgba(var(--accent-rgb), 0.16);
  }
  .log-toolbar {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 20px;
  }
  .log-search {
    flex: 1;
    min-width: 0;
    padding: 4px 10px;
    font-size: 12px;
    border-radius: 5px;
    border: 1px solid rgba(255, 255, 255, 0.14);
    background: rgba(0, 0, 0, 0.25);
    color: #e4e4e7;
  }
  .action-btn {
    flex-shrink: 0;
    padding: 4px 10px;
    font-size: 12px;
    cursor: pointer;
    border-radius: 5px;
    border: 1px solid rgba(255, 255, 255, 0.14);
    background: rgba(255, 255, 255, 0.05);
    color: #c9d1d9;
  }
  .action-btn:hover:not(:disabled) { background: rgba(255, 255, 255, 0.12); }
  .action-btn:disabled { opacity: 0.45; cursor: default; }
  .action-btn.active {
    border-color: rgba(var(--accent-rgb), 0.5);
    background: rgba(var(--accent-rgb), 0.16);
  }
  .action-btn.danger { color: #fca5a5; border-color: rgba(239, 68, 68, 0.35); }
  .action-btn.danger:hover:not(:disabled) { background: rgba(239, 68, 68, 0.18); }
  .log-note {
    display: flex;
    align-items: center;
    gap: 4px;
    margin: 8px 20px 0;
    font-size: 11px;
    color: #9ca3af;
    min-width: 0;
  }
  .log-path {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .log-body {
    flex: 1;
    min-height: 0;
    margin: 8px 20px 0;
    padding: 12px;
    overflow: auto;
    border-radius: 6px;
    background: rgba(0, 0, 0, 0.28);
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
    font-size: 11px;
    line-height: 1.55;
    color: #c9d1d9;
    /* Not wrapped: lines carry a timestamp and a prefix, and wrapping makes a
       column of them impossible to scan. */
    white-space: pre;
    user-select: text;
  }
</style>
