<script lang="ts">
  /**
   * Every task from every project, ordered by deadline.
   *
   * The per-project task panel answers "what is left here"; this answers "what
   * is due next", which is a different question and cannot be assembled from
   * the panel — tasks live one file per working directory, and the panel only
   * ever sees the active session's.
   */
  import { onMount } from 'svelte';
  import { GetAllTasks } from '../../../../wailsjs/go/main/App';
  import { t } from '../../i18n';
  import { deadlineState } from '../../utils/taskDueDate';
  import { selectSession } from '../../stores/sessions';
  import { showSessionView, goBack } from '../../stores/navigation';
  import Select from '../common/Select.svelte';
  import { isGradient, getGradientCSS } from '../../utils/rowColors';

  type OverviewTask = {
    id: string;
    title: string;
    description?: string;
    details?: string;
    status: string;
    priority: string;
    dueAt?: string;
    projectId: string;
    projectName: string;
    projectPath: string;
    sessionId?: string;
    sessionName?: string;
    sessionColor?: string;
    overdue: boolean;
    dependencies?: string[];
    subtasks?: { id: string; title: string; done: boolean }[];
  };

  let tasks: OverviewTask[] = [];
  let loading = true;
  let error = '';
  let selected: OverviewTask | null = null;

  let projectFilter = 'all';
  let statusFilter = 'open';

  async function load() {
    loading = true;
    error = '';
    try {
      tasks = (await GetAllTasks()) || [];
    } catch (e) {
      error = String(e);
      tasks = [];
    } finally {
      loading = false;
    }
  }

  onMount(load);

  // The default project comes back with an empty name: its label is a
  // translated string, so the backend cannot supply it without picking a
  // language for everyone.
  $: projectLabel = (name: string) => name || $t('project.default');

  // Rebuilt from the loaded set rather than kept as state, so a project whose
  // last task was just completed drops out of the filter by itself.
  $: projects = Array.from(
    new Map(tasks.map((task) => [task.projectId, task.projectName])).entries(),
  ).sort((a, b) => projectLabel(a[1]).localeCompare(projectLabel(b[1])));

  $: projectOptions = [
    { value: 'all', label: $t('allTasks.allProjects') },
    ...projects.map(([id, name]) => ({ value: id, label: projectLabel(name) })),
  ];

  $: statusOptions = [
    { value: 'open', label: $t('allTasks.statusOpen') },
    { value: 'overdue', label: $t('allTasks.statusOverdue') },
    { value: 'all', label: $t('allTasks.statusAll') },
  ];

  $: visible = tasks.filter((task) => {
    if (projectFilter !== 'all' && task.projectId !== projectFilter) return false;
    if (statusFilter === 'open' && task.status === 'done') return false;
    if (statusFilter === 'overdue' && !task.overdue) return false;
    return true;
  });

  /**
   * Tasks grouped by the session they belong to.
   *
   * Measured on this machine: every task file has exactly one session in its
   * directory, so the session IS the natural grouping — a flat list repeated
   * the same session name on every row and told the reader nothing.
   *
   * Built from the filtered set, so a group whose tasks are all filtered out
   * disappears rather than sitting there empty.
   */
  $: groups = (() => {
    const byKey = new Map<string, { key: string; label: string; sessionId?: string; color: string; tasks: OverviewTask[] }>();

    for (const task of visible) {
      // Group on the session when there is one, otherwise on the directory —
      // two sessions in one directory share a task file, and splitting them
      // would show the same task twice under different headings.
      const key = task.sessionId || task.projectPath;
      let group = byKey.get(key);
      if (!group) {
        group = {
          key,
          label: task.sessionName || projectLabel(task.projectName),
          sessionId: task.sessionId,
          color: task.sessionColor || '',
          tasks: [],
        };
        byKey.set(key, group);
      }
      group.tasks.push(task);
    }

    // Groups with something overdue first, then by the soonest deadline in the
    // group: the list should lead with what needs attention.
    return Array.from(byKey.values()).sort((a, b) => {
      const aLate = a.tasks.some((task) => task.overdue);
      const bLate = b.tasks.some((task) => task.overdue);
      if (aLate !== bLate) return aLate ? -1 : 1;

      const soonest = (tasks: OverviewTask[]) =>
        tasks.reduce((best, task) => (task.dueAt && (!best || task.dueAt < best) ? task.dueAt : best), '');
      const aSoon = soonest(a.tasks);
      const bSoon = soonest(b.tasks);
      if (aSoon && bSoon) return aSoon < bSoon ? -1 : 1;
      if (aSoon) return -1;
      if (bSoon) return 1;
      return a.label.localeCompare(b.label);
    });
  })();

  // Collapsed groups, by key. Default is expanded: a view that opens fully
  // folded shows nothing but headings, and the point is to see the tasks.
  let collapsed = new Set<string>();
  function toggleGroup(key: string) {
    collapsed.has(key) ? collapsed.delete(key) : collapsed.add(key);
    collapsed = collapsed; // reassign so Svelte notices the mutation
  }

  // Counted over everything, not over the filtered view: the number says what
  // is waiting, and a filter that hides some of it would make the count agree
  // with the screen while disagreeing with reality.
  $: overdueCount = tasks.filter((task) => task.overdue).length;

  const statusLabels: Record<string, string> = {};
  $: {
    statusLabels['backlog'] = $t('tasks.statusPending');
    statusLabels['in-progress'] = $t('tasks.statusInProgress');
    statusLabels['done'] = $t('tasks.statusDone');
    statusLabels['deferred'] = $t('tasks.statusDeferred');
  }

  const priorityColors: Record<string, string> = {
    critical: '#ef4444',
    high: '#f59e0b',
    medium: '#3b82f6',
    low: '#6b7280',
  };

  function formatDue(value: string | undefined): string {
    if (!value) return '';
    return new Date(value).toLocaleString(undefined, {
      year: 'numeric', month: 'short', day: 'numeric',
      hour: '2-digit', minute: '2-digit',
    });
  }

  /**
   * A dependency's title, from its id.
   *
   * Ids are internal handles and appear nowhere else in the interface, so
   * printing one tells the reader nothing about which task is being waited for.
   * Resolved within the same task file — dependencies never cross projects.
   */
  function dependencyTitle(id: string, task: OverviewTask): string {
    const match = tasks.find(
      (candidate) => candidate.id === id && candidate.projectPath === task.projectPath,
    );
    return match ? match.title : id;
  }

  /** Expanded rows, by task key. Collapsed by default: the list is for scanning. */
  let expanded = new Set<string>();
  function toggleTask(key: string) {
    expanded.has(key) ? expanded.delete(key) : expanded.add(key);
    expanded = expanded; // reassign so Svelte notices the mutation
  }

  /** Open the session the task belongs to, and leave this view for it. */
  async function jumpToSession(task: OverviewTask) {
    if (!task.sessionId) return;
    await selectSession(task.sessionId);
    showSessionView();
  }
