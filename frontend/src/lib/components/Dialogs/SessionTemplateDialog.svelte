<script lang="ts">
  import * as App from '../../../../wailsjs/go/main/App';
  import { main as models } from '../../../../wailsjs/go/models';
  import type { main } from '../../../../wailsjs/go/models';
  import Select from '../common/Select.svelte';
  import AgentIcon from '../common/AgentIcon.svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import { agents, loadAgents } from '../../stores/agents';
  import { groups, assignToGroup, startSession, loadSessions } from '../../stores/sessions';
  import { t } from '../../i18n';
  import { get } from 'svelte/store';
  import { activeProjectId } from '../../stores/projects';
  import { autoFocusDialog } from '../../utils/dialogActions';

  export let show = false;
  /** Pre-selects a template and jumps straight to "use it" when opened from the picker. */
  export let useTemplateId = '';

  type Template = main.SessionTemplateInfo;
  type Tab = main.TemplateTabInfo;

  let templates: Template[] = [];
  let loading = false;
  let error = '';
  let loadGeneration = 0;
  let operationGeneration = 0;
  let dialogProjectId = '';
  let hasDialogProject = false;

  // Which panel the dialog is showing. Kept as one variable rather than three
  // booleans so two panels can never be open at once.
  type Mode = 'list' | 'edit' | 'use';
  let mode: Mode = 'list';

  // Template editor. `editingId === ''` means "new template".
  let editingId = '';
  let fName = '';
  let fDescription = '';
  let fSessionName = '';
  let fPath = '';
  let fAgent = 'claude';
  let fAutoYes = false;
  let fExtraArgs = '';
  let fTabs: Tab[] = [];

  // "Use template" form.
  let useTarget: Template | null = null;
  let uName = '';
  let uPath = '';
  let uGroupId = '';
  let uAutoStart = true;
  let creating = false;
  let saving = false;

  let showDelete = false;
  let deleteTarget: Template | null = null;

  // Single guard block: the tracking variable is assigned inside the same
  // reactive statement that reads it, so the block cannot re-enter and race
  // with itself when `show` and `useTemplateId` change in the same tick.
  //
  // The key is prefixed rather than being the id alone: opening the manager
  // with no preselected template twice in a row would otherwise produce the
  // same empty key as the closed state and never reload.
  let lastInitKey = '';
  $: {
    const key = show ? `open|${useTemplateId}` : '';
    if (key && key !== lastInitKey) {
      lastInitKey = key;
      void open(useTemplateId);
    } else if (!show && lastInitKey !== '') {
      lastInitKey = '';
      operationGeneration++;
      loadGeneration++;
      resetAll();
    }
  }

  // Editing the global template library may survive a project switch, but a
  // "use template" workflow carries project-scoped group/session identities.
  // Cancel that workflow rather than finishing its later steps in the new
  // project with colliding ids.
  $: if (show) {
    if (!hasDialogProject) {
      hasDialogProject = true;
      dialogProjectId = $activeProjectId;
    } else if (dialogProjectId !== $activeProjectId) {
      dialogProjectId = $activeProjectId;
      operationGeneration++;
      creating = false;
      if (mode === 'use') {
        mode = 'list';
        useTarget = null;
      }
    }
  } else if (hasDialogProject) {
    hasDialogProject = false;
    dialogProjectId = '';
  }

  $: if (show && $agents.length === 0) {
    loadAgents();
  }

  async function open(preselect: string) {
    const generation = ++operationGeneration;
    mode = 'list';
    await load();
    if (!show || generation !== operationGeneration || `open|${useTemplateId}` !== lastInitKey) return;
    if (!preselect) return;
    const found = templates.find((tpl) => tpl.id === preselect);
    if (found) startUse(found);
  }

  async function load() {
    const generation = ++loadGeneration;
    loading = true;
    error = '';
    try {
      const nextTemplates = (await App.GetSessionTemplates()) || [];
      if (!show || generation !== loadGeneration) return;
      templates = nextTemplates;
    } catch (e) {
      if (!show || generation !== loadGeneration) return;
      error = String(e);
    } finally {
      if (generation === loadGeneration) loading = false;
    }
  }

  function resetAll() {
    mode = 'list';
    error = '';
    creating = false;
    saving = false;
    useTarget = null;
    clearForm();
  }

  function clearForm() {
    editingId = '';
    fName = '';
    fDescription = '';
    fSessionName = '';
    fPath = '';
    fAgent = 'claude';
    fAutoYes = false;
    fExtraArgs = '';
    fTabs = [];
  }

  // Agent options for both the main window and each tab. Terminal is a valid
  // tab agent and a valid session agent, so one list serves both.
  $: agentOptions = $agents.map((a) => ({ value: a.type, label: a.name }));
  $: groupOptions = [
    { value: '', label: $t('templates.noGroup') },
    ...$groups.map((g) => ({ value: g.id, label: g.name })),
  ];

  function newTemplate() {
    operationGeneration++;
    saving = false;
    clearForm();
    mode = 'edit';
  }

  function editTemplate(tpl: Template) {
    operationGeneration++;
    saving = false;
    editingId = tpl.id;
    fName = tpl.name;
    fDescription = tpl.description || '';
    fSessionName = tpl.sessionName || '';
    fPath = tpl.path || '';
    fAgent = tpl.agent || 'claude';
    fAutoYes = tpl.autoYes;
    fExtraArgs = tpl.extraArgs || '';
    // Copied, not referenced: cancelling an edit must leave the list intact.
    fTabs = (tpl.tabs || []).map((tab) => ({ ...tab }));
    mode = 'edit';
  }

  function addTab() {
    // The generated binding is a class, so an object literal would not be
    // assignable; createFrom is its own constructor.
    fTabs = [...fTabs, models.TemplateTabInfo.createFrom({
      name: '', agent: 'terminal', customCommand: '', autoYes: false, extraArgs: '', workDir: ''
    })];
  }

  function removeTab(index: number) {
    fTabs = fTabs.filter((_, i) => i !== index);
  }

  function setTabAgent(index: number, agent: string) {
    fTabs = fTabs.map((tab, i) => (i === index ? { ...tab, agent } : tab));
  }

  $: canSave = fName.trim().length > 0 && !saving;

  async function saveTemplate() {
    if (!canSave) return;
    const generation = operationGeneration;
    const targetEditingId = editingId;
    const name = fName.trim();
    const description = fDescription.trim();
    const sessionName = fSessionName.trim();
    const templatePath = fPath.trim();
    const agent = fAgent;
    const autoYes = fAutoYes;
    const extraArgs = fExtraArgs.trim();
    const tabs = fTabs.map((tab) => ({ ...tab, name: tab.name.trim() || tab.agent }));
    saving = true;
    error = '';
    try {
      // A tab with no name would show up in the tab bar as an empty label;
      // the agent type is the obvious stand-in.
      await App.SaveSessionTemplate(
        targetEditingId, name, description, sessionName,
        templatePath, agent, autoYes, extraArgs, tabs
      );
      if (!show || generation !== operationGeneration || mode !== 'edit' || editingId !== targetEditingId) return;
      mode = 'list';
      clearForm();
      await load();
    } catch (e) {
      if (!show || generation !== operationGeneration || mode !== 'edit' || editingId !== targetEditingId) return;
      error = String(e);
    } finally {
      if (generation === operationGeneration) saving = false;
    }
  }

  async function browseTemplatePath() {
    const generation = operationGeneration;
    const targetEditingId = editingId;
    const initialPath = fPath;
    try {
      const picked = await App.BrowseDirectory(initialPath || '');
      if (picked && show && generation === operationGeneration && mode === 'edit' &&
          editingId === targetEditingId && fPath === initialPath) fPath = picked;
    } catch (e) {
      console.error('Browse failed:', e);
    }
  }

  function askDelete(tpl: Template) {
    operationGeneration++;
    deleteTarget = tpl;
    showDelete = true;
  }

  async function confirmDelete() {
    const target = deleteTarget;
    const generation = operationGeneration;
    deleteTarget = null;
    if (!target) return;
    error = '';
    try {
      await App.DeleteSessionTemplate(target.id);
      if (!show || generation !== operationGeneration) return;
      if (editingId === target.id) {
        mode = 'list';
        clearForm();
      }
      await load();
    } catch (e) {
      if (!show || generation !== operationGeneration) return;
      error = String(e);
    }
  }

  function startUse(tpl: Template) {
    operationGeneration++;
    creating = false;
    useTarget = tpl;
    uName = tpl.sessionName || tpl.name;
    // A template pinned to a directory pre-fills it; a reusable one starts
    // blank so the user has to say where this session belongs.
    uPath = tpl.path || '';
    uGroupId = '';
    uAutoStart = true;
    error = '';
    mode = 'use';
  }

  async function browseUsePath() {
    const generation = operationGeneration;
    const targetId = useTarget?.id;
    const initialPath = uPath;
    try {
      const picked = await App.BrowseDirectory(initialPath || '');
      if (picked && show && generation === operationGeneration && mode === 'use' &&
          useTarget?.id === targetId && uPath === initialPath) {
        uPath = picked;
        // The folder name is the best guess for the session name, matching
        // what the new-session dialog does.
        if (!uName.trim() || uName === (useTarget?.sessionName || useTarget?.name)) {
          // Both separators: the directory picker returns a native path.
          const folder = picked.replace(/[/\\]+$/, '').split(/[/\\]/).pop();
          if (folder) uName = folder;
        }
      }
    } catch (e) {
      console.error('Browse failed:', e);
    }
  }

  $: canCreate = !!useTarget && uPath.trim().length > 0 && uName.trim().length > 0 && !creating;

  async function createFromTemplate() {
    if (!useTarget || !canCreate) return;
    const generation = operationGeneration;
    const target = useTarget;
    const sessionName = uName.trim();
    const sessionPath = uPath.trim();
    const autoStart = uAutoStart;
    creating = true;
    error = '';
    const groupId = uGroupId;
    const targetProjectId = get(activeProjectId);
    try {
      const created = await App.CreateSessionFromTemplate(target.id, sessionName, sessionPath, targetProjectId);
      if (targetProjectId !== get(activeProjectId)) return;
      if (created) {
        if (groupId) {
          await assignToGroup(created.id, groupId);
          if (targetProjectId !== get(activeProjectId)) return;
        }
        // Starting is what actually spawns the tabs: the backend stores them
        // as followed windows and the session start recreates each one.
        if (autoStart) await startSession(created.id);
        else await loadSessions();
      }
      if (!show || generation !== operationGeneration || useTarget?.id !== target.id) return;
      show = false;
    } catch (e) {
      if (!show || generation !== operationGeneration || useTarget?.id !== target.id) return;
      error = String(e);
    } finally {
      if (generation === operationGeneration) creating = false;
    }
  }

  function close() {
    operationGeneration++;
    loadGeneration++;
    loading = false;
    creating = false;
    saving = false;
    showDelete = false;
    deleteTarget = null;
    show = false;
  }

  function cancelEdit() {
    operationGeneration++;
    saving = false;
    mode = 'list';
    clearForm();
  }

  function cancelUse() {
    operationGeneration++;
    creating = false;
    mode = 'list';
    useTarget = null;
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape') return;
    e.stopPropagation();
    if (mode !== 'list') {
      if (mode === 'edit') cancelEdit();
      else cancelUse();
      return;
    }
    close();
  }

  function focusInput(node: HTMLInputElement) {
    const frame = requestAnimationFrame(() => {
      const dialog = node.closest('[role="dialog"]');
      if (!dialog?.contains(document.activeElement)) node.focus();
    });
    return { destroy: () => cancelAnimationFrame(frame) };
  }

  // Hoisted out of markup so no casts are needed there.
  $: deleteName = deleteTarget?.name || '';
  $: useTitle = useTarget ? $t('templates.useTitle', { name: useTarget.name }) : '';
  $: useTabCount = useTarget?.tabs?.length || 0;

  function tabSummary(tpl: Template): string {
    const names = (tpl.tabs || []).map((tab) => tab.name || tab.agent);
    return names.join(' · ');
  }
