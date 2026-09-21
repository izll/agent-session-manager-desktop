<script lang="ts">
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import { BrowserOpenURL } from '../../../../wailsjs/runtime/runtime';
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { createEventDispatcher, onDestroy } from 'svelte';
  import { agents, loadAgents, loadAgentsForServer, type Agent } from '../../stores/agents';
  import { sessions, groups, selectedSession, createSession, startSession, assignToGroup } from '../../stores/sessions';
  import { get } from 'svelte/store';
  import { activeProjectId } from '../../stores/projects';
  import AgentIcon from '../common/AgentIcon.svelte';
  import Select from '../common/Select.svelte';
  import RemoteDirPicker from './RemoteDirPicker.svelte';
  import * as App from '../../../../wailsjs/go/main/App';
  import type { main } from '../../../../wailsjs/go/models';
  import { t } from '../../i18n';

  export let show = false;

  // The agent the user has picked, and whether its command is on PATH. The
  // backend answers this with the list, so the dialog can say so before
  // anything is filled in rather than failing at the end.
  $: chosenAgent = availableAgents.find((a) => a.type === selectedAgent);
  $: agentMissing = !!chosenAgent && chosenAgent.installed === false;

  const dispatch = createEventDispatcher();

  let name = '';
  let path = '';
  let selectedAgent = 'claude';
  let autoYes = false;

  // A checkout of its own, on its own branch, so this session's agent does not
  // edit the same files as another working the same project.
  //
  // Off by default: a session working directly in the project is what every
  // session did before, and is still what most people want.
  let useWorktree = false;
  // Only offered where it can work: a directory outside a repository has
  // nothing to make a worktree from, and a session on a server would need the
  // checkout made there, which this does not do yet.
  let pathIsRepo = false;
  let repoCheckedFor = '';
  $: canUseWorktree = pathIsRepo && !serverId;

  // Where the worktree would actually land.
  //
  // Shown rather than described, because the session's name is transformed on
  // the way: git will not take accents or punctuation in a branch name, so
  // "hibajavítás" becomes "hibajav-t-s". Nobody would guess that, and the
  // directory is on their disk afterwards.
  let plannedWorktree: main.PlannedWorktree | null = null;
  let planGeneration = 0;

  $: void refreshWorktreePlan(useWorktree && canUseWorktree ? path.trim() : '', name.trim());

  async function refreshWorktreePlan(forPath: string, forName: string) {
    if (!forPath) {
      plannedWorktree = null;
      return;
    }
    const generation = ++planGeneration;
    try {
      const plan = await App.PlanWorktreeFor(forPath, forName);
      if (generation !== planGeneration) return;
      plannedWorktree = plan?.dir ? plan : null;
    } catch {
      if (generation !== planGeneration) return;
      plannedWorktree = null;
    }
  }

  $: if (path.trim() !== repoCheckedFor) {
    repoCheckedFor = path.trim();
    useWorktree = false;
    void checkPathIsRepo(repoCheckedFor);
  }

  async function checkPathIsRepo(candidate: string) {
    if (!candidate) {
      pathIsRepo = false;
      return;
    }
    try {
      const root = await App.RepositoryRootOf(candidate);
      if (candidate !== repoCheckedFor) return;
      pathIsRepo = !!root;
    } catch {
      if (candidate !== repoCheckedFor) return;
      pathIsRepo = false;
    }
  }
  let autoStart = true;
  let isSubmitting = false;
  let error = '';
  let selectedGroupId = '';
  // Which machine the session runs on. Empty is this computer — what every
  // session was before remote support, and what most still are.
  let serverId = '';
  let servers: main.ServerInfo[] = [];
  // The native folder picker opens on this computer, which is the wrong
  // machine once a server is chosen.
  let showRemotePicker = false;
  let groupInitialized = false;
  let extraArgs = '';
  let operationGeneration = 0;
  let previousShow = false;
  let dialogProjectId = '';
  let hasDialogProject = false;

  // This dialog is mounted once and may be closed/reopened while a native
  // directory picker or backend mutation is still pending. Treat every open
  // cycle as a distinct operation target so a completion from the old form
  // cannot write into or close its replacement.
  $: {
    if (show !== previousShow) {
      previousShow = show;
      operationGeneration++;
      if (!show) isSubmitting = false;
    }
  }

  // Groups and session ids are project-scoped. A form opened in A must not
  // finish its create -> assign -> start sequence in B after a project switch.
  $: if (show) {
    if (!hasDialogProject) {
      hasDialogProject = true;
      dialogProjectId = $activeProjectId;
    } else if (dialogProjectId !== $activeProjectId) {
      close();
    }
  } else if (hasDialogProject) {
    hasDialogProject = false;
    dialogProjectId = '';
  }

  // Resume session selection
  interface ResumeSession {
    id: string;
    displayName: string;
    timestamp: string;
  }
  let availableSessions: ResumeSession[] = [];
  let selectedResumeId: string = '';
  let isLoadingSessions = false;
  let showForkWarning = false;
  let conflictingSessionName = '';

  $: if (show && $agents.length === 0) {
    loadAgents();
  }

  // The server list is loaded each time the dialog opens rather than once:
  // a server can be added while the app is running, and an empty picker after
  // adding one reads as the feature not working.
  let serversLoadedFor = false;
  $: if (show && !serversLoadedFor) {
    serversLoadedFor = true;
    void loadServers();
  }
  $: if (!show) {
    serversLoadedFor = false;
  }

  async function loadServers() {
    try {
      servers = (await App.GetServers()) || [];
      // Preselect the default, so someone who works mainly on a server does
      // not choose it every time.
      if (!serverId) {
        serverId = servers.find(s => s.isDefault)?.id || '';
      }
    } catch {
      // A server list that cannot be read leaves the picker local-only, which
      // is the behaviour without any servers at all — not worth an error in
      // front of someone creating a local session.
      servers = [];
    }
  }

  // Which agents are available where this session will run. The store holds
  // this computer's answer, which is the wrong machine for a remote session:
  // an agent installed here but not there would be offered and then fail to
  // start, and one installed there but not here would be marked missing with
  // an offer to install it locally, where it would do nothing.
  let serverAgents: Agent[] = [];
  let agentsForGeneration = 0;
  $: void refreshAgentsFor(serverId);

  async function refreshAgentsFor(id: string) {
    if (!id) {
      serverAgents = [];
      return;
    }
    const generation = ++agentsForGeneration;
    const list = await loadAgentsForServer(id);
    if (generation !== agentsForGeneration) return;
    serverAgents = list;
  }

  // The list the picker shows: the server's when one is chosen, this
  // computer's otherwise.
  $: availableAgents = serverId ? serverAgents : $agents;

  $: groupOptions = [
    { value: '', label: $t('newSession.noGroup') },
    ...$groups.map(group => ({ value: group.id, label: group.name })),
  ];

  $: serverOptions = [
    { value: '', label: $t('servers.thisComputer') },
    ...servers.map(s => ({ value: s.id, label: s.displayName })),
  ];

  // Set default group from selected session ONLY when dialog first opens
  $: if (show && !groupInitialized) {
    selectedGroupId = $selectedSession?.groupId || '';
    groupInitialized = true;
  }
  $: if (!show) {
    groupInitialized = false;
  }

  // Auto-fill name from path (only if user hasn't manually edited the name field)
  //
  // Splits on both separators: a Windows path has no forward slashes at all, so
  // splitting on "/" alone left the whole of
  // "C:\Users\User\Documents\asmgr-teszt" sitting in the name field.
  let userTouchedName = false;
  $: if (path && !userTouchedName) {
    const parts = path.replace(/[/\\]+$/, '').split(/[/\\]/);
    const folderName = parts[parts.length - 1] || '';
    if (folderName) {
      name = folderName;
    }
  }

  function handleNameInput() {
    userTouchedName = true;
  }

  // Debounced path change handler
  let pathDebounceTimer: ReturnType<typeof setTimeout> | null = null;
  let resumeLookupGeneration = 0;
  let scheduledResumeKey = '';
  $: {
    const key = show ? `${path}\u0000${selectedAgent}` : '';
    if (key !== scheduledResumeKey) {
      scheduledResumeKey = key;
      const generation = ++resumeLookupGeneration;
      if (pathDebounceTimer) { clearTimeout(pathDebounceTimer); pathDebounceTimer = null; }
      isLoadingSessions = false;
      availableSessions = [];
      selectedResumeId = '';
      if (show && path.trim() && selectedAgent) {
        const searchPath = path;
        const agent = selectedAgent;
        pathDebounceTimer = setTimeout(() => {
          pathDebounceTimer = null;
          void loadAvailableSessions(searchPath, agent, generation);
        }, 500);
      }
    }
  }

  async function loadAvailableSessions(searchPath: string, agent: string, generation: number) {
    if (!searchPath.trim()) {
      availableSessions = [];
      return;
    }

    // Check if agent supports resume
    const agentConfig = availableAgents.find(a => a.type === agent);
    if (!agentConfig?.supportsResume) {
      availableSessions = [];
      return;
    }

    isLoadingSessions = true;
    try {
      const result = await App.GetResumeSessions(agent, searchPath.trim());
      if (!show || generation !== resumeLookupGeneration || path !== searchPath || selectedAgent !== agent) return;
      availableSessions = result || [];
    } catch (e) {
      if (!show || generation !== resumeLookupGeneration || path !== searchPath || selectedAgent !== agent) return;
      console.error('Failed to load sessions:', e);
      availableSessions = [];
    } finally {
      if (show && generation === resumeLookupGeneration && path === searchPath && selectedAgent === agent) {
        isLoadingSessions = false;
      }
    }
  }

  onDestroy(() => {
    resumeLookupGeneration++;
    if (pathDebounceTimer) clearTimeout(pathDebounceTimer);
  });

  function checkSessionConflict(resumeId: string): string | null {
    if (!resumeId) return null;

    const existingSessions = get(sessions);
    for (const sess of existingSessions) {
      // Check main session
      if (sess.resumeSessionId === resumeId) {
        return sess.name;
      }
      // Check followed windows (tabs).
      //
      // resume_session_id, not resumeSessionId: the session is renamed for the
      // UI, its tabs are the stored structure passed through unchanged. Read as
      // camelCase this never matched, so the warning that a conversation is
      // already open somewhere never fired for a tab — and opening it twice is
      // exactly what the warning exists to prevent.
      if (sess.followedWindows) {
        for (const fw of sess.followedWindows) {
          if (fw.resume_session_id === resumeId) {
            return `${sess.name} (tab)`;
          }
        }
      }
    }
    return null;
  }

  function handleResumeSelect(resumeId: string) {
    const conflict = checkSessionConflict(resumeId);
    if (conflict) {
      conflictingSessionName = conflict;
      showForkWarning = true;
      selectedResumeId = resumeId;
    } else {
      selectedResumeId = resumeId;
      showForkWarning = false;
    }
  }

  function close() {
    operationGeneration++;
    isSubmitting = false;
    show = false;
    resetForm();
    dispatch('close');
  }

  function resetForm() {
    name = '';
    path = '';
    selectedAgent = 'claude';
    autoYes = false;
    useWorktree = false;
    autoStart = true;
    error = '';
    userTouchedName = false;
    selectedGroupId = '';
    extraArgs = '';
    availableSessions = [];
    selectedResumeId = '';
    showForkWarning = false;
    conflictingSessionName = '';
  }

  async function handleSubmit(fork: boolean = false) {
    if (isSubmitting) return;
    if (!name.trim() || !path.trim()) {
      error = $t('newSession.nameRequired');
      return;
    }

    // If there's a conflict and user hasn't confirmed fork, show warning
    if (selectedResumeId && !fork) {
      const conflict = checkSessionConflict(selectedResumeId);
      if (conflict) {
        conflictingSessionName = conflict;
        showForkWarning = true;
        return;
      }
    }

    const generation = operationGeneration;
    const sessionName = name.trim();
    const sessionPath = path.trim();
    const agent = selectedAgent;
    const shouldAutoStart = autoStart;
    const resumeId = selectedResumeId;
    const automaticYes = autoYes;
    const args = extraArgs.trim();
    const targetProjectId = $activeProjectId;
    isSubmitting = true;
    error = '';

    // Save group selection before createSession changes selectedSessionId,
    // which triggers the reactive statement and resets selectedGroupId to ''
    const groupId = selectedGroupId;

    try {
      const session = await createSession(sessionName, sessionPath, agent, automaticYes, args, serverId,
        canUseWorktree && useWorktree);
      if (targetProjectId !== $activeProjectId) return;
      if (session) {
        if (groupId) {
          await assignToGroup(session.id, groupId);
          if (targetProjectId !== $activeProjectId) return;
        }
        // If resuming, start with resume ID
        if (resumeId && shouldAutoStart) {
          await startSession(session.id, resumeId);
        } else if (shouldAutoStart) {
          await startSession(session.id);
        }
      }
      if (!show || generation !== operationGeneration) return;
      close();
    } catch (e) {
      if (!show || generation !== operationGeneration) return;
      error = String(e);
    } finally {
      if (generation === operationGeneration) isSubmitting = false;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      claimKeyForDialog();
      e.stopPropagation();
      if (showForkWarning) {
        showForkWarning = false;
      } else {
        close();
      }
    }
  }

  // Hands over to the template manager rather than embedding it: the dialogs
  // overlap, and two open at once would stack their overlays.
  function openTemplates() {
    close();
    window.dispatchEvent(new CustomEvent('command:templates', { detail: { templateId: '' } }));
  }

  async function browsePath() {
    const generation = operationGeneration;
    const initialPath = path;
    try {
      if (serverId) {
        // A path on the server cannot be chosen with a picker that browses
        // this computer's disk.
        showRemotePicker = true;
        return;
      }
      const selectedPath = await App.BrowseDirectory(initialPath || '');
      if (selectedPath && show && generation === operationGeneration && path === initialPath) {
        path = selectedPath;
      }
    } catch (e) {
      console.error('Browse failed:', e);
    }
  }
