<script lang="ts">
  /**
   * Types a saved password into the tab in front of you.
   *
   * The password never comes here: the list holds names, and the backend carries
   * the secret from its store straight to the tab. So no store, log or error in the
   * interface can end up holding one. A master password does pass through, in the
   * field that asks for it, and the field is emptied at once.
   */
  import { tick } from 'svelte';
  import { get } from 'svelte/store';
  import * as App from '../../../../wailsjs/go/main/App';
  import { t } from '../../i18n';
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { describeBackendError } from '../../utils/backendError';
  import { filterPasswords, isCustomSource, loadPressEnter, savePressEnter, sourceDisplayName, tabRunsAgent, type PasswordEntry } from '../../utils/passwordPicker';
  import { activeProjectId } from '../../stores/projects';
  import { sessions } from '../../stores/sessions';
  import BusySpinner from '../common/BusySpinner.svelte';
  import DialogCloseButton from '../common/DialogCloseButton.svelte';

  export let show = false;
  export let sessionId = '';
  export let windowIdx = 0;
  export let onOpenManager: (() => void) | null = null;

  type Source = { source: string; name?: string; available: boolean; enabled: boolean; locked: boolean; error: string };

  let entries: PasswordEntry[] = [];
  let sources: Source[] = [];
  let loading = false;
  let error = '';
  let query = '';
  let selectedIdx = 0;
  let pressEnter = false;
  let inserting = false;
  let insertingEntry: PasswordEntry | null = null;
  // The dialog can open under the pointer, and the row beneath it must not take the
  // selection, so that Enter types a password nobody chose.
  let mouseActive = false;
  let unlockSource = '';
  let master = '';
  let unlocking = false;
  let masterInput: HTMLInputElement | null = null;
  let confirmEntry: PasswordEntry | null = null;
  const CONFIRM_ARM_MS = 400;
  let confirmArmed = false;
  let confirmTimer: ReturnType<typeof setTimeout> | undefined;
  let generation = 0;
  let targetSessionId = '';
  let targetWindowIdx = 0;
  let targetProjectId = '';
  let searchInput: HTMLInputElement | null = null;

  let lastShow = false;
  $: {
    if (show && !lastShow) void open();
    if (!show && lastShow) reset();
    lastShow = show;
  }

  // A password must reach the tab it was picked for. If that changes behind the
  // dialog, close rather than type into another.
  $: if (show && targetSessionId &&
         (sessionId !== targetSessionId || windowIdx !== targetWindowIdx ||
          $activeProjectId !== targetProjectId)) close();

  $: shown = filterPasswords(entries, query);
  $: if (selectedIdx >= shown.length) selectedIdx = Math.max(0, shown.length - 1);
  $: lockedSources = sources.filter((source) => source.enabled && source.locked && source.available);
  $: failedSources = sources.filter((source) => source.error);
  // A tool behind custom commands does its own locking, which is not ours to offer.
  $: anyOpen = sources.some((source) => source.source !== 'local' && !isCustomSource(source.source) && source.enabled && !source.locked);
  $: sourceLabel = (source: string) => sourceDisplayName(source, sources, $t);

  function reset() {
    entries = [];
    sources = [];
    error = '';
    query = '';
    selectedIdx = 0;
    pressEnter = loadPressEnter();
    inserting = false;
    mouseActive = false;
    unlockSource = '';
    master = '';
    unlocking = false;
    confirmEntry = null;
    targetSessionId = '';
  }

  async function open() {
    const current = ++generation;
    reset();
    targetSessionId = sessionId;
    targetWindowIdx = windowIdx;
    targetProjectId = get(activeProjectId);
    await load(current, false);
  }

  async function load(current: number, refresh: boolean) {
    loading = true;
    try {
      const list = await App.GetPasswords(refresh);
      if (!show || current !== generation) return;
      entries = (list?.entries || []) as PasswordEntry[];
      sources = (list?.sources || []) as Source[];
    } catch (e) {
      if (show && current === generation) error = describeBackendError(e);
    } finally {
      if (current === generation) loading = false;
    }
  }

  // The field that had the focus is gone with the master password, and keys only
  // reach this dialog from inside it.
  async function backToSearch() {
    unlockSource = '';
    master = '';
    await tick();
    searchInput?.focus();
  }

  function close() {
    generation++;
    show = false;
  }

  function askUnlock(source: string) {
    unlockSource = source;
    master = '';
    error = '';
  }

  async function submitUnlock() {
    if (unlocking || !master) return;
    const current = generation;
    const source = unlockSource;
    const typed = master;
    // Out of the field at once, whatever the answer.
    master = '';
    // The field is about to be disabled, and keys only reach this dialog from inside
    // it: without somewhere else to be, Escape would stop working until it is over.
    masterInput?.closest<HTMLElement>('[data-dialog]')?.focus();
    unlocking = true;
    error = '';
    try {
      await App.UnlockPasswordSource(source, typed);
      if (!show || current !== generation) return;
      await load(current, false);
      await backToSearch();
    } catch (e) {
      if (show && current === generation) {
        error = describeBackendError(e);
        // Back to where the next try is typed.
        unlocking = false;
        await tick();
        masterInput?.focus();
      }
    } finally {
      if (current === generation) unlocking = false;
    }
  }

  async function lockAll() {
    const current = generation;
    try {
      await App.LockPasswordSources();
      if (show && current === generation) await load(current, false);
    } catch (e) {
      if (show && current === generation) error = describeBackendError(e);
    }
  }

  function pick(entry: PasswordEntry | undefined, confirming = false) {
    if (!entry || inserting || !targetSessionId) return;
    // An agent passes on what it reads to its model provider, which is rarely
    // where a password was meant to go.
    const session = get(sessions).find((candidate) => candidate.id === targetSessionId);
    if (tabRunsAgent(session, targetWindowIdx) && !(confirming && confirmEntry === entry)) {
      if (confirmEntry !== entry) armConfirmButton();
      confirmEntry = entry;
      return;
    }
    void insert(entry);
  }

  // The warning pushes the rows up as it opens, so the second click of a double click
  // can land on this button. Measured: that was a walk straight through it.
  function armConfirmButton() {
    clearTimeout(confirmTimer);
    confirmArmed = false;
    confirmTimer = setTimeout(() => { confirmArmed = true; }, CONFIRM_ARM_MS);
  }

  // A held Enter goes on repeating after the dialog closes, into the terminal, where
  // it would submit the password. Held only: the next real press is the user's.
  function swallowHeldEnter() {
    const down = (e: KeyboardEvent) => {
      if (e.key === 'Enter' && e.repeat) {
        e.preventDefault();
        e.stopImmediatePropagation();
      }
    };
    const up = (e: KeyboardEvent) => {
      if (e.key !== 'Enter') return;
      window.removeEventListener('keydown', down, true);
      window.removeEventListener('keyup', up, true);
    };
    window.addEventListener('keydown', down, true);
    window.addEventListener('keyup', up, true);
  }

  async function insert(entry: PasswordEntry) {
    const current = generation;
    inserting = true;
    insertingEntry = entry;
    error = '';
    try {
      await App.InsertPassword(entry.source, entry.id, targetSessionId, targetWindowIdx, pressEnter, targetProjectId);
      if (show && current === generation) close();
    } catch (e) {
      if (show && current === generation) {
        error = describeBackendError(e);
        confirmEntry = null;
      }
    } finally {
      if (current === generation) inserting = false;
    }
  }

  function move(step: number) {
    if (!shown.length) return;
    selectedIdx = (selectedIdx + step + shown.length) % shown.length;
    confirmEntry = null;
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      claimKeyForDialog();
      e.stopPropagation();
      // Closing would only hide that it may still arrive, so this calls off the fetch
      // and the dialog closes once the backend has said it stopped.
      if (inserting) {
        void App.CancelPasswordInsert();
        return;
      }
      if (confirmEntry) confirmEntry = null;
      else if (unlockSource) void backToSearch();
      else close();
      return;
    }
    if (unlockSource) {
      if (e.key === 'Enter') {
        e.preventDefault();
        void submitUnlock();
      }
      return;
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      move(1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      move(-1);
    } else if (e.key === 'Enter') {
      // On a button or the checkbox, Enter is theirs, not a request to type.
      const inside = e.target as HTMLElement | null;
      if (inside && inside.tagName !== 'INPUT' && inside !== e.currentTarget) return;
      if (inside instanceof HTMLInputElement && inside.type === 'checkbox') return;
      e.preventDefault();
      // Holding it must not walk through the warning.
      if (e.repeat) return;
      swallowHeldEnter();
      pick(shown[selectedIdx], true);
    }
  }

  function focusOnMount(element: HTMLInputElement) {
    element.focus();
  }
