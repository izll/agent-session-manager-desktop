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

  // Entries read from ~/.ssh/config, offered as a starting point. Loaded when
  // the picker is opened rather than with the dialog: most visits are to edit
  // an existing server, and reading a file nobody asked about is work for
  // nothing.
  let showImport = false;
  let configHosts: main.SSHConfigHostInfo[] = [];
  let loadingImport = false;

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

  // What is running on a server, and whose it is.
  //
  // A server is shared: it can hold sessions from this computer, from another
  // of the user's machines, and ones started by hand. Listed here so a session
  // that this app has lost track of — or that belongs somewhere else — is
  // visible rather than running unseen until the machine is rebooted.
  let sessionsFor = '';
  let serverSessions: main.RemoteSessionInfo[] = [];
  let loadingSessions = false;
  let sessionsError = '';
  let showViews = false;
  // Which row has its multiplexer id shown. One at a time: the id is wanted
  // occasionally and in the way the rest of the time.
  let detailsFor = '';
  // The listing has a generation of its own.
  //
  // operationGeneration is shared by eleven operations in this dialog, and
  // every one of them bumps it — so a connection test, a save or a host-key
  // accept starting while the listing was in flight made its reply look stale
  // and it was thrown away, leaving whatever was on screen before. The
  // listing only needs protection from a NEWER listing.
  let sessionsGeneration = 0;

  async function loadServerSessions(srv: Server) {
    if (sessionsFor === srv.id) {
      // Second press closes it.
      sessionsFor = '';
      serverSessions = [];
      return;
    }
    const generation = ++sessionsGeneration;
    sessionsFor = srv.id;
    serverSessions = [];
    sessionsError = '';
    loadingSessions = true;
    try {
      const list = await App.ListServerSessions(srv.id);
      if (!show || generation !== sessionsGeneration) return;
      serverSessions = (list || []) as main.RemoteSessionInfo[];
    } catch (e) {
      if (!show || generation !== sessionsGeneration) return;
      sessionsError = String(e);
    } finally {
      if (generation === sessionsGeneration) loadingSessions = false;
    }
  }

  // The helper sessions the app creates to show one window are bookkeeping,
  // not work, so they are hidden until asked for.
  $: visibleSessions = showViews
    ? serverSessions
    : serverSessions.filter(s => !s.view);
  $: hiddenViewCount = serverSessions.filter(s => s.view).length;

  // Stopping a session on a server ends whatever is running in it, which is
  // the whole point of it being there — so it is asked about first.
  let killTarget: main.RemoteSessionInfo | null = null;
  let killing = false;
  function askKill(entry: main.RemoteSessionInfo) {
    killTarget = entry;
  }

  async function confirmKill() {
    const entry = killTarget;
    const serverId = sessionsFor;
    if (!entry || !serverId || killing) return;
    const generation = ++operationGeneration;
    killing = true;
    try {
      await App.KillServerSession(serverId, entry.name);
      if (!show || generation !== operationGeneration) return;
      killTarget = null;
      const srv = servers.find(s => s.id === serverId);
      if (srv) {
        // Reopen the list so it shows what is actually left. Its own
        // generation guards it, so this cannot be discarded by the kill.
        sessionsFor = '';
        await loadServerSessions(srv);
      }
    } catch (e) {
      if (!show || generation !== operationGeneration) return;
      sessionsError = String(e);
      killTarget = null;
    } finally {
      if (generation === operationGeneration) killing = false;
    }
  }

  // The translator is passed in rather than read inside: a helper that reads a
  // store gives Svelte nothing to watch, so the text would keep the language
  // it had when the list was first drawn. The project has a test for exactly
  // this mistake.
  function describeOwner(translate: (key: string) => string, entry: main.RemoteSessionInfo): string {
    // A view belongs to the app by construction — the app is the only thing
    // that creates them — so saying "started by hand" about one was simply
    // wrong: they carry no ownership tags because nothing tags them, not
    // because nobody knows where they came from.
    if (entry.view) return translate('servers.sessionView');
    if (entry.ours) return translate('servers.sessionOurs');
    if (entry.thisMachine) return translate('servers.sessionLostTrack');
    if (entry.owner) return translate('servers.sessionOtherMachine').replace('{machine}', entry.owner);
    return translate('servers.sessionUntagged');
  }

  function formatCreated(unixSeconds: number): string {
    if (!unixSeconds) return '';
    return new Date(unixSeconds * 1000).toLocaleString();
  }

  // A step's detail is a key when the backend had something to say in the
  // user's language, and a plain value — a path, a version, a fingerprint —
  // when it did not. Both arrive in the same field.
  //
  // The translator is passed in rather than read inside, or Svelte would have
  // nothing to watch and the text would keep whatever language it was first
  // drawn in.
  function describeStepDetail(translate: (key: string) => string, detail: string): string {
    if (!detail.startsWith('detail.')) return detail;
    const [key, ...values] = detail.split('|');
    const text = translate(key);
    if (text === key) return values.length > 0 ? values.join(' ') : key;
    return values.reduce((message, value, index) =>
      message.split(`{${index}}`).join(value), text);
  }

  // The connection test can find agents in a directory the server's
  // non-interactive shell does not have on its PATH. Applying it here saves
  // the user reopening the editor to retype a path we already know.
  let applyingPath = false;
  async function applySuggestedExtraPath(srv: Server) {
    const suggested = testResult?.suggestedExtraPath;
    if (!suggested || applyingPath) return;
    const generation = ++operationGeneration;
    applyingPath = true;
    try {
      const updated = await App.SaveServer({
        id: srv.id,
        name: srv.name,
        host: srv.host,
        port: srv.port,
        user: srv.user,
        authMethod: srv.authMethod,
        keyPath: srv.keyPath,
        jumpHostId: srv.jumpHostId,
        // The only field this changes. Everything else is carried across
        // unchanged, because SaveServer replaces the whole entry.
        extraPath: suggested,
        isDefault: srv.isDefault,
        password: '',
      } as main.ServerSaveRequest);
      if (!show || generation !== operationGeneration) return;
      servers = (updated || []) as Server[];
      const refreshed = servers.find(s => s.id === srv.id);
      if (refreshed) await runTest(refreshed);
    } catch (e) {
      if (!show || generation !== operationGeneration) return;
      error = String(e);
    } finally {
      if (generation === operationGeneration) applyingPath = false;
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

  async function openImport() {
    showImport = true;
    loadingImport = true;
    error = '';
    const generation = ++operationGeneration;
    try {
      const hosts = await App.GetSSHConfigHosts();
      if (!show || generation !== operationGeneration) return;
      configHosts = hosts || [];
    } catch (e) {
      if (!show || generation !== operationGeneration) return;
      error = String(e);
    } finally {
      if (generation === operationGeneration) loadingImport = false;
    }
  }

  // The entry fills the form rather than saving itself. What lands in the list
  // is what the user looked at and confirmed — an SSH config can hold machines
  // they have no intention of running agents on.
  function useConfigHost(host: main.SSHConfigHostInfo) {
    showImport = false;
    editing = true;
    editingId = '';
    fName = host.alias || host.hostName;
    fHost = host.hostName;
    fPort = host.port || null;
    fUser = host.user;
    // A named key file means that key; without one, ssh-agent is the better
    // guess than a path we would have to invent.
    fAuthMethod = host.keyPath ? 'key' : 'agent';
    fKeyPath = host.keyPath || '';
    fJumpHostId = '';
    fExtraPath = '';
    fIsDefault = servers.length === 0;
    fPassword = '';
    hasStoredPassword = false;
  }

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
              <button class="btn-secondary" on:click={() => (editing = false)}>{$t('common.cancel')}</button>
              <button class="btn-primary" on:click={save} disabled={saving}>
                {saving ? $t('common.saving') : $t('common.save')}
              </button>
            </div>
          </div>
        {:else}
          <div class="toolbar">
            <button class="btn-secondary" on:click={openImport}>{$t('servers.fromSshConfig')}</button>
            <button class="btn-primary" on:click={startNew}>{$t('servers.add')}</button>
          </div>

          {#if showImport}
            <div class="import-panel">
              <div class="import-head">
                <span>{$t('servers.fromSshConfigTitle')}</span>
                <button class="icon-btn" on:click={() => (showImport = false)}>×</button>
              </div>
              {#if loadingImport}
                <p class="empty">{$t('common.loading')}</p>
              {:else if configHosts.length === 0}
                <p class="empty">{$t('servers.noSshConfig')}</p>
              {:else}
                <ul class="list">
                  {#each configHosts as host (host.alias)}
                    <li class="row">
                      <div class="row-main">
                        <div class="row-name">{host.alias}</div>
                        <div class="row-detail">
                          {host.user}@{host.hostName}{host.port && host.port !== 22 ? ':' + host.port : ''}
                          {#if host.keyPath} · {host.keyPath}{/if}
                        </div>
                      </div>
                      {#if host.alreadyAdded}
                        <span class="already">{$t('servers.alreadyAdded')}</span>
                      {:else}
                        <button class="btn-secondary small" on:click={() => useConfigHost(host)}>
                          {$t('servers.useThis')}
                        </button>
                      {/if}
                    </li>
                  {/each}
                </ul>
              {/if}
            </div>
          {:else if loading}
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
                    <!-- Drawn rather than typed: the arrows and the exchange
                         mark exist as characters, but they come from whatever
                         font has them and sit at whatever size and baseline
                         that font chose — beside the app's own icons they read
                         as something pasted in. -->
                    <button
                      class="icon-btn"
                      class:active={sessionsFor === srv.id}
                      title={$t('servers.listSessions')}
                      on:click={() => loadServerSessions(srv)}
                    >
                      <!-- Stacked layers: what is running on the machine. -->
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                           stroke-linecap="round" stroke-linejoin="round">
                        <rect x="3" y="4" width="18" height="6" rx="1"/>
                        <rect x="3" y="14" width="18" height="6" rx="1"/>
                        <path d="M7 7h.01M7 17h.01"/>
                      </svg>
                    </button>
                    <button
                      class="icon-btn"
                      title={$t('servers.test')}
                      disabled={testing}
                      on:click={() => runTest(srv)}
                    >
                      <!-- Two arrows passing: a round trip to the machine. -->
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                           stroke-linecap="round" stroke-linejoin="round">
                        <path d="M4 8h13l-3-3"/>
                        <path d="M20 16H7l3 3"/>
                      </svg>
                    </button>
                    <button
                      class="icon-btn"
                      title={$t('servers.moveUp')}
                      disabled={index === 0}
                      on:click={() => move(srv, -1)}
                    >
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                           stroke-linecap="round" stroke-linejoin="round">
                        <path d="M6 14l6-6 6 6"/>
                      </svg>
                    </button>
                    <button
                      class="icon-btn"
                      title={$t('servers.moveDown')}
                      disabled={index === servers.length - 1}
                      on:click={() => move(srv, 1)}
                    >
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                           stroke-linecap="round" stroke-linejoin="round">
                        <path d="M6 10l6 6 6-6"/>
                      </svg>
                    </button>
                    <button class="icon-btn" title={$t('common.edit')} on:click={() => startEdit(srv)}>
                      <!-- A pencil. -->
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                           stroke-linecap="round" stroke-linejoin="round">
                        <path d="M12 20h9"/>
                        <path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z"/>
                      </svg>
                    </button>
                    <button class="icon-btn danger" title={$t('common.delete')} on:click={() => askDelete(srv)}>
                      <!-- A bin, not a cross: this removes the entry rather
                           than closing anything. -->
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                           stroke-linecap="round" stroke-linejoin="round">
                        <path d="M3 6h18M8 6V4h8v2M19 6l-1 14H6L5 6"/>
                        <path d="M10 11v6M14 11v6"/>
                      </svg>
                    </button>
                  </div>
                </li>
                {#if sessionsFor === srv.id}
                  <li class="result">
                    {#if loadingSessions}
                      <span class="result-line pending">{$t('common.loading')}</span>
                    {:else if sessionsError}
                      <div class="error-line">{sessionsError}</div>
                    {:else if visibleSessions.length === 0}
                      <span class="result-line pending">{$t('servers.noSessions')}</span>
                    {:else}
                      <ul class="session-list">
                        {#each visibleSessions as entry (entry.name)}
                          <li class="session-row" class:foreign={!entry.ours}>
                            <span class="session-main">
                              <!-- The tab's name leads. Putting the session
                                   first buried it: a session with no project
                                   tag falls back to its raw multiplexer name,
                                   which fills the whole line on its own and
                                   the tab name was never reached. -->
                              <span class="session-name" title={entry.name}>
                                {entry.view && entry.viewOf
                                  ? $t('servers.sessionViewOf').replace('{window}', entry.viewOf)
                                  : (entry.project || entry.name)}
                              </span>
                              <span class="session-owner">
                                {#if entry.view && entry.viewSession}{entry.viewSession} · {/if}{describeOwner($t, entry)}
                              </span>
                            </span>
                            <span class="session-meta">
                              {#if entry.path}<span class="session-path" title={entry.path}>{entry.path}</span>{/if}
                              <span>{$t('servers.sessionWindows').replace('{n}', String(entry.windows))}</span>
                              {#if entry.attached}<span class="session-attached">{$t('servers.sessionAttached')}</span>{/if}
                              {#if entry.created}<span>{formatCreated(entry.created)}</span>{/if}
                            </span>
                            <button
                              class="icon-btn"
                              class:active={detailsFor === entry.name}
                              title={$t('servers.sessionDetails')}
                              on:click={() => (detailsFor = detailsFor === entry.name ? '' : entry.name)}
                            >
                              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                                   stroke-linecap="round" stroke-linejoin="round">
                                <circle cx="12" cy="12" r="10"/>
                                <path d="M12 16v-4M12 8h.01"/>
                              </svg>
                            </button>
                            <button
                              class="icon-btn danger"
                              title={$t('servers.killSession')}
                              on:click={() => askKill(entry)}
                            >
                              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                                   stroke-linecap="round" stroke-linejoin="round">
                                <path d="M18 6L6 18M6 6l12 12"/>
                              </svg>
                            </button>
                          </li>
                          {#if detailsFor === entry.name}
                            <li class="session-details">
                              <code>{entry.name}</code>
                            </li>
                          {/if}
                        {/each}
                      </ul>
                    {/if}
                    {#if hiddenViewCount > 0}
                      <button class="link-btn" on:click={() => (showViews = !showViews)}>
                        {showViews
                          ? $t('servers.hideViewSessions')
                          : $t('servers.showViewSessions').replace('{n}', String(hiddenViewCount))}
                      </button>
                    {/if}
                  </li>
                {/if}
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
                          {#if step.detail}<span class="step-detail">{describeStepDetail($t, step.detail)}</span>{/if}
                        </span>
                      {/each}

                      {#if testResult.suggestedExtraPath}
                        <div class="install-offer">
                          <p>{$t('servers.extraPathOffer')}</p>
                          <code>{testResult.suggestedExtraPath}</code>
                          <button
                            class="btn-secondary small"
                            disabled={applyingPath}
                            on:click={() => applySuggestedExtraPath(srv)}
                          >
                            {applyingPath ? $t('servers.extraPathApplying') : $t('servers.extraPathApply')}
                          </button>
                        </div>
                      {/if}

                      {#if multiplexerMissing}
                        <div class="install-offer">
                          {#if installedVersion}
                            <p>{$t('servers.tmuxInstalled').replace('{version}', installedVersion)}</p>
                          {:else if installPlan}
                            {#if installPlan.possible}
                              <p>{$t('servers.tmuxWillRun')}</p>
                              <code>{installPlan.command}</code>
                              <button class="btn-secondary small" on:click={() => runInstall(srv)} disabled={installing}>
                                {installing ? $t('servers.tmuxInstalling') : $t('servers.tmuxInstallRun')}
                              </button>
                            {:else if installPlan.wouldRemove?.length}
                              <p class="warn">{$t('servers.tmuxWouldRemove')}</p>
                              <code>{installPlan.wouldRemove.join(', ')}</code>
                              <p>{$t('servers.tmuxRemoveRefused')}</p>
                            {:else}
                              <p>{$t('servers.tmuxCannotInstall')}</p>
                              {#if installPlan.reason}<code>{installPlan.reason}</code>{/if}
                            {/if}
                          {:else}
                            <p>{$t('servers.tmuxMissing')}</p>
                            <button class="btn-secondary small" on:click={() => planInstall(srv)}>
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
                          <button class="btn-secondary small" on:click={() => acceptHostKey(srv)}>
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
                          <button class="btn-secondary small" on:click={() => runTest(srv)}>
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

<!-- Stopping a session on a server ends what is running in it, and that work
     is the reason it lives there. Always asked, never assumed. -->
<ConfirmDialog
  show={killTarget !== null}
  title={$t('servers.killSessionTitle')}
  message={killTarget
    ? $t('servers.killSessionMessage')
        .replace('{name}', killTarget.project || killTarget.name)
        .replace('{owner}', describeOwner($t, killTarget))
    : ''}
  on:confirm={confirmKill}
  on:cancel={() => (killTarget = null)}
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
    /* Written when there was one button here; a second one arrived and the two
       sat against each other. */
    gap: 8px;
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
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 0;
  }

  .icon-btn svg {
    width: 14px;
    height: 14px;
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
    /* The connection test is read closely — a fingerprint is compared
       character by character — so it is set at body size rather than the
       11px used for labels elsewhere in the dialog. */
    font-size: 13px;
    line-height: 1.5;
    color: #a1a1aa;
  }

  .result-line .mark {
    width: 14px;
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

  .session-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .session-row {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 7px 8px;
    border-radius: 5px;
    /* Body size, like the connection test's lines: this is a list of things
       running on someone's machine, read to decide what to stop — not a row
       of labels. */
    font-size: 13px;
    line-height: 1.5;
  }

  .session-row:hover {
    background: rgba(255, 255, 255, 0.04);
  }

  /* A session this app does not own is dimmed rather than hidden: it is on
     the machine and using it, and pretending otherwise is how it ends up
     running unseen. */
  .session-row.foreign {
    opacity: 0.75;
  }

  .session-main {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
    flex: 1;
  }

  .session-name {
    color: #e4e4e7;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .session-owner {
    font-size: 12px;
    color: #8b8b93;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .session-meta {
    display: flex;
    align-items: center;
    gap: 12px;
    color: #71717a;
    font-size: 12px;
    flex: none;
  }

  .session-path {
    max-width: 240px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: 'JetBrains Mono', 'Menlo', monospace;
  }

  .session-attached {
    color: #4ade80;
  }

  .session-details {
    padding: 2px 8px 8px 8px;
  }

  .session-details code {
    font-family: 'JetBrains Mono', 'Menlo', monospace;
    font-size: 11px;
    color: #8b8b93;
    word-break: break-all;
  }

  .icon-btn.danger:hover {
    color: #f87171;
  }

  .icon-btn.active {
    color: var(--accent);
  }

  .link-btn {
    align-self: flex-start;
    background: none;
    border: none;
    padding: 6px 0;
    color: #8b8b93;
    font-size: 12px;
    cursor: pointer;
    text-decoration: underline;
  }

  .import-panel {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .import-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    font-size: 12px;
    color: #a1a1aa;
  }

  .already {
    font-size: 11px;
    color: #71717a;
    flex: none;
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

  .install-offer p.warn {
    color: #fbbf24;
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

  /* Matching the command manager, so the two dialogs do not each invent
     their own buttons. */
  .btn-primary,
  .btn-secondary {
    padding: 7px 16px;
    border-radius: 7px;
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
  }

  .btn-secondary {
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(255, 255, 255, 0.05);
    color: #a1a1aa;
  }

  .btn-secondary:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.09);
    color: #e4e4e7;
  }

  .btn-primary {
    border: 1px solid var(--accent);
    background: linear-gradient(135deg, var(--accent-dark), var(--accent));
    color: var(--accent-ink);
  }

  .btn-primary:disabled,
  .btn-secondary:disabled {
    opacity: 0.45;
    cursor: default;
  }

  /* The inline actions inside a test result sit next to text rather than in a
     button row, so they are smaller. */
  .btn-secondary.small {
    padding: 4px 10px;
    font-size: 11px;
    font-weight: 500;
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
