import { writable, derived, get } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';

// Types - matching Task Master format
export type TaskStatus = 'pending' | 'in-progress' | 'done' | 'blocked' | 'deferred';
export type TaskPriority = 'low' | 'medium' | 'high' | 'critical';

export interface Subtask {
  id: string;
  title: string;
  description?: string;
  status: TaskStatus;
  details?: string;
}

export interface Task {
  id: string;
  title: string;
  description: string;
  status: TaskStatus;
  priority: TaskPriority;
  tags: string[];
  subtasks: Subtask[];
  dependencies: string[];
  complexity?: number;
  details?: string;
  createdAt?: string;
}

export interface TaskFilter {
  status: TaskStatus | 'all';
  priority: TaskPriority | 'all';
  searchText: string;
}

export interface TaskMasterStatus {
  initialized: boolean;
  running: boolean;
  error: string | null;
  tools?: number;
}

export type TaskSortBy = 'priority' | 'status' | 'created-asc' | 'created-desc';

// Stores
export const tasks = writable<Task[]>([]);
export const taskSortBy = writable<TaskSortBy>('priority');
export const hideDone = writable<boolean>(true);
export const taskFilter = writable<TaskFilter>({
  status: 'all',
  priority: 'all',
  searchText: ''
});
export const selectedTaskId = writable<string | null>(null);
export const isLoadingTasks = writable<boolean>(false);
export const taskError = writable<string | null>(null);
export const taskMasterStatus = writable<TaskMasterStatus>({
  initialized: false,
  running: false,
  error: null
});
export const useMCPMode = writable<boolean>(true); // Default to MCP mode

let activeTasksSessionId = '';
let activeStatusSessionId = '';
let tasksLoadGeneration = 0;
let statusLoadGeneration = 0;

function normalizeStatus(status: string): TaskStatus {
  const normalized = status === 'backlog' ? 'pending' : status;
  return ['pending', 'in-progress', 'done', 'blocked', 'deferred'].includes(normalized)
    ? normalized as TaskStatus
    : 'pending';
}

function normalizePriority(priority: string): TaskPriority {
  return ['low', 'medium', 'high', 'critical'].includes(priority)
    ? priority as TaskPriority
    : 'medium';
}

function normalizeTask(task: any): Task {
  return {
    ...task,
    status: normalizeStatus(task.status),
    priority: normalizePriority(task.priority),
    tags: task.tags || [],
    dependencies: task.dependencies || [],
    subtasks: (task.subtasks || []).map((subtask: any) => ({
      ...subtask,
      status: normalizeStatus(subtask.status || (subtask.done ? 'done' : 'pending'))
    }))
  };
}

// Derived stores
export const selectedTask = derived(
  [tasks, selectedTaskId],
  ([$tasks, $selectedTaskId]) =>
    $tasks.find(t => t.id === $selectedTaskId) || null
);

export const filteredTasks = derived(
  [tasks, taskFilter, hideDone],
  ([$tasks, $filter, $hideDone]) => {
    let filtered = [...$tasks];

    // Hide done tasks if enabled (and status filter is not explicitly 'done')
    if ($hideDone && $filter.status !== 'done') {
      filtered = filtered.filter(t => t.status !== 'done');
    }

    // Filter by status
    if ($filter.status !== 'all') {
      filtered = filtered.filter(t => t.status === $filter.status);
    }

    // Filter by priority
    if ($filter.priority !== 'all') {
      filtered = filtered.filter(t => t.priority === $filter.priority);
    }

    // Filter by search text
    if ($filter.searchText) {
      const lower = $filter.searchText.toLowerCase();
      filtered = filtered.filter(t =>
        t.title.toLowerCase().includes(lower) ||
        t.description.toLowerCase().includes(lower) ||
        t.tags.some(tag => tag.toLowerCase().includes(lower))
      );
    }

    return filtered;
  }
);