</script>

<div class="all-tasks">
  <header class="head">
    <div class="filters">
      <!-- The way back sits with the controls rather than in the app header:
           this is the view's own toolbar, and that is where its actions
           belong. -->
      <button class="back-btn" on:click={goBack} title={$t('allTasks.back')}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M19 12H5"/>
          <path d="M12 19l-7-7 7-7"/>
        </svg>
        {$t('allTasks.back')}
      </button>
      {#if overdueCount > 0}
        <span class="overdue-pill">{$t('allTasks.overdueCount', { count: String(overdueCount) })}</span>
      {/if}
      <Select
        value={projectFilter}
        options={projectOptions}
        on:change={(e) => (projectFilter = e.detail)}
      />
      <Select
        value={statusFilter}
        options={statusOptions}
        on:change={(e) => (statusFilter = e.detail)}
      />
      <button class="refresh" on:click={load} title={$t('common.refresh')} aria-label={$t('common.refresh')}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <path d="M21 12a9 9 0 1 1-3-6.7"/>
          <path d="M21 3v6h-6"/>
        </svg>
      </button>
    </div>
  </header>

  {#if loading}
    <p class="state">{$t('common.loading')}</p>
  {:else if error}
    <p class="state error">{error}</p>
  {:else if visible.length === 0}
    <p class="state">{$t('allTasks.empty')}</p>
  {:else}
    <div class="tree">
      {#each groups as group (group.key)}
        {@const openCount = group.tasks.filter((task) => task.status !== 'done').length}
        {@const lateCount = group.tasks.filter((task) => task.overdue).length}

        <!-- The heading is a row, not one button: the caret folds the group
             and the name opens the session, and a button inside a button is
             not valid markup. -->
        <div class="group-head" class:collapsed={collapsed.has(group.key)}>
          <button
            class="caret-btn"
            on:click={() => toggleGroup(group.key)}
            aria-expanded={!collapsed.has(group.key)}
            aria-label={collapsed.has(group.key) ? $t('allTasks.expandGroup') : $t('allTasks.collapseGroup')}
          >
            <svg class="caret" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
              <path d="M9 18l6-6-6-6"/>
            </svg>
          </button>

          <button
            class="group-name"
            on:click={() => toggleGroup(group.key)}
            style={group.color
              ? (isGradient(group.color)
                  ? `background:${getGradientCSS(group.color)};-webkit-background-clip:text;background-clip:text;color:transparent;`
                  : `color:${group.color}`)
              : ''}
          >{group.label}</button>

          <span class="group-counts">
            {#if lateCount > 0}
              <span class="count late">{$t('allTasks.overdueCount', { count: String(lateCount) })}</span>
            {/if}
            {#if openCount > 0}
              <span class="count">{$t('allTasks.openCount', { count: String(openCount) })}</span>
            {/if}
          </span>

          {#if group.sessionId}
            <button
              class="jump-btn"
              on:click={() => jumpToSession(group.tasks[0])}
              title={$t('allTasks.openSession')}
              aria-label={$t('allTasks.openSession')}
            >
              <!-- Arrow leaving a box: this leaves the list for the session. -->
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>
                <path d="M15 3h6v6"/>
                <path d="M10 14L21 3"/>
              </svg>
            </button>
          {/if}
        </div>

        {#if !collapsed.has(group.key)}
          {#each group.tasks as task (task.id)}
            {@const taskKey = group.key + ':' + task.id}
            {@const subs = task.subtasks || []}

            <div class="task-line">
              <!-- The expander is its own control, so opening the subtasks and
                   opening the task are separate acts. Only drawn when there is
                   something to expand: a caret that does nothing is worse than
                   no caret. -->
              {#if subs.length > 0}
                <button
                  class="sub-caret"
                  class:open={expanded.has(taskKey)}
                  on:click={() => toggleTask(taskKey)}
                  aria-expanded={expanded.has(taskKey)}
                  aria-label={expanded.has(taskKey) ? $t('allTasks.collapseGroup') : $t('allTasks.expandGroup')}
                >
                  <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M9 18l6-6-6-6"/>
                  </svg>
                </button>
              {:else}
                <span class="sub-caret-space"></span>
              {/if}

              <!-- The whole row opens the task: a click target the size of one
                   word is a target people miss. -->
              <button
                class="task-row"
                class:done={task.status === 'done'}
                on:click={() => (selected = task)}
              >
                <span class="due {deadlineState(task.dueAt, task.status)}">
                  {task.dueAt ? formatDue(task.dueAt) : ''}
                </span>
                <span class="name" title={task.title}>
                  <!-- A short dash marks where the title starts, so a row with
                       no deadline does not begin in empty space. -->
                  <span class="bullet">–</span>{task.title}
                  {#if subs.length > 0}
                    <!-- A badge rather than bare text: the count sits at the
                         end of a title of arbitrary length, and as plain text
                         it read as part of the sentence. All done gets a
                         different colour, so a finished checklist is visible
                         without reading the numbers. -->
                    <span
                      class="sub-count"
                      class:complete={subs.every((sub) => sub.done)}
                      title={$t('tasks.subtasks')}
                    >
                      <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M9 11l3 3L20 5"/>
                      </svg>
                      {subs.filter((sub) => sub.done).length}/{subs.length}
                    </span>
                  {/if}
                </span>
                <span class="priority" style="color: {priorityColors[task.priority] || '#6b7280'}">
                  {task.priority}
                </span>
                <span class="status">{statusLabels[task.status] || task.status}</span>
              </button>
            </div>

            {#if expanded.has(taskKey)}
              <ul class="inline-subs">
                {#each subs as subtask (subtask.id)}
                  <li class:done={subtask.done}>
                    <span class="tick">{subtask.done ? '✓' : '○'}</span>
                    {subtask.title}
                  </li>
                {/each}
              </ul>
            {/if}
          {/each}
        {/if}
      {/each}
    </div>
  {/if}
</div>

{#if selected}
  <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
  <div class="overlay" on:click={() => (selected = null)}>
    <div class="detail" on:click|stopPropagation role="dialog" aria-modal="true">
      <h3>{selected.title}</h3>

      <dl class="meta">
        <dt>{$t('allTasks.sessionColumn')}</dt>
        <dd>{selected.sessionName || projectLabel(selected.projectName)}</dd>

        <dt>{$t('tasks.dueAt')}</dt>
        <dd class={deadlineState(selected.dueAt, selected.status)}>
          {selected.dueAt ? formatDue(selected.dueAt) : $t('allTasks.noDeadline')}
        </dd>

        <dt>{$t('tasks.priority')}</dt>
        <dd>{selected.priority}</dd>

        <dt>{$t('allTasks.statusColumn')}</dt>
        <dd>{statusLabels[selected.status] || selected.status}</dd>

        <dt>{$t('allTasks.pathColumn')}</dt>
        <dd class="path">{selected.projectPath}</dd>
      </dl>

      {#if selected.description}
        <p class="body">{selected.description}</p>
      {/if}
      {#if selected.details}
        <span class="details-label">{$t('tasks.implementationDetails')}</span>
        <pre class="body details">{selected.details}</pre>
      {/if}

      {#if selected.subtasks && selected.subtasks.length > 0}
        <span class="details-label">
          {$t('tasks.subtasks')}
          ({selected.subtasks.filter((sub) => sub.done).length}/{selected.subtasks.length})
        </span>
        <ul class="sub-list">
          {#each selected.subtasks as subtask (subtask.id)}
            <li class:done={subtask.done}>
              <!-- Read-only here: this view spans every project, and the task
                   file it would write belongs to another session's panel. -->
              <span class="tick">{subtask.done ? '✓' : '○'}</span>
              {subtask.title}
            </li>
          {/each}
        </ul>
      {/if}

      {#if selected.dependencies && selected.dependencies.length > 0}
        <span class="details-label">{$t('tasks.dependencies')}</span>
        <ul class="sub-list">
          {#each selected.dependencies as dep}
            <li><span class="tick">–</span>{dependencyTitle(dep, selected)}</li>
          {/each}
        </ul>
      {/if}

      <div class="detail-actions">
        {#if selected.sessionId}
          <button class="btn-primary" on:click={() => { const task = selected; selected = null; if (task) jumpToSession(task); }}>
            {$t('allTasks.openSession')}
          </button>
        {/if}
        <button class="btn-cancel" on:click={() => (selected = null)}>{$t('common.close')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .all-tasks {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
    padding: 16px 20px;
    box-sizing: border-box;
  }

  /* No title here: the header already names the view, and repeating it inside
     the page said the same thing twice on one screen. */
  .head {
    display: flex;
    margin-bottom: 12px;
  }

  .filters {
    width: 100%;
  }

  .filters {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .overdue-pill {
    font-size: 12px;
    padding: 3px 9px;
    border-radius: 999px;
    background: rgba(239, 68, 68, 0.15);
    color: #f87171;
    font-weight: 600;
    margin-right: 4px;
  }

  .back-btn {
    display: flex;
    align-items: center;
    gap: 5px;
    padding: 5px 10px;
    font-size: 13px;
    background: transparent;
    border: 1px solid #374151;
    border-radius: 6px;
    color: inherit;
    cursor: pointer;
    white-space: nowrap;
    margin-right: auto;
  }

  .back-btn:hover {
    background: rgba(107, 114, 128, 0.15);
  }

  .refresh {
    background: transparent;
    border: 1px solid #374151;
    color: inherit;
    border-radius: 6px;
    padding: 5px 8px;
    cursor: pointer;
    display: flex;
    align-items: center;
  }

  .refresh:hover {
    background: rgba(107, 114, 128, 0.15);
  }

  .state {
    color: #9ca3af;
    font-size: 14px;
  }

  .state.error {
    color: #f87171;
  }

  .tree {
    overflow-y: auto;
    flex: 1;
    min-height: 0;
  }

  /* A group heading is the session; the tasks under it are indented so the
     hierarchy is visible without lines or guides. */
  .group-head {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    box-sizing: border-box;
    padding: 9px 10px;
    margin-top: 6px;
    border-bottom: 1px solid rgba(107, 114, 128, 0.2);
    font-size: 13px;
    font-weight: 600;
  }

  .group-head:hover {
    background: rgba(107, 114, 128, 0.08);
  }

  .caret-btn {
    display: flex;
    align-items: center;
    padding: 2px;
    background: transparent;
    border: none;
    color: inherit;
    cursor: pointer;
    border-radius: 4px;
    flex-shrink: 0;
  }

  .caret-btn:hover {
    background: rgba(107, 114, 128, 0.2);
  }

  .caret {
    transition: transform 0.12s ease;
    transform: rotate(90deg);
    color: #6b7280;
  }

  .group-head.collapsed .caret {
    transform: rotate(0deg);
  }

  /* The name folds the group, same as the caret — a heading that looks like a
     heading should behave like one. It is a button, so the colour has to be
     stated: a button left alone takes the browser's default black, which on
     this background is invisible, and that is exactly how a session with no
     colour of its own disappeared. */
  .group-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: inherit;
    background: transparent;
    border: none;
    padding: 0;
    color: inherit;
    cursor: pointer;
    text-align: left;
  }

  /* Opens the session this group belongs to. Sits at the end of the row, past
     the counts, so it never competes with the caret for the same click. */
  .jump-btn {
    display: flex;
    align-items: center;
    padding: 4px;
    margin-left: 8px;
    background: transparent;
    border: none;
    /* Without this a button takes the browser's default text colour, which
       rendered a session with no colour of its own in black on a dark
       background — invisible. */
    color: #6b7280;
    cursor: pointer;
    border-radius: 4px;
    flex-shrink: 0;
  }

  .jump-btn:hover {
    background: rgba(107, 114, 128, 0.2);
    color: #d1d5db;
  }

  .group-counts {
    margin-left: auto;
    display: flex;
    gap: 6px;
    flex-shrink: 0;
  }

  .count {
    font-size: 11px;
    font-weight: 500;
    padding: 2px 8px;
    border-radius: 999px;
    background: rgba(107, 114, 128, 0.18);
    color: #9ca3af;
  }

  .count.late {
    background: rgba(239, 68, 68, 0.16);
    color: #f87171;
    font-weight: 600;
  }

  /* One grid for every task row, so the columns line up down the whole tree
     rather than each row sizing itself to its own text. */
  .task-row {
    display: grid;
    /* The deadline column sizes to its content rather than to a fixed width.
       At 150px a column holding nothing but a dash left a gap wider than the
       task titles beside it — measured on screen, most tasks have no deadline
       at all. min-content keeps the dates aligned without reserving space for
       dates that are not there. */
    grid-template-columns: minmax(0, max-content) minmax(0, 1fr) 80px 110px;
    gap: 16px;
    align-items: center;
    width: 100%;
    box-sizing: border-box;
    padding: 7px 10px 7px 0;
    flex: 1;
    min-width: 0;
    background: transparent;
    border: none;
    color: inherit;
    font-family: inherit;
    font-size: 13px;
    text-align: left;
    cursor: pointer;
  }

  .task-line {
    display: flex;
    align-items: center;
  }

  .sub-caret,
  .sub-caret-space {
    width: 20px;
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    margin-left: 10px;
  }

  .sub-caret {
    padding: 3px;
    background: transparent;
    border: none;
    color: #6b7280;
    cursor: pointer;
    border-radius: 3px;
  }

  .sub-caret svg {
    transition: transform 0.12s ease;
  }

  .sub-caret.open svg {
    transform: rotate(90deg);
  }

  .sub-caret:hover {
    background: rgba(107, 114, 128, 0.2);
    color: #d1d5db;
  }

  .bullet {
    color: #6b7280;
    margin-right: 6px;
  }

  .sub-count {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-size: 10px;
    font-variant-numeric: tabular-nums;
    padding: 1px 6px;
    border-radius: 999px;
    background: rgba(107, 114, 128, 0.18);
    color: #9ca3af;
    margin-left: 8px;
    vertical-align: middle;
  }

  .sub-count.complete {
    background: rgba(74, 222, 128, 0.15);
    color: #4ade80;
  }

  .inline-subs {
    list-style: none;
    margin: 0;
    padding: 0 0 6px 60px;
    font-size: 12px;
  }

  .inline-subs li {
    padding: 2px 0;
    color: #9ca3af;
  }

  .inline-subs li.done {
    color: #6b7280;
    text-decoration: line-through;
  }

  .task-row:hover {
    background: rgba(107, 114, 128, 0.1);
  }

  .task-row.done .name {
    text-decoration: line-through;
    opacity: 0.55;
  }

  /* Colour carries urgency, but the date is always spelled out: colour alone
     excludes anyone who cannot tell these two apart. */
  .due {
    color: #9ca3af;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }

  .due.soon {
    color: #fbbf24;
  }

  .due.overdue {
    color: #f87171;
    font-weight: 600;
  }

  .name,
  .priority,
  .status {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .status {
    color: #9ca3af;
    font-size: 12px;
  }

  .priority {
    font-size: 12px;
  }

  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
  }

  .detail {
    background: var(--bg-secondary, #1e293b);
    border: 1px solid rgba(107, 114, 128, 0.3);
    border-radius: 10px;
    padding: 20px 22px;
    width: min(560px, 90vw);
    max-height: 80vh;
    overflow-y: auto;
  }

  .detail h3 {
    margin: 0 0 16px;
    font-size: 16px;
  }

  .meta {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 6px 14px;
    margin: 0 0 16px;
    font-size: 13px;
  }

  .meta dt {
    color: #6b7280;
    font-size: 12px;
  }

  .meta dd {
    margin: 0;
  }

  .meta dd.soon {
    color: #fbbf24;
  }

  .meta dd.overdue {
    color: #f87171;
    font-weight: 600;
  }

  .meta dd.path {
    font-family: monospace;
    font-size: 12px;
    word-break: break-all;
  }

  .body {
    font-size: 13px;
    line-height: 1.55;
    color: #d1d5db;
    margin: 0 0 12px;
    white-space: pre-wrap;
  }

  .details-label {
    display: block;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: #6b7280;
    margin-bottom: 4px;
  }

  .details {
    font-family: monospace;
    font-size: 12px;
    background: rgba(0, 0, 0, 0.25);
    padding: 10px;
    border-radius: 6px;
    overflow-x: auto;
  }

  .sub-list {
    list-style: none;
    margin: 0 0 14px;
    padding: 0;
    font-size: 13px;
  }

  .sub-list li {
    padding: 3px 0;
    color: #d1d5db;
  }

  .sub-list li.done {
    color: #6b7280;
    text-decoration: line-through;
  }

  .tick {
    display: inline-block;
    width: 16px;
    color: #6b7280;
  }

  .detail-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 4px;
  }

  .btn-primary,
  .btn-cancel {
    padding: 7px 14px;
    border-radius: 6px;
    font-size: 13px;
    cursor: pointer;
    border: 1px solid transparent;
  }

  .btn-primary {
    background: var(--accent, #3b82f6);
    color: #fff;
  }

  .btn-cancel {
    background: transparent;
    border-color: #374151;
    color: inherit;
  }
</style>