</script>

{#if show}
  <div
    class="dialog-overlay picker-overlay"
    class:busy={loading || unlocking || inserting}
    use:autoFocusDialog
    tabindex="-1"
    role="dialog"
    aria-modal="true"
    data-dialog="password-picker"
    on:keydown={handleKeydown}
  >
    <div class="dialog-content picker">
      <div class="dialog-header">
        <h2>{$t('passwords.title')}</h2>
        <DialogCloseButton on:click={() => { if (inserting) void App.CancelPasswordInsert(); else close(); }} />
      </div>

      {#if !targetSessionId}
        <div class="dialog-body">
          <div class="state warn">{$t('passwords.noSession')}</div>
        </div>
        {#if onOpenManager}
          <div class="dialog-footer picker-footer">
            <span></span>
            <button class="btn-secondary" data-action="manage" on:click={() => { close(); onOpenManager?.(); }}>{$t('passwords.manage')}</button>
          </div>
        {/if}
      {:else}
        <div class="search-wrap">
          <input
            class="search"
            use:focusOnMount
            bind:this={searchInput}
            bind:value={query}
            on:input={() => { selectedIdx = 0; confirmEntry = null; }}
            placeholder={$t('passwords.searchPlaceholder')}
            autocomplete="off"
            spellcheck="false"
          />
        </div>

        <div class="dialog-body list-body">
          {#if error}<div class="error-line" role="alert">{error}</div>{/if}

          {#each lockedSources as source (source.source)}
            {#if unlockSource === source.source}
              <div class="unlock-row" data-unlock={source.source}>
                <input
                  type="password"
                  use:focusOnMount
                  bind:this={masterInput}
                  disabled={unlocking}
                  bind:value={master}
                  placeholder={$t('passwords.masterPlaceholder', { source: sourceLabel(source.source) })}
                  autocomplete="off"
                />
                <button class="btn-primary" disabled={!master || unlocking} on:click={submitUnlock}>{#if unlocking}<BusySpinner />{/if} {$t('passwords.unlock')}</button>
              </div>
            {:else}
              <button class="locked-row" data-locked={source.source} on:click={() => askUnlock(source.source)}>
                🔒 {$t('passwords.lockedSource', { source: sourceLabel(source.source) })}
              </button>
            {/if}
          {/each}

          {#each failedSources as source (source.source)}
            <div class="state warn" data-failed={source.source}>{sourceLabel(source.source)}: {describeBackendError(source.error)}</div>
          {/each}

          {#each shown as entry, index (`${entry.source}:${entry.id}`)}
            <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
            <div
              class="row"
              class:selected={index === selectedIdx}
              data-entry={entry.name}
              on:mousemove={() => { if (!mouseActive) { mouseActive = true; selectedIdx = index; } }}
              on:mouseenter={() => { if (mouseActive) selectedIdx = index; }}
              on:click={() => { selectedIdx = index; pick(entry); }}
            >
              <span class="row-main">
                <span class="row-name">{entry.name}</span>
                {#if entry.username || entry.group}
                  <span class="row-sub">{[entry.group, entry.username].filter(Boolean).join(' · ')}</span>
                {/if}
              </span>
              {#if inserting && insertingEntry === entry}<BusySpinner label={$t('common.loading')} />{/if}
              <span class="row-source">{sourceLabel(entry.source)}</span>
            </div>
            {#if confirmEntry === entry}
              <div class="confirm-row" data-confirm role="alert">
                <span>{$t('passwords.agentWarning')}</span>
                <button class="btn-primary" disabled={inserting || !confirmArmed} on:click={() => insert(entry)}>{#if inserting}<BusySpinner />{/if} {$t('passwords.insertAnyway')}</button>
              </div>
            {/if}
          {:else}
            {#if loading}
              <div class="state" data-loading><BusySpinner /> {$t('common.loading')}</div>
            {:else}<div class="state" data-empty>{entries.length ? $t('passwords.noMatch') : $t('passwords.empty')}</div>{/if}
          {/each}
        </div>

        <div class="dialog-footer picker-footer">
          <label class="enter-toggle">
            <input type="checkbox" bind:checked={pressEnter} on:change={() => savePressEnter(pressEnter)} />
            <span>{$t('passwords.sendEnter')}</span>
          </label>
          <span class="footer-actions">
            {#if anyOpen}
              <button class="btn-secondary" data-action="lock" on:click={lockAll}>{$t('passwords.lockAll')}</button>
            {/if}
            {#if onOpenManager}
              <button class="btn-secondary" data-action="manage" on:click={() => { close(); onOpenManager?.(); }}>{$t('passwords.manage')}</button>
            {/if}
          </span>
        </div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .picker-overlay:focus { outline: none; }
  .picker {
    width: min(560px, 94vw);
    max-width: min(560px, 94vw);
    max-height: 84vh;
    display: flex;
    flex-direction: column;
  }
  .search-wrap { padding: 16px 24px 0; }
  .search, .unlock-row input {
    width: 100%;
    box-sizing: border-box;
    padding: 8px 11px;
    border-radius: 7px;
    font-size: 13px;
    font-family: inherit;
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(0, 0, 0, 0.25);
    color: #e4e4e7;
  }
  .search:focus, .unlock-row input:focus {
    outline: none;
    border-color: rgba(var(--accent-rgb), 0.6);
  }
  .list-body {
    flex: 1;
    min-height: 120px;
    overflow-y: auto;
  }
  .state { padding: 22px 0; text-align: center; font-size: 13px; color: #71717a; }
  .state.warn { padding: 6px 0; color: #fbbf24; font-size: 12px; text-align: left; }
  .error-line { margin-bottom: 8px; color: #fb7185; font-size: 13px; }
  .locked-row {
    display: block;
    width: 100%;
    margin-bottom: 6px;
    padding: 7px 9px;
    font-size: 12px;
    font-family: inherit;
    text-align: left;
    color: #d1d5db;
    background: rgba(255, 255, 255, 0.03);
    border: 1px dashed rgba(255, 255, 255, 0.15);
    border-radius: 7px;
    cursor: pointer;
  }
  .locked-row:hover { background: rgba(255, 255, 255, 0.07); }
  .unlock-row {
    display: flex;
    gap: 8px;
    margin-bottom: 6px;
  }
  .unlock-row input { flex: 1; min-width: 0; }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 7px 9px;
    border-radius: 7px;
    border: 1px solid transparent;
    cursor: pointer;
  }
  .row.selected {
    background: rgba(var(--accent-rgb), 0.14);
    border-color: rgba(var(--accent-rgb), 0.3);
  }
  .row-main { display: flex; flex-direction: column; gap: 2px; flex: 1; min-width: 0; }
  .row-name { font-size: 13px; color: #e4e4e7; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .row-sub { font-size: 11px; color: #9ca3af; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .busy { cursor: wait; }
  .row-source { flex-shrink: 0; font-size: 11px; color: #6b7280; }
  .confirm-row {
    display: flex;
    align-items: center;
    gap: 10px;
    margin: 4px 0 6px;
    padding: 8px 10px;
    font-size: 12px;
    color: #fbbf24;
    background: rgba(251, 191, 36, 0.08);
    border: 1px solid rgba(251, 191, 36, 0.3);
    border-radius: 7px;
  }
  .confirm-row span { flex: 1; }
  .picker-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
  }
  .enter-toggle {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: #9ca3af;
    cursor: pointer;
  }
  .footer-actions { display: flex; gap: 8px; }
</style>
