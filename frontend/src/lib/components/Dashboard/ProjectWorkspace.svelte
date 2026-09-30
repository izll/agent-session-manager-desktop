<script lang="ts">
  /**
   * The project's own tasks and note, side by side behind one switch.
   *
   * Shown on the dashboard and in the project tasks window. The two halves are
   * the session panels in their project mode rather than new ones, so the
   * project list has everything a session's list has — priorities, deadlines,
   * subtasks, dependencies, undo — and the note autosaves and guards unsaved
   * drafts the same way.
   *
   * Both halves stay mounted while the other is shown: switching must not
   * drop a note draft that is still being saved.
   */
  import TaskPanel from '../MainPanel/TaskPanel.svelte';
  import Notes from '../MainPanel/Notes.svelte';
  import { t } from '../../i18n';
  import { projectTasksView, projectOpenTaskCount } from '../../stores/projectTasks';

  export let active = true;
</script>

<div class="project-workspace">
  <div class="workspace-switch" role="tablist" aria-label={$t('projectTasks.title')}>
    <button
      type="button"
      role="tab"
      aria-selected={$projectTasksView === 'tasks'}
      class:selected={$projectTasksView === 'tasks'}
      on:click={() => projectTasksView.set('tasks')}
    >
      {$t('projectTasks.tasksTab')}
      {#if $projectOpenTaskCount > 0}<span class="switch-count">{$projectOpenTaskCount}</span>{/if}
    </button>
    <button
      type="button"
      role="tab"
      aria-selected={$projectTasksView === 'notes'}
      class:selected={$projectTasksView === 'notes'}
      on:click={() => projectTasksView.set('notes')}
    >{$t('projectTasks.notesTab')}</button>
  </div>
  <div class="workspace-panel" class:shown={$projectTasksView === 'tasks'}>
    <TaskPanel project active={active && $projectTasksView === 'tasks'} on:taskSent />
  </div>
  <div class="workspace-panel" class:shown={$projectTasksView === 'notes'}>
    <Notes project active={active && $projectTasksView === 'notes'} />
  </div>
</div>

<style>
  .project-workspace {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }

  /* The same two-state switch the notes use for tab / session. */
  .workspace-switch {
    display: inline-flex;
    align-self: flex-start;
    flex-shrink: 0;
    margin: 0 0 10px;
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 6px;
    overflow: hidden;
  }
  .workspace-switch button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 5px 14px;
    border: none;
    background: transparent;
    color: #9ca3af;
    font-size: 12px;
    cursor: pointer;
    white-space: nowrap;
  }
  .workspace-switch button + button {
    border-left: 1px solid rgba(255, 255, 255, 0.1);
  }
  .workspace-switch button:hover {
    color: #e4e4e7;
  }
  .workspace-switch button.selected {
    background: rgba(var(--accent-rgb), 0.18);
    color: var(--accent-pale, #e4e4e7);
  }
  .switch-count {
    padding: 0 6px;
    border-radius: 999px;
    background: rgba(var(--accent-rgb), 0.25);
    font-size: 11px;
  }

  .workspace-panel {
    display: none;
    flex: 1;
    min-height: 0;
    border: 1px solid rgba(255, 255, 255, 0.06);
    border-radius: 10px;
    overflow: hidden;
  }
  .workspace-panel.shown {
    display: block;
  }
</style>
