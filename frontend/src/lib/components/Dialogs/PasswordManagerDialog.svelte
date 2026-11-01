<script lang="ts">
  /**
   * Where passwords come from: the app's own list, a KeePassXC database, Bitwarden accounts,
   * or any other tool described by two commands.
   *
   * An entry being edited does not hold its password. An empty field keeps the one
   * stored, which is why the form has nothing to leak.
   */
  import { get } from 'svelte/store';
  import * as App from '../../../../wailsjs/go/main/App';
  import { t } from '../../i18n';
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { describeBackendError } from '../../utils/backendError';
  import { CUSTOM_SOURCE_PRESETS, isBitwardenAccount, isCustomSource, type PasswordEntry } from '../../utils/passwordPicker';
  import { activeProjectId } from '../../stores/projects';
  import { loadSessions, selectedSessionId, selectedWindowIdx } from '../../stores/sessions';
  import BusySpinner from '../common/BusySpinner.svelte';
  import DialogCloseButton from '../common/DialogCloseButton.svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';

  export let show = false;
  /** Raised when closing, so the picker can come back with the changes. */
  export let onDone: (() => void) | null = null;
  /** The tab a Bitwarden login opens beside. */
  export let sessionId = '';
  export let windowIdx = 0;

  type Source = { source: string; available: boolean; enabled: boolean; locked: boolean; detail: string; keyFile: string; error?: string; name?: string; tool?: string; listCommand?: string; getCommand?: string };

  let entries: PasswordEntry[] = [];
  let sources: Source[] = [];
  let keyringAvailable = true;
  let error = '';
  let generation = 0;

  let editing = false;
  let editId = '';
  let fName = '';
  let fUsername = '';
  let fPassword = '';
  let saving = false;

  let keepassDb = '';
  let keepassKey = '';
  let customEditing = false;
  let customId = '';
  let cName = '';
  let cList = '';
  let cGet = '';
  let customSaving = false;
  let bitwardenInstalled = true;
  let addingBitwarden = false;
  let bName = '';
  let bServer = '';
  let bitwardenSaving = false;
  let loadingList = false;
  let openingLogin = '';
  let deleteTarget: PasswordEntry | null = null;
  let showDelete = false;

  let lastShow = false;
  $: {
    if (show && !lastShow) void load();
    if (!show && lastShow) reset();
    lastShow = show;
  }

  $: localEntries = entries.filter((entry) => entry.source === 'local');
  $: keepass = sources.find((source) => source.source === 'keepassxc');
  $: bitwardenAccounts = sources.filter((source) => isBitwardenAccount(source.source));
  $: customSources = sources.filter((source) => isCustomSource(source.source));

  function reset() {
    generation++;
    entries = [];
    sources = [];
    error = '';
    editing = false;
    fPassword = '';
    customEditing = false;
    addingBitwarden = false;
    deleteTarget = null;
    showDelete = false;
  }

  async function load() {
    const current = ++generation;
    loadingList = true;
    try {
      const list = await App.GetPasswords(false);
      if (!show || current !== generation) return;
      entries = (list?.entries || []) as PasswordEntry[];
      sources = (list?.sources || []) as Source[];
      keyringAvailable = !!list?.keyringAvailable;
      bitwardenInstalled = !!list?.bitwardenInstalled;
      const database = sources.find((source) => source.source === 'keepassxc');
      keepassDb = database?.detail || '';
      keepassKey = database?.keyFile || '';
    } catch (e) {
      if (show && current === generation) error = describeBackendError(e);
    } finally {
      if (current === generation) loadingList = false;
    }
  }

  function close() {
    show = false;
    onDone?.();
  }

  function startNew() {
    editing = true;
    editId = '';
    fName = '';
    fUsername = '';
    fPassword = '';
    error = '';
  }

  function startEdit(entry: PasswordEntry) {
    editing = true;
    editId = entry.id;
    fName = entry.name;
    fUsername = entry.username;
    fPassword = '';
    error = '';
  }

  function cancelEdit() {
    editing = false;
    fPassword = '';
  }

  async function saveEntry() {
    if (saving) return;
    saving = true;
    error = '';
    const password = fPassword;
    try {
      await App.SaveLocalPassword(editId, fName, fUsername, password);
      fPassword = '';
      editing = false;
      await load();
    } catch (e) {
      error = describeBackendError(e);
    } finally {
      saving = false;
    }
  }

  async function confirmDelete() {
    const target = deleteTarget;
    deleteTarget = null;
    if (!target) return;
    try {
      if (isCustomSource(target.source)) await App.DeleteCustomPasswordSource(target.id);
      else if (isBitwardenAccount(target.source)) await App.DeleteBitwardenAccount(target.id);
      else await App.DeleteLocalPassword(target.id);
      await load();
    } catch (e) {
      error = describeBackendError(e);
    }
  }

  function startCustom(source: Source | null) {
    customEditing = true;
    customId = source ? source.source.slice('custom:'.length) : '';
    cName = source?.name || '';
    cList = source?.listCommand || '';
    cGet = source?.getCommand || '';
    error = '';
  }

  function applyPreset(preset: (typeof CUSTOM_SOURCE_PRESETS)[number]) {
    cName = preset.name;
    cList = preset.list;
    cGet = preset.get;
  }

  async function saveCustom() {
    if (customSaving) return;
    customSaving = true;
    error = '';
    try {
      await App.SaveCustomPasswordSource(customId, cName, cList, cGet);
      customEditing = false;
      await load();
    } catch (e) {
      error = describeBackendError(e);
    } finally {
      customSaving = false;
    }
  }

  async function saveKeePass() {
    error = '';
    try {
      await App.SetKeePassXCDatabase(keepassDb, keepassKey);
      await load();
    } catch (e) {
      error = describeBackendError(e);
    }
  }

  async function browse(field: 'db' | 'key') {
    const started = field === 'db' ? keepassDb : keepassKey;
    try {
      const path = await App.BrowsePasswordFile(started);
      if (!path) return;
      if (field === 'db' && keepassDb === started) keepassDb = path;
      if (field === 'key' && keepassKey === started) keepassKey = path;
      await saveKeePass();
    } catch (e) {
      error = describeBackendError(e);
    }
  }

  function startBitwarden() {
    addingBitwarden = true;
    bName = '';
    bServer = '';
    error = '';
  }

  async function saveBitwarden() {
    if (bitwardenSaving) return;
    bitwardenSaving = true;
    error = '';
    try {
      await App.SaveBitwardenAccount(bName, bServer);
      addingBitwarden = false;
      await load();
    } catch (e) {
      error = describeBackendError(e);
    } finally {
      bitwardenSaving = false;
    }
  }

  // The answers go to a terminal tab, so that is where this dialog gives way to,
  // without the picker coming back over it.
  async function openBitwardenLogin(account: Source) {
    if (openingLogin || !sessionId) return;
    const targetSession = sessionId;
    const targetProject = get(activeProjectId);
    openingLogin = account.source;
    error = '';
    try {
      const newIdx = await App.OpenBitwardenLogin(account.source.slice('bitwarden:'.length), targetSession, windowIdx, targetProject);
      await loadSessions();
      if (newIdx >= 0 && get(selectedSessionId) === targetSession && get(activeProjectId) === targetProject) {
        selectedWindowIdx.set(newIdx);
      }
      show = false;
    } catch (e) {
      // The tab can exist even when the command could not be typed into it.
      void loadSessions();
      error = describeBackendError(e);
    } finally {
      openingLogin = '';
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape' || showDelete) return;
    claimKeyForDialog();
    e.stopPropagation();
    if (addingBitwarden) addingBitwarden = false;
    else if (customEditing) customEditing = false;
    else if (editing) cancelEdit();
    else close();
  }
</script>

{#if show}
  <div
    class="dialog-overlay"
    class:busy={loadingList || bitwardenSaving || customSaving || saving || !!openingLogin}
    use:autoFocusDialog
    tabindex="-1"
    role="dialog"
    aria-modal="true"
    data-dialog="password-manager"
    on:keydown={handleKeydown}
  >
    <div class="dialog-content manager">
      <div class="dialog-header">
        <h2>{$t('passwords.managerTitle')}</h2>
        {#if loadingList}<span class="title-busy"><BusySpinner label={$t('common.loading')} /></span>{/if}
        <DialogCloseButton on:click={close} />
      </div>
      <div class="dialog-body manager-body">
        {#if error}<div class="error-line" role="alert">{error}</div>{/if}

        <section data-section="local">
          <h3>{$t('passwords.source.local')}</h3>
          {#if !keyringAvailable}
            <p class="hint warn" data-no-keyring>{$t('passwords.noKeyring')}</p>
          {/if}
          {#each localEntries as entry (entry.id)}
            <div class="entry-row" data-entry={entry.name}>
              <span class="entry-main">
                <span class="entry-name">{entry.name}</span>
                {#if entry.username}<span class="entry-sub">{entry.username}</span>{/if}
              </span>
              <button class="btn-secondary small" data-action="edit" on:click={() => startEdit(entry)}>{$t('passwords.edit')}</button>
              <button class="btn-secondary small" data-action="delete" on:click={() => { deleteTarget = entry; showDelete = true; }}>{$t('passwords.delete')}</button>
            </div>
          {/each}

          {#if editing}
            <form class="entry-form" data-form on:submit|preventDefault={saveEntry}>
              <label>
                <span>{$t('passwords.fieldName')}</span>
                <input bind:value={fName} autocomplete="off" />
              </label>
              <label>
                <span>{$t('passwords.fieldUsername')}</span>
                <input bind:value={fUsername} autocomplete="off" />
              </label>
              <label>
                <span>{$t('passwords.fieldPassword')}</span>
                <input
                  type="password"
                  bind:value={fPassword}
                  autocomplete="new-password"
                  placeholder={editId ? $t('passwords.passwordKept') : ''}
                />
              </label>
              <div class="form-actions">
                <button type="button" class="btn-secondary" on:click={cancelEdit}>{$t('common.cancel')}</button>
                <button type="submit" class="btn-primary" disabled={saving}>{#if saving}<BusySpinner />{/if} {$t('passwords.save')}</button>
              </div>
            </form>
          {:else}
            <button class="btn-secondary" data-action="add" disabled={!keyringAvailable} on:click={startNew}>{$t('passwords.addEntry')}</button>
          {/if}
        </section>

        <section data-section="keepassxc">
          <h3>{$t('passwords.source.keepassxc')}</h3>
          {#if keepass && !keepass.available}
            <p class="hint warn" data-not-installed>{$t('passwords.notInstalled', { tool: 'keepassxc-cli' })}</p>
          {/if}
          <label class="path-field">
            <span>{$t('passwords.keepassDatabase')}</span>
            <span class="path-row">
              <input bind:value={keepassDb} on:change={saveKeePass} placeholder="~/passwords.kdbx" />
              <button type="button" class="btn-secondary" data-action="browse-db" on:click|preventDefault={() => browse('db')}>{$t('newSession.browse')}</button>
            </span>
          </label>
          <label class="path-field">
            <span>{$t('passwords.keepassKeyFile')}</span>
            <span class="path-row">
              <input bind:value={keepassKey} on:change={saveKeePass} />
              <button type="button" class="btn-secondary" data-action="browse-key" on:click|preventDefault={() => browse('key')}>{$t('newSession.browse')}</button>
            </span>
          </label>
        </section>

        <section data-section="bitwarden">
          <h3>{$t('passwords.source.bitwarden')}</h3>
          {#if !bitwardenInstalled}
            <p class="hint warn" data-not-installed>{$t('passwords.notInstalled', { tool: 'bw' })}</p>
          {/if}
          <p class="hint" data-bitwarden-hint>{$t('passwords.bitwardenHint')}</p>
          {#each bitwardenAccounts as account (account.source)}
            <div class="entry-row" data-account={account.name}>
              <span class="entry-main">
                <span class="entry-name">{account.name}</span>
                <span class="entry-sub command">{account.detail || 'bitwarden.com'}</span>
              </span>
              {#if account.available}
                <button class="btn-secondary small" data-action="bitwarden-login" disabled={!sessionId || !!openingLogin} on:click={() => openBitwardenLogin(account)}>{#if openingLogin === account.source}<BusySpinner />{/if} {$t('passwords.bitwardenLogin')}</button>
              {/if}
              <button class="btn-secondary small" data-action="delete-account" on:click={() => { deleteTarget = { source: account.source, id: account.source.slice('bitwarden:'.length), name: account.name || '', username: '', group: '' }; showDelete = true; }}>{$t('passwords.delete')}</button>
            </div>
          {/each}
          {#if addingBitwarden}
            <form class="entry-form" data-bitwarden-form on:submit|preventDefault={saveBitwarden}>
              <label>
                <span>{$t('passwords.fieldName')}</span>
                <input data-field="account-name" bind:value={bName} autocomplete="off" />
              </label>
              <label>
                <span>{$t('passwords.bitwardenServer')}</span>
                <input data-field="account-server" class="command" bind:value={bServer} autocomplete="off" spellcheck="false" placeholder="https://vault.example.com" />
              </label>
              <div class="form-actions">
                <button type="button" class="btn-secondary" on:click={() => { addingBitwarden = false; }}>{$t('common.cancel')}</button>
                <button type="submit" class="btn-primary" disabled={bitwardenSaving}>{#if bitwardenSaving}<BusySpinner />{/if} {$t('passwords.save')}</button>
              </div>
            </form>
          {:else if bitwardenInstalled}
            <button class="btn-secondary" data-action="add-account" on:click={startBitwarden}>{$t('passwords.bitwardenAdd')}</button>
          {/if}
        </section>

        <section data-section="custom">
          <h3>{$t('passwords.customTitle')}</h3>
          <p class="hint">{$t('passwords.customHint', { placeholder: '{name}' })}</p>
          {#each customSources as source (source.source)}
            <div class="entry-row" data-custom={source.name}>
              <span class="entry-main">
                <span class="entry-name">{source.name}</span>
                <span class="entry-sub command">{source.listCommand}</span>
                {#if !source.available}
                  <span class="entry-sub warn" data-not-installed>{$t('passwords.notInstalled', { tool: source.tool || '' })}</span>
                {:else if source.error}
                  <span class="entry-sub warn" data-failed>{describeBackendError(source.error)}</span>
                {/if}
              </span>
              <button class="btn-secondary small" data-action="edit-custom" on:click={() => startCustom(source)}>{$t('passwords.edit')}</button>
              <button class="btn-secondary small" data-action="delete-custom" on:click={() => { deleteTarget = { source: source.source, id: source.source.slice('custom:'.length), name: source.name || '', username: '', group: '' }; showDelete = true; }}>{$t('passwords.delete')}</button>
            </div>
          {/each}

          {#if customEditing}
            <form class="entry-form" data-custom-form on:submit|preventDefault={saveCustom}>
              {#if !customId}
                <div class="presets">
                  {#each CUSTOM_SOURCE_PRESETS as preset (preset.name)}
                    <button type="button" class="btn-secondary small" data-preset={preset.name} on:click={() => applyPreset(preset)}>{preset.name}</button>
                  {/each}
                </div>
              {/if}
              <label>
                <span>{$t('passwords.fieldName')}</span>
                <input data-field="name" bind:value={cName} autocomplete="off" />
              </label>
              <label>
                <span>{$t('passwords.customList')}</span>
                <input data-field="list" class="command" bind:value={cList} autocomplete="off" spellcheck="false" />
              </label>
              <label>
                <span>{$t('passwords.customGet')}</span>
                <input data-field="get" class="command" bind:value={cGet} autocomplete="off" spellcheck="false" />
              </label>
              <div class="form-actions">
                <button type="button" class="btn-secondary" on:click={() => { customEditing = false; }}>{$t('common.cancel')}</button>
                <button type="submit" class="btn-primary" disabled={customSaving}>{#if customSaving}<BusySpinner />{/if} {$t('passwords.save')}</button>
              </div>
            </form>
          {:else}
            <button class="btn-secondary" data-action="add-custom" on:click={() => startCustom(null)}>{$t('passwords.customAdd')}</button>
          {/if}
        </section>
      </div>
      <div class="dialog-footer">
        <button class="btn-secondary" on:click={close}>{$t('common.close')}</button>
      </div>
    </div>
  </div>

  <ConfirmDialog
    bind:show={showDelete}
    title={$t('passwords.delete')}
    message={$t('passwords.deleteConfirm', { name: deleteTarget?.name ?? '' })}
    confirmText={$t('passwords.delete')}
    variant="danger"
    on:confirm={confirmDelete}
    on:cancel={() => { deleteTarget = null; }}
  />
{/if}

<style>
  .busy { cursor: wait; }
  .title-busy { margin-left: 8px; margin-right: auto; display: inline-flex; }
  .manager {
    width: min(600px, 94vw);
    max-width: min(600px, 94vw);
    max-height: 84vh;
    display: flex;
    flex-direction: column;
  }
  .manager-body {
    flex: 1;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 18px;
  }
  section { display: flex; flex-direction: column; gap: 8px; align-items: stretch; }
  section > .btn-secondary { align-self: flex-start; }
  h3 { margin: 0; font-size: 13px; font-weight: 600; color: #e4e4e7; }
  .hint { margin: 0; font-size: 12px; color: #9ca3af; line-height: 1.4; }
  .hint.warn { color: #fbbf24; }
  .error-line { color: #fb7185; font-size: 13px; }
  .entry-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    border-radius: 6px;
    background: rgba(255, 255, 255, 0.03);
  }
  .entry-main { display: flex; flex-direction: column; flex: 1; min-width: 0; }
  .entry-name { font-size: 13px; color: #e4e4e7; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .entry-sub { font-size: 11px; color: #9ca3af; }
  .small { padding: 3px 8px; font-size: 12px; }
  .entry-form { display: flex; flex-direction: column; gap: 8px; padding: 10px; border-radius: 8px; background: rgba(255, 255, 255, 0.03); }
  .entry-form label, .path-field { display: flex; flex-direction: column; gap: 3px; font-size: 12px; color: #9ca3af; }
  input:not([type='checkbox']) {
    width: 100%;
    box-sizing: border-box;
    padding: 6px 9px;
    font-size: 13px;
    font-family: inherit;
    color: #e4e4e7;
    background: rgba(0, 0, 0, 0.25);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 6px;
  }
  input:focus { outline: none; border-color: rgba(var(--accent-rgb), 0.6); }
  .form-actions { display: flex; justify-content: flex-end; gap: 8px; }
  .path-row { display: flex; gap: 6px; }
  .path-row input { flex: 1; min-width: 0; }
  .presets { display: flex; flex-wrap: wrap; gap: 6px; }
  .command { font-family: var(--font-mono, monospace); }
  .entry-sub.command { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .entry-sub.warn { color: #fbbf24; }
</style>
