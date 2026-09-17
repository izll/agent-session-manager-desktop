<script lang="ts">
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import * as App from '../../../../wailsjs/go/main/App';
  import type { main, remote } from '../../../../wailsjs/go/models';
  import Select from '../common/Select.svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import { t } from '../../i18n';
  import { autoFocusDialog } from '../../utils/dialogActions';

  export let show = false;

  type Server = main.ServerInfo;

  let servers: Server[] = [];
  let loading = false;
  let error = '';
  let loadGeneration = 0;
  let operationGeneration = 0;
  let saving = false;
  let keyringAvailable = true;

  // Editor state. `editingId === ''` means "new server".
  let editing = false;
  let editingId = '';
  let fName = '';
  let fHost = '';
  let fPort: number | null = null;
  let fUser = '';
  let fAuthMethod = 'agent';
  let fKeyPath = '';
  let fJumpHostId = '';
  let fExtraPath = '';
  let fIsDefault = false;
  let fPassword = '';
  // Whether the entry being edited already has a password stored. Drives the
  // "saved" placeholder: an empty password field means "leave it alone", not
  // "clear it", and the user has to be able to tell those apart.
  let hasStoredPassword = false;

  let showDelete = false;
  let deleteTarget: Server | null = null;

  // Connection test state. The result is a list of steps rather than a verdict:
  // reaching the machine, proving who we are and finding tmux there are three
  // different problems with three different fixes.
  let testing = false;
  let testResult: main.ConnectionTestResult | null = null;
  let testedId = '';
  // Asked for only when the attempt needs them, and kept out of the saved
  // entry: a passphrase is never stored, and a password typed here is only
  // stored when the user saves the server with it.
  let askPassword = false;
  let attemptPassword = '';
  let attemptPassphrase = '';

  // Installing tmux on the server. Offered when the test finds none, because
  // without a multiplexer nothing here works at all — but shown as a plan
  // first: this installs software on someone's machine, and the exact command
  // belongs in front of them before they agree to it.
  let installPlan: remote.MultiplexerPlan | null = null;
  let installing = false;
  let installedVersion = '';

  let lastShow = false;
  $: {
    if (show && !lastShow) void load();
    if (!show && lastShow) {
      operationGeneration++;
      loadGeneration++;
      resetAll();
    }
    lastShow = show;
  }

  async function load() {
    const generation = ++loadGeneration;
    loading = true;
    error = '';
    try {
      const [list, keyring] = await Promise.all([
        App.GetServers(),
        App.KeyringAvailable(),
      ]);
      if (!show || generation !== loadGeneration) return;
      servers = (list || []) as Server[];
      keyringAvailable = keyring;
    } catch (e) {
      if (!show || generation !== loadGeneration) return;
      error = String(e);
    } finally {
      if (generation === loadGeneration) loading = false;
    }
  }

  function resetAll() {
    editing = false;
    error = '';
    showDelete = false;
    deleteTarget = null;
  }

  function close() {
    show = false;
  }

  function startNew() {
    editing = true;
    editingId = '';
    fName = '';
    fHost = '';
    fPort = null;
    fUser = '';
    fAuthMethod = 'agent';
    fKeyPath = '';
    fJumpHostId = '';
    fExtraPath = '';
    fIsDefault = servers.length === 0;
    fPassword = '';
    hasStoredPassword = false;
  }

  function startEdit(srv: Server) {
    editing = true;
    editingId = srv.id;
    fName = srv.name || '';
    fHost = srv.host || '';
    fPort = srv.port || null;
    fUser = srv.user || '';
    fAuthMethod = srv.authMethod || 'agent';
    fKeyPath = srv.keyPath || '';
    fJumpHostId = srv.jumpHostId || '';
    fExtraPath = srv.extraPath || '';
    fIsDefault = !!srv.isDefault;
    fPassword = '';
    hasStoredPassword = !!srv.hasPassword;
  }

  async function save() {
    if (saving) return;
    if (!fHost.trim() || !fUser.trim()) {
      error = $t('servers.hostAndUserRequired');
      return;
    }
    const generation = ++operationGeneration;
    saving = true;
    error = '';
    try {
      const updated = await App.SaveServer({
        id: editingId,
        name: fName.trim(),
        host: fHost.trim(),
        port: fPort || 0,
        user: fUser.trim(),
        authMethod: fAuthMethod,
        keyPath: fKeyPath.trim(),
        jumpHostId: fJumpHostId,
        extraPath: fExtraPath.trim(),
        isDefault: fIsDefault,
        password: fPassword,
      } as main.ServerSaveRequest);
      if (!show || generation !== operationGeneration) return;
      servers = (updated || []) as Server[];
      editing = false;
      fPassword = '';
    } catch (e) {
      if (!show || generation !== operationGeneration) return;
      error = String(e);
    } finally {
      if (generation === operationGeneration) saving = false;
    }
  }

  async function runTest(srv: Server) {
    if (testing) return;
    const generation = ++operationGeneration;
    testing = true;
    testedId = srv.id;
    testResult = null;
    error = '';
    try {
      const result = await App.TestServerConnection(srv.id, attemptPassword, attemptPassphrase);
      if (!show || generation !== operationGeneration) return;
      testResult = result;
      askPassword = !!result?.needsPassword || !!result?.needsPassphrase;
    } catch (e) {
      if (!show || generation !== operationGeneration) return;
      error = String(e);
    } finally {
      if (generation === operationGeneration) testing = false;
    }
  }

  // Accepting a key is a separate call that carries the fingerprint the user
  // was shown — a key that changed between the test and the click must not be
  // accepted on the strength of the earlier prompt.
  async function acceptHostKey(srv: Server) {
    if (!testResult?.hostKey) return;
    const generation = ++operationGeneration;
    try {
      await App.AcceptServerHostKey(srv.id, testResult.hostKey);
      if (!show || generation !== operationGeneration) return;
      await load();
      const refreshed = servers.find(s => s.id === srv.id);
      if (refreshed) await runTest(refreshed);
    } catch (e) {
      if (!show || generation !== operationGeneration) return;
      error = String(e);
    }
  }

  // The translator is passed in rather than read inside: called from markup, a
  // helper that reaches for a store on its own gives Svelte nothing to watch,
  // and the step names would keep the language they were first rendered in.
  async function planInstall(srv: Server) {
    const generation = ++operationGeneration;
    error = '';
    try {
      const plan = await App.PlanServerMultiplexerInstall(srv.id, attemptPassword, attemptPassphrase);
      if (!show || generation !== operationGeneration) return;
      installPlan = plan;
    } catch (e) {
      if (!show || generation !== operationGeneration) return;
      error = String(e);
    }
  }

  async function runInstall(srv: Server) {
    if (!installPlan || installing) return;
    const generation = ++operationGeneration;
    installing = true;
    error = '';
    try {
      const version = await App.InstallServerMultiplexer(
        srv.id, attemptPassword, attemptPassphrase, installPlan);
      if (!show || generation !== operationGeneration) return;
      installedVersion = version;
      installPlan = null;
      await runTest(srv);
    } catch (e) {
      if (!show || generation !== operationGeneration) return;
      error = String(e);
    } finally {
      if (generation === operationGeneration) installing = false;
    }
  }

  // Whether the test found the server unusable for want of tmux.
  $: multiplexerMissing = (testResult?.steps || []).some(
    step => step.name === 'multiplexer' && step.status === 'failed');

  function stepLabel(translate: (key: string) => string, name: string): string {
    return translate('servers.step.' + name);
  }

  function askDelete(srv: Server) {
    deleteTarget = srv;
    showDelete = true;
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    const generation = ++operationGeneration;
    const id = deleteTarget.id;
    showDelete = false;
    deleteTarget = null;
    error = '';
    try {
      const updated = await App.DeleteServer(id);
      if (!show || generation !== operationGeneration) return;
      servers = (updated || []) as Server[];
      if (editingId === id) editing = false;
    } catch (e) {
      if (!show || generation !== operationGeneration) return;
      error = String(e);
    }
  }

  async function move(srv: Server, direction: number) {
    const from = servers.findIndex(s => s.id === srv.id);
    const to = from + direction;
    if (from < 0 || to < 0 || to >= servers.length) return;

    const reordered = [...servers];
    const [moved] = reordered.splice(from, 1);
    reordered.splice(to, 0, moved);
    servers = reordered;

    const generation = ++operationGeneration;
    try {
      const updated = await App.ReorderServers(reordered.map(s => s.id));
      if (!show || generation !== operationGeneration) return;
      servers = (updated || []) as Server[];
    } catch (e) {
      if (!show || generation !== operationGeneration) return;
      error = String(e);
      void load();
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      claimKeyForDialog();
      e.stopPropagation();
      if (showDelete) {
        showDelete = false;
        deleteTarget = null;
      } else if (editing) {
        editing = false;
      } else {
        close();
      }
    }
  }

  function focusInput(node: HTMLInputElement) {
    node.focus();
    node.select();
  }

  // A server cannot jump through itself, and the backend rejects loops — but
  // offering the choice and then refusing the save is worse than not offering
  // it, so the entry being edited is left out of its own list.
  $: jumpOptions = [
    { value: '', label: $t('servers.noJumpHost') },
    ...servers
      .filter(s => s.id !== editingId)
      .map(s => ({ value: s.id, label: s.displayName })),
  ];

  $: authOptions = [
    { value: 'agent', label: $t('servers.authAgent') },
    { value: 'key', label: $t('servers.authKey') },
    { value: 'password', label: $t('servers.authPassword') },
  ];
