<script lang="ts">
  import { onMount, onDestroy, createEventDispatcher } from 'svelte';
  import { get } from 'svelte/store';
  import { selectedSessionId } from '../../stores/sessions';
  import Select from '../common/Select.svelte';
  import ConfirmDialog from '../Dialogs/ConfirmDialog.svelte';
  import { createFieldDictation } from '../../utils/dictationField';
  import * as DictationService from '../../../../wailsjs/go/main/DictationService';
  import { EventsOn } from '../../../../wailsjs/runtime/runtime';
  import { t } from '../../i18n';
  import {
    tasks,
    taskFilter,
    sortedFilteredTasks,
    taskStats,
    selectedTaskId,
    isLoadingTasks,
    taskError,
    taskMasterStatus,
    useMCPMode,
    loadTasks,
    checkTaskMasterStatus,
    initializeTaskMaster,
    parsePRD,
    addTask,
    addManualTask,
    updateTask,
    updateTaskDirect,
    removeTask,
    setTaskStatus,
    expandTask,
    expandAllTasks,
    analyzeComplexity,
    sendTaskToAgent,
    selectTask,
    setTaskFilter,
    getNextTask,
    addSubtask,
    removeSubtask,
    clearSubtasks,
    setSubtaskStatus,
    addDependency,
    removeDependency,
    taskSortBy,
    setTaskSortBy,
    hideDone,
    toggleHideDone,
    type Task,
    type TaskStatus,
    type TaskPriority,
    type TaskSortBy,
    type Subtask
  } from '../../stores/tasks';

  export let active = false;

  const dispatch = createEventDispatcher();

  let lastSessionId: string | null = null;
  let taskPanelLoadGeneration = 0;
  let showPRDModal = false;
  let showAddTaskModal = false;
  let showComplexityModal = false;
  let complexityReport = '';

  // PRD form
  let prdContent = '';
  let prdNumTasks = 10;

  // Add task form
  let newTaskPrompt = '';
  let newTaskPriority: TaskPriority = 'medium';
  let newTaskResearch = true;
  let useManualMode = true; // Default to manual mode (no API key required)
  let newTaskTitle = '';
  let newTaskDescription = '';
  let newTaskDetails = '';

  // Context menu
  let contextMenuTask: Task | null = null;
  let contextMenuX = 0;
  let contextMenuY = 0;

  // Edit task modal
  let showEditTaskModal = false;
  let editTaskId = '';
  let editTaskTitle = '';
  let editTaskDescription = '';
  let editTaskDetails = '';
  let editTaskPriority: TaskPriority = 'medium';
  let editTaskError = '';

  // Add subtask modal
  let showAddSubtaskModal = false;
  let addSubtaskTaskId = '';
  let newSubtaskTitle = '';
  let newSubtaskDescription = '';

  // Edit subtask modal
  let showEditSubtaskModal = false;
  let editSubtaskId = '';
  let editSubtaskTitle = '';
  let editSubtaskDescription = '';

  // Delete confirm dialog
  let showDeleteConfirm = false;
  let deleteTaskId = '';
  let deleteTaskTitle = '';

  // Remove subtask confirm dialog
  let showRemoveSubtaskConfirm = false;
  let removeSubtaskId = '';

  // Dependency modal
  let showDependencyModal = false;
  let dependencyTaskId = '';
  let newDependencyId = '';

  function handleSortChange(event: CustomEvent<string>) {
    setTaskSortBy(event.detail as TaskSortBy);
  }

  function handleStatusFilterChange(event: CustomEvent<string>) {
    setTaskFilter({ status: event.detail as TaskStatus | 'all' });
  }

  function handleNewPriorityChange(event: CustomEvent<string>) {
    newTaskPriority = event.detail as TaskPriority;
  }

  function handleEditPriorityChange(event: CustomEvent<string>) {
    editTaskPriority = event.detail as TaskPriority;
  }

  // Dictation support - one controller, follows focused field in dialog
  let activeDictationEl: HTMLTextAreaElement | HTMLInputElement | null = null;
  const dictation = createFieldDictation(() => {
    // Always prefer currently focused field in dialog (allows switching fields mid-dictation)
    const active = document.activeElement;
    if (active && (active instanceof HTMLTextAreaElement || (active instanceof HTMLInputElement && active.type === 'text'))) {
      const inDialog = active.closest('.dialog-content');
      if (inDialog) {
        activeDictationEl = active as HTMLTextAreaElement | HTMLInputElement;
        return activeDictationEl;
      }
    }
    // Fallback to last known active field (e.g. when terminal steals focus)
    return activeDictationEl;
  });
  const dictationListening = dictation.listening;

  // Element refs for dictation
  let addTitleEl: HTMLInputElement;
  let addDescEl: HTMLTextAreaElement;
  let addDetailsEl: HTMLTextAreaElement;
  let addPromptEl: HTMLTextAreaElement;
  let editTitleEl: HTMLInputElement;
  let editDescEl: HTMLTextAreaElement;
  let editDetailsEl: HTMLTextAreaElement;
  let subtaskTitleEl: HTMLInputElement;
  let subtaskDescEl: HTMLTextAreaElement;

  // Track focused field during dictation for immediate visual feedback
  function handleDialogFocusIn(e: FocusEvent) {
    if (!$dictationListening) return;
    const target = e.target;
    if (target instanceof HTMLTextAreaElement || (target instanceof HTMLInputElement && target.type === 'text')) {
      activeDictationEl = target;
    }
  }

  async function toggleModalDictation() {
    if ($dictationListening) {
      await dictation.stop();
      activeDictationEl = null;
    } else {
      await dictation.toggle();
    }
  }

  // When a modal with form fields opens, preemptively set dictation target to 'field'
  // so hotkey also routes to form fields. When modal closes, restore to 'terminal'.
  let modalFieldCleanup: (() => void) | null = null;

  function setupModalFieldTarget() {
    if (modalFieldCleanup) return; // already set up
    DictationService.SetDictationTarget('field').catch(() => {});

    // Listen for hotkey-triggered dictation (state changes we didn't initiate)
    const unsubState = EventsOn('dictation:state', (isListening: boolean) => {
      if (isListening && !$dictationListening) {
        // Hotkey started dictation while modal is open - set up field listeners
        // Use the currently focused field; fallback to first field in modal
        if (!activeDictationEl) {
          const active = document.activeElement;
          if (active && (active instanceof HTMLTextAreaElement || (active instanceof HTMLInputElement && active.type === 'text'))) {
            const inDialog = active.closest('.dialog-content');
            if (inDialog) activeDictationEl = active as HTMLTextAreaElement | HTMLInputElement;
          }
          if (!activeDictationEl) {
            if (showAddTaskModal) activeDictationEl = addTitleEl || addDescEl;
            else if (showEditTaskModal) activeDictationEl = editTitleEl || editDescEl;
            else if (showAddSubtaskModal) activeDictationEl = subtaskTitleEl || subtaskDescEl;
          }
        }
        dictation.startExternally();
      } else if (!isListening && $dictationListening) {
        // Stopped externally - clean up listeners without toggling
        dictation.stopExternally();
        activeDictationEl = null;
        // Re-set field target since modal is still open (for next hotkey press)
        DictationService.SetDictationTarget('field').catch(() => {});
      }
    });

    modalFieldCleanup = () => {
      unsubState();
      DictationService.SetDictationTarget('terminal').catch(() => {});
    };
  }

  function cleanupModalFieldTarget() {
    if ($dictationListening) {
      dictation.stop();
      activeDictationEl = null;
    }
    if (modalFieldCleanup) {
      modalFieldCleanup();
      modalFieldCleanup = null;
    }
  }

  // Set up / clean up field target when modals open/close
  $: if (showAddTaskModal || showEditTaskModal || showAddSubtaskModal) {
    setupModalFieldTarget();
  } else {
    cleanupModalFieldTarget();
  }

  onDestroy(() => {
    dictation.destroy();
    cleanupModalFieldTarget();
  });

  onMount(() => {
    loadTasksIfNeeded();
  });

  async function loadTasksIfNeeded(force = false) {
    const sessionId = get(selectedSessionId);
    if (!sessionId) {
      taskPanelLoadGeneration++;
      await loadTasks('');
      return;
    }

    if (!force && sessionId === lastSessionId) return;
    const generation = ++taskPanelLoadGeneration;
    lastSessionId = sessionId;

    await checkTaskMasterStatus(sessionId);
    if (generation !== taskPanelLoadGeneration || sessionId !== get(selectedSessionId)) return;
    await loadTasks(sessionId);
  }

  // Reload when tab becomes active
  let wasActive = false;
  $: if (active && !wasActive) {
    wasActive = true;
    loadTasksIfNeeded(true);
  } else if (!active) {
    wasActive = false;
  }

  // Watch for session changes
  $: if ($selectedSessionId !== lastSessionId) {
    loadTasksIfNeeded();
  }

  // Priority colors
  const priorityColors: Record<TaskPriority, string> = {
    critical: '#ef4444',
    high: '#f97316',
    medium: '#eab308',
    low: '#22c55e'
  };

  const priorityLabels: Record<TaskPriority, string> = {
    critical: 'Critical',
    high: 'High',
    medium: 'Medium',
    low: 'Low'
  };

  const statusLabels: Record<string, string> = {
    pending: 'Pending',
    'in-progress': 'In Progress',
    done: 'Done',
    blocked: 'Blocked',
    deferred: 'Deferred'
  };

  const statusColors: Record<string, string> = {
    pending: '#9ca3af',
    'in-progress': '#3b82f6',
    done: '#22c55e',
    blocked: '#ef4444',
    deferred: '#6b7280'
  };

  // Initialize Task Master
  async function handleInit() {
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;

    try {
      await initializeTaskMaster(sessionId);
    } catch (e) {
      console.error('Failed to initialize:', e);
    }
  }

  // Parse PRD
  async function handleParsePRD() {
    const sessionId = get(selectedSessionId);
    if (!sessionId || !prdContent.trim()) return;

    try {
      await parsePRD(sessionId, prdContent, prdNumTasks);
      showPRDModal = false;
      prdContent = '';
    } catch (e) {
      console.error('Failed to parse PRD:', e);
    }
  }

  // Add task
  async function handleAddTask() {
    console.log('[TaskPanel] handleAddTask called');
    const sessionId = get(selectedSessionId);
    console.log('[TaskPanel] sessionId:', sessionId);
    if (!sessionId) {
      console.log('[TaskPanel] No sessionId, returning early');
      return;
    }

    try {
      if (useManualMode) {
        // Manual mode - no AI required
        console.log('[TaskPanel] Manual mode, title:', newTaskTitle);
        if (!newTaskTitle.trim()) {
          console.log('[TaskPanel] Empty title, returning early');
          return;
        }
        console.log('[TaskPanel] Calling addManualTask...');
        await addManualTask(sessionId, newTaskTitle, newTaskDescription, newTaskDetails, newTaskPriority);
        console.log('[TaskPanel] addManualTask completed');
        newTaskTitle = '';
        newTaskDescription = '';
        newTaskDetails = '';
      } else {
        // AI mode - requires API key
        console.log('[TaskPanel] AI mode, prompt:', newTaskPrompt);
        if (!newTaskPrompt.trim()) {
          console.log('[TaskPanel] Empty prompt, returning early');
          return;
        }
        console.log('[TaskPanel] Calling addTask...');
        await addTask(sessionId, newTaskPrompt, newTaskResearch, newTaskPriority);
        console.log('[TaskPanel] addTask completed');
        newTaskPrompt = '';
      }
      showAddTaskModal = false;
      console.log('[TaskPanel] Modal closed');
    } catch (e) {
      console.error('[TaskPanel] Failed to add task:', e);
    }
  }

  // Delete task
  function handleDeleteTask(taskId: string) {
    const task = $tasks.find(t => t.id === taskId);
    deleteTaskId = taskId;
    deleteTaskTitle = task?.title || taskId;
    showDeleteConfirm = true;
    contextMenuTask = null;
  }

  async function confirmDeleteTask() {
    const sessionId = get(selectedSessionId);
    if (!sessionId || !deleteTaskId) return;
    await removeTask(sessionId, deleteTaskId);
    deleteTaskId = '';
    deleteTaskTitle = '';
  }

  // Move task status
  async function handleMoveTask(taskId: string, newStatus: TaskStatus) {
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;

    await setTaskStatus(sessionId, taskId, newStatus);
    contextMenuTask = null;
  }

  // Expand task
  async function handleExpandTask(taskId: string) {
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;

    try {
      await expandTask(sessionId, taskId, true, false);
    } catch (e) {
      console.error('Failed to expand task:', e);
    }
    contextMenuTask = null;
  }

  // Expand all
  async function handleExpandAll() {
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;

    try {
      await expandAllTasks(sessionId, true);
    } catch (e) {
      console.error('Failed to expand all:', e);
    }
  }

  // Analyze complexity
  async function handleAnalyzeComplexity() {
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;

    try {
      complexityReport = await analyzeComplexity(sessionId, true);
      showComplexityModal = true;
    } catch (e) {
      console.error('Failed to analyze complexity:', e);
    }
  }

  // Get next task
  async function handleGetNextTask() {
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;

    const task = await getNextTask(sessionId);
    if (task) {
      selectTask(task.id);
    }
  }

  // Send to agent
  async function handleSendToAgent(taskId: string) {
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;

    try {
      await sendTaskToAgent(sessionId, taskId);
      dispatch('taskSent', { taskId });
    } catch (e) {
      console.error('Failed to send task to agent:', e);
    }
    contextMenuTask = null;
  }

  // Context menu
  function showContextMenu(event: MouseEvent, task: Task) {
    event.preventDefault();
    contextMenuTask = task;
    contextMenuX = event.clientX;
    contextMenuY = event.clientY;
  }

  function closeContextMenu() {
    contextMenuTask = null;
  }

  // Open edit task modal
  function openEditTaskModal(task: Task) {
    editTaskId = task.id;
    editTaskTitle = task.title;
    editTaskDescription = task.description || '';
    editTaskDetails = task.details || '';
    editTaskPriority = task.priority;
    editTaskError = '';
    showEditTaskModal = true;
    contextMenuTask = null;
  }

  // Save edited task
  async function handleSaveEditTask() {
    console.log('[TaskPanel] handleSaveEditTask called');
    const sessionId = get(selectedSessionId);
    console.log('[TaskPanel] sessionId:', sessionId, 'editTaskId:', editTaskId);
    if (!sessionId || !editTaskId) return;

    editTaskError = '';
    try {
      console.log('[TaskPanel] calling updateTaskDirect...', { editTaskTitle, editTaskDescription, editTaskDetails, editTaskPriority });
      await updateTaskDirect(sessionId, editTaskId, editTaskTitle, editTaskDescription, editTaskDetails, editTaskPriority);
      console.log('[TaskPanel] updateTaskDirect success');
      showEditTaskModal = false;
    } catch (e) {
      console.error('[TaskPanel] Failed to update task:', e);
      editTaskError = String(e);
    }
  }

  // Open add subtask modal
  function openAddSubtaskModal(taskId: string) {
    addSubtaskTaskId = taskId;
    newSubtaskTitle = '';
    newSubtaskDescription = '';
    showAddSubtaskModal = true;
  }

  // Add subtask
  async function handleAddSubtask() {
    const sessionId = get(selectedSessionId);
    if (!sessionId || !addSubtaskTaskId || !newSubtaskTitle.trim()) return;

    try {
      await addSubtask(sessionId, addSubtaskTaskId, newSubtaskTitle, newSubtaskDescription);
      showAddSubtaskModal = false;
    } catch (e) {
      console.error('Failed to add subtask:', e);
    }
  }

  // Toggle subtask status
  async function handleToggleSubtaskStatus(subtaskId: string, currentStatus: string) {
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;

    const newStatus = currentStatus === 'done' ? 'pending' : 'done';
    try {
      await setSubtaskStatus(sessionId, subtaskId, newStatus as TaskStatus);
    } catch (e) {
      console.error('Failed to toggle subtask status:', e);
    }
  }

  // Remove subtask
  function handleRemoveSubtask(subtaskId: string) {
    removeSubtaskId = subtaskId;
    showRemoveSubtaskConfirm = true;
  }

  async function confirmRemoveSubtask() {
    const sessionId = get(selectedSessionId);
    if (!sessionId || !removeSubtaskId) return;
    try {
      await removeSubtask(sessionId, removeSubtaskId);
    } catch (e) {
      console.error('Failed to remove subtask:', e);
    }
    removeSubtaskId = '';
  }

  // Open dependency modal
  function openDependencyModal(taskId: string) {
    dependencyTaskId = taskId;
    newDependencyId = '';
    showDependencyModal = true;
  }

  // Add dependency
  async function handleAddDependency() {
    const sessionId = get(selectedSessionId);
    if (!sessionId || !dependencyTaskId || !newDependencyId.trim()) return;

    try {
      await addDependency(sessionId, dependencyTaskId, newDependencyId);
      newDependencyId = '';
    } catch (e) {
      console.error('Failed to add dependency:', e);
    }
  }

  // Remove dependency
  async function handleRemoveDependency(taskId: string, depId: string) {
    const sessionId = get(selectedSessionId);
    if (!sessionId) return;

    try {
      await removeDependency(sessionId, taskId, depId);
    } catch (e) {
      console.error('Failed to remove dependency:', e);
    }
  }

  // Get task by ID for dependency dropdown
  function getTaskById(id: string): Task | undefined {
    return $tasks.find(t => t.id === id);
  }

  function formatRelativeDate(dateStr: string): string {
    const date = new Date(dateStr);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMin = Math.floor(diffMs / 60000);
    const diffHr = Math.floor(diffMs / 3600000);
    const diffDays = Math.floor(diffMs / 86400000);

    if (diffMin < 1) return $t('tasks.timeJustNow');
    if (diffMin < 60) return $t('tasks.timeMinAgo', { n: diffMin });
    if (diffHr < 24) return $t('tasks.timeHourAgo', { n: diffHr });
    if (diffDays < 7) return $t('tasks.timeDayAgo', { n: diffDays });
    return date.toLocaleDateString();
  }

  function handleGlobalClick() {
    if (contextMenuTask) {
      closeContextMenu();
    }
  }
