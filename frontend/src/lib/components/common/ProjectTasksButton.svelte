<script lang="ts">
  /**
   * The header's way into the project's own tasks and note, with the number
   * of its open tasks — the project list's counterpart of the all-tasks
   * button beside it.
   *
   * Its own component, so the badge and the toggle can be exercised without
   * the whole app around them. It matches the header's other icon buttons.
   */
  import { t } from '../../i18n';
  import { projectOpenTaskCount, projectTasksOpen } from '../../stores/projectTasks';
  import { effectiveBindings, formatBinding } from '../../stores/shortcuts';

  const isMac = typeof navigator !== 'undefined' && /Mac/i.test(navigator.platform);
  // The key follows a rebinding, like the help does; a shortcut switched off
  // is left out rather than shown as a key that does nothing.
  $: keys = ($effectiveBindings.get('projectTasks.open') || []).map((binding) => formatBinding(binding, isMac)).join(' / ');
  $: label = keys ? `${$t('projectTasks.title')} (${keys})` : $t('projectTasks.title');
</script>

<button
  class="project-tasks-button"
  class:active-view={$projectTasksOpen}
  aria-pressed={$projectTasksOpen}
  on:click={() => projectTasksOpen.update((open) => !open)}
  title={label}
  aria-label={label}
>
  <!-- A clipboard: a list kept for the whole project, where the all-tasks
       button beside it is a checkbox. -->
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <rect x="5" y="4" width="14" height="17" rx="2"/>
    <path d="M9 4V3h6v1"/>
    <path d="M9 10h6M9 14h6M9 18h3"/>
  </svg>
  {#if $projectOpenTaskCount > 0}
    <span class="count-badge">{$projectOpenTaskCount > 99 ? '99+' : $projectOpenTaskCount}</span>
  {/if}
</button>

<style>
  /* The header's .btn .btn-ghost .btn-icon, which are App's own styles. */
  .project-tasks-button {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    margin: 2px 0;
    padding: 0;
    border-radius: 8px;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.1);
    color: #a1a1aa;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .project-tasks-button:hover {
    background: rgba(255, 255, 255, 0.1);
    border-color: rgba(255, 255, 255, 0.2);
    color: white;
  }

  .project-tasks-button.active-view {
    color: var(--accent-lighter);
    background: rgba(var(--accent-rgb), 0.15);
    border-color: rgba(var(--accent-rgb), 0.3);
  }

  /* The all-tasks button's badge: in the corner, neutral — outstanding work
     is not an error. */
  .count-badge {
    position: absolute;
    top: 1px;
    right: 1px;
    min-width: 13px;
    height: 13px;
    padding: 0 3px;
    box-sizing: border-box;
    border-radius: 999px;
    background: rgba(107, 114, 128, 0.95);
    color: #fff;
    font-size: 9px;
    font-weight: 600;
    line-height: 13px;
    text-align: center;
    box-shadow: 0 0 0 1.5px var(--bg-primary, #0f172a);
    pointer-events: none;
  }
</style>