export const tasksByStatus = derived(tasks, ($tasks) => {
  const result: Record<string, Task[]> = {
    'pending': [],
    'in-progress': [],
    'done': [],
    'blocked': [],
    'deferred': []
  };

  for (const task of $tasks) {
    if (result[task.status]) {
      result[task.status].push(task);
    }
  }

  return result;
});

export const taskStats = derived(tasks, ($tasks) => {
  const total = $tasks.length;
  const done = $tasks.filter(t => t.status === 'done').length;
  const inProgress = $tasks.filter(t => t.status === 'in-progress').length;
  const pending = $tasks.filter(t => t.status === 'pending').length;
  const blocked = $tasks.filter(t => t.status === 'blocked').length;

  return { total, done, inProgress, pending, blocked };
});

// Priority order for sorting
const priorityOrder: Record<TaskPriority, number> = {
  'critical': 0,
  'high': 1,
  'medium': 2,
  'low': 3
};

// Status order for sorting (done goes last)
const statusOrder: Record<string, number> = {
  'in-progress': 0,
  'blocked': 1,
  'pending': 2,
  'deferred': 3,
  'done': 4
};

export const sortedFilteredTasks = derived(
  [filteredTasks, taskSortBy],
  ([$filtered, $sortBy]) => {
    return [...$filtered].sort((a, b) => {
      if ($sortBy === 'status') {
        // Sort by status first (done last)
        const sa = statusOrder[a.status] ?? 2;
        const sb = statusOrder[b.status] ?? 2;
        if (sa !== sb) return sa - sb;
        // Then by priority
        const pa = priorityOrder[a.priority] ?? 3;
        const pb = priorityOrder[b.priority] ?? 3;
        if (pa !== pb) return pa - pb;
        // Then by ID
        const idA = parseFloat(a.id) || 0;
        const idB = parseFloat(b.id) || 0;
        return idA - idB;
      }

      if ($sortBy === 'created-desc' || $sortBy === 'created-asc') {
        const ascending = $sortBy === 'created-asc';
        const ca = a.createdAt || '';
        const cb = b.createdAt || '';
        if (ca && cb) return ascending ? ca.localeCompare(cb) : cb.localeCompare(ca);
        if (ca) return -1;
        if (cb) return 1;
        // Fallback to ID
        const idA = parseFloat(a.id) || 0;
        const idB = parseFloat(b.id) || 0;
        return idA - idB;
      }

      // Default: sort by priority
      const pa = priorityOrder[a.priority] ?? 3;
      const pb = priorityOrder[b.priority] ?? 3;
      if (pa !== pb) return pa - pb;
      const idA = parseFloat(a.id) || 0;
      const idB = parseFloat(b.id) || 0;
      return idA - idB;
    });
  }
);

// ============================================================================
// Task Master MCP Actions
// ============================================================================

// Check Task Master status
export async function checkTaskMasterStatus(sessionId: string) {
  activeStatusSessionId = sessionId;
  const generation = ++statusLoadGeneration;
  if (!sessionId) {
    taskMasterStatus.set({ initialized: false, running: false, error: 'No session selected' });
    return;
  }

  try {
    const status = await App.TaskMasterStatus(sessionId);
    if (generation !== statusLoadGeneration || sessionId !== activeStatusSessionId) return;
    taskMasterStatus.set(status as TaskMasterStatus);
  } catch (e) {
    if (generation !== statusLoadGeneration || sessionId !== activeStatusSessionId) return;
    taskMasterStatus.set({ initialized: false, running: false, error: String(e) });
  }
}

// Initialize Task Master for a project
export async function initializeTaskMaster(sessionId: string) {
  if (!sessionId) return;

  isLoadingTasks.set(true);
  taskError.set(null);

  try {
    await App.TaskMasterInit(sessionId);
    await checkTaskMasterStatus(sessionId);
    await loadTasks(sessionId);
  } catch (e) {
    taskError.set(String(e));
    throw e;
  } finally {
    isLoadingTasks.set(false);
  }
}

