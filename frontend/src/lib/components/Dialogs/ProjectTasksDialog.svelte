<script lang="ts">
  /**
   * The project's own tasks and note, reachable from anywhere: the header
   * button, its shortcut and the command palette open it over whatever is on
   * screen.
   *
   * It always shows the active project's list. Switching project while it is
   * open reloads both halves for the new one; an unsaved note first asks, as
   * every note does.
   */
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import DialogCloseButton from '../common/DialogCloseButton.svelte';
  import ProjectWorkspace from '../Dashboard/ProjectWorkspace.svelte';
  import { projects, activeProjectId } from '../../stores/projects';
  import { selectSession, selectWindow } from '../../stores/sessions';
  import { showSessionView } from '../../stores/navigation';
  import { t } from '../../i18n';

  export let show = false;

  $: projectName = $projects.find((project) => project.id === $activeProjectId)?.name || $t('project.default');
  $: title = $t('projectTasks.windowTitle', { project: projectName });

  function close() {
    show = false;
  }

  function handleKeydown(event: KeyboardEvent) {
    // Something inside already answered this Escape — the note's find bar,
    // a task dialog — and only that should close.
    if (event.key !== 'Escape' || event.defaultPrevented) return;
    event.preventDefault();
    event.stopPropagation();
    claimKeyForDialog();
    close();
  }

  // A task sent to an agent is followed there, as the session panel does:
  // the point of sending is to watch the agent pick it up.
  function handleTaskSent(event: CustomEvent<{ sessionId: string; windowIdx?: number }>) {
    close();
    selectSession(event.detail.sessionId);
    if (typeof event.detail.windowIdx === 'number') selectWindow(event.detail.windowIdx);
    showSessionView();
    window.dispatchEvent(new CustomEvent('main-panel:set-view', { detail: { view: 'terminal' } }));
  }
</script>

{#if show}
  <div
    class="dialog-overlay"
    use:autoFocusDialog
    role="dialog"
    aria-modal="true"
    aria-label={title}
    tabindex="-1"
    on:keydown={handleKeydown}
  >
    <div class="dialog-content project-tasks-dialog">
      <div class="dialog-header">
        <h2 title={title}>{title}</h2>
        <DialogCloseButton on:click={close} />
      </div>
      <div class="project-tasks-host">
        <ProjectWorkspace active={show} on:taskSent={handleTaskSent} />
      </div>
    </div>
  </div>
{/if}

<style>
  /* Room for a real list: this is a place to work in, not a prompt. */
  .project-tasks-dialog {
    max-width: min(1040px, calc(100vw - 32px));
    height: min(86vh, 900px);
  }

  .project-tasks-host {
    flex: 1;
    min-height: 0;
    padding: 14px 20px 20px;
  }
</style>
