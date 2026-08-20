<script lang="ts">
  import { onMount, onDestroy, createEventDispatcher } from 'svelte';
  import { claimMenu, releaseMenu } from '../../utils/openMenu';
  import { autoFocusField } from '../../utils/dialogActions';
  import { get } from 'svelte/store';
  import { selectedSessionId } from '../../stores/sessions';
  import { settings } from '../../stores/settings';
  import Select from '../common/Select.svelte';
  import ConfirmDialog from '../Dialogs/ConfirmDialog.svelte';
  import { createFieldDictation } from '../../utils/dictationField';
  import * as DictationService from '../../../../wailsjs/go/main/DictationService';
  import { EventsOn } from '../../../../wailsjs/runtime/runtime';
  import { t } from '../../i18n';
  import { offerUndo } from '../../stores/undo';
  import { toLocalInputValue, fromLocalInputValue, deadlineState } from '../../utils/taskDueDate';
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
    effectiveTaskProvider,
    loadTasks,
    checkTaskMasterStatus,
    initializeTaskMaster,
    parsePRD,
    prepareTasksSession,
    addTask,
    addManualTask,
    restoreDeletedTask,
    restoreDeletedSubtask,
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
    parentTaskId,
    removeDependency,
    taskSortBy,
    setTaskSortBy,
    hideDone,
    toggleHideDone,
    type Task,
    type TaskStatus,
    type TaskPriority,
    type TaskSortBy,
    type Subtask,
    type TaskProvider
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
  // Without Task Master there is no AI mode to fall into — a dialog opened
  // while it was on, then reopened after switching it off, would otherwise
  // still be on a tab whose button is gone.
  $: if (!$settings.taskMasterEnabled || $effectiveTaskProvider !== 'mcp') useManualMode = true;
  let loadedTaskMasterSetting = get(settings).taskMasterEnabled;
  let newTaskTitle = '';
  let newTaskDescription = '';
  let newTaskDetails = '';

  // Context menu
  let contextMenuTask: Task | null = null;
  let contextMenuX = 0;
  let contextMenuY = 0;

  // Edit task modal
  let showEditTaskModal = false;
  let searchInputEl: HTMLInputElement | undefined;
  let editTaskDueAt = '';
  // Whether the task belongs to this session or to the project as a whole.
  // Only session-owned tasks trigger the warning when a session is closed, so
  // this is what decides whether the work is guarded.
  let editTaskSessionScoped = true;
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

  type TaskActionTarget = {
    sessionId: string;
    provider: TaskProvider;
    generation: number;
  };
  let actionGeneration = 0;
  let actionOperationRevision = 0;
  let actionIdentity = '';
  let contextMenuTarget: TaskActionTarget | null = null;
  let modalTarget: TaskActionTarget | null = null;

  type TargetOperation = {
    target: TaskActionTarget;
    revision: number;
  };

  function captureActionTarget(): TaskActionTarget | null {
    const sessionId = get(selectedSessionId);
    const provider = get(effectiveTaskProvider);
    if (!sessionId || !provider) return null;
    return { sessionId, provider, generation: actionGeneration };
  }

  function targetIsCurrent(target: TaskActionTarget | null): target is TaskActionTarget {
    return !!target && target.generation === actionGeneration &&
      target.sessionId === get(selectedSessionId) &&
      target.provider === get(effectiveTaskProvider);
  }

  function claimActionUI() {
    actionOperationRevision++;
  }

  function beginTargetOperation(target: TaskActionTarget | null): TargetOperation | null {
    if (!targetIsCurrent(target)) return null;
    return { target, revision: ++actionOperationRevision };
  }

  function operationIsCurrent(operation: TargetOperation | null): operation is TargetOperation {
    return !!operation && operation.revision === actionOperationRevision &&
      targetIsCurrent(operation.target);
  }

  function closeTargetActions() {
    claimActionUI();
    if (contextMenuTask) closeContextMenu();
    contextMenuTarget = null;
    modalTarget = null;
    showEditTaskModal = false;
    showAddSubtaskModal = false;
    showDeleteConfirm = false;
    showRemoveSubtaskConfirm = false;
    showDependencyModal = false;
    showAddTaskModal = false;
    showPRDModal = false;
  }

  function openAddTaskModal() {
    const target = captureActionTarget();
    if (!targetIsCurrent(target)) return;
    claimActionUI();
    modalTarget = target;
    showAddTaskModal = true;
  }

  function openPRDModal() {
    const target = captureActionTarget();
    if (!targetIsCurrent(target) || target.provider !== 'mcp') return;
    claimActionUI();
    modalTarget = target;
    showPRDModal = true;
  }

  // A task id is only meaningful inside the provider/session snapshot that
  // produced it. Provider probes deliberately pass through null, which also
  // invalidates open actions while the replacement list is unknown.
  $: {
    const identity = `${$selectedSessionId || ''}:${$effectiveTaskProvider || 'loading'}`;
    if (identity !== actionIdentity) {
      actionIdentity = identity;
      actionGeneration++;
      closeTargetActions();
    }
  }

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

  // Declared above onDestroy, which clears the interval: the reactive block
  // that starts it is further down, and reading a `let` before its declaration
  // is a runtime error rather than an undefined.
  let nowTick = Date.now();
  let clock: ReturnType<typeof setInterval> | null = null;

  onDestroy(() => {
    dictation.destroy();
    cleanupModalFieldTarget();
    // The panel can be destroyed while still active — switching session, or
    // closing the window — and an interval outlives its component.
    if (clock) clearInterval(clock);
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
    prepareTasksSession(sessionId);

    // Only ask about Task Master when it is switched on. The check runs it, and
    // running it is what triggers the npx install the opt-in exists to prevent
    // — the whole point of the setting. Off, the panel uses the app's own task
    // storage and never reaches for it.
    const useTaskMaster = get(settings).taskMasterEnabled;
    useMCPMode.set(useTaskMaster);
    if (useTaskMaster) {
      await checkTaskMasterStatus(sessionId);
      if (generation !== taskPanelLoadGeneration || sessionId !== get(selectedSessionId)) return;
    }
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

  // Keeps "3 minutes ago" advancing while the panel is on screen.
  //
  // The elapsed time comes from the clock, and a clock is not something Svelte
  // can watch: new Date() does not "change", it just answers differently each
  // call. So the value has to be re-read on a timer — but only while the panel
  // is visible. A timer redrawing a list nobody is looking at is exactly the
  // background work that costs this app its one WebKit main thread.
  //
  // A minute is the resolution the text itself has: below an hour it counts
  // whole minutes, so ticking faster would redraw without changing anything.
  $: if (active && !clock) {
    // Re-read at once, so a panel reopened after a while is not showing the
    // time it was closed at until the first tick lands.
    nowTick = Date.now();
    clock = setInterval(() => { nowTick = Date.now(); }, 60_000);
  } else if (!active && clock) {
    clearInterval(clock);
    clock = null;
  }

  // Watch for session changes
  $: if ($selectedSessionId !== lastSessionId) {
    loadTasksIfNeeded();
  }

  // Settings is a live overlay: closing it does not deactivate this panel.
  // Invalidate the visible provider immediately so a list loaded from MCP
  // cannot keep sending mutations there after Task Master was switched off.
  $: if ($settings.taskMasterEnabled !== loadedTaskMasterSetting) {
    loadedTaskMasterSetting = $settings.taskMasterEnabled;
    useMCPMode.set(loadedTaskMasterSetting);
    if (active) void loadTasksIfNeeded(true);
  }

  // Priority colors
  const priorityColors: Record<TaskPriority, string> = {
    critical: '#ef4444',
    high: '#f97316',
    medium: '#eab308',
    low: '#22c55e'
  };

  // Reactive, not const: a const map is built once with whatever language was
  // loaded at the time, so switching language left the badges in the old one.
  $: priorityLabels = {
    critical: $t('tasks.priorityCritical'),
    high: $t('tasks.priorityHigh'),
    medium: $t('tasks.priorityMedium'),
    low: $t('tasks.priorityLow'),
  } as Record<TaskPriority, string>;

  $: statusLabels = {
    pending: $t('tasks.statusPending'),
    'in-progress': $t('tasks.statusInProgress'),
    done: $t('tasks.statusDone'),
    blocked: $t('tasks.statusBlocked'),
    deferred: $t('tasks.statusDeferred'),
  } as Record<string, string>;

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
    const target = modalTarget;
    const operation = beginTargetOperation(target);
    if (!operation || target?.provider !== 'mcp' || !prdContent.trim()) return;
    const content = prdContent;
    const count = prdNumTasks;

    try {
      await parsePRD(operation.target.sessionId, content, count);
      if (!operationIsCurrent(operation) || modalTarget !== operation.target || !showPRDModal) return;
      showPRDModal = false;
      prdContent = '';
    } catch (e) {
      console.error('Failed to parse PRD:', e);
    }
  }

  // Add task
  async function handleAddTask() {
    console.log('[TaskPanel] handleAddTask called');
    const target = modalTarget;
    const operation = beginTargetOperation(target);
    if (!operation) return;
    const sessionId = operation.target.sessionId;
    const manual = useManualMode;
    const title = newTaskTitle;
    const description = newTaskDescription;
    const details = newTaskDetails;
    const prompt = newTaskPrompt;
    const priority = newTaskPriority;
    const research = newTaskResearch;
    console.log('[TaskPanel] sessionId:', sessionId);
    if (!sessionId) {
      console.log('[TaskPanel] No sessionId, returning early');
      return;
    }

    try {
      if (manual) {
        // Manual mode - no AI required
        console.log('[TaskPanel] Manual mode, title:', newTaskTitle);
        if (!title.trim()) {
          console.log('[TaskPanel] Empty title, returning early');
          return;
        }
        console.log('[TaskPanel] Calling addManualTask...');
        await addManualTask(sessionId, title, description, details, priority, operation.target.provider);
        if (!operationIsCurrent(operation) || modalTarget !== operation.target || !showAddTaskModal) return;
        console.log('[TaskPanel] addManualTask completed');
        newTaskTitle = '';
        newTaskDescription = '';
        newTaskDetails = '';
      } else {
        // AI mode - requires API key
        console.log('[TaskPanel] AI mode, prompt:', newTaskPrompt);
        if (!prompt.trim()) {
          console.log('[TaskPanel] Empty prompt, returning early');
          return;
        }
        console.log('[TaskPanel] Calling addTask...');
        if (operation.target.provider !== 'mcp') return;
        await addTask(sessionId, prompt, research, priority, operation.target.provider);
        if (!operationIsCurrent(operation) || modalTarget !== operation.target || !showAddTaskModal) return;
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
    const target = contextMenuTarget ?? captureActionTarget();
    if (!targetIsCurrent(target)) return;
    claimActionUI();
    const task = $tasks.find(t => t.id === taskId);
    modalTarget = target;
    deleteTaskId = taskId;
    deleteTaskTitle = task?.title || taskId;
    showDeleteConfirm = true;
    if (contextMenuTask) closeContextMenu();
  }

  async function confirmDeleteTask() {
    const target = modalTarget;
    const operation = beginTargetOperation(target);
    if (!operation || !deleteTaskId) {
      showDeleteConfirm = false;
      return;
    }
    const sessionId = operation.target.sessionId;
    const taskId = deleteTaskId;

    // Capture the complete snapshot and the provider used for deletion. Undo
    // restores the original ID and must not follow a later provider toggle.
    const removed = $tasks.find((task) => task.id === taskId);

    const provider = await removeTask(sessionId, taskId, operation.target.provider);
    if (!operationIsCurrent(operation) || modalTarget !== operation.target) return;
    if (removed) {
      offerUndo({
        message: $t('undo.taskDeleted', { title: removed.title }),
        undo: () => restoreDeletedTask(sessionId, removed, provider),
      });
    }
    deleteTaskId = '';
    deleteTaskTitle = '';
    modalTarget = null;
  }

  // Move task status
  async function handleMoveTask(taskId: string, newStatus: TaskStatus, requestedTarget?: TaskActionTarget | null) {
    const target = requestedTarget ?? captureActionTarget();
    const operation = beginTargetOperation(target);
    if (!operation) return;
    const sessionId = operation.target.sessionId;

    // Captured before the change: after it, the store holds the new value and
    // there is nothing left to go back to.
    const previous = $tasks.find((task) => task.id === taskId)?.status;

    const provider = await setTaskStatus(sessionId, taskId, newStatus, operation.target.provider);
    if (!operationIsCurrent(operation)) return;
    if (contextMenuTarget === operation.target) closeContextMenu();

    if (previous && previous !== newStatus) {
      offerUndo({
        message: $t('undo.taskStatus', { status: statusLabels[newStatus] || newStatus }),
        undo: () => setTaskStatus(sessionId, taskId, previous as TaskStatus, provider).then(() => undefined),
      });
    }
  }

  // Expand task
  async function handleExpandTask(taskId: string, requestedTarget?: TaskActionTarget | null) {
    const target = requestedTarget ?? captureActionTarget();
    const operation = beginTargetOperation(target);
    if (!operation || target?.provider !== 'mcp') return;
    const sessionId = operation.target.sessionId;

    try {
      await expandTask(sessionId, taskId, true, false);
      if (!operationIsCurrent(operation)) return;
    } catch (e) {
      console.error('Failed to expand task:', e);
    }
    if (contextMenuTarget === operation.target) closeContextMenu();
  }

  // Expand all
  async function handleExpandAll() {
    const target = captureActionTarget();
    const operation = beginTargetOperation(target);
    if (!operation || target?.provider !== 'mcp') return;
    const sessionId = operation.target.sessionId;

    try {
      await expandAllTasks(sessionId, true);
    } catch (e) {
      console.error('Failed to expand all:', e);
    }
  }

  // Analyze complexity
  async function handleAnalyzeComplexity() {
    const target = captureActionTarget();
    const operation = beginTargetOperation(target);
    if (!operation || target?.provider !== 'mcp') return;
    const sessionId = operation.target.sessionId;

    try {
      const report = await analyzeComplexity(sessionId, true);
      if (!operationIsCurrent(operation)) return;
      complexityReport = report;
      showComplexityModal = true;
    } catch (e) {
      console.error('Failed to analyze complexity:', e);
    }
  }

  // Get next task
  async function handleGetNextTask() {
    const target = captureActionTarget();
    const operation = beginTargetOperation(target);
    if (!operation) return;

    const task = await getNextTask(operation.target.sessionId, operation.target.provider);
    if (!operationIsCurrent(operation)) return;
    if (task) {
      selectTask(task.id);
    }
  }

  // Send to agent
  async function handleSendToAgent(taskId: string, requestedTarget?: TaskActionTarget | null) {
    const target = requestedTarget ?? captureActionTarget();
    const operation = beginTargetOperation(target);
    if (!operation) return;
    const sessionId = operation.target.sessionId;

    try {
      await sendTaskToAgent(sessionId, taskId, operation.target.provider);
      if (!operationIsCurrent(operation)) return;
      dispatch('taskSent', { taskId });
    } catch (e) {
      console.error('Failed to send task to agent:', e);
    }
    if (contextMenuTarget === operation.target) closeContextMenu();
  }

  // Context menu
  function showContextMenu(event: MouseEvent, task: Task) {
    event.preventDefault();
    const target = captureActionTarget();
    if (!target) return;
    claimActionUI();
    contextMenuTask = task;
    contextMenuTarget = target;
    contextMenuX = event.clientX;
    contextMenuY = event.clientY;
    // See SessionItem: contextmenu doesn't fire the click that closes menus.
    claimMenu(closeContextMenu);
  }

  function closeContextMenu() {
    contextMenuTask = null;
    contextMenuTarget = null;
    releaseMenu(closeContextMenu);
  }

  // Open edit task modal
  function openEditTaskModal(task: Task, requestedTarget?: TaskActionTarget | null) {
    const target = requestedTarget ?? captureActionTarget();
    if (!targetIsCurrent(target)) return;
    claimActionUI();
    modalTarget = target;
    editTaskId = task.id;
    editTaskTitle = task.title;
    editTaskDescription = task.description || '';
    editTaskDetails = task.details || '';
    editTaskPriority = task.priority;
    // datetime-local wants "YYYY-MM-DDTHH:mm" in LOCAL time, while the task
    // carries RFC 3339 with a zone. Feeding it the raw string leaves the field
    // blank, and slicing the string instead of converting shows a deadline in
    // the wrong hour for anyone not on UTC.
    editTaskDueAt = task.dueAt ? toLocalInputValue(task.dueAt) : '';
    editTaskSessionScoped = !!task.sessionId;
    editTaskError = '';
    showEditTaskModal = true;
    if (contextMenuTask) closeContextMenu();
  }

  // Save edited task
  async function handleSaveEditTask() {
    console.log('[TaskPanel] handleSaveEditTask called');
    const target = modalTarget;
    const operation = beginTargetOperation(target);
    const sessionId = operation?.target.sessionId;
    console.log('[TaskPanel] sessionId:', sessionId, 'editTaskId:', editTaskId);
    if (!operation || !sessionId || !editTaskId) {
      showEditTaskModal = false;
      return;
    }

    editTaskError = '';
    const taskId = editTaskId;
    const title = editTaskTitle;
    const description = editTaskDescription;
    const details = editTaskDetails;
    const priority = editTaskPriority;
    const dueAt = fromLocalInputValue(editTaskDueAt);
    const sessionScoped = editTaskSessionScoped;
    try {
      console.log('[TaskPanel] calling updateTaskDirect...', { editTaskTitle, editTaskDescription, editTaskDetails, editTaskPriority });
      await updateTaskDirect(sessionId, taskId, title, description, details, priority, dueAt, sessionScoped, operation.target.provider);
      if (!operationIsCurrent(operation) || modalTarget !== operation.target || !showEditTaskModal) return;
      console.log('[TaskPanel] updateTaskDirect success');
      showEditTaskModal = false;
    } catch (e) {
      if (!operationIsCurrent(operation) || modalTarget !== operation.target || !showEditTaskModal) return;
      console.error('[TaskPanel] Failed to update task:', e);
      editTaskError = String(e);
    }
  }

  // Open add subtask modal
  function openAddSubtaskModal(taskId: string, requestedTarget?: TaskActionTarget | null) {
    const target = requestedTarget ?? captureActionTarget();
    if (!targetIsCurrent(target)) return;
    claimActionUI();
    modalTarget = target;
    addSubtaskTaskId = taskId;
    newSubtaskTitle = '';
    newSubtaskDescription = '';
    showAddSubtaskModal = true;
  }

  // Add subtask
  async function handleAddSubtask() {
    const target = modalTarget;
    const operation = beginTargetOperation(target);
    if (!operation || !addSubtaskTaskId || !newSubtaskTitle.trim()) {
      showAddSubtaskModal = false;
      return;
    }
    const sessionId = operation.target.sessionId;
    const taskId = addSubtaskTaskId;
    const title = newSubtaskTitle;
    const description = newSubtaskDescription;

    try {
      await addSubtask(sessionId, taskId, title, description, operation.target.provider);
      if (!operationIsCurrent(operation) || modalTarget !== operation.target || !showAddSubtaskModal) return;
      showAddSubtaskModal = false;
    } catch (e) {
      console.error('Failed to add subtask:', e);
    }
  }

  // Toggle subtask status
  /**
   * Flip a subtask between done and pending.
   *
   * Takes the current state as a boolean rather than a status string: the
   * app's own storage has no status field on subtasks, so passing
   * `subtask.status` handed this `undefined`, and every click computed "not
   * done" and set it to done — a tick that could never be undone.
   */
  async function handleToggleSubtaskStatus(subtaskId: string, currentlyDone: boolean) {
    const target = captureActionTarget();
    const operation = beginTargetOperation(target);
    if (!operation) return;
    const sessionId = operation.target.sessionId;

    const newStatus = currentlyDone ? 'pending' : 'done';
    try {
      const provider = await setSubtaskStatus(sessionId, subtaskId, newStatus as TaskStatus, operation.target.provider);
      if (!operationIsCurrent(operation)) return;
      offerUndo({
        message: currentlyDone ? $t('undo.subtaskUnchecked') : $t('undo.subtaskChecked'),
        undo: () => setSubtaskStatus(sessionId, subtaskId, (currentlyDone ? 'done' : 'pending') as TaskStatus, provider).then(() => undefined),
      });
    } catch (e) {
      console.error('Failed to toggle subtask status:', e);
    }
  }

  // Remove subtask
  function handleRemoveSubtask(subtaskId: string) {
    const target = captureActionTarget();
    if (!targetIsCurrent(target)) return;
    claimActionUI();
    modalTarget = target;
    removeSubtaskId = subtaskId;
    showRemoveSubtaskConfirm = true;
  }

  async function confirmRemoveSubtask() {
    const target = modalTarget;
    const operation = beginTargetOperation(target);
    if (!operation || !removeSubtaskId) {
      showRemoveSubtaskConfirm = false;
      return;
    }
    const sessionId = operation.target.sessionId;
    const subtaskId = removeSubtaskId;

    // Kept before the delete so Undo can restore the exact provider snapshot,
    // including identity, completion state and implementation details.
    const parentId = parentTaskId(subtaskId);
    const childId = String(subtaskId).slice(parentId.length + 1);
    const removed = $tasks
      .find((task) => task.id === parentId)?.subtasks
      ?.find((sub) => String(sub.id) === childId);

    try {
      const provider = await removeSubtask(sessionId, subtaskId, operation.target.provider);
      if (!operationIsCurrent(operation) || modalTarget !== operation.target) return;
      if (removed) {
        offerUndo({
          message: $t('undo.subtaskDeleted', { title: removed.title }),
          undo: () => restoreDeletedSubtask(sessionId, parentId, removed, provider),
        });
      }
    } catch (e) {
      console.error('Failed to remove subtask:', e);
    }
    if (!operationIsCurrent(operation) || modalTarget !== operation.target) return;
    removeSubtaskId = '';
    modalTarget = null;
  }

  // Open dependency modal
  function openDependencyModal(taskId: string, requestedTarget?: TaskActionTarget | null) {
    const target = requestedTarget ?? captureActionTarget();
    if (!targetIsCurrent(target)) return;
    claimActionUI();
    modalTarget = target;
    dependencyTaskId = taskId;
    newDependencyId = '';
    showDependencyModal = true;
  }

  /**
   * The tasks that can be depended on: everything in this tab except the task
   * being edited and the ones already listed.
   *
   * A task cannot wait for itself, and offering a dependency that is already
   * set invites a click that does nothing.
   */
  $: dependencyOptions = (() => {
    const current = getTaskById(dependencyTaskId, $tasks);
    const already = new Set(current?.dependencies || []);
    const choices = $tasks
      .filter((task) => task.id !== dependencyTaskId && !already.has(task.id))
      .map((task) => ({ value: task.id, label: task.title }));

    // A placeholder first, so the field does not arrive with an arbitrary task
    // already chosen — that reads as a decision the user did not make.
    return [{ value: '', label: $t('tasks.selectDependency') }, ...choices];
  })();

  /**
   * Whether a subtask is finished.
   *
   * The two backends describe this differently: Task Master sends a status
   * string, the app's own storage sends a `done` boolean. Reading only
   * `status === 'done'` meant every checkbox stayed empty outside MCP mode —
   * ticking one saved correctly and then rendered unticked, which read as the
   * click having been lost.
   */
  function isSubtaskDone(subtask: { status?: string; done?: boolean }): boolean {
    return subtask.done === true || subtask.status === 'done';
  }

  // Add dependency
  async function handleAddDependency() {
    const target = modalTarget;
    const operation = beginTargetOperation(target);
    if (!operation || !dependencyTaskId || !newDependencyId.trim()) return;
    const sessionId = operation.target.sessionId;
    const taskId = dependencyTaskId;
    const dependencyId = newDependencyId;

    try {
      await addDependency(sessionId, taskId, dependencyId, operation.target.provider);
      if (!operationIsCurrent(operation) || modalTarget !== operation.target || !showDependencyModal) return;
      newDependencyId = '';
    } catch (e) {
      console.error('Failed to add dependency:', e);
    }
  }

  // Remove dependency
  async function handleRemoveDependency(taskId: string, depId: string) {
    const target = modalTarget ?? captureActionTarget();
    const operation = beginTargetOperation(target);
    if (!operation) return;
    const sessionId = operation.target.sessionId;

    try {
      const provider = await removeDependency(sessionId, taskId, depId, operation.target.provider);
      if (!operationIsCurrent(operation)) return;
      offerUndo({
        message: $t('undo.dependencyRemoved'),
        undo: () => addDependency(sessionId, taskId, depId, provider).then(() => undefined),
      });
    } catch (e) {
      console.error('Failed to remove dependency:', e);
    }
  }

  // Get task by ID for dependency dropdown
  // Takes the list rather than reading the store: called from the markup,
  // Svelte re-runs this only when its arguments change, so a store read inside
  // would leave the dependency rows showing whatever they first rendered with.
  /**
   * A dependency as the reader knows it: the other task's title.
   *
   * Falls back to the id when the task is gone — a dependency on something
   * deleted still has to be visible, or it cannot be removed.
   */
  function dependencyName(depId: string, all: Task[]): string {
    return getTaskById(String(depId), all)?.title || `#${depId}`;
  }

  function getTaskById(id: string, all: Task[]): Task | undefined {
    return all.find(t => t.id === id);
  }

  // Takes the translate function rather than reading $t inside: called from the
  // markup, Svelte re-runs this only when its arguments change, so a language
  // switch would leave the old wording on screen.
  //
  // `now` is passed in for the same reason as `tr`: read inside, neither the
  // clock nor the translation would be a dependency Svelte can see, and the
  // row would keep whatever it first rendered.
  function formatRelativeDate(dateStr: string, tr: typeof $t, now: number): string {
    const date = new Date(dateStr);
    const diffMs = now - date.getTime();
    const diffMin = Math.floor(diffMs / 60000);
    const diffHr = Math.floor(diffMs / 3600000);
    const diffDays = Math.floor(diffMs / 86400000);

    if (diffMin < 1) return tr('tasks.timeJustNow');
    if (diffMin < 60) return tr('tasks.timeMinAgo', { n: diffMin });
    if (diffHr < 24) return tr('tasks.timeHourAgo', { n: diffHr });
    if (diffDays < 7) return tr('tasks.timeDayAgo', { n: diffDays });
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
      {#if $settings.taskMasterEnabled && $taskMasterStatus.running}
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
          { value: 'created-asc', label: $t('tasks.sortCreatedAsc') },
          { value: 'completed-desc', label: $t('tasks.sortCompletedDesc') },
          { value: 'completed-asc', label: $t('tasks.sortCompletedAsc') }
        ]}
        on:change={handleSortChange}
      />
      <Select
        small
        value={$taskFilter.status}
        options={[
          { value: 'all', label: $t('tasks.statusAll') },
          { value: 'pending', label: $t('tasks.statusPending') },
          { value: 'in-progress', label: $t('tasks.statusInProgress') },
          { value: 'done', label: $t('tasks.statusDone') },
          { value: 'blocked', label: $t('tasks.statusBlocked') }
        ]}
        on:change={handleStatusFilterChange}
      />
    </div>
  </div>

  <!-- Action Bar
       Keeping a task list needs nothing from Task Master: tasks are stored by
       the app itself (session/tasks.go), and adding, editing and completing
       them work on their own. Only the actions that CALL Task Master are gated
       — parsing a PRD, expanding a task into subtasks, scoring complexity —
       along with the button that installs it. -->
  <div class="action-bar">
    <!-- Filtering, not searching: the list narrows as you type rather than
         jumping between matches, which is what a list of this length wants. -->
    <div class="task-search">
      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <circle cx="11" cy="11" r="8"/>
        <path d="M21 21l-4.35-4.35"/>
      </svg>
      <input
        type="text"
        bind:this={searchInputEl}
        value={$taskFilter.searchText}
        on:input={(e) => taskFilter.update((f) => ({ ...f, searchText: e.currentTarget.value }))}
        placeholder={$t('tasks.searchPlaceholder')}
      />
      {#if $taskFilter.searchText}
        <button
          class="clear-search"
          on:click={() => taskFilter.update((f) => ({ ...f, searchText: '' }))}
          aria-label={$t('common.clear')}
        >×</button>
      {/if}
    </div>

    <button class="action-btn" on:click={openAddTaskModal} disabled={$isLoadingTasks || !$effectiveTaskProvider}>
      {$t('tasks.addTask')}
    </button>
    <button class="action-btn next" on:click={handleGetNextTask} disabled={$isLoadingTasks}>
      {$t('tasks.nextTask')}
    </button>
    {#if $settings.taskMasterEnabled}
      {#if !$taskMasterStatus.running}
        <button class="action-btn init" on:click={handleInit} disabled={$isLoadingTasks}>
          {$t('tasks.initialize')}
        </button>
      {:else if $effectiveTaskProvider === 'mcp'}
        <button class="action-btn" on:click={openPRDModal} disabled={$isLoadingTasks}>
          {$t('tasks.parsePRD')}
        </button>
        <button class="action-btn" on:click={handleExpandAll} disabled={$isLoadingTasks}>
          {$t('tasks.expandAll')}
        </button>
        <button class="action-btn" on:click={handleAnalyzeComplexity} disabled={$isLoadingTasks}>
          {$t('tasks.analyze')}
        </button>
      {/if}
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
        {#if $settings.taskMasterEnabled && !$taskMasterStatus.running}
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

            <div class="task-content">
              <div class="task-title-row">
                <span class="task-name" class:completed={task.status === 'done'} title={task.title}>{task.title}</span>
                <div class="task-meta-row">
                  <div class="optional-meta">
                    {#if task.createdAt}
                      <span class="created-at" title={new Date(task.createdAt).toLocaleString()}>
                        {formatRelativeDate(task.createdAt, $t, nowTick)}
                      </span>
                    {/if}

                    {#if task.dueAt}
                      <span
                        class="due-badge {deadlineState(task.dueAt, task.status)}"
                        title={new Date(task.dueAt).toLocaleString()}
                      >
                        {new Date(task.dueAt).toLocaleString(undefined, {
                          month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit'
                        })}
                      </span>
                    {/if}

                    {#if task.complexity}
                      <span class="complexity-badge" title={$t('tasks.complexityScore')}>
                        C:{task.complexity}
                      </span>
                    {/if}

                    {#if task.subtasks && task.subtasks.length > 0}
                      <!-- Subtask progress, so a checklist part-done is visible
                           without expanding the task. Green when all of it is. -->
                      <span
                        class="subtask-badge"
                        class:complete={task.subtasks.every(isSubtaskDone)}
                        title={$t('tasks.subtasks')}
                      >
                        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">
                          <path d="M9 11l3 3L20 5"/>
                        </svg>
                        {task.subtasks.filter(isSubtaskDone).length}/{task.subtasks.length}
                      </span>
                    {/if}

                    {#if task.dependencies && task.dependencies.length > 0}
                      <span class="dep-count-badge" title={$t('tasks.dependencies')}>
                        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                          <path d="M10 13a5 5 0 0 0 7 0l3-3a5 5 0 0 0-7-7l-1 1"/>
                          <path d="M14 11a5 5 0 0 0-7 0l-3 3a5 5 0 0 0 7 7l1-1"/>
                        </svg>
                        {task.dependencies.length}
                      </span>
                    {/if}
                  </div>
                  <div class="trailing-meta">
                    <span
                      class="status-badge meta-column"
                      style="background: {statusColors[task.status] || '#9ca3af'}20; color: {statusColors[task.status] || '#9ca3af'}"
                    >
                      {statusLabels[task.status] || task.status}
                    </span>
                    <span
                      class="priority-badge meta-column"
                      style="background: {priorityColors[task.priority] || '#9ca3af'}20; color: {priorityColors[task.priority] || '#9ca3af'}"
                    >
                      {priorityLabels[task.priority] || task.priority}
                    </span>
                  </div>
                </div>
              </div>

              {#if task.description && $selectedTaskId === task.id}
                <p class="task-description">{task.description}</p>
              {/if}

              {#if task.details && $selectedTaskId === task.id}
                <!-- Labelled, because a box of monospace text under a task
                     could be anything — output, a note, a command. The same
                     wording the edit dialog uses for the field, so the two
                     name the same thing.

                     The notes are usually pasted output or a plan with its own
                     indentation, hence the box and the monospace face; as body
                     text the structure collapsed into an unreadable run. -->
                <div class="details-block">
                  <span class="details-label">{$t('tasks.implementationDetails')}</span>
                  <pre class="task-details">{task.details}</pre>
                </div>
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
                  <span>{$t('tasks.subtasks')} ({task.subtasks ? task.subtasks.filter(isSubtaskDone).length : 0}/{task.subtasks ? task.subtasks.length : 0})</span>
                  <button class="add-subtask-btn" on:click|stopPropagation={() => openAddSubtaskModal(task.id)}>
                    {$t('tasks.addSubtask')}
                  </button>
                </div>
                {#if task.subtasks && task.subtasks.length > 0}
                  {#each task.subtasks as subtask (subtask.id)}
                    <div class="subtask-item">
                      <input
                        type="checkbox"
                        checked={isSubtaskDone(subtask)}
                        on:click|stopPropagation={() => handleToggleSubtaskStatus(`${task.id}.${subtask.id}`, isSubtaskDone(subtask))}
                        class="subtask-checkbox"
                      />
                      <span class="subtask-title" class:done={isSubtaskDone(subtask)}>{subtask.title}</span>
                      <button
                        class="subtask-remove-btn"
                        on:click|stopPropagation={() => handleRemoveSubtask(`${task.id}.${subtask.id}`)}
                        title={$t('tasks.removeSubtask')}
                      >
                        ×
                      </button>
                    </div>
                  {/each}
                {:else if $effectiveTaskProvider === 'mcp'}
                  <!-- Breaking a task into subtasks is Task Master's, not ours. -->
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
                    {$t('tasks.addDependency')}
                  </button>
                </div>
                {#if task.dependencies && task.dependencies.length > 0}
                  <div class="dependencies-list">
                    {#each task.dependencies as dep}
                      <!-- Named, not numbered. The id is an internal handle
                           and means nothing to the reader; a dependency is
                           only useful if you can tell which task it is. -->
                      <span class="dep-badge" title={dependencyName(dep, $tasks)}>
                        {dependencyName(dep, $tasks)}
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
                {#if $effectiveTaskProvider === 'mcp'}
                  <button class="action-btn" on:click|stopPropagation={() => handleExpandTask(task.id)}>
                    {$t('tasks.expand')}
                  </button>
                {/if}
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
  {@const menuTask = contextMenuTask}
  <div
    class="context-menu"
    style="left: {contextMenuX}px; top: {contextMenuY}px"
    on:click|stopPropagation
  >
    <button on:click={() => handleSendToAgent(menuTask.id, contextMenuTarget)}>{$t('tasks.sendToAgent')}</button>
    <button on:click={() => openEditTaskModal(menuTask, contextMenuTarget)}>{$t('tasks.editTaskMenu')}</button>
    {#if contextMenuTarget?.provider === 'mcp'}
      <button on:click={() => handleExpandTask(menuTask.id, contextMenuTarget)}>{$t('tasks.expandTask')}</button>
    {/if}
    <button on:click={() => openAddSubtaskModal(menuTask.id, contextMenuTarget)}>{$t('tasks.addSubtaskMenu')}</button>
    <button on:click={() => openDependencyModal(menuTask.id, contextMenuTarget)}>{$t('tasks.manageDependencies')}</button>
    <div class="menu-divider"></div>
    <button on:click={() => handleMoveTask(menuTask.id, 'pending', contextMenuTarget)}>{$t('tasks.setPending')}</button>
    <button on:click={() => handleMoveTask(menuTask.id, 'in-progress', contextMenuTarget)}>{$t('tasks.setInProgress')}</button>
    <button on:click={() => handleMoveTask(menuTask.id, 'done', contextMenuTarget)}>{$t('tasks.setDone')}</button>
    <button on:click={() => handleMoveTask(menuTask.id, 'blocked', contextMenuTarget)}>{$t('tasks.setBlocked')}</button>
    <div class="menu-divider"></div>
    <button class="danger" on:click={() => handleDeleteTask(menuTask.id)}>{$t('tasks.deleteTask')}</button>
  </div>
{/if}

<!-- PRD Modal -->
{#if showPRDModal}
  <div class="dialog-overlay" use:autoFocusField on:click={() => showPRDModal = false}>
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
  <div class="dialog-overlay" use:autoFocusField on:click={() => showAddTaskModal = false}>
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
        <!-- Mode toggle. The AI half asks Task Master to write the task from a
             description, so with the integration off there is only one way to
             add a task and no choice to present. -->
        {#if $effectiveTaskProvider === 'mcp'}
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
        {/if}

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
              { value: 'low', label: $t('tasks.priorityLow') },
              { value: 'medium', label: $t('tasks.priorityMedium') },
              { value: 'high', label: $t('tasks.priorityHigh') },
              { value: 'critical', label: $t('tasks.priorityCritical') }
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
  <div class="dialog-overlay" use:autoFocusField on:click={() => showEditTaskModal = false}>
    <div class="dialog-content large" on:click|stopPropagation on:focusin={handleDialogFocusIn}>
      <div class="dialog-header">
        <h2>{$t('tasks.editTask')}</h2>
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
              { value: 'low', label: $t('tasks.priorityLow') },
              { value: 'medium', label: $t('tasks.priorityMedium') },
              { value: 'high', label: $t('tasks.priorityHigh') },
              { value: 'critical', label: $t('tasks.priorityCritical') }
            ]}
            on:change={handleEditPriorityChange}
          />
        </label>
        <label>
          {$t('tasks.dueAt')}
          <input
            type="datetime-local"
            bind:value={editTaskDueAt}
            class="due-input"
          />
        </label>
        <label class="checkbox-label">
          <input type="checkbox" bind:checked={editTaskSessionScoped} />
          {$t('tasks.belongsToSession')}
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
  <div class="dialog-overlay" use:autoFocusField on:click={() => showAddSubtaskModal = false}>
    <div class="dialog-content large" on:click|stopPropagation on:focusin={handleDialogFocusIn}>
      <div class="dialog-header">
        <!-- Named, not numbered: the id is an internal handle, and the task's
             own title says which one this subtask is being added to. -->
        <h2>{getTaskById(addSubtaskTaskId, $tasks)?.title || $t('tasks.addSubtaskMenu')}</h2>
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
    <div class="dialog-content dependency-dialog" on:click|stopPropagation>
      <div class="dialog-header">
        <!-- Named, not numbered. The id is an internal handle and tells the
             reader nothing; the task's own title says which one this is. -->
        <h2>{getTaskById(dependencyTaskId, $tasks)?.title || $t('tasks.manageDependencies')}</h2>
        <button class="close-btn" on:click={() => showDependencyModal = false}>×</button>
      </div>
      <div class="dialog-body">
        <p class="dialog-hint">{$t('tasks.dependencyHint')}</p>

        <!-- Current dependencies -->
        {#if getTaskById(dependencyTaskId, $tasks)?.dependencies?.length}
          <div class="current-deps">
            <span class="dep-section-label">{$t('tasks.currentDependencies')}</span>
            <div class="deps-list">
              {#each getTaskById(dependencyTaskId, $tasks)?.dependencies || [] as dep}
                <span class="dep-item" title={dependencyName(dep, $tasks)}>
                  {dependencyName(dep, $tasks)}
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

        <!-- Picked from a list, not typed.
             The field used to ask for a task ID with "e.g., 1 or 2.1" as the
             hint. The id is an internal handle: it is not shown anywhere in
             the list, so answering meant hunting for a number the interface
             never told you. Every task in this tab is already known here, so
             the choice is a dropdown. -->
        <label class="dep-select-label">
          {$t('tasks.addDependency')}
          <Select
            value={newDependencyId}
            options={dependencyOptions}
            on:change={(e) => (newDependencyId = e.detail)}
            searchable
          />
        </label>

        <div class="dep-add-row">
          <button
            class="btn-primary"
            on:click={handleAddDependency}
            disabled={!newDependencyId || $isLoadingTasks}
          >
            {$t('common.add')}
          </button>
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
    container-type: inline-size;
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
    font-size: 13px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #6b7280;
  }

  .task-count {
    font-size: 12px;
    color: #4ade80;
    background: rgba(74, 222, 128, 0.1);
    padding: 2px 8px;
    border-radius: 10px;
  }

  .mcp-badge {
    font-size: 11px;
    color: var(--accent-light);
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

  .task-search {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 8px;
    background: rgba(0, 0, 0, 0.25);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 6px;
    color: #6b7280;
    flex: 1;
    min-width: 120px;
    max-width: 280px;
  }

  .task-search:focus-within {
    border-color: rgba(var(--accent-rgb), 0.5);
  }

  .task-search input {
    flex: 1;
    min-width: 0;
    padding: 5px 0;
    background: transparent;
    border: none;
    color: #e5e7eb;
    font-size: 13px;
    font-family: inherit;
  }

  .task-search input:focus {
    outline: none;
  }

  .clear-search {
    background: transparent;
    border: none;
    color: #6b7280;
    font-size: 16px;
    line-height: 1;
    cursor: pointer;
    padding: 0 2px;
  }

  .clear-search:hover {
    color: #d1d5db;
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
    font-size: 13px;
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
    background: rgba(var(--accent-rgb), 0.2);
    border-color: rgba(var(--accent-rgb), 0.3);
    color: var(--accent-light);
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
    font-size: 13px;
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
    background: rgba(var(--accent-rgb), 0.1);
    border-color: rgba(var(--accent-rgb), 0.3);
  }

  .task-item.done {
    opacity: 0.6;
  }

  .task-main {
    display: flex;
    align-items: center;
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
    border-color: rgba(var(--accent-rgb), 0.5);
    background: rgba(var(--accent-rgb), 0.1);
  }

  .task-checkbox.checked {
    background: rgba(var(--accent-rgb), 0.3);
    border-color: rgba(var(--accent-rgb), 0.6);
    color: #e4e4e7;
  }

  .task-checkbox.checked:hover {
    background: rgba(var(--accent-rgb), 0.4);
    border-color: rgba(var(--accent-rgb), 0.7);
  }

  .task-content {
    flex: 1;
    min-width: 0;
  }

  .task-title-row {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
    overflow: hidden;
  }

  /* Metadata is anchored from the right: priority is the outside column,
     status sits immediately inside it, and optional values grow further left.
     Missing optional values therefore leave no holes and cannot move the two
     columns that must line up. */
  .task-meta-row {
    flex: 0 1 auto;
    min-width: 0;
    display: flex;
    align-items: center;
    margin-left: auto;
  }

  .optional-meta {
    flex: 0 1 auto;
    min-width: 0;
    overflow: hidden;
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 6px;
  }

  .trailing-meta {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 6px;
    margin-left: 6px;
  }

  .meta-column {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    justify-content: center;
    box-sizing: border-box;
  }

  /* Sized in ch — the width of a "0" — plus room for the pill's own padding
     and for translated labels. A label that still exceeds its slot is clipped
     instead of creating a second visual row or pushing the shared columns. */
  .meta-column.priority-badge {
    width: 14ch;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .meta-column.status-badge {
    width: 18ch;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  /* The window can be at its 800px minimum with a 500px sidebar, leaving the
     main panel close to 300px. Optional metadata gives way first; the two task
     state columns remain inside the row and keep a small title sliver. */
  @container (max-width: 420px) {
    .task-title-row { gap: 6px; }
    .optional-meta { display: none; }
    .task-meta-row { flex: 0 0 auto; }
    .trailing-meta { gap: 4px; margin-left: 0; }
    .meta-column.status-badge { width: 14ch; }
    .meta-column.priority-badge { width: 12ch; }
  }

  .subtask-badge,
  .dep-count-badge {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-size: 10px;
    font-variant-numeric: tabular-nums;
    padding: 2px 7px;
    border-radius: 999px;
    background: rgba(107, 114, 128, 0.18);
    color: #9ca3af;
  }

  .subtask-badge.complete {
    background: rgba(74, 222, 128, 0.15);
    color: #4ade80;
  }

  .task-name {
    flex: 1 1 auto;
    min-width: 0;
    font-size: 14px;
    color: white;
    font-weight: 500;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .task-name.completed {
    text-decoration: line-through;
    color: #6b7280;
  }

  .priority-badge, .status-badge {
    font-size: 11px;
    padding: 2px 8px;
    border-radius: 10px;
    font-weight: 500;
    text-transform: uppercase;
  }

  .created-at {
    font-size: 11px;
    color: #6b7280;
    flex-shrink: 0;
    white-space: nowrap;
  }

  /* A deadline reads as a badge like priority and status, not as body text —
     it is the field people scan the list for. Colour carries the urgency, but
     the date itself is always shown: colour alone excludes anyone who cannot
     distinguish these two, and reads as decoration on a calm list. */
  .due-badge {
    font-size: 11px;
    padding: 2px 6px;
    border-radius: 4px;
    flex-shrink: 0;
    white-space: nowrap;
    background: rgba(107, 114, 128, 0.12);
    color: #6b7280;
  }

  .due-badge.soon {
    background: rgba(245, 158, 11, 0.15);
    color: #b45309;
  }

  .due-badge.overdue {
    background: rgba(239, 68, 68, 0.15);
    color: #b91c1c;
    font-weight: 600;
  }

  .due-input {
    width: 100%;
    box-sizing: border-box;
  }

  .complexity-badge {
    font-size: 11px;
    color: var(--accent-light);
    background: rgba(167, 139, 250, 0.15);
    padding: 2px 6px;
    border-radius: 4px;
    flex-shrink: 0;
  }

  .task-description {
    margin: 8px 0;
    font-size: 13px;
    color: #9ca3af;
    line-height: 1.5;
    white-space: pre-wrap;
  }

  .details-block {
    margin: 8px 0;
  }

  .details-label {
    display: block;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: #6b7280;
    margin-bottom: 4px;
  }

  .task-details {
    margin: 0;
    font-family: monospace;
    font-size: 12px;
    color: #d1d5db;
    line-height: 1.5;
    white-space: pre-wrap;
    background: rgba(0, 0, 0, 0.25);
    padding: 10px;
    border-radius: 6px;
    overflow-x: auto;
  }

  .task-tags {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    margin-top: 8px;
  }

  .tag {
    font-size: 12px;
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



  .subtask-item span.done {
    text-decoration: line-through;
    color: #6b7280;
  }

  .subtask-status {
    margin-left: auto;
    font-size: 11px;
    text-transform: uppercase;
  }

  .expand-btn {
    width: 100%;
    padding: 10px;
    background: rgba(var(--accent-rgb), 0.1);
    border: 1px dashed rgba(var(--accent-rgb), 0.3);
    border-radius: 8px;
    color: var(--accent-light);
    font-size: 13px;
    cursor: pointer;
    margin-bottom: 16px;
  }

  .expand-btn:hover {
    background: rgba(var(--accent-rgb), 0.2);
  }

  .dependencies {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 16px;
    flex-wrap: wrap;
  }

  .dep-label {
    font-size: 13px;
    color: #6b7280;
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
    font-size: 13px;
    cursor: pointer;
    transition: all 0.2s;
  }

  .task-actions .action-btn:hover {
    background: rgba(255, 255, 255, 0.1);
  }

  .task-actions .action-btn.primary {
    background: rgba(var(--accent-rgb), 0.2);
    border-color: rgba(var(--accent-rgb), 0.3);
    color: var(--accent-light);
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
    background: rgba(var(--accent-rgb), 0.1);
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

  /* Wider than the other small dialogs: the options are task titles, and at
     400px most of them wrapped onto three or four lines each, which turned a
     short list into a wall. */
  .dialog-content.dependency-dialog {
    /* Task titles are sentences, not labels. At 720px the longer ones still
       wrapped; this fits most of them on one line while staying inside a
       laptop screen. */
    max-width: min(960px, 92vw);
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
    font-size: 13px;
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
    background: rgba(var(--accent-rgb), 0.2);
    color: var(--accent-light);
  }

  .api-info {
    display: block;
    margin-top: 8px;
    color: #60a5fa;
    font-size: 13px;
  }

  /* Subtasks Section Enhanced */
  .subtasks-section {
    margin-bottom: 16px;
  }

  .subtasks-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 13px;
    color: #6b7280;
    margin-bottom: 8px;
  }

  .add-subtask-btn {
    background: rgba(var(--accent-rgb), 0.1);
    border: 1px solid rgba(var(--accent-rgb), 0.3);
    color: var(--accent-light);
    padding: 2px 8px;
    border-radius: 4px;
    font-size: 12px;
    cursor: pointer;
  }

  .add-subtask-btn:hover {
    background: rgba(var(--accent-rgb), 0.2);
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

  /* Drawn rather than left to the platform.
     accent-color only tints the tick; an unchecked box keeps the platform's
     own rendering, which in this webview is a solid white square — bright
     enough on a dark panel to read as the only lit thing in the row. */
  .subtask-checkbox {
    appearance: none;
    -webkit-appearance: none;
    width: 15px;
    height: 15px;
    flex-shrink: 0;
    margin: 0;
    border: 1px solid rgba(255, 255, 255, 0.25);
    border-radius: 4px;
    background: rgba(255, 255, 255, 0.04);
    cursor: pointer;
    position: relative;
    transition: background 0.12s ease, border-color 0.12s ease;
  }

  .subtask-checkbox:hover {
    border-color: rgba(var(--accent-rgb), 0.6);
  }

  .subtask-checkbox:checked {
    background: var(--accent);
    border-color: var(--accent);
  }

  /* The tick is drawn with borders rather than a glyph, so it does not depend
     on a font having the character and cannot be shifted by line-height. */
  .subtask-checkbox:checked::after {
    content: '';
    position: absolute;
    left: 4px;
    top: 1px;
    width: 4px;
    height: 8px;
    border: solid #fff;
    border-width: 0 2px 2px 0;
    transform: rotate(45deg);
  }

  .subtask-checkbox:focus-visible {
    outline: 2px solid rgba(var(--accent-rgb), 0.6);
    outline-offset: 2px;
  }

  /* The dialogs' own checkboxes get the same treatment, for the same reason —
     they sit on the same dark panels. */
  .checkbox-label input[type='checkbox'] {
    appearance: none;
    -webkit-appearance: none;
    width: 15px;
    height: 15px;
    flex-shrink: 0;
    margin: 0;
    border: 1px solid rgba(255, 255, 255, 0.25);
    border-radius: 4px;
    background: rgba(255, 255, 255, 0.04);
    cursor: pointer;
    position: relative;
  }

  .checkbox-label input[type='checkbox']:checked {
    background: var(--accent);
    border-color: var(--accent);
  }

  .checkbox-label input[type='checkbox']:checked::after {
    content: '';
    position: absolute;
    left: 4px;
    top: 1px;
    width: 4px;
    height: 8px;
    border: solid #fff;
    border-width: 0 2px 2px 0;
    transform: rotate(45deg);
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
    font-size: 13px;
    color: #6b7280;
    margin-bottom: 8px;
  }

  .add-dep-btn {
    background: rgba(59, 130, 246, 0.1);
    border: 1px solid rgba(59, 130, 246, 0.3);
    color: #60a5fa;
    padding: 2px 8px;
    border-radius: 4px;
    font-size: 12px;
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
    font-size: 12px;
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
    font-size: 13px;
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
    font-size: 12px;
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
    background: rgba(var(--accent-rgb), 0.1);
    color: var(--accent-light);
    padding: 4px 10px;
    border-radius: 6px;
    font-size: 13px;
  }

  .dep-remove-inline {
    background: transparent;
    border: none;
    color: var(--accent-light);
    cursor: pointer;
    font-size: 14px;
    padding: 0;
    opacity: 0.7;
  }

  .dep-remove-inline:hover {
    opacity: 1;
    color: #ef4444;
  }

  /* The label lays its children out in a row, so the select took only the
     width its own text needed — in a 720px dialog the list still wrapped every
     task title onto four lines. Made a block so the select fills the dialog. */
  .dep-select-label {
    display: block;
  }

  .dep-select-label :global(.custom-select) {
    width: 100%;
    margin-top: 6px;
  }

  /* The trigger is a flex child of an inline-block; it has to be told to fill
     its parent, or the parent's 100% buys nothing. */
  .dep-select-label :global(.select-trigger) {
    width: 100%;
  }

  .dep-add-row {
    display: flex;
    justify-content: flex-end;
    margin-top: 10px;
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
    color: var(--accent);
    animation: mic-pulse 1.5s ease-in-out infinite;
  }

  textarea.dictating,
  input.dictating {
    border-color: rgba(var(--accent-rgb), 0.5) !important;
    box-shadow: 0 0 0 3px rgba(var(--accent-rgb), 0.15) !important;
  }

  @keyframes mic-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.5; }
  }
</style>