// Parse PRD into tasks
export async function parsePRD(sessionId: string, prdContent: string, numTasks: number = 10) {
  if (!sessionId || !prdContent.trim()) return;

  isLoadingTasks.set(true);
  taskError.set(null);

  try {
    await App.TaskMasterParsePRD(sessionId, prdContent, numTasks);
    await loadTasks(sessionId);
  } catch (e) {
    taskError.set(String(e));
    throw e;
  } finally {
    isLoadingTasks.set(false);
  }
}

// Preserve createdAt from existing tasks when merging with fresh data
function mergeCreatedAt(newTasks: Task[]): Task[] {
  const existing = get(tasks);
  if (existing.length === 0) return newTasks;
  const createdAtMap = new Map<string, string>();
  for (const t of existing) {
    if (t.createdAt) createdAtMap.set(t.id, t.createdAt);
  }
  return newTasks.map(t => ({
    ...t,
    createdAt: t.createdAt || createdAtMap.get(t.id)
  }));
}

// Load tasks from Task Master
export async function loadTasks(sessionId: string) {
  activeTasksSessionId = sessionId;
  const generation = ++tasksLoadGeneration;
  if (!sessionId) {
    tasks.set([]);
    taskError.set(null);
    isLoadingTasks.set(false);
    return;
  }

  isLoadingTasks.set(true);
  taskError.set(null);

  try {
    // Try MCP mode first
    if (get(useMCPMode)) {
      try {
        const result = await App.TaskMasterGetTasks(sessionId, '');
        if (generation !== tasksLoadGeneration || sessionId !== activeTasksSessionId) return;
        tasks.set(mergeCreatedAt((result || []).map(normalizeTask)));
        return;
      } catch (e) {
        // Fall back to local mode if MCP fails
        console.warn('MCP mode failed, trying local mode:', e);
      }
    }

    // Local mode fallback (using our session/tasks.go)
    const result = await App.GetTasks(sessionId);
    // Convert local task format to MCP format
    if (generation !== tasksLoadGeneration || sessionId !== activeTasksSessionId) return;
    const converted = (result || []).map(normalizeTask);
    tasks.set(mergeCreatedAt(converted));
  } catch (e) {
    if (generation !== tasksLoadGeneration || sessionId !== activeTasksSessionId) return;
    console.error('Failed to load tasks:', e);
    taskError.set(String(e));
    tasks.set([]);
  } finally {
    if (generation === tasksLoadGeneration && sessionId === activeTasksSessionId) {
      isLoadingTasks.set(false);
    }
  }
}

// Get next task to work on
export async function getNextTask(sessionId: string): Promise<Task | null> {
  if (!sessionId) return null;

  try {
    if (get(useMCPMode)) {
      const task = await App.TaskMasterNextTask(sessionId);
      return task ? normalizeTask(task) : null;
    } else {
      const task = await App.GetNextTask(sessionId);
      return task ? normalizeTask(task) : null;
    }
  } catch (e) {
    taskError.set(String(e));
    return null;
  }
}

// Set task status
export async function setTaskStatus(sessionId: string, taskId: string, status: TaskStatus) {
  if (!sessionId) return;

  try {
    if (get(useMCPMode)) {
      await App.TaskMasterSetStatus(sessionId, taskId, status);
    } else {
      await App.MoveTask(sessionId, taskId, status);
    }

    if (sessionId === activeTasksSessionId) {
      tasks.update(t => t.map(task =>
        task.id === taskId ? { ...task, status } : task
      ));
    }
  } catch (e) {
    taskError.set(String(e));
    throw e;
  }
}