</script>

{#if show}
  <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
  <div
    class="dialog-overlay manager-overlay"
    use:autoFocusDialog
    tabindex="-1"
    role="dialog"
    aria-modal="true"
    on:keydown={handleKeydown}
  >
    <div class="dialog-content manager">
      <div class="dialog-header">
        <h2>{$t('servers.managerTitle')}</h2>
        <button class="close-btn" on:click={close}>×</button>
      </div>

      <div class="dialog-body">
        {#if error}<div class="error-line">{error}</div>{/if}

        {#if editing}
          <div class="form">
            <h3 class="form-title">
              {editingId ? $t('servers.editServer') : $t('servers.newServer')}
            </h3>

            <label class="field">
              <span class="field-label">{$t('servers.fieldName')}</span>
              <input use:focusInput bind:value={fName} placeholder={$t('servers.namePlaceholder')} />
            </label>

            <div class="field-row">
              <label class="field grow">
                <span class="field-label">{$t('servers.fieldHost')}</span>
                <input bind:value={fHost} placeholder="192.168.1.10" />
              </label>
              <label class="field port">
                <span class="field-label">{$t('servers.fieldPort')}</span>
                <input type="number" bind:value={fPort} placeholder="22" min="1" max="65535" />
              </label>
            </div>

            <label class="field">
              <span class="field-label">{$t('servers.fieldUser')}</span>
              <input bind:value={fUser} placeholder="root" />
            </label>

            <label class="field">
              <span class="field-label">{$t('servers.fieldAuth')}</span>
              <Select bind:value={fAuthMethod} options={authOptions} />
            </label>

            {#if fAuthMethod === 'key'}
              <label class="field">
                <span class="field-label">{$t('servers.fieldKeyPath')}</span>
                <input bind:value={fKeyPath} placeholder="~/.ssh/id_ed25519" />
              </label>
            {/if}

            {#if fAuthMethod === 'password'}
              <label class="field">
                <span class="field-label">{$t('servers.fieldPassword')}</span>
                <input
                  type="password"
                  bind:value={fPassword}
                  placeholder={hasStoredPassword ? $t('servers.passwordStored') : ''}
                />
              </label>
              {#if !keyringAvailable}
                <p class="hint warn">{$t('servers.noKeyringHint')}</p>
              {/if}
            {/if}

            <label class="field">
              <span class="field-label">{$t('servers.fieldJumpHost')}</span>
              <Select bind:value={fJumpHostId} options={jumpOptions} />
            </label>

            <label class="field">
              <span class="field-label">{$t('servers.fieldExtraPath')}</span>
              <input bind:value={fExtraPath} placeholder="~/.local/bin:~/.nvm/versions/node/current/bin" />
              <span class="hint">{$t('servers.extraPathHint')}</span>
            </label>

            <label class="check">
              <input type="checkbox" bind:checked={fIsDefault} />
              <span>{$t('servers.makeDefault')}</span>
            </label>

            <div class="form-actions">
              <button class="btn" on:click={() => (editing = false)}>{$t('common.cancel')}</button>
              <button class="btn primary" on:click={save} disabled={saving}>
                {saving ? $t('common.saving') : $t('common.save')}
              </button>
            </div>
          </div>
        {:else}
          <div class="toolbar">
            <button class="btn primary" on:click={startNew}>{$t('servers.add')}</button>
          </div>

          {#if loading}
            <p class="empty">{$t('common.loading')}</p>
          {:else if servers.length === 0}
            <p class="empty">{$t('servers.empty')}</p>
          {:else}
            <ul class="list">
              {#each servers as srv, index (srv.id)}
                <li class="row">
                  <div class="row-main">
                    <div class="row-name">
                      {srv.displayName}
                      {#if srv.isDefault}<span class="badge">{$t('servers.default')}</span>{/if}
                    </div>
                    <div class="row-detail">
                      {srv.user}@{srv.host}{srv.port && srv.port !== 22 ? ':' + srv.port : ''}
                      · {srv.authMethod === 'agent'
                        ? $t('servers.authAgent')
                        : srv.authMethod === 'key'
                          ? $t('servers.authKey')
                          : $t('servers.authPassword')}
                    </div>
                  </div>
                  <div class="row-actions">
                    <button
                      class="icon-btn"
                      title={$t('servers.test')}
                      disabled={testing}
                      on:click={() => runTest(srv)}>⇄</button>
                    <button
                      class="icon-btn"
                      title={$t('servers.moveUp')}
                      disabled={index === 0}
                      on:click={() => move(srv, -1)}>▲</button>
                    <button
                      class="icon-btn"
                      title={$t('servers.moveDown')}
                      disabled={index === servers.length - 1}
                      on:click={() => move(srv, 1)}>▼</button>
                    <button class="icon-btn" title={$t('common.edit')} on:click={() => startEdit(srv)}>✎</button>
                    <button class="icon-btn danger" title={$t('common.delete')} on:click={() => askDelete(srv)}>×</button>
                  </div>
                </li>
                {#if testedId === srv.id && (testing || testResult)}
                  <li class="result">
                    {#if testing}
                      <span class="result-line pending">{$t('servers.testing')}</span>
                    {:else if testResult}
                      {#each testResult.steps || [] as step}
                        <span class="result-line {step.status}">
                          <span class="mark">
                            {step.status === 'ok' ? '✓' : step.status === 'failed' ? '✗' : '!'}
                          </span>
                          <span class="step-name">{stepLabel($t, step.name)}</span>
                          {#if step.detail}<span class="step-detail">{step.detail}</span>{/if}
                        </span>
                      {/each}

                      {#if multiplexerMissing}
                        <div class="install-offer">
                          {#if installedVersion}
                            <p>{$t('servers.tmuxInstalled').replace('{version}', installedVersion)}</p>
                          {:else if installPlan}
                            {#if installPlan.possible}
                              <p>{$t('servers.tmuxWillRun')}</p>
                              <code>{installPlan.command}</code>
                              <button class="btn small" on:click={() => runInstall(srv)} disabled={installing}>
                                {installing ? $t('servers.tmuxInstalling') : $t('servers.tmuxInstallRun')}
                              </button>
                            {:else}
                              <p>{$t('servers.tmuxCannotInstall')}</p>
                              {#if installPlan.reason}<code>{installPlan.reason}</code>{/if}
                            {/if}
                          {:else}
                            <p>{$t('servers.tmuxMissing')}</p>
                            <button class="btn small" on:click={() => planInstall(srv)}>
                              {$t('servers.tmuxInstallOffer')}
                            </button>
                          {/if}
                        </div>
                      {/if}

                      {#if testResult.hostKeyChanged}
                        <div class="host-key danger">
                          <p>{$t('servers.hostKeyChanged')}</p>
                        </div>
                      {:else if testResult.hostKeyIsNew}
                        <div class="host-key">
                          <p>{$t('servers.hostKeyNew')}</p>
                          <code>{testResult.hostKey}</code>
                          <button class="btn small" on:click={() => acceptHostKey(srv)}>
                            {$t('servers.hostKeyAccept')}
                          </button>
                        </div>
                      {/if}

                      {#if askPassword}
                        <div class="retry">
                          <input
                            type="password"
                            placeholder={testResult.needsPassphrase
                              ? $t('servers.passphrasePrompt')
                              : $t('servers.passwordPrompt')}
                            bind:value={attemptPassword}
                            on:keydown={e => { if (e.key === 'Enter') runTest(srv); }}
                          />
                          <button class="btn small" on:click={() => runTest(srv)}>
                            {$t('servers.test')}
                          </button>
                        </div>
                      {/if}
                    {/if}
                  </li>
                {/if}
              {/each}
            </ul>
          {/if}
        {/if}
      </div>
    </div>
  </div>
{/if}

<ConfirmDialog
  bind:show={showDelete}
  title={$t('servers.deleteTitle')}
  message={deleteTarget ? $t('servers.deleteMessage').replace('{name}', deleteTarget.displayName) : ''}
  on:confirm={confirmDelete}
  on:cancel={() => {
    showDelete = false;
    deleteTarget = null;
  }}
/>

<style>
  .manager {
    width: min(680px, 92vw);
    max-height: 82vh;
    display: flex;
    flex-direction: column;
  }

  .dialog-body {
    overflow-y: auto;
    padding: 14px 18px 18px;
  }

  .toolbar {
    display: flex;
    justify-content: flex-end;
    margin-bottom: 12px;
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 9px 11px;
    border-radius: 7px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.06);
  }

  .row-main {
    flex: 1;
    min-width: 0;
  }

  .row-name {
    display: flex;
    align-items: center;
    gap: 7px;
    font-size: 13px;
    color: #e4e4e7;
  }

  .row-detail {
    font-size: 11px;
    color: #8b8b93;
    margin-top: 2px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .badge {
    font-size: 10px;
    padding: 1px 6px;
    border-radius: 999px;
    background: var(--accent-dark, #3f3f46);
    color: #e4e4e7;
  }

  .row-actions {
    display: flex;
    gap: 3px;
  }

  .icon-btn {
    background: transparent;
    border: 1px solid transparent;
    color: #a1a1aa;
    border-radius: 5px;
    width: 26px;
    height: 26px;
    cursor: pointer;
    font-size: 13px;
  }

  .icon-btn:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.07);
    color: #e4e4e7;
  }

  .icon-btn:disabled {
    opacity: 0.3;
    cursor: default;
  }

  .icon-btn.danger:hover:not(:disabled) {
    color: #f87171;
  }

  .result {
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 3px;
    margin: -3px 0 4px;
    padding: 8px 11px 10px;
    border-radius: 0 0 7px 7px;
    background: rgba(0, 0, 0, 0.22);
    border: 1px solid rgba(255, 255, 255, 0.06);
    border-top: none;
  }

  .result-line {
    display: flex;
    align-items: baseline;
    gap: 7px;
    font-size: 11px;
    color: #a1a1aa;
  }

  .result-line .mark {
    width: 12px;
    flex: none;
    text-align: center;
  }

  .result-line.ok .mark { color: #4ade80; }
  .result-line.failed .mark { color: #f87171; }
  .result-line.attention .mark { color: #fbbf24; }
  .result-line.pending { color: #71717a; font-style: italic; }

  .step-name {
    color: #d4d4d8;
    flex: none;
  }

  .step-detail {
    color: #71717a;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .install-offer {
    margin-top: 7px;
    padding: 8px 10px;
    border-radius: 6px;
    background: rgba(96, 165, 250, 0.08);
    border: 1px solid rgba(96, 165, 250, 0.25);
    display: flex;
    flex-direction: column;
    gap: 6px;
    align-items: flex-start;
  }

  .install-offer p {
    margin: 0;
    font-size: 11px;
    color: #d4d4d8;
  }

  .install-offer code {
    font-size: 11px;
    color: #e4e4e7;
    background: rgba(0, 0, 0, 0.3);
    padding: 4px 7px;
    border-radius: 4px;
    word-break: break-all;
    line-height: 1.5;
  }

  .host-key {
    margin-top: 7px;
    padding: 8px 10px;
    border-radius: 6px;
    background: rgba(251, 191, 36, 0.08);
    border: 1px solid rgba(251, 191, 36, 0.25);
    display: flex;
    flex-direction: column;
    gap: 6px;
    align-items: flex-start;
  }

  .host-key.danger {
    background: rgba(248, 113, 113, 0.1);
    border-color: rgba(248, 113, 113, 0.3);
  }

  .host-key p {
    margin: 0;
    font-size: 11px;
    color: #d4d4d8;
  }

  .host-key code {
    font-size: 11px;
    color: #e4e4e7;
    background: rgba(0, 0, 0, 0.3);
    padding: 3px 6px;
    border-radius: 4px;
    word-break: break-all;
  }

  .retry {
    display: flex;
    gap: 6px;
    margin-top: 7px;
  }

  .retry input {
    flex: 1;
    background: rgba(0, 0, 0, 0.25);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 6px;
    padding: 5px 8px;
    color: #e4e4e7;
    font-size: 12px;
  }

  .btn.small {
    padding: 4px 10px;
    font-size: 11px;
  }

  .form {
    display: flex;
    flex-direction: column;
    gap: 11px;
  }

  .form-title {
    margin: 0 0 2px;
    font-size: 13px;
    color: #e4e4e7;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .field-row {
    display: flex;
    gap: 10px;
  }

  .field.grow {
    flex: 1;
  }

  .field.port {
    width: 96px;
  }

  .field-label {
    font-size: 11px;
    color: #a1a1aa;
  }

  .field input {
    background: rgba(0, 0, 0, 0.25);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 6px;
    padding: 7px 9px;
    color: #e4e4e7;
    font-size: 13px;
  }

  .field input:focus {
    outline: none;
    border-color: var(--accent, #6366f1);
  }

  .hint {
    font-size: 11px;
    color: #71717a;
    margin: 0;
  }

  .hint.warn {
    color: #fbbf24;
  }

  .check {
    display: flex;
    align-items: center;
    gap: 7px;
    font-size: 12px;
    color: #d4d4d8;
  }

  .form-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 4px;
  }

  .empty {
    text-align: center;
    color: #71717a;
    font-size: 12px;
    padding: 26px 0;
  }

  .error-line {
    background: rgba(248, 113, 113, 0.1);
    border: 1px solid rgba(248, 113, 113, 0.3);
    color: #fca5a5;
    border-radius: 6px;
    padding: 8px 10px;
    font-size: 12px;
    margin-bottom: 11px;
  }
</style>