</script>

{#if show}
  <div
    class="dialog-overlay" use:autoFocusDialog
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2>{$t('newSession.title')}</h2>
        <!-- Someone creating a session is exactly who wants a template; the
             sidebar's icon button alone would be easy to miss from here. -->
        <button class="template-link" on:click={openTemplates}>{$t('newSession.fromTemplate')}</button>
        <button class="close-btn" on:click={close}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/>
            <line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>

      {#if error}
        <div class="error-message">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="12" cy="12" r="10"/>
            <line x1="12" y1="8" x2="12" y2="12"/>
            <line x1="12" y1="16" x2="12.01" y2="16"/>
          </svg>
          {error}
        </div>
      {/if}

      <!-- Shown before anything is submitted, not only after the failure: the
           backend already knows the command is missing. The install page is
           opened in the real browser rather than pasting a shell command,
           because the instructions differ by platform and change. -->
      {#if agentMissing}
        <div class="error-message agent-missing">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M12 9v4"/><path d="M12 17h.01"/>
            <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
          </svg>
          <span>{$t('agentInstall.missing', { name: chosenAgent?.name || selectedAgent })}</span>
          {#if chosenAgent?.installUrl}
            <button
              type="button"
              class="install-link"
              on:click={() => BrowserOpenURL(chosenAgent?.installUrl || '')}
            >{$t('agentInstall.open')}</button>
          {/if}
        </div>
      {/if}

      <form on:submit|preventDefault={() => handleSubmit(false)}>
        <!-- Agent Type -->
        <div class="form-group">
          <span class="form-label">{$t('newSession.agentType')}</span>
          <div class="agent-grid">
            {#each availableAgents as agent (agent.type)}
              <button
                type="button"
                class="agent-btn {selectedAgent === agent.type ? 'selected' : ''}"
                class:not-installed={agent.installed === false}
                title={agent.installed === false ? $t('agentInstall.notInstalled') : agent.name}
                on:click={() => selectedAgent = agent.type}
              >
                <span class="agent-icon-wrapper">
                  <AgentIcon agent={agent.type} size="md" />
                </span>
                <span class="agent-name">{agent.name}</span>
              </button>
            {/each}
          </div>
        </div>

        <!-- Path -->
        <div class="form-group">
          <label class="form-label" for="path">{$t('newSession.projectPath')}</label>
          <div class="path-input-group">
            <input
              id="path"
              type="text"
              bind:value={path}
              placeholder="/home/user/project"
              class="form-input"
            />
            <button type="button" class="browse-btn" on:click={browsePath} title={$t('newSession.browse')}>
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>
              </svg>
            </button>
          </div>
        </div>

        <!-- Name -->
        <div class="form-group">
          <label class="form-label" for="name">{$t('newSession.sessionName')}</label>
          <input
            id="name"
            type="text"
            bind:value={name}
            on:input={handleNameInput}
            placeholder="my-project"
            class="form-input"
          />
        </div>

        <!-- Which machine this session runs on. Shown only when there is a
             choice to make: someone with no servers configured should not have
             to read a field that always says the same thing. -->
        {#if servers.length > 0}
          <div class="form-group">
            <span class="form-label">{$t('servers.runsOn')}</span>
            <Select
              value={serverId}
              options={serverOptions}
              on:change={e => { serverId = e.detail; }}
            />
            {#if serverId}
              <p class="field-hint">{$t('servers.runsOnHint')}</p>
            {/if}
          </div>
        {/if}

        <!-- Group -->
        {#if $groups.length > 0}
          <div class="form-group">
            <span class="form-label">{$t('newSession.group')}</span>
            <Select
              value={selectedGroupId}
              options={groupOptions}
              on:change={e => { selectedGroupId = e.detail; }}
            />
          </div>
        {/if}

        <!-- Extra CLI Arguments (hidden for custom agent) -->
        {#if selectedAgent !== 'custom' && selectedAgent !== 'terminal'}
          <div class="form-group">
            <label class="form-label" for="extra-args">{$t('newSession.extraArgs')}</label>
            <input
              id="extra-args"
              type="text"
              bind:value={extraArgs}
              placeholder={$t('newSession.extraArgsPlaceholder')}
              class="form-input"
            />
            <span class="form-hint">{$t('newSession.extraArgsHint')}</span>
          </div>
        {/if}

        <!-- Available Sessions to Resume -->
        {#if availableSessions.length > 0 || isLoadingSessions}
          <div class="form-group">
            <span class="form-label">{$t('newSession.resumePrevious')}</span>
            {#if isLoadingSessions}
              <div class="loading-sessions">
                <svg class="spinner" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83"/>
                </svg>
                {$t('newSession.loadingSessions')}
              </div>
            {:else}
              <div class="session-list">
                <button
                  type="button"
                  class="session-item {selectedResumeId === '' ? 'selected' : ''}"
                  on:click={() => { selectedResumeId = ''; showForkWarning = false; }}
                >
                  <span class="session-icon new">+</span>
                  <span class="session-info">
                    <span class="session-name">{$t('newSession.startFresh')}</span>
                    <span class="session-desc">{$t('newSession.newConversation')}</span>
                  </span>
                </button>
                {#each availableSessions as sess (sess.id)}
                  {@const isConflict = checkSessionConflict(sess.id)}
                  <button
                    type="button"
                    class="session-item {selectedResumeId === sess.id ? 'selected' : ''} {isConflict ? 'conflict' : ''}"
                    on:click={() => handleResumeSelect(sess.id)}
                  >
                    <span class="session-icon resume">↻</span>
                    <span class="session-info">
                      <span class="session-name">{sess.displayName}</span>
                      <span class="session-desc">
                        {sess.timestamp}
                        {#if isConflict}
                          <span class="conflict-badge">{$t('newSession.inUse', { name: isConflict })}</span>
                        {/if}
                      </span>
                    </span>
                  </button>
                {/each}
              </div>
            {/if}
          </div>
        {/if}

        <!-- Fork Warning -->
        {#if showForkWarning}
          <div class="fork-warning">
            <div class="warning-icon">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
                <line x1="12" y1="9" x2="12" y2="13"/>
                <line x1="12" y1="17" x2="12.01" y2="17"/>
              </svg>
            </div>
            <div class="warning-content">
              <div class="warning-title">{$t('newSession.sessionInUse')}</div>
              <div class="warning-text">
                {$t('newSession.forkWarning', { name: conflictingSessionName })}
              </div>
            </div>
            <div class="warning-actions">
              <button type="button" class="btn-warning-cancel" on:click={() => { showForkWarning = false; selectedResumeId = ''; }}>
                {$t('newSession.forkCancel')}
              </button>
              <button type="button" class="btn-warning-fork" on:click={() => handleSubmit(true)}>
                {$t('newSession.forkCreate')}
              </button>
            </div>
          </div>
        {/if}

        <!-- Options -->
        <div class="form-options">
          {#if selectedAgent !== 'terminal'}
            <!-- A plain shell has no permission prompts to auto-approve. -->
            <label class="checkbox-label">
              <input type="checkbox" bind:checked={autoYes} class="checkbox-input" />
              <span class="checkbox-custom"></span>
              <span class="checkbox-text">{$t('newSession.autoApprove')}</span>
            </label>
          {/if}

          {#if canUseWorktree}
            <label class="checkbox-label">
              <input type="checkbox" bind:checked={useWorktree} class="checkbox-input" />
              <span class="checkbox-custom"></span>
              <span class="checkbox-text">{$t('newSession.ownWorktree')}</span>
            </label>
            {#if useWorktree}
              <p class="field-hint">{$t('newSession.ownWorktreeHint')}</p>
              {#if plannedWorktree}
                <p class="field-hint worktree-plan" title={plannedWorktree.dir}>
                  {plannedWorktree.dir}<br />{plannedWorktree.branch}
                </p>
              {/if}
            {/if}
          {/if}

          <label class="checkbox-label">
            <input type="checkbox" bind:checked={autoStart} class="checkbox-input" />
            <span class="checkbox-custom"></span>
            <span class="checkbox-text">{$t('newSession.startImmediately')}</span>
          </label>
        </div>

        <!-- Actions -->
        <div class="dialog-actions">
          <button type="button" class="btn-cancel" on:click={close}>
            {$t('newSession.cancel')}
          </button>
          <button type="submit" class="btn-primary" disabled={isSubmitting}>
            {#if isSubmitting}
              <svg class="spinner" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83"/>
              </svg>
              {$t('newSession.creating')}
            {:else}
              {$t('newSession.createSession')}
            {/if}
          </button>
        </div>
      </form>
    </div>
  </div>
{/if}

<RemoteDirPicker
  bind:show={showRemotePicker}
  {serverId}
  startPath={path}
  on:chosen={e => { path = e.detail; }}
/>

<style>
  /* Component-specific: wider dialog for agent grid */
  /* Bounded, so the dialog cannot grow past the window.
     It had a width but no height, and with the agent grid, the resume list and
     the options all open it ran off the screen — taking the title with it at
     the top and the Create button at the bottom, so there was no way to finish
     or to close it.
     The header and the actions stay put and the middle scrolls: those two are
     how the dialog is used, and they are the parts that must never be the ones
     off screen. */
  .dialog-content {
    max-width: 480px;
    max-height: min(90vh, 900px);
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .dialog-content > :global(.dialog-header) {
    flex-shrink: 0;
  }

  /* The form holds everything between the header and the buttons. It is the
     scrolling part, and min-height: 0 is what allows it to shrink below its
     content — without it a flex child refuses to, and the overflow moves back
     out to the dialog. */
  .dialog-content > form {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }

  /* Sits to the left of the close button, which the shared header pushes to
     the far right. */
  .template-link {
    margin-left: auto;
    margin-right: 12px;
    border: 0;
    background: none;
    padding: 0;
    cursor: pointer;
    font-size: 12px;
    color: var(--accent-light);
    text-decoration: underline;
  }
  .template-link:hover {
    color: var(--accent-pale);
  }

  /* Component-specific: error message with icon and margin */
  .error-message {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 16px 24px;
  }

  form {
    padding: 24px;
  }

  .form-group {
    margin-bottom: 20px;
  }

  .form-label {
    display: block;
    font-size: 13px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #9ca3af;
    margin-bottom: 10px;
  }

  .agent-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 8px;
  }

  .agent-btn {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 12px 8px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 12px;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  /* Dimmed rather than disabled: the user may want to pick it, see why it
     cannot start, and install it from the offer below. */
  .agent-btn.not-installed .agent-icon-wrapper,
  .agent-btn.not-installed .agent-name {
    opacity: 0.45;
  }

  .agent-missing {
    color: #fbbf24;
  }

  .install-link {
    margin-left: auto;
    flex-shrink: 0;
    padding: 4px 10px;
    border-radius: 6px;
    border: 1px solid rgba(var(--accent-rgb), 0.5);
    background: rgba(var(--accent-rgb), 0.12);
    color: var(--accent-light);
    font-size: 12px;
    cursor: pointer;
  }

  .install-link:hover {
    background: rgba(var(--accent-rgb), 0.2);
  }

  .agent-btn:hover {
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(255, 255, 255, 0.15);
  }

  .agent-btn.selected {
    background: linear-gradient(135deg, rgba(var(--accent-rgb), 0.2) 0%, rgba(99, 102, 241, 0.15) 100%);
    border-color: rgba(var(--accent-rgb), 0.5);
    box-shadow: 0 0 20px rgba(var(--accent-rgb), 0.15);
  }

  .agent-icon-wrapper {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
  }

  .agent-name {
    font-size: 12px;
    color: #9ca3af;
  }

  .agent-btn.selected .agent-name {
    color: var(--accent-light);
  }

  .path-input-group {
    display: flex;
    gap: 8px;
  }

  .path-input-group .form-input {
    flex: 1;
  }

  .browse-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 0 14px;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 10px;
    color: #9ca3af;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .browse-btn:hover {
    background: rgba(255, 255, 255, 0.1);
    border-color: rgba(255, 255, 255, 0.15);
    color: white;
  }

  .form-input {
    width: 100%;
    padding: 12px 16px;
    background: rgba(0, 0, 0, 0.2);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 10px;
    font-size: 14px;
    color: white;
    transition: all 0.2s ease;
  }

  .form-hint {
    display: block;
    font-size: 12px;
    color: #6b7280;
    margin-top: 6px;
  }

  .form-input::placeholder {
    color: #4b5563;
  }

  .form-input:focus {
    outline: none;
    border-color: rgba(var(--accent-rgb), 0.5);
    box-shadow: 0 0 0 3px rgba(var(--accent-rgb), 0.1);
  }

  /* A row of checkboxes, wrapping when they do not fit.
     align-items: flex-start keeps a wrapped line from stretching its
     neighbours, and the hint below the worktree box is a block of its own so
     it sits under that checkbox rather than beside it. */
  .form-options {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    gap: 12px 24px;
    margin-bottom: 24px;
  }

  /* No .field-hint rule existed here, so every hint in this dialog was
     rendering as ordinary body text. */
  .field-hint {
    margin: 4px 0 0;
    font-size: 11px;
    color: #8b8b93;
    flex-basis: 100%;
  }

  /* The resolved path and branch, shown while the worktree box is ticked.
     Monospace because it is a path: the point is to read it exactly, and a
     proportional font makes a long one harder to check. It wraps rather than
     being cut — there is room under the checkbox, and a truncated path
     answers nothing. */
  .worktree-plan {
    font-family: var(--font-mono, ui-monospace, monospace);
    color: #a1a1aa;
    overflow-wrap: anywhere;
    line-height: 1.5;
  }

  .checkbox-label {
    display: flex;
    align-items: center;
    gap: 10px;
    cursor: pointer;
    user-select: none;
  }

  .checkbox-input {
    display: none;
  }

  .checkbox-custom {
    width: 18px;
    height: 18px;
    background: rgba(0, 0, 0, 0.2);
    border: 1px solid rgba(255, 255, 255, 0.15);
    border-radius: 5px;
    position: relative;
    transition: all 0.2s ease;
  }

  .checkbox-input:checked + .checkbox-custom {
    background: linear-gradient(135deg, var(--accent) 0%, var(--accent-dark) 100%);
    border-color: transparent;
  }

  .checkbox-input:checked + .checkbox-custom::after {
    content: '';
    position: absolute;
    left: 6px;
    top: 2px;
    width: 5px;
    height: 10px;
    border: solid white;
    border-width: 0 2px 2px 0;
    transform: rotate(45deg);
  }

  .checkbox-text {
    font-size: 13px;
    color: #9ca3af;
  }

  /* Kept at the bottom of the dialog rather than at the end of the form, so
     the buttons are reachable however long the form gets. The background is
     needed now that content scrolls underneath it. */
  .dialog-actions {
    display: flex;
    justify-content: flex-end;
    gap: 12px;
    padding-top: 8px;
    border-top: 1px solid rgba(255, 255, 255, 0.05);
    position: sticky;
    bottom: 0;
    flex-shrink: 0;
    background: var(--bg-raised);
    margin-top: auto;
  }

  /* Component-specific: primary button with flex for spinner */
  .btn-primary {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .spinner {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }

  /* Session list styles */
  .loading-sessions {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px;
    color: #9ca3af;
    font-size: 13px;
  }

  .session-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
    max-height: 200px;
    overflow-y: auto;
    padding: 4px;
    background: rgba(0, 0, 0, 0.15);
    border-radius: 10px;
  }

  .session-item {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 12px;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.05);
    border-radius: 8px;
    cursor: pointer;
    transition: all 0.2s ease;
    text-align: left;
  }

  .session-item:hover {
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(255, 255, 255, 0.1);
  }

  .session-item.selected {
    background: linear-gradient(135deg, rgba(var(--accent-rgb), 0.15) 0%, rgba(99, 102, 241, 0.1) 100%);
    border-color: rgba(var(--accent-rgb), 0.4);
  }

  .session-item.conflict {
    border-color: rgba(251, 191, 36, 0.3);
  }

  .session-item.conflict.selected {
    border-color: rgba(251, 191, 36, 0.5);
  }

  .session-icon {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    border-radius: 6px;
    font-size: 14px;
    font-weight: 600;
  }

  .session-icon.new {
    background: rgba(34, 197, 94, 0.15);
    color: #4ade80;
  }

  .session-icon.resume {
    background: rgba(var(--accent-rgb), 0.15);
    color: var(--accent-light);
  }

  .session-info {
    flex: 1;
    min-width: 0;
  }

  .session-name {
    display: block;
    font-size: 13px;
    font-weight: 500;
    color: #e4e4e7;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .session-desc {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: #6b7280;
    margin-top: 2px;
  }

  .conflict-badge {
    padding: 2px 6px;
    background: rgba(251, 191, 36, 0.15);
    border-radius: 4px;
    font-size: 11px;
    color: #fbbf24;
  }

  /* Fork warning styles */
  .fork-warning {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 16px;
    background: rgba(251, 191, 36, 0.08);
    border: 1px solid rgba(251, 191, 36, 0.25);
    border-radius: 10px;
    margin-bottom: 20px;
  }

  .warning-icon {
    color: #fbbf24;
  }

  .warning-content {
    flex: 1;
  }

  .warning-title {
    font-size: 14px;
    font-weight: 600;
    color: #fbbf24;
    margin-bottom: 4px;
  }

  .warning-text {
    font-size: 13px;
    color: #9ca3af;
    line-height: 1.5;
  }

  .warning-text strong {
    color: #e4e4e7;
  }

  .warning-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 8px;
  }

  .btn-warning-cancel {
    padding: 8px 16px;
    background: transparent;
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 8px;
    font-size: 13px;
    font-weight: 500;
    color: #9ca3af;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .btn-warning-cancel:hover {
    background: rgba(255, 255, 255, 0.05);
    color: white;
  }

  .btn-warning-fork {
    padding: 8px 16px;
    background: linear-gradient(135deg, rgba(251, 191, 36, 0.2) 0%, rgba(245, 158, 11, 0.15) 100%);
    border: 1px solid rgba(251, 191, 36, 0.4);
    border-radius: 8px;
    font-size: 13px;
    font-weight: 600;
    color: #fbbf24;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .btn-warning-fork:hover {
    background: linear-gradient(135deg, rgba(251, 191, 36, 0.3) 0%, rgba(245, 158, 11, 0.25) 100%);
    box-shadow: 0 0 15px rgba(251, 191, 36, 0.2);
  }
</style>