// Add a new task (MCP mode with AI)
export async function addTask(sessionId: string, prompt: string, research: boolean = false, priority: string = 'medium') {
  if (!sessionId || !prompt.trim()) return;

  isLoadingTasks.set(true);
  taskError.set(null);

  try {
    let newTask: any;
    if (get(useMCPMode)) {
      newTask = await App.TaskMasterAddTask(sessionId, prompt, research, priority);
    } else {
      await App.CreateTask(sessionId, prompt, '', priority, []);
    }
    // Pre-inject createdAt so mergeCreatedAt preserves it across loadTasks
    if (newTask?.id) {
      const now = new Date().toISOString();
      tasks.update(t => [...t, { ...newTask, createdAt: newTask.createdAt || now } as Task]);
    }
    await loadTasks(sessionId);
  } catch (e) {
    taskError.set(String(e));
    throw e;
  } finally {
    isLoadingTasks.set(false);
  }
}

// Add a new task manually (no AI required)
export async function addManualTask(sessionId: string, title: string, description: string = '', details: string = '', priority: string = 'medium') {
  if (!sessionId || !title.trim()) return;

  isLoadingTasks.set(true);
  taskError.set(null);

  try {
    let newTask: any;
    if (get(useMCPMode)) {
      newTask = await App.TaskMasterAddManualTask(sessionId, title, description, details, priority);
    } else {
      await App.CreateTask(sessionId, title, description, priority, []);
    }
    // Pre-inject createdAt so mergeCreatedAt preserves it across loadTasks
    if (newTask?.id) {
      const now = new Date().toISOString();
      tasks.update(t => [...t, { ...newTask, createdAt: newTask.createdAt || now } as Task]);
    }
    await loadTasks(sessionId);
  } catch (e) {
    taskError.set(String(e));
    throw e;
  } finally {
    isLoadingTasks.set(false);
  }
}

// Update task (MCP mode with AI)
export async function updateTask(sessionId: string, taskId: string, prompt: string, research: boolean = false) {
  if (!sessionId || !taskId) return;

  try {
    if (get(useMCPMode)) {
      await App.TaskMasterUpdateTask(sessionId, taskId, prompt, research);
    } else {
      await App.UpdateTask(sessionId, taskId, { description: prompt });
    }
    await loadTasks(sessionId);
  } catch (e) {
    taskError.set(String(e));
    throw e;
  }
}

// Update subtask with implementation notes
export async function updateSubtask(sessionId: string, subtaskId: string, notes: string) {
  if (!sessionId || !subtaskId) return;

  try {
    await App.TaskMasterUpdateSubtask(sessionId, subtaskId, notes);
    await loadTasks(sessionId);
  } catch (e) {
    taskError.set(String(e));
    throw e;
  }
}

// Expand task into subtasks
export async function expandTask(sessionId: string, taskId: string, research: boolean = true, force: boolean = false) {
  if (!sessionId || !taskId) return;

  isLoadingTasks.set(true);
  taskError.set(null);

  try {
    await App.TaskMasterExpandTask(sessionId, taskId, research, force);
    await loadTasks(sessionId);
  } catch (e) {
    taskError.set(String(e));
    throw e;
  } finally {
    isLoadingTasks.set(false);
  }
}

// Expand all eligible tasks
export async function expandAllTasks(sessionId: string, research: boolean = true) {
  if (!sessionId) return;

  isLoadingTasks.set(true);
  taskError.set(null);

  try {
    await App.TaskMasterExpandAll(sessionId, research);
    await loadTasks(sessionId);
  } catch (e) {
    taskError.set(String(e));
    throw e;
  } finally {
    isLoadingTasks.set(false);
  }
}

// Analyze complexity
export async function analyzeComplexity(sessionId: string, research: boolean = true): Promise<string> {
  if (!sessionId) return '';

  isLoadingTasks.set(true);
  taskError.set(null);

  try {
    const result = await App.TaskMasterAnalyzeComplexity(sessionId, research);
    await loadTasks(sessionId);
    return result;
  } catch (e) {
    taskError.set(String(e));
    throw e;
  } finally {
    isLoadingTasks.set(false);
  }
}