</script>

<svelte:window on:click={handleGlobalClick} />

<div class="task-panel">
  <div class="task-header">
    <div class="header-left">
      <span class="task-title">{$t('tasks.title')}</span>
      {#if $taskStats.total > 0}
        <span class="task-count">
          {$taskStats.done}/{$taskStats.total}
        </span>
      {/if}
      {#if $taskMasterStatus.running}
        <span class="mcp-badge">{$t('tasks.mcp')}</span>
      {/if}
    </div>
    <div class="header-right">
      <button
        class="hide-done-btn"
        class:active={$hideDone}
        on:click={toggleHideDone}
        title={$hideDone ? $t('tasks.showDone') : $t('tasks.hideDone')}
      >
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          {#if $hideDone}
            <path d="M17.94 17.94A10.07 10.07 0 0112 20c-7 0-11-8-11-8a18.45 18.45 0 015.06-5.94M9.9 4.24A9.12 9.12 0 0112 4c7 0 11 8 11 8a18.5 18.5 0 01-2.16 3.19m-6.72-1.07a3 3 0 11-4.24-4.24"/>
            <line x1="1" y1="1" x2="23" y2="23"/>
          {:else}
            <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/>
            <circle cx="12" cy="12" r="3"/>
          {/if}
        </svg>
      </button>
      <Select
        small
        value={$taskSortBy}
        options={[
          { value: 'priority', label: $t('tasks.sortPriority') },
          { value: 'status', label: $t('tasks.sortStatus') },
          { value: 'created-desc', label: $t('tasks.sortCreatedDesc') },
          { value: 'created-asc', label: $t('tasks.sortCreatedAsc') }
        ]}
        on:change={handleSortChange}
      />
      <Select
        small
        value={$taskFilter.status}
        options={[
          { value: 'all', label: 'All Status' },
          { value: 'pending', label: 'Pending' },
          { value: 'in-progress', label: 'In Progress' },
          { value: 'done', label: 'Done' },
          { value: 'blocked', label: 'Blocked' }
        ]}
        on:change={handleStatusFilterChange}
      />
    </div>
  </div>

  <!-- Action Bar -->
  <div class="action-bar">
    {#if !$taskMasterStatus.running}
      <button class="action-btn init" on:click={handleInit} disabled={$isLoadingTasks}>
        {$t('tasks.initialize')}
      </button>
    {:else}
      <button class="action-btn" on:click={() => showPRDModal = true} disabled={$isLoadingTasks}>
        {$t('tasks.parsePRD')}
      </button>
      <button class="action-btn" on:click={() => showAddTaskModal = true} disabled={$isLoadingTasks}>
        {$t('tasks.addTask')}
      </button>
      <button class="action-btn" on:click={handleExpandAll} disabled={$isLoadingTasks}>
        {$t('tasks.expandAll')}
      </button>
      <button class="action-btn" on:click={handleAnalyzeComplexity} disabled={$isLoadingTasks}>
        {$t('tasks.analyze')}
      </button>
      <button class="action-btn next" on:click={handleGetNextTask} disabled={$isLoadingTasks}>
        {$t('tasks.nextTask')}
      </button>
    {/if}
  </div>

  {#if $taskError}
    <div class="error-banner">
      {$taskError}
    </div>
  {/if}

  <div class="task-list">
    {#if $isLoadingTasks}
      <div class="loading">{$t('tasks.loading')}</div>
    {:else if $sortedFilteredTasks.length === 0}
      <div class="empty">
        {#if !$taskMasterStatus.running}
          {$t('tasks.initHint')}
        {:else if $tasks.length === 0}
          {$t('tasks.noTasks')}
        {:else}
          {$t('tasks.noMatch')}
        {/if}
      </div>
    {:else}
      {#each $sortedFilteredTasks as task (task.id)}
        <div
          class="task-item"
          class:selected={$selectedTaskId === task.id}
          class:done={task.status === 'done'}
          on:click={() => selectTask(task.id === $selectedTaskId ? null : task.id)}
          on:contextmenu={(e) => showContextMenu(e, task)}
          on:keydown={(e) => e.key === 'Enter' && selectTask(task.id)}
          role="button"
          tabindex="0"
        >
          <div class="task-main">
            <button
              class="task-checkbox"
              class:checked={task.status === 'done'}
              on:click|stopPropagation={() => handleMoveTask(task.id, task.status === 'done' ? 'pending' : 'done')}
              title={task.status === 'done' ? 'Mark as pending' : 'Mark as done'}
            >
              {#if task.status === 'done'}
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">
                  <polyline points="20 6 9 17 4 12"/>
                </svg>
              {/if}
            </button>
            <div class="task-id">{task.id}</div>
            <div class="task-content">
              <div class="task-title-row">
                <span class="task-name" class:completed={task.status === 'done'}>{task.title}</span>
                <span
                  class="priority-badge"
                  style="background: {priorityColors[task.priority] || '#9ca3af'}20; color: {priorityColors[task.priority] || '#9ca3af'}"
                >
                  {priorityLabels[task.priority] || task.priority}
                </span>
                <span
                  class="status-badge"
                  style="background: {statusColors[task.status] || '#9ca3af'}20; color: {statusColors[task.status] || '#9ca3af'}"
                >
                  {statusLabels[task.status] || task.status}
                </span>
                {#if task.createdAt}
                  <span class="created-at" title={new Date(task.createdAt).toLocaleString()}>
                    {formatRelativeDate(task.createdAt)}
                  </span>
                {/if}
              </div>

              {#if task.complexity}
                <span class="complexity-badge" title={$t('tasks.complexityScore')}>
                  C:{task.complexity}
                </span>
              {/if}

              {#if task.description && $selectedTaskId === task.id}
                <p class="task-description">{task.description}</p>
              {/if}

              {#if task.details && $selectedTaskId === task.id}
                <p class="task-details">{task.details}</p>
              {/if}

              {#if task.tags && task.tags.length > 0}
                <div class="task-tags">
                  {#each task.tags as tag}
                    <span class="tag">{tag}</span>
                  {/each}
                </div>
              {/if}
            </div>
          </div>

          {#if $selectedTaskId === task.id}
            <div class="task-details-panel">
              <!-- Subtasks Section -->
              <div class="subtasks-section">
                <div class="subtasks-header">
                  <span>{$t('tasks.subtasks')} ({task.subtasks ? task.subtasks.filter(s => s.status === 'done').length : 0}/{task.subtasks ? task.subtasks.length : 0})</span>
                  <button class="add-subtask-btn" on:click|stopPropagation={() => openAddSubtaskModal(task.id)}>
                    {$t('tasks.addSubtask')}
                  </button>
                </div>
                {#if task.subtasks && task.subtasks.length > 0}
                  {#each task.subtasks as subtask (subtask.id)}
                    <div class="subtask-item">
                      <input
                        type="checkbox"
                        checked={subtask.status === 'done'}
                        on:click|stopPropagation={() => handleToggleSubtaskStatus(`${task.id}.${subtask.id}`, subtask.status)}
                        class="subtask-checkbox"
                      />
                      <span class="subtask-id">{subtask.id}</span>
                      <span class="subtask-title" class:done={subtask.status === 'done'}>{subtask.title}</span>
                      <button
                        class="subtask-remove-btn"
                        on:click|stopPropagation={() => handleRemoveSubtask(`${task.id}.${subtask.id}`)}
                        title={$t('tasks.removeSubtask')}
                      >
                        ×
                      </button>
                    </div>
                  {/each}
                {:else}
                  <button class="expand-btn" on:click|stopPropagation={() => handleExpandTask(task.id)}>
                    {$t('tasks.expandSubtasks')}
                  </button>
                {/if}
              </div>

              <!-- Dependencies Section -->
              <div class="dependencies-section">
                <div class="dependencies-header">
                  <span>{$t('tasks.dependencies')}</span>
                  <button class="add-dep-btn" on:click|stopPropagation={() => openDependencyModal(task.id)}>
                    {$t('tasks.addSubtask')}
                  </button>
                </div>
                {#if task.dependencies && task.dependencies.length > 0}
                  <div class="dependencies-list">
                    {#each task.dependencies as dep}
                      <span class="dep-badge">
                        {dep}
                        <button
                          class="dep-remove-btn"
                          on:click|stopPropagation={() => handleRemoveDependency(task.id, dep)}
                          title={$t('tasks.removeDependency')}
                        >
                          ×
                        </button>
                      </span>
                    {/each}
                  </div>
                {:else}
                  <span class="no-deps">{$t('tasks.noDependencies')}</span>
                {/if}
              </div>

              <div class="task-actions">
                <button class="action-btn primary" on:click|stopPropagation={() => handleSendToAgent(task.id)}>
                  {$t('tasks.sendToAgent')}
                </button>
                <button class="action-btn edit" on:click|stopPropagation={() => openEditTaskModal(task)}>
                  {$t('tasks.edit')}
                </button>
                <button class="action-btn" on:click|stopPropagation={() => handleExpandTask(task.id)}>
                  {$t('tasks.expand')}
                </button>
                <button class="action-btn danger" on:click|stopPropagation={() => handleDeleteTask(task.id)}>
                  {$t('common.delete')}
                </button>
              </div>
            </div>
          {/if}
        </div>
      {/each}
    {/if}
  </div>
</div>

<!-- Context Menu -->
{#if contextMenuTask}
  <div
    class="context-menu"
    style="left: {contextMenuX}px; top: {contextMenuY}px"
    on:click|stopPropagation
  >
    <button on:click={() => handleSendToAgent(contextMenuTask.id)}>{$t('tasks.sendToAgent')}</button>
    <button on:click={() => openEditTaskModal(contextMenuTask)}>{$t('tasks.editTaskMenu')}</button>
    <button on:click={() => handleExpandTask(contextMenuTask.id)}>{$t('tasks.expandTask')}</button>
    <button on:click={() => openAddSubtaskModal(contextMenuTask.id)}>{$t('tasks.addSubtaskMenu')}</button>
    <button on:click={() => openDependencyModal(contextMenuTask.id)}>{$t('tasks.manageDependencies')}</button>
    <div class="menu-divider"></div>
    <button on:click={() => handleMoveTask(contextMenuTask.id, 'pending')}>{$t('tasks.setPending')}</button>
    <button on:click={() => handleMoveTask(contextMenuTask.id, 'in-progress')}>{$t('tasks.setInProgress')}</button>
    <button on:click={() => handleMoveTask(contextMenuTask.id, 'done')}>{$t('tasks.setDone')}</button>
    <button on:click={() => handleMoveTask(contextMenuTask.id, 'blocked')}>{$t('tasks.setBlocked')}</button>
    <div class="menu-divider"></div>
    <button class="danger" on:click={() => handleDeleteTask(contextMenuTask.id)}>{$t('tasks.deleteTask')}</button>
  </div>
{/if}

<!-- PRD Modal -->
{#if showPRDModal}
  <div class="dialog-overlay" on:click={() => showPRDModal = false}>
    <div class="dialog-content large" on:click|stopPropagation>
      <div class="dialog-header">
        <h2>{$t('tasks.parsePRDTitle')}</h2>
        <button class="close-btn" on:click={() => showPRDModal = false}>×</button>
      </div>
      <div class="dialog-body">
        <p class="dialog-hint">
          {$t('tasks.parsePRDDesc')}
        </p>
        <label>
          {$t('tasks.prdContent')}
          <textarea
            bind:value={prdContent}
            placeholder="# Project Title&#10;&#10;## Overview&#10;Describe your project...&#10;&#10;## Requirements&#10;- Feature 1&#10;- Feature 2&#10;..."
            rows="15"
          ></textarea>
        </label>
        <label>
          {$t('tasks.numberOfTasks')}
          <input type="number" bind:value={prdNumTasks} min="1" max="50" />
        </label>
      </div>
      <div class="dialog-footer">
        <button class="btn-cancel" on:click={() => showPRDModal = false}>{$t('common.cancel')}</button>
        <button class="btn-primary" on:click={handleParsePRD} disabled={!prdContent.trim() || $isLoadingTasks}>
          {$isLoadingTasks ? $t('tasks.parsing') : $t('tasks.parsePRD')}
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- Add Task Modal -->
{#if showAddTaskModal}
  <div class="dialog-overlay" on:click={() => showAddTaskModal = false}>
    <div class="dialog-content large" on:click|stopPropagation on:focusin={handleDialogFocusIn}>
      <div class="dialog-header">
        <h2>{$t('tasks.addNewTask')}</h2>
        <div class="header-actions">
          <button class="mic-btn" class:active={$dictationListening} on:click|preventDefault={toggleModalDictation} title={$t('tabBar.dictateToField')}>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z"/><path d="M19 10v2a7 7 0 0 1-14 0v-2"/><line x1="12" y1="19" x2="12" y2="23"/><line x1="8" y1="23" x2="16" y2="23"/></svg>
          </button>
          <button class="close-btn" on:click={() => showAddTaskModal = false}>×</button>
        </div>
      </div>
      <div class="dialog-body">
        <!-- Mode toggle -->
        <div class="mode-toggle">
          <button
            class="mode-btn"
            class:active={useManualMode}
            on:click={() => useManualMode = true}
          >
            {$t('tasks.manual')}
          </button>
          <button
            class="mode-btn"
            class:active={!useManualMode}
            on:click={() => useManualMode = false}
          >
            {$t('tasks.aiGenerated')}
          </button>
        </div>

        {#if useManualMode}
          <p class="dialog-hint">
            {$t('tasks.manualDesc')}
          </p>
          <label>
            {$t('tasks.titleLabel')}
            <input
              type="text"
              bind:value={newTaskTitle}
              bind:this={addTitleEl}
              class:dictating={$dictationListening && activeDictationEl === addTitleEl}
              placeholder={$t('tasks.titlePlaceholder')}
            />
          </label>
          <label>
            {$t('tasks.description')}
            <textarea
              bind:value={newTaskDescription}
              bind:this={addDescEl}
              class:dictating={$dictationListening && activeDictationEl === addDescEl}
              placeholder={$t('tasks.descPlaceholder')}
              rows="3"
            ></textarea>
          </label>
          <label>
            {$t('tasks.implementationDetails')}
            <textarea
              bind:value={newTaskDetails}
              bind:this={addDetailsEl}
              class:dictating={$dictationListening && activeDictationEl === addDetailsEl}
              placeholder={$t('tasks.implPlaceholder')}
              rows="3"
            ></textarea>
          </label>
        {:else}
          <p class="dialog-hint">
            {$t('tasks.aiDesc')}
            <span class="api-info">{$t('tasks.aiNote')}</span>
          </p>
          <label>
            {$t('tasks.taskDescription')}
            <textarea
              bind:value={newTaskPrompt}
              bind:this={addPromptEl}
              class:dictating={$dictationListening && activeDictationEl === addPromptEl}
              placeholder={$t('tasks.aiPlaceholder')}
              rows="5"
            ></textarea>
          </label>
          <label class="checkbox-label">
            <input type="checkbox" bind:checked={newTaskResearch} />
            {$t('tasks.researchMode')}
          </label>
        {/if}

        <label>
          {$t('tasks.priority')}
          <Select
            value={newTaskPriority}
            options={[
              { value: 'low', label: 'Low' },
              { value: 'medium', label: 'Medium' },
              { value: 'high', label: 'High' },
              { value: 'critical', label: 'Critical' }
            ]}
            on:change={handleNewPriorityChange}
          />
        </label>
      </div>
      <div class="dialog-footer">
        <button class="btn-cancel" on:click={() => showAddTaskModal = false}>{$t('common.cancel')}</button>
        <button
          class="btn-primary"
          on:click={handleAddTask}
          disabled={(useManualMode ? !newTaskTitle.trim() : !newTaskPrompt.trim()) || $isLoadingTasks}
        >
          {$isLoadingTasks ? $t('tasks.adding') : $t('tasks.addTaskBtn')}
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- Complexity Modal -->
{#if showComplexityModal}
  <div class="dialog-overlay" on:click={() => showComplexityModal = false}>
    <div class="dialog-content large" on:click|stopPropagation>
      <div class="dialog-header">
        <h2>{$t('tasks.complexityAnalysis')}</h2>
        <button class="close-btn" on:click={() => showComplexityModal = false}>×</button>
      </div>
      <div class="dialog-body">
        <pre class="complexity-report">{complexityReport}</pre>
      </div>
      <div class="dialog-footer">
        <button class="btn-primary" on:click={() => showComplexityModal = false}>{$t('common.close')}</button>
      </div>
    </div>
  </div>
{/if}

<!-- Edit Task Modal -->
{#if showEditTaskModal}
  <div class="dialog-overlay" on:click={() => showEditTaskModal = false}>
    <div class="dialog-content large" on:click|stopPropagation on:focusin={handleDialogFocusIn}>
      <div class="dialog-header">
        <h2>{$t('tasks.editTask', { id: editTaskId })}</h2>
        <div class="header-actions">
          <button class="mic-btn" class:active={$dictationListening} on:click|preventDefault={toggleModalDictation} title={$t('tabBar.dictateToField')}>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z"/><path d="M19 10v2a7 7 0 0 1-14 0v-2"/><line x1="12" y1="19" x2="12" y2="23"/><line x1="8" y1="23" x2="16" y2="23"/></svg>
          </button>
          <button class="close-btn" on:click={() => showEditTaskModal = false}>×</button>
        </div>
      </div>
      <div class="dialog-body">
        <label>
          {$t('tasks.titleLabel')}
          <input
            type="text"
            bind:value={editTaskTitle}
            bind:this={editTitleEl}
            class:dictating={$dictationListening && activeDictationEl === editTitleEl}
            placeholder={$t('tasks.titlePlaceholder')}
          />
        </label>
        <label>
          {$t('tasks.description')}
          <textarea
            bind:value={editTaskDescription}
            bind:this={editDescEl}
            class:dictating={$dictationListening && activeDictationEl === editDescEl}
            placeholder={$t('tasks.descPlaceholder')}
            rows="3"
          ></textarea>
        </label>
        <label>
          {$t('tasks.implementationDetails')}
          <textarea
            bind:value={editTaskDetails}
            bind:this={editDetailsEl}
            class:dictating={$dictationListening && activeDictationEl === editDetailsEl}
            placeholder={$t('tasks.implPlaceholder')}
            rows="5"
          ></textarea>
        </label>
        <label>
          {$t('tasks.priority')}
          <Select
            value={editTaskPriority}
            options={[
              { value: 'low', label: 'Low' },
              { value: 'medium', label: 'Medium' },
              { value: 'high', label: 'High' },
              { value: 'critical', label: 'Critical' }
            ]}
            on:change={handleEditPriorityChange}
          />
        </label>
      </div>
      {#if editTaskError}
        <div class="error-banner" style="margin: 0 16px;">{editTaskError}</div>
      {/if}
      <div class="dialog-footer">
        <button class="btn-cancel" on:click={() => showEditTaskModal = false}>{$t('common.cancel')}</button>
        <button
          class="btn-primary"
          on:click={handleSaveEditTask}
          disabled={!editTaskTitle.trim() || $isLoadingTasks}
        >
          {$isLoadingTasks ? $t('common.loading') : $t('common.save')}
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- Add Subtask Modal -->
{#if showAddSubtaskModal}
  <div class="dialog-overlay" on:click={() => showAddSubtaskModal = false}>
    <div class="dialog-content small" on:click|stopPropagation on:focusin={handleDialogFocusIn}>
      <div class="dialog-header">
        <h2>{$t('tasks.addSubtaskMenu')} - #{addSubtaskTaskId}</h2>
        <div class="header-actions">
          <button class="mic-btn" class:active={$dictationListening} on:click|preventDefault={toggleModalDictation} title={$t('tabBar.dictateToField')}>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z"/><path d="M19 10v2a7 7 0 0 1-14 0v-2"/><line x1="12" y1="19" x2="12" y2="23"/><line x1="8" y1="23" x2="16" y2="23"/></svg>
          </button>
          <button class="close-btn" on:click={() => showAddSubtaskModal = false}>×</button>
        </div>
      </div>
      <div class="dialog-body">
        <label>
          {$t('tasks.titleLabel')}
          <input
            type="text"
            bind:value={newSubtaskTitle}
            bind:this={subtaskTitleEl}
            class:dictating={$dictationListening && activeDictationEl === subtaskTitleEl}
            placeholder={$t('tasks.titlePlaceholder')}
          />
        </label>
        <label>
          {$t('tasks.description')}
          <textarea
            bind:value={newSubtaskDescription}
            bind:this={subtaskDescEl}
            class:dictating={$dictationListening && activeDictationEl === subtaskDescEl}
            placeholder={$t('tasks.descPlaceholder')}
            rows="2"
          ></textarea>
        </label>
      </div>
      <div class="dialog-footer">
        <button class="btn-cancel" on:click={() => showAddSubtaskModal = false}>{$t('common.cancel')}</button>
        <button
          class="btn-primary"
          on:click={handleAddSubtask}
          disabled={!newSubtaskTitle.trim() || $isLoadingTasks}
        >
          {$isLoadingTasks ? $t('tasks.adding') : $t('tasks.addSubtaskMenu')}
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- Dependency Modal -->
{#if showDependencyModal}
  <div class="dialog-overlay" on:click={() => showDependencyModal = false}>
    <div class="dialog-content small" on:click|stopPropagation>
      <div class="dialog-header">
        <h2>{$t('tasks.manageDependencies')} - #{dependencyTaskId}</h2>
        <button class="close-btn" on:click={() => showDependencyModal = false}>×</button>
      </div>
      <div class="dialog-body">
        <p class="dialog-hint">
          Add task IDs that this task depends on. The task won't be suggested until its dependencies are completed.
        </p>

        <!-- Current dependencies -->
        {#if getTaskById(dependencyTaskId)?.dependencies?.length}
          <div class="current-deps">
            <span class="dep-section-label">Current dependencies:</span>
            <div class="deps-list">
              {#each getTaskById(dependencyTaskId)?.dependencies || [] as dep}
                <span class="dep-item">
                  #{dep}
                  <button
                    class="dep-remove-inline"
                    on:click={() => handleRemoveDependency(dependencyTaskId, dep)}
                  >
                    ×
                  </button>
                </span>
              {/each}
            </div>
          </div>
        {/if}

        <label>
          Add Dependency (Task ID)
          <div class="dep-input-row">
            <input
              type="text"
              bind:value={newDependencyId}
              placeholder="e.g., 1 or 2.1"
            />
            <button
              class="add-dep-inline-btn"
              on:click={handleAddDependency}
              disabled={!newDependencyId.trim() || $isLoadingTasks}
            >
              Add
            </button>
          </div>
        </label>

        <!-- Available tasks for reference -->
        <div class="available-tasks">
          <span class="dep-section-label">Available tasks:</span>
          <div class="tasks-ref-list">
            {#each $tasks.filter(t => t.id !== dependencyTaskId) as t}
              <button
                class="task-ref-btn"
                on:click={() => { newDependencyId = t.id; }}
              >
                #{t.id} - {t.title.substring(0, 30)}{t.title.length > 30 ? '...' : ''}
              </button>
            {/each}
          </div>
        </div>
      </div>
      <div class="dialog-footer">
        <button class="btn-primary" on:click={() => showDependencyModal = false}>{$t('common.close')}</button>
      </div>
    </div>
  </div>
{/if}

<!-- Delete Task Confirm -->
<ConfirmDialog
  bind:show={showDeleteConfirm}
  title={$t('tasks.deleteTask')}
  message={$t('tasks.deleteMessage', { title: deleteTaskTitle })}
  confirmText={$t('common.delete')}
  cancelText={$t('common.cancel')}
  variant="danger"
  on:confirm={confirmDeleteTask}
/>

<!-- Remove Subtask Confirm -->
<ConfirmDialog
  bind:show={showRemoveSubtaskConfirm}
  title={$t('tasks.removeSubtask')}
  message={$t('tasks.removeSubtaskMessage')}
  confirmText={$t('tasks.remove')}
  cancelText={$t('common.cancel')}
  variant="danger"
  on:confirm={confirmRemoveSubtask}
/>

<style>
  .task-panel {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: #0a0a0f;
  }

  .task-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 16px;
    background: rgba(0, 0, 0, 0.3);
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  }

  .header-left {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .task-title {
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #6b7280;
  }

  .task-count {
    font-size: 11px;
    color: #4ade80;
    background: rgba(74, 222, 128, 0.1);
    padding: 2px 8px;
    border-radius: 10px;
  }

  .mcp-badge {
    font-size: 10px;
    color: #a78bfa;
    background: rgba(167, 139, 250, 0.15);
    padding: 2px 6px;
    border-radius: 4px;
    font-weight: 600;
  }

  .header-right {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .hide-done-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 6px;
    color: #6b7280;
    cursor: pointer;
    transition: all 0.2s;
    padding: 0;
  }

  .hide-done-btn:hover {
    background: rgba(255, 255, 255, 0.1);
    color: #9ca3af;
  }

  .hide-done-btn.active {
    background: rgba(34, 197, 94, 0.15);
    border-color: rgba(34, 197, 94, 0.3);
    color: #4ade80;
  }

  .action-bar {
    display: flex;
    gap: 8px;
    padding: 10px 16px;
    background: rgba(0, 0, 0, 0.2);
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
    flex-wrap: wrap;
  }

  .action-bar .action-btn {
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.1);
    color: #9ca3af;
    padding: 6px 12px;
    border-radius: 6px;
    font-size: 12px;
    cursor: pointer;
    transition: all 0.2s;
  }

  .action-bar .action-btn:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.1);
    color: white;
  }

  .action-bar .action-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .action-bar .action-btn.init {
    background: rgba(139, 92, 246, 0.2);
    border-color: rgba(139, 92, 246, 0.3);
    color: #a78bfa;
  }

  .action-bar .action-btn.next {
    background: rgba(74, 222, 128, 0.15);
    border-color: rgba(74, 222, 128, 0.3);
    color: #4ade80;
  }

  .error-banner {
    padding: 10px 16px;
    background: rgba(239, 68, 68, 0.1);
    border-bottom: 1px solid rgba(239, 68, 68, 0.2);
    color: #f87171;
    font-size: 12px;
  }

  .task-list {
    flex: 1;
    overflow-y: auto;
    padding: 12px;
  }

  .loading, .empty {
    text-align: center;
    padding: 40px 20px;
    color: #6b7280;
    font-size: 14px;
  }

  .task-item {
    background: rgba(255, 255, 255, 0.02);
    border: 1px solid rgba(255, 255, 255, 0.05);
    border-radius: 10px;
    padding: 12px;
    margin-bottom: 8px;
    cursor: pointer;
    transition: all 0.2s;
  }

  .task-item:hover {
    background: rgba(255, 255, 255, 0.04);
    border-color: rgba(255, 255, 255, 0.1);
  }

  .task-item.selected {
    background: rgba(139, 92, 246, 0.1);
    border-color: rgba(139, 92, 246, 0.3);
  }

  .task-item.done {
    opacity: 0.6;
  }

  .task-main {
    display: flex;
    align-items: flex-start;
    gap: 12px;
  }

  .task-checkbox {
    width: 20px;
    height: 20px;
    border-radius: 6px;
    border: 1.5px solid rgba(255, 255, 255, 0.15);
    background: rgba(255, 255, 255, 0.03);
    cursor: pointer;
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 0;
    color: transparent;
    transition: all 0.2s ease;
  }

  .task-checkbox:hover {
    border-color: rgba(139, 92, 246, 0.5);
    background: rgba(139, 92, 246, 0.1);
  }

  .task-checkbox.checked {
    background: rgba(139, 92, 246, 0.3);
    border-color: rgba(139, 92, 246, 0.6);
    color: #e4e4e7;
  }

  .task-checkbox.checked:hover {
    background: rgba(139, 92, 246, 0.4);
    border-color: rgba(139, 92, 246, 0.7);
  }

  .task-id {
    font-family: 'JetBrains Mono', monospace;
    font-size: 11px;
    color: #6b7280;
    background: rgba(255, 255, 255, 0.05);
    padding: 2px 6px;
    border-radius: 4px;
    flex-shrink: 0;
  }

  .task-content {
    flex: 1;
    min-width: 0;
  }

  .task-title-row {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .task-name {
    font-size: 14px;
    color: white;
    font-weight: 500;
  }

  .task-name.completed {
    text-decoration: line-through;
    color: #6b7280;
  }

  .priority-badge, .status-badge {
    font-size: 10px;
    padding: 2px 8px;
    border-radius: 10px;
    font-weight: 500;
    text-transform: uppercase;
  }

  .created-at {
    font-size: 10px;
    color: #6b7280;
    margin-left: auto;
    flex-shrink: 0;
    white-space: nowrap;
  }

  .complexity-badge {
    font-size: 10px;
    color: #a78bfa;
    background: rgba(167, 139, 250, 0.15);
    padding: 2px 6px;
    border-radius: 4px;
    margin-left: auto;
  }

  .task-description, .task-details {
    margin: 8px 0;
    font-size: 13px;
    color: #9ca3af;
    line-height: 1.5;
    white-space: pre-wrap;
  }

  .task-tags {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    margin-top: 8px;
  }

  .tag {
    font-size: 11px;
    background: rgba(255, 255, 255, 0.05);
    color: #9ca3af;
    padding: 2px 8px;
    border-radius: 10px;
  }

  .task-details-panel {
    margin-top: 16px;
    padding-top: 16px;
    border-top: 1px solid rgba(255, 255, 255, 0.05);
  }

  .subtasks-section {
    margin-bottom: 16px;
  }

  .subtasks-header {
    font-size: 12px;
    color: #6b7280;
    margin-bottom: 8px;
  }

  .subtask-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 0;
    font-size: 13px;
    color: #d1d5db;
  }

  .subtask-id {
    font-family: 'JetBrains Mono', monospace;
    font-size: 10px;
    color: #6b7280;
  }

  .subtask-item span.done {
    text-decoration: line-through;
    color: #6b7280;
  }

  .subtask-status {
    margin-left: auto;
    font-size: 10px;
    text-transform: uppercase;
  }

  .expand-btn {
    width: 100%;
    padding: 10px;
    background: rgba(139, 92, 246, 0.1);
    border: 1px dashed rgba(139, 92, 246, 0.3);
    border-radius: 8px;
    color: #a78bfa;
    font-size: 12px;
    cursor: pointer;
    margin-bottom: 16px;
  }

  .expand-btn:hover {
    background: rgba(139, 92, 246, 0.2);
  }

  .dependencies {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 16px;
    flex-wrap: wrap;
  }

  .dep-label {
    font-size: 12px;
    color: #6b7280;
  }

  .dep-badge {
    font-size: 11px;
    background: rgba(255, 255, 255, 0.05);
    color: #9ca3af;
    padding: 2px 8px;
    border-radius: 4px;
  }

  .task-actions {
    display: flex;
    gap: 8px;
  }

  .task-actions .action-btn {
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.1);
    color: #9ca3af;
    padding: 8px 16px;
    border-radius: 6px;
    font-size: 12px;
    cursor: pointer;
    transition: all 0.2s;
  }

  .task-actions .action-btn:hover {
    background: rgba(255, 255, 255, 0.1);
  }

  .task-actions .action-btn.primary {
    background: rgba(139, 92, 246, 0.2);
    border-color: rgba(139, 92, 246, 0.3);
    color: #a78bfa;
  }

  .task-actions .action-btn.danger {
    color: #ef4444;
    border-color: rgba(239, 68, 68, 0.2);
  }

  /* Context Menu */
  .context-menu {
    position: fixed;
    background: #1a1a2e;
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 8px;
    padding: 6px 0;
    min-width: 160px;
    z-index: 1000;
    box-shadow: 0 10px 40px rgba(0, 0, 0, 0.5);
  }

  .context-menu button {
    display: block;
    width: 100%;
    background: transparent;
    border: none;
    color: #d1d5db;
    padding: 8px 16px;
    font-size: 13px;
    text-align: left;
    cursor: pointer;
  }

  .context-menu button:hover {
    background: rgba(139, 92, 246, 0.1);
  }

  .context-menu button.danger {
    color: #ef4444;
  }

  .menu-divider {
    height: 1px;
    background: rgba(255, 255, 255, 0.1);
    margin: 4px 0;
  }

  /* Dialog content size variants */
  .dialog-content.large {
    max-width: 700px;
  }

  .dialog-content.small {
    max-width: 400px;
  }

  /* Dialog body form styles */
  .dialog-hint {
    font-size: 13px;
    color: #9ca3af;
    margin-bottom: 16px;
    line-height: 1.5;
  }

  .dialog-body label {
    display: block;
    margin-bottom: 16px;
    font-size: 12px;
    color: #9ca3af;
  }

  .dialog-body input,
  .dialog-body textarea,
  .dialog-body select {
    display: block;
    width: 100%;
    margin-top: 6px;
    background: rgba(0, 0, 0, 0.3);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 8px;
    padding: 10px 12px;
    color: white;
    font-size: 14px;
  }

  .dialog-body textarea {
    min-height: 100px;
    resize: vertical;
    font-family: inherit;
  }

  .checkbox-label {
    display: flex !important;
    align-items: center;
    gap: 8px;
  }

  .checkbox-label input {
    width: auto !important;
    margin: 0 !important;
  }

  .complexity-report {
    background: rgba(0, 0, 0, 0.3);
    border-radius: 8px;
    padding: 16px;
    font-size: 13px;
    color: #d1d5db;
    white-space: pre-wrap;
    overflow-x: auto;
    max-height: 400px;
    overflow-y: auto;
  }

  /* Scrollbar */
  .task-list::-webkit-scrollbar {
    width: 6px;
  }

  .task-list::-webkit-scrollbar-track {
    background: transparent;
  }

  .task-list::-webkit-scrollbar-thumb {
    background: rgba(139, 92, 246, 0.3);
    border-radius: 3px;
  }

  /* Mode toggle */
  .mode-toggle {
    display: flex;
    gap: 4px;
    margin-bottom: 16px;
    background: rgba(0, 0, 0, 0.3);
    padding: 4px;
    border-radius: 8px;
  }

  .mode-btn {
    flex: 1;
    padding: 8px 16px;
    background: transparent;
    border: none;
    color: #6b7280;
    font-size: 13px;
    border-radius: 6px;
    cursor: pointer;
    transition: all 0.2s;
  }

  .mode-btn:hover {
    color: #9ca3af;
  }

  .mode-btn.active {
    background: rgba(139, 92, 246, 0.2);
    color: #a78bfa;
  }

  .api-info {
    display: block;
    margin-top: 8px;
    color: #60a5fa;
    font-size: 12px;
  }

  /* Subtasks Section Enhanced */
  .subtasks-section {
    margin-bottom: 16px;
  }

  .subtasks-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 12px;
    color: #6b7280;
    margin-bottom: 8px;
  }

  .add-subtask-btn {
    background: rgba(139, 92, 246, 0.1);
    border: 1px solid rgba(139, 92, 246, 0.3);
    color: #a78bfa;
    padding: 2px 8px;
    border-radius: 4px;
    font-size: 11px;
    cursor: pointer;
  }

  .add-subtask-btn:hover {
    background: rgba(139, 92, 246, 0.2);
  }

  .subtask-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    font-size: 13px;
    color: #d1d5db;
    background: rgba(255, 255, 255, 0.02);
    border-radius: 6px;
    margin-bottom: 4px;
  }

  .subtask-item:hover {
    background: rgba(255, 255, 255, 0.05);
  }

  .subtask-checkbox {
    width: 16px;
    height: 16px;
    cursor: pointer;
    accent-color: #8b5cf6;
  }

  .subtask-id {
    font-family: 'JetBrains Mono', monospace;
    font-size: 10px;
    color: #6b7280;
    flex-shrink: 0;
  }

  .subtask-title {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .subtask-title.done {
    text-decoration: line-through;
    color: #6b7280;
  }

  .subtask-remove-btn {
    background: transparent;
    border: none;
    color: #6b7280;
    font-size: 16px;
    cursor: pointer;
    padding: 0 4px;
    opacity: 0;
    transition: opacity 0.2s;
  }

  .subtask-item:hover .subtask-remove-btn {
    opacity: 1;
  }

  .subtask-remove-btn:hover {
    color: #ef4444;
  }

  /* Dependencies Section */
  .dependencies-section {
    margin-bottom: 16px;
  }

  .dependencies-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 12px;
    color: #6b7280;
    margin-bottom: 8px;
  }

  .add-dep-btn {
    background: rgba(59, 130, 246, 0.1);
    border: 1px solid rgba(59, 130, 246, 0.3);
    color: #60a5fa;
    padding: 2px 8px;
    border-radius: 4px;
    font-size: 11px;
    cursor: pointer;
  }

  .add-dep-btn:hover {
    background: rgba(59, 130, 246, 0.2);
  }

  .dependencies-list {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }

  .dep-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 11px;
    background: rgba(255, 255, 255, 0.05);
    color: #9ca3af;
    padding: 4px 8px;
    border-radius: 4px;
  }

  .dep-remove-btn {
    background: transparent;
    border: none;
    color: #6b7280;
    font-size: 14px;
    cursor: pointer;
    padding: 0;
    line-height: 1;
  }

  .dep-remove-btn:hover {
    color: #ef4444;
  }

  .no-deps {
    font-size: 12px;
    color: #4b5563;
    font-style: italic;
  }

  /* Edit button style */
  .task-actions .action-btn.edit {
    background: rgba(59, 130, 246, 0.1);
    border-color: rgba(59, 130, 246, 0.3);
    color: #60a5fa;
  }

  /* Dependency Modal Styles */
  .current-deps {
    margin-bottom: 16px;
  }

  .dep-section-label {
    display: block;
    font-size: 11px;
    color: #6b7280;
    margin-bottom: 8px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .deps-list {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  .dep-item {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: rgba(139, 92, 246, 0.1);
    color: #a78bfa;
    padding: 4px 10px;
    border-radius: 6px;
    font-size: 12px;
  }

  .dep-remove-inline {
    background: transparent;
    border: none;
    color: #a78bfa;
    cursor: pointer;
    font-size: 14px;
    padding: 0;
    opacity: 0.7;
  }

  .dep-remove-inline:hover {
    opacity: 1;
    color: #ef4444;
  }

  .dep-input-row {
    display: flex;
    gap: 8px;
    margin-top: 6px;
  }

  .dep-input-row input {
    flex: 1;
  }

  .add-dep-inline-btn {
    background: rgba(139, 92, 246, 0.2);
    border: 1px solid rgba(139, 92, 246, 0.3);
    color: #a78bfa;
    padding: 8px 16px;
    border-radius: 8px;
    cursor: pointer;
    font-size: 13px;
  }

  .add-dep-inline-btn:hover:not(:disabled) {
    background: rgba(139, 92, 246, 0.3);
  }

  .add-dep-inline-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .available-tasks {
    margin-top: 16px;
  }

  .tasks-ref-list {
    max-height: 150px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .task-ref-btn {
    background: rgba(255, 255, 255, 0.02);
    border: 1px solid rgba(255, 255, 255, 0.05);
    color: #9ca3af;
    padding: 8px 12px;
    border-radius: 6px;
    text-align: left;
    cursor: pointer;
    font-size: 12px;
  }

  .task-ref-btn:hover {
    background: rgba(255, 255, 255, 0.05);
    color: white;
  }

  .header-actions {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .mic-btn {
    background: none;
    border: none;
    cursor: pointer;
    color: #6b7280;
    padding: 4px 6px;
    border-radius: 4px;
    display: flex;
    align-items: center;
    transition: color 0.2s;
  }

  .mic-btn:hover {
    color: #9ca3af;
  }

  .mic-btn.active {
    color: #8b5cf6;
    animation: mic-pulse 1.5s ease-in-out infinite;
  }

  textarea.dictating,
  input.dictating {
    border-color: rgba(139, 92, 246, 0.5) !important;
    box-shadow: 0 0 0 3px rgba(139, 92, 246, 0.15) !important;
  }

  @keyframes mic-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.5; }
  }
</style>