</script>

{#if show}
  <div
    class="dialog-overlay template-overlay"
    use:autoFocusDialog
    tabindex="-1"
    role="dialog"
    aria-modal="true"
    on:click|self={close}
    on:keydown={handleKeydown}
  >
    <div class="dialog-content manager">
      <div class="dialog-header">
        <h2>
          {#if mode === 'use'}{useTitle}
          {:else if mode === 'edit'}{editingId ? $t('templates.editTitle') : $t('templates.newTitle')}
          {:else}{$t('templates.managerTitle')}{/if}
        </h2>
        <button class="close-btn" on:click={close}>×</button>
      </div>

      <div class="dialog-body">
        {#if error}<div class="error-line">{error}</div>{/if}

        {#if mode === 'use' && useTarget}
          <div class="form">
            {#if useTarget.needsPath}
              <p class="use-hint">{$t('templates.pathRequiredHint')}</p>
            {/if}

            <label class="field">
              <span class="field-label">{$t('templates.fieldSessionName')}</span>
              <input use:focusInput bind:value={uName} placeholder={$t('templates.sessionNamePlaceholder')} />
            </label>

            <div class="field">
              <span class="field-label">{$t('templates.fieldPath')}</span>
              <div class="path-row">
                <input bind:value={uPath} placeholder={$t('templates.pathPlaceholder')} />
                <button class="btn-secondary browse" on:click={browseUsePath}>{$t('templates.browse')}</button>
              </div>
            </div>

            <div class="field">
              <span class="field-label">{$t('templates.fieldGroup')}</span>
              <Select value={uGroupId} options={groupOptions} on:change={(e) => (uGroupId = e.detail)} />
            </div>

            <label class="toggle-row">
              <input type="checkbox" bind:checked={uAutoStart} />
              <span class="toggle-main">
                <span class="toggle-label">{$t('templates.autoStart')}</span>
                <span class="toggle-hint">{$t('templates.autoStartHint')}</span>
              </span>
            </label>

            <div class="summary">
              <span class="summary-label">{$t('templates.willCreate')}</span>
              <span class="summary-chip">
                <AgentIcon agent={useTarget.agent} size="xs" />
                {useTarget.agent}
              </span>
              {#each useTarget.tabs || [] as tab, i (i)}
                <span class="summary-chip">
                  <AgentIcon agent={tab.agent} size="xs" />
                  {tab.name || tab.agent}
                </span>
              {/each}
              {#if useTabCount === 0}
                <span class="summary-none">{$t('templates.noTabs')}</span>
              {/if}
            </div>

            <div class="form-actions">
              <button class="btn-secondary" on:click={cancelUse}>
                {$t('common.cancel')}
              </button>
              <button class="btn-primary" disabled={!canCreate} on:click={createFromTemplate}>
                {creating ? $t('templates.creating') : $t('templates.create')}
              </button>
            </div>
          </div>

        {:else if mode === 'edit'}
          <div class="form">
            <label class="field">
              <span class="field-label">{$t('templates.fieldName')}</span>
              <input use:focusInput bind:value={fName} placeholder={$t('templates.namePlaceholder')} />
            </label>

            <label class="field">
              <span class="field-label">{$t('templates.fieldDescription')}</span>
              <input bind:value={fDescription} placeholder={$t('templates.descriptionPlaceholder')} />
            </label>

            <div class="field">
              <span class="field-label">{$t('templates.fieldPath')}</span>
              <div class="path-row">
                <input bind:value={fPath} placeholder={$t('templates.pathOptionalPlaceholder')} />
                <button class="btn-secondary browse" on:click={browseTemplatePath}>{$t('templates.browse')}</button>
              </div>
              <span class="field-hint">{$t('templates.pathOptionalHint')}</span>
            </div>

            <label class="field">
              <span class="field-label">{$t('templates.fieldSessionName')}</span>
              <input bind:value={fSessionName} placeholder={$t('templates.sessionNameOptionalPlaceholder')} />
            </label>

            <div class="field">
              <span class="field-label">{$t('templates.fieldAgent')}</span>
              <Select value={fAgent} options={agentOptions} on:change={(e) => (fAgent = e.detail)} />
            </div>

            <label class="field">
              <span class="field-label">{$t('templates.fieldExtraArgs')}</span>
              <input bind:value={fExtraArgs} placeholder={$t('templates.extraArgsPlaceholder')} />
            </label>

            <label class="toggle-row">
              <input type="checkbox" bind:checked={fAutoYes} />
              <span class="toggle-main">
                <span class="toggle-label">{$t('templates.autoYes')}</span>
                <span class="toggle-hint">{$t('templates.autoYesHint')}</span>
              </span>
            </label>

            <div class="tabs-section">
              <div class="section-head">
                <span class="section-name">{$t('templates.tabs')}</span>
                <button class="link-btn add" on:click={addTab}>{$t('templates.addTab')}</button>
              </div>
              {#if fTabs.length === 0}
                <div class="section-empty">{$t('templates.tabsEmptyHint')}</div>
              {/if}
              {#each fTabs as tab, i (i)}
                <div class="tab-row">
                  <input class="tab-name" bind:value={tab.name} placeholder={$t('templates.tabNamePlaceholder')} />
                  <div class="tab-agent">
                    <Select small value={tab.agent} options={agentOptions} on:change={(e) => setTabAgent(i, e.detail)} />
                  </div>
                  <input class="tab-args" bind:value={tab.extraArgs} placeholder={$t('templates.tabArgsPlaceholder')} />
                  <button class="icon-btn danger" title={$t('templates.removeTab')} on:click={() => removeTab(i)}>×</button>
                </div>
              {/each}
            </div>

            <div class="form-actions">
              <button class="btn-secondary" on:click={cancelEdit}>
                {$t('common.cancel')}
              </button>
              <button class="btn-primary" disabled={!canSave} on:click={saveTemplate}>{$t('common.save')}</button>
            </div>
          </div>

        {:else if loading}
          <div class="state">{$t('common.loading')}</div>

        {:else if templates.length === 0}
          <div class="empty">
            <p class="empty-title">{$t('templates.emptyTitle')}</p>
            <p class="empty-hint">{$t('templates.emptyHint')}</p>
          </div>

        {:else}
          {#each templates as tpl (tpl.id)}
            <div class="tpl-row">
              <div class="tpl-main">
                <span class="tpl-name">
                  <AgentIcon agent={tpl.agent} size="xs" />
                  {tpl.name}
                </span>
                <span class="tpl-meta">
                  {#if tpl.needsPath}
                    <span class="tpl-badge">{$t('templates.anyDirectory')}</span>
                  {:else}
                    <span class="tpl-path">{tpl.path}</span>
                  {/if}
                  {#if tpl.tabs && tpl.tabs.length > 0}
                    <span class="tpl-tabs">{$t('templates.tabCount', { count: String(tpl.tabs.length) })}: {tabSummary(tpl)}</span>
                  {/if}
                </span>
                {#if tpl.description}<span class="tpl-desc">{tpl.description}</span>{/if}
              </div>
              <button class="btn-primary small" on:click={() => startUse(tpl)}>{$t('templates.use')}</button>
              <button class="icon-btn" title={$t('templates.editTitle')} on:click={() => editTemplate(tpl)}>✎</button>
              <button class="icon-btn danger" title={$t('templates.deleteTemplate')} on:click={() => askDelete(tpl)}>×</button>
            </div>
          {/each}
        {/if}
      </div>

      {#if mode === 'list'}
        <div class="dialog-footer">
          <button class="btn-primary" on:click={newTemplate}>{$t('templates.addTemplate')}</button>
        </div>
      {/if}
    </div>
  </div>
{/if}

<ConfirmDialog
  bind:show={showDelete}
  title={$t('templates.deleteTemplate')}
  message={$t('templates.deleteTemplateMessage', { name: deleteName })}
  confirmText={$t('common.delete')}
  cancelText={$t('common.cancel')}
  variant="danger"
  on:confirm={confirmDelete}
/>

<style>
  .template-overlay:focus { outline: none; }

  .manager {
    width: min(720px, 94vw);
    max-width: min(720px, 94vw);
    max-height: 84vh;
    display: flex;
    flex-direction: column;
  }
  .dialog-header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 14px 18px; border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  }
  .dialog-header h2 { margin: 0; font-size: 15px; color: #e4e4e7; }
  .close-btn { border: 0; background: none; color: #71717a; font-size: 20px; cursor: pointer; }
  .close-btn:hover { color: #e4e4e7; }

  .dialog-body { padding: 14px 18px; overflow-y: auto; flex: 1; min-height: 0; }
  .state { padding: 26px 0; text-align: center; font-size: 13px; color: #71717a; }
  .error-line { color: #fb7185; font-size: 13px; margin-bottom: 10px; }

  .empty { padding: 26px 0; text-align: center; }
  .empty-title { margin: 0 0 6px; font-size: 14px; color: #e4e4e7; }
  .empty-hint { margin: 0; font-size: 13px; color: #71717a; line-height: 1.6; }

  .tpl-row {
    display: flex; align-items: center; gap: 8px; padding: 8px 9px;
    border-radius: 7px; border: 1px solid transparent;
  }
  .tpl-row:hover { background: rgba(255, 255, 255, 0.04); }
  .tpl-main { display: flex; flex-direction: column; gap: 3px; flex: 1; min-width: 0; }
  .tpl-name { display: flex; align-items: center; gap: 6px; font-size: 13px; color: #e4e4e7; }
  .tpl-meta { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .tpl-path {
    font-family: 'JetBrains Mono', monospace; font-size: 11px; color: #6b7280;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 320px;
  }
  .tpl-badge {
    font-size: 11px; color: var(--accent-light);
    background: rgba(var(--accent-rgb), 0.15); border-radius: 999px; padding: 1px 8px;
  }
  .tpl-tabs { font-size: 11px; color: #71717a; }
  .tpl-desc { font-size: 12px; color: #71717a; }

  .icon-btn {
    border: 1px solid rgba(255, 255, 255, 0.1); background: rgba(255, 255, 255, 0.04);
    color: #a1a1aa; border-radius: 6px; width: 24px; height: 24px;
    font-size: 13px; cursor: pointer; flex-shrink: 0;
  }
  .icon-btn:hover { color: #e4e4e7; background: rgba(255, 255, 255, 0.08); }
  .icon-btn.danger:hover { color: #fb7185; border-color: rgba(251, 113, 133, 0.4); }

  .link-btn {
    border: 0; background: none; padding: 0; cursor: pointer;
    font-size: 12px; color: var(--accent-light); text-decoration: underline;
  }
  .link-btn:hover { color: var(--accent-pale); }

  .form { display: flex; flex-direction: column; gap: 10px; }
  .field { display: flex; flex-direction: column; gap: 3px; }
  .field-label { font-size: 12px; color: #a1a1aa; }
  .field-hint { font-size: 11px; color: #6b7280; }
  .field input {
    padding: 7px 10px; border-radius: 7px; font-size: 13px;
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(0, 0, 0, 0.25); color: #e4e4e7;
    font-family: inherit; width: 100%; box-sizing: border-box;
  }
  .field input:focus { outline: none; border-color: rgba(var(--accent-rgb), 0.6); }

  .use-hint {
    margin: 0; font-size: 12px; color: var(--accent-light); line-height: 1.5;
    background: rgba(var(--accent-rgb), 0.1); border-radius: 7px; padding: 8px 10px;
  }

  .path-row { display: flex; gap: 6px; }
  .path-row input { flex: 1; min-width: 0; }
  .browse { flex-shrink: 0; }

  .toggle-row { display: flex; align-items: flex-start; gap: 9px; cursor: pointer; }
  .toggle-row input { margin-top: 2px; accent-color: var(--accent); }
  .toggle-main { display: flex; flex-direction: column; gap: 1px; }
  .toggle-label { font-size: 13px; color: #e4e4e7; }
  .toggle-hint { font-size: 11px; color: #6b7280; }

  .tabs-section { display: flex; flex-direction: column; gap: 6px; }
  .section-head { display: flex; align-items: center; gap: 6px; padding: 4px 2px; }
  .section-name {
    font-size: 11px; text-transform: uppercase; letter-spacing: 0.06em; color: #6b7280;
  }
  .section-head .add { margin-left: auto; }
  .section-empty { font-size: 12px; color: #52525b; padding: 2px 2px 4px; line-height: 1.5; }

  .tab-row { display: flex; align-items: center; gap: 6px; }
  .tab-row input {
    padding: 6px 9px; border-radius: 6px; font-size: 13px;
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(0, 0, 0, 0.25); color: #e4e4e7; font-family: inherit;
    min-width: 0;
  }
  .tab-row input:focus { outline: none; border-color: rgba(var(--accent-rgb), 0.6); }
  .tab-name { flex: 1 1 30%; }
  .tab-agent { flex: 0 0 130px; }
  .tab-args { flex: 1 1 30%; font-family: 'JetBrains Mono', monospace; font-size: 12px; }

  .summary {
    display: flex; align-items: center; gap: 6px; flex-wrap: wrap;
    padding: 8px 10px; border-radius: 7px; background: rgba(255, 255, 255, 0.03);
  }
  .summary-label { font-size: 11px; color: #6b7280; }
  .summary-none { font-size: 11px; color: #52525b; }
  .summary-chip {
    display: inline-flex; align-items: center; gap: 4px;
    font-size: 12px; color: #d4d4d8;
    background: rgba(255, 255, 255, 0.06); border-radius: 999px; padding: 2px 9px;
  }

  .form-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }

  .dialog-footer {
    display: flex; justify-content: flex-end; gap: 8px;
    padding: 12px 18px; border-top: 1px solid rgba(255, 255, 255, 0.06);
  }
  .btn-primary, .btn-secondary {
    padding: 7px 16px; border-radius: 7px; font-size: 13px; font-weight: 600; cursor: pointer;
  }
  .btn-primary.small { padding: 5px 12px; font-size: 12px; flex-shrink: 0; }
  .btn-secondary {
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(255, 255, 255, 0.05); color: #a1a1aa;
  }
  .btn-primary {
    border: 1px solid var(--accent);
    background: linear-gradient(135deg, var(--accent-dark), var(--accent)); color: var(--accent-ink);
  }
  .btn-primary:disabled { opacity: 0.45; cursor: default; }
</style>
