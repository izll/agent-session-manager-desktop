<script lang="ts">
  import * as App from '../../../../wailsjs/go/main/App';
  import type { main } from '../../../../wailsjs/go/models';
  import Select from '../common/Select.svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import { t } from '../../i18n';
  import { autoFocusDialog } from '../../utils/dialogActions';

  export let show = false;

  type Cmd = main.SavedCommandInfo;
  type Group = main.CommandGroupInfo;

  let commands: Cmd[] = [];
  let groups: Group[] = [];
  let loading = false;
  let error = '';
  let loadGeneration = 0;
  let operationGeneration = 0;
  let savingCommand = false;
  let savingGroup = false;

  // Command editor state. `editingId === ''` means "new command".
  let editing = false;
  let editingId = '';
  let fName = '';
  let fCommand = '';
  let fDescription = '';
  let fGroupId = '';
  let fSendEnter = true;

  // Group editor state.
  let editingGroup = false;
  let editingGroupId = '';
  let gName = '';

  // Deletion confirmations.
  let showDeleteCmd = false;
  let deleteCmdTarget: Cmd | null = null;
  let showDeleteGroup = false;
  let deleteGroupTarget: Group | null = null;

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
      const lib = await App.GetCommands();
      if (!show || generation !== loadGeneration) return;
      commands = (lib?.commands || []) as Cmd[];
      groups = (lib?.groups || []) as Group[];
    } catch (e) {
      if (!show || generation !== loadGeneration) return;
      error = String(e);
    } finally {
      if (generation === loadGeneration) loading = false;
    }
  }

  function resetAll() {
    editing = false;
    editingGroup = false;
    error = '';
    savingCommand = false;
    savingGroup = false;
    clearForm();
  }

  $: groupNames = new Map(groups.map(g => [g.id, g.name]));

  interface Section {
    id: string;
    name: string;
    items: Cmd[];
  }

  $: sections = (() => {
    const out: Section[] = [];
    for (const g of groups) {
      out.push({ id: g.id, name: g.name, items: commands.filter(c => c.groupId === g.id) });
    }
    const loose = commands.filter(c => !c.groupId || !groupNames.has(c.groupId));
    if (loose.length) out.push({ id: '', name: $t('commands.ungrouped'), items: loose });
    return out;
  })();

  // Same rule the backend uses to derive placeholders: `{name}` counts, but a
  // `${VAR}` is a shell variable and must be left alone.
  // Mirrors session/commands.go: {{name}} or {{name:default}}. Doubled braces
  // are what keep awk '{print $1}' and jq '{name: .name}' out of it.
  const PLACEHOLDER_RE = /\{\{([\p{L}\p{N}_][\p{L}\p{N}_ -]*?)(?::([^}]*))?\}\}/gu;

  // Shows the default alongside the name, so it is visible that
  // {{sorok:100}} was understood as a default rather than part of the name.
  function detectPlaceholders(text: string): string[] {
    const out: string[] = [];
    const seen = new Set<string>();
    for (const m of text.matchAll(PLACEHOLDER_RE)) {
      const name = m[1];
      if (seen.has(name)) continue;
      seen.add(name);
      out.push(m[2] ? `${name} = ${m[2]}` : name);
    }
    return out;
  }

  $: detected = detectPlaceholders(fCommand);

  // "No group" first, so the default choice is the top one.
  $: groupOptions = [
    { value: '', label: $t('commands.noGroup') },
    ...groups.map((g) => ({ value: g.id, label: g.name })),
  ];

  function clearForm() {
    editingId = '';
    fName = '';
    fCommand = '';
    fDescription = '';
    fGroupId = '';
    fSendEnter = true;
  }

  function newCommand(groupId = '') {
    operationGeneration++;
    savingCommand = false;
    clearForm();
    fGroupId = groupId;
    editing = true;
  }

  function editCommand(c: Cmd) {
    operationGeneration++;
    savingCommand = false;
    editingId = c.id;
    fName = c.name;
    fCommand = c.command;
    fDescription = c.description || '';
    fGroupId = c.groupId || '';
    fSendEnter = c.sendEnter;
    editing = true;
  }

  $: canSave = fName.trim().length > 0 && fCommand.trim().length > 0 && !savingCommand;

  async function saveCommand() {
    if (!canSave) return;
    const generation = operationGeneration;
    const targetId = editingId;
    const submitted = {
      name: fName.trim(), command: fCommand, description: fDescription.trim(),
      groupId: fGroupId, sendEnter: fSendEnter,
    };
    savingCommand = true;
    error = '';
    try {
      await App.SaveCommand(
        targetId, submitted.name, submitted.command, submitted.description,
        submitted.groupId, submitted.sendEnter
      );
      if (!show || generation !== operationGeneration || !editing || editingId !== targetId) return;
      editing = false;
      clearForm();
      await load();
    } catch (e) {
      if (!show || generation !== operationGeneration || !editing || editingId !== targetId) return;
      error = String(e);
    } finally {
      if (generation === operationGeneration) savingCommand = false;
    }
  }

  function askDeleteCommand(c: Cmd) {
    deleteCmdTarget = c;
    showDeleteCmd = true;
  }

  async function confirmDeleteCommand() {
    const target = deleteCmdTarget;
    deleteCmdTarget = null;
    if (!target) return;
    const generation = ++operationGeneration;
    error = '';
    try {
      await App.DeleteCommand(target.id);
      if (show && generation === operationGeneration && editingId === target.id) {
        editing = false;
        clearForm();
      }
      await load();
    } catch (e) {
      if (!show || generation !== operationGeneration) return;
      error = String(e);
    }
  }

  function newGroup() {
    operationGeneration++;
    savingGroup = false;
    editingGroupId = '';
    gName = '';
    editingGroup = true;
  }

  function editGroup(g: Group) {
    operationGeneration++;
    savingGroup = false;
    editingGroupId = g.id;
    gName = g.name;
    editingGroup = true;
  }

  async function saveGroup() {
    if (!gName.trim() || savingGroup) return;
    const generation = operationGeneration;
    const targetId = editingGroupId;
    const name = gName.trim();
    savingGroup = true;
    error = '';
    try {
      await App.SaveCommandGroup(targetId, name);
      if (!show || generation !== operationGeneration || !editingGroup || editingGroupId !== targetId) return;
      editingGroup = false;
      gName = '';
      editingGroupId = '';
      await load();
    } catch (e) {
      if (!show || generation !== operationGeneration || !editingGroup || editingGroupId !== targetId) return;
      error = String(e);
    } finally {
      if (generation === operationGeneration) savingGroup = false;
    }
  }

  function askDeleteGroup(g: Group) {
    deleteGroupTarget = g;
    showDeleteGroup = true;
  }

  async function confirmDeleteGroup() {
    const target = deleteGroupTarget;
    deleteGroupTarget = null;
    if (!target) return;
    const generation = ++operationGeneration;
    error = '';
    try {
      await App.DeleteCommandGroup(target.id);
      if (show && generation === operationGeneration && fGroupId === target.id) fGroupId = '';
      await load();
    } catch (e) {
      if (!show || generation !== operationGeneration) return;
      error = String(e);
    }
  }

  function close() {
    operationGeneration++;
    loadGeneration++;
    loading = false;
    savingCommand = false;
    savingGroup = false;
    show = false;
  }

  function cancelCommandEdit() {
    operationGeneration++;
    savingCommand = false;
    editing = false;
    clearForm();
  }

  function cancelGroupEdit() {
    operationGeneration++;
    savingGroup = false;
    editingGroup = false;
  }

  // This dialog can be opened from the picker, so a bubbling Escape would
  // close both. Handled on the overlay to keep stopPropagation meaningful.
  function handleKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape') return;
    e.stopPropagation();
    if (editing) {
      cancelCommandEdit();
      return;
    }
    if (editingGroup) {
      cancelGroupEdit();
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

  // Names for the confirm prompts; kept out of markup so no casts are needed.
  $: deleteCmdName = deleteCmdTarget?.name || '';
  $: deleteGroupName = deleteGroupTarget?.name || '';
</script>

{#if show}
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
        <h2>{$t('commands.managerTitle')}</h2>
        <button class="close-btn" on:click={close}>×</button>
      </div>

      <div class="dialog-body">
        {#if error}<div class="error-line">{error}</div>{/if}

        {#if editing}
          <div class="form">
            <h3 class="form-title">
              {editingId ? $t('commands.editCommand') : $t('commands.newCommand')}
            </h3>

            <label class="field">
              <span class="field-label">{$t('commands.fieldName')}</span>
              <input use:focusInput bind:value={fName} placeholder={$t('commands.namePlaceholder')} />
            </label>

            <label class="field">
              <span class="field-label">{$t('commands.fieldCommand')}</span>
              <textarea bind:value={fCommand} rows="3" placeholder={$t('commands.commandPlaceholder')}></textarea>
            </label>

            <div class="placeholder-note">
              {#if detected.length > 0}
                <span class="ph-label">{$t('commands.detected')}</span>
                {#each detected as p (p)}
                  <span class="ph-chip">{p}</span>
                {/each}
              {:else}
                <span class="ph-hint">{$t('commands.noPlaceholders')}</span>
              {/if}
            </div>

            <label class="field">
              <span class="field-label">{$t('commands.fieldDescription')}</span>
              <input bind:value={fDescription} placeholder={$t('commands.descriptionPlaceholder')} />
            </label>

            <div class="field">
              <span class="field-label">{$t('commands.fieldGroup')}</span>
              <!-- The shared Select, not a bare <select>: a native one renders
                   with the browser's own white background inside this dark
                   dialog. -->
              <Select
                value={fGroupId}
                options={groupOptions}
                on:change={(e) => fGroupId = e.detail}
              />
            </div>

            <label class="toggle-row">
              <input type="checkbox" bind:checked={fSendEnter} />
              <span class="toggle-main">
                <span class="toggle-label">{$t('commands.sendEnter')}</span>
                <span class="toggle-hint">{$t('commands.sendEnterHint')}</span>
              </span>
            </label>

            <div class="form-actions">
              <button class="btn-secondary" on:click={cancelCommandEdit} disabled={savingCommand}>
                {$t('common.cancel')}
              </button>
              <button class="btn-primary" disabled={!canSave} on:click={saveCommand}>
                {$t('common.save')}
              </button>
            </div>
          </div>
        {:else if editingGroup}
          <div class="form">
            <h3 class="form-title">
              {editingGroupId ? $t('commands.editGroup') : $t('commands.newGroup')}
            </h3>
            <label class="field">
              <span class="field-label">{$t('commands.fieldGroupName')}</span>
              <input use:focusInput bind:value={gName} placeholder={$t('commands.groupNamePlaceholder')} />
            </label>
            <div class="form-actions">
              <button class="btn-secondary" on:click={cancelGroupEdit} disabled={savingGroup}>{$t('common.cancel')}</button>
              <button class="btn-primary" disabled={!gName.trim() || savingGroup} on:click={saveGroup}>{$t('common.save')}</button>
            </div>
          </div>
        {:else if loading}
          <div class="state">{$t('common.loading')}</div>
        {:else if commands.length === 0 && groups.length === 0}
          <div class="empty">
            <p class="empty-title">{$t('commands.emptyTitle')}</p>
            <p class="empty-hint">{$t('commands.managerEmptyHint')}</p>
          </div>
        {:else}
          {#each sections as sec (sec.id)}
            <div class="section">
              <div class="section-head">
                <span class="section-name">{sec.name}</span>
                {#if sec.id}
                  {@const g = groups.find(x => x.id === sec.id)}
                  {#if g}
                    <button class="icon-btn" title={$t('commands.editGroup')} on:click={() => editGroup(g)}>✎</button>
                    <button class="icon-btn danger" title={$t('commands.deleteGroup')} on:click={() => askDeleteGroup(g)}>×</button>
                  {/if}
                {/if}
                <button class="link-btn add" on:click={() => newCommand(sec.id)}>{$t('commands.addHere')}</button>
              </div>

              {#if sec.items.length === 0}
                <div class="section-empty">{$t('commands.groupEmpty')}</div>
              {:else}
                {#each sec.items as c (c.id)}
                  <div class="cmd-row">
                    <div class="cmd-main">
                      <span class="cmd-name">{c.name}</span>
                      <span class="cmd-text">{c.command}</span>
                      {#if c.description}<span class="cmd-desc">{c.description}</span>{/if}
                    </div>
                    <button class="icon-btn" title={$t('commands.editCommand')} on:click={() => editCommand(c)}>✎</button>
                    <button class="icon-btn danger" title={$t('commands.deleteCommand')} on:click={() => askDeleteCommand(c)}>×</button>
                  </div>
                {/each}
              {/if}
            </div>
          {/each}
        {/if}
      </div>

      {#if !editing && !editingGroup}
        <div class="dialog-footer">
          <button class="btn-secondary" on:click={newGroup}>{$t('commands.addGroup')}</button>
          <button class="btn-primary" on:click={() => newCommand()}>{$t('commands.addCommand')}</button>
        </div>
      {/if}
    </div>
  </div>
{/if}

<ConfirmDialog
  bind:show={showDeleteCmd}
  title={$t('commands.deleteCommand')}
  message={$t('commands.deleteCommandMessage', { name: deleteCmdName })}
  confirmText={$t('common.delete')}
  cancelText={$t('common.cancel')}
  variant="danger"
  on:confirm={confirmDeleteCommand}
/>

<ConfirmDialog
  bind:show={showDeleteGroup}
  title={$t('commands.deleteGroup')}
  message={$t('commands.deleteGroupMessage', { name: deleteGroupName })}
  confirmText={$t('common.delete')}
  cancelText={$t('common.cancel')}
  variant="danger"
  on:confirm={confirmDeleteGroup}
/>

<style>
  /* Opens on top of the picker, which sits at the default overlay level. */
  .manager-overlay { z-index: 60; }
  .manager-overlay:focus { outline: none; }

  .manager {
    width: min(680px, 94vw);
    max-width: min(680px, 94vw);
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

  .section { margin-bottom: 14px; }
  .section-head { display: flex; align-items: center; gap: 6px; padding: 4px 2px; }
  .section-name {
    font-size: 11px; text-transform: uppercase; letter-spacing: 0.06em; color: #6b7280;
  }
  .section-head .add { margin-left: auto; }
  .section-empty { font-size: 12px; color: #52525b; padding: 4px 9px; }

  .cmd-row {
    display: flex; align-items: center; gap: 8px; padding: 7px 9px;
    border-radius: 7px; border: 1px solid transparent;
  }
  .cmd-row:hover { background: rgba(255, 255, 255, 0.04); }
  .cmd-main { display: flex; flex-direction: column; gap: 2px; flex: 1; min-width: 0; }
  .cmd-name { font-size: 13px; color: #e4e4e7; }
  .cmd-text {
    font-family: 'JetBrains Mono', monospace; font-size: 11px; color: #6b7280;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .cmd-desc { font-size: 12px; color: #71717a; }

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
  .form-title { margin: 0; font-size: 13px; color: #e4e4e7; }
  .field { display: flex; flex-direction: column; gap: 3px; }
  .field-label { font-size: 12px; color: #a1a1aa; }
  .field input, .field textarea {
    padding: 7px 10px; border-radius: 7px; font-size: 13px;
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(0, 0, 0, 0.25); color: #e4e4e7;
    font-family: inherit; resize: vertical;
  }
  .field textarea { font-family: 'JetBrains Mono', monospace; }
  .field input:focus, .field textarea:focus {
    outline: none; border-color: rgba(var(--accent-rgb), 0.6);
  }

  .placeholder-note {
    display: flex; align-items: center; gap: 6px; flex-wrap: wrap;
    margin-top: -4px; min-height: 18px;
  }
  .ph-label { font-size: 11px; color: #6b7280; }
  .ph-hint { font-size: 11px; color: #52525b; }
  .ph-chip {
    font-family: 'JetBrains Mono', monospace; font-size: 11px; color: #a5b4fc;
    background: rgba(var(--accent-rgb), 0.15); border-radius: 999px; padding: 1px 8px;
  }

  .toggle-row { display: flex; align-items: flex-start; gap: 9px; cursor: pointer; }
  .toggle-row input { margin-top: 2px; accent-color: var(--accent); }
  .toggle-main { display: flex; flex-direction: column; gap: 1px; }
  .toggle-label { font-size: 13px; color: #e4e4e7; }
  .toggle-hint { font-size: 11px; color: #6b7280; }

  .form-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }

  .dialog-footer {
    display: flex; justify-content: flex-end; gap: 8px;
    padding: 12px 18px; border-top: 1px solid rgba(255, 255, 255, 0.06);
  }
  .btn-primary, .btn-secondary {
    padding: 7px 16px; border-radius: 7px; font-size: 13px; font-weight: 600; cursor: pointer;
  }
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