// Remove a task
export async function removeTask(sessionId: string, taskId: string) {
  if (!sessionId || !taskId) return;

  try {
    if (get(useMCPMode)) {
      await App.TaskMasterRemoveTask(sessionId, taskId);
    } else {
      await App.DeleteTask(sessionId, taskId);
    }
    tasks.update(t => t.filter(task => task.id !== taskId));
    if (get(selectedTaskId) === taskId) {
      selectedTaskId.set(null);
    }
  } catch (e) {
    taskError.set(String(e));
    throw e;
  }
}

// Send task to agent
export async function sendTaskToAgent(sessionId: string, taskId: string) {
  if (!sessionId || !taskId) return;

  try {
    if (get(useMCPMode)) {
      await App.TaskMasterSendToAgent(sessionId, taskId);
    } else {
      await App.SendTaskToAgent(sessionId, taskId);
    }
  } catch (e) {
    taskError.set(String(e));
    throw e;
  }
}

// Update task directly (no AI)
export async function updateTaskDirect(sessionId: string, taskId: string, title: string, description: string, details: string, priority: string) {
  if (!sessionId || !taskId) return;

  try {
    await App.TaskMasterUpdateTaskDirect(sessionId, taskId, title, description, details, priority);
    await loadTasks(sessionId);
  } catch (e) {
    taskError.set(String(e));
    throw e;
  }
}

// Add subtask to a task
export async function addSubtask(sessionId: string, taskId: string, title: string, description: string = '') {
  if (!sessionId || !taskId || !title.trim()) return;

  try {
    await App.TaskMasterAddSubtask(sessionId, taskId, title, description);
    await loadTasks(sessionId);
  } catch (e) {
    taskError.set(String(e));
    throw e;
  }
}

// Remove a subtask
export async function removeSubtask(sessionId: string, subtaskId: string) {
  if (!sessionId || !subtaskId) return;

  try {
    await App.TaskMasterRemoveSubtask(sessionId, subtaskId);
    await loadTasks(sessionId);
  } catch (e) {
    taskError.set(String(e));
    throw e;
  }
}

// Clear all subtasks from a task
export async function clearSubtasks(sessionId: string, taskId: string) {
  if (!sessionId || !taskId) return;

  try {
    await App.TaskMasterClearSubtasks(sessionId, taskId);
    await loadTasks(sessionId);
  } catch (e) {
    taskError.set(String(e));
    throw e;
  }
}

// Set subtask status
export async function setSubtaskStatus(sessionId: string, subtaskId: string, status: TaskStatus) {
  if (!sessionId || !subtaskId) return;

  try {
    await App.TaskMasterSetSubtaskStatus(sessionId, subtaskId, status);
    await loadTasks(sessionId);
  } catch (e) {
    taskError.set(String(e));
    throw e;
  }
}

// Add dependency to a task
export async function addDependency(sessionId: string, taskId: string, dependsOnId: string) {
  if (!sessionId || !taskId || !dependsOnId) return;

  try {
    await App.TaskMasterAddDependency(sessionId, taskId, dependsOnId);
    await loadTasks(sessionId);
  } catch (e) {
    taskError.set(String(e));
    throw e;
  }
}

// Remove dependency from a task
export async function removeDependency(sessionId: string, taskId: string, dependsOnId: string) {
  if (!sessionId || !taskId || !dependsOnId) return;

  try {
    await App.TaskMasterRemoveDependency(sessionId, taskId, dependsOnId);
    await loadTasks(sessionId);
  } catch (e) {
    taskError.set(String(e));
    throw e;
  }
}

// ============================================================================
// UI Helpers
// ============================================================================

export function selectTask(id: string | null) {
  selectedTaskId.set(id);
}

export function setTaskFilter(filter: Partial<TaskFilter>) {
  taskFilter.update(f => ({ ...f, ...filter }));
}

export function setTaskSortBy(sortBy: TaskSortBy) {
  taskSortBy.set(sortBy);
}

export function toggleHideDone() {
  hideDone.update(v => !v);
}

export function clearTaskFilter() {
  taskFilter.set({
    status: 'all',
    priority: 'all',
    searchText: ''
  });
}

export function toggleMCPMode() {
  useMCPMode.update(v => !v);
}
