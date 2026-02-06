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

// Stores
export const tasks = writable<Task[]>([]);
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

// Derived stores
export const selectedTask = derived(
  [tasks, selectedTaskId],
  ([$tasks, $selectedTaskId]) =>
    $tasks.find(t => t.id === $selectedTaskId) || null
);

export const filteredTasks = derived(
  [tasks, taskFilter],
  ([$tasks, $filter]) => {
    let filtered = [...$tasks];

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

export const sortedFilteredTasks = derived(filteredTasks, ($filtered) => {
  return [...$filtered].sort((a, b) => {
    // Sort by priority first
    const pa = priorityOrder[a.priority] ?? 3;
    const pb = priorityOrder[b.priority] ?? 3;
    if (pa !== pb) return pa - pb;

    // Then by ID (numeric order)
    const idA = parseFloat(a.id) || 0;
    const idB = parseFloat(b.id) || 0;
    return idA - idB;
  });
});

// ============================================================================
// Task Master MCP Actions
// ============================================================================

// Check Task Master status
export async function checkTaskMasterStatus(sessionId: string) {
  if (!sessionId) {
    taskMasterStatus.set({ initialized: false, running: false, error: 'No session selected' });
    return;
  }

  try {
    const status = await App.TaskMasterStatus(sessionId);
    taskMasterStatus.set(status as TaskMasterStatus);
  } catch (e) {
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

// Load tasks from Task Master
export async function loadTasks(sessionId: string) {
  if (!sessionId) {
    tasks.set([]);
    return;
  }

  isLoadingTasks.set(true);
  taskError.set(null);

  try {
    // Try MCP mode first
    if (get(useMCPMode)) {
      try {
        const result = await App.TaskMasterGetTasks(sessionId, '');
        tasks.set(result || []);
        return;
      } catch (e) {
        // Fall back to local mode if MCP fails
        console.warn('MCP mode failed, trying local mode:', e);
      }
    }

    // Local mode fallback (using our session/tasks.go)
    const result = await App.GetTasks(sessionId);
    // Convert local task format to MCP format
    const converted = (result || []).map((t: any) => ({
      ...t,
      status: t.status === 'backlog' ? 'pending' : t.status,
      subtasks: (t.subtasks || []).map((st: any) => ({
        ...st,
        status: st.done ? 'done' : 'pending'
      }))
    }));
    tasks.set(converted);
  } catch (e) {
    console.error('Failed to load tasks:', e);
    taskError.set(String(e));
    tasks.set([]);
  } finally {
    isLoadingTasks.set(false);
  }
}

// Get next task to work on
export async function getNextTask(sessionId: string): Promise<Task | null> {
  if (!sessionId) return null;

  try {
    if (get(useMCPMode)) {
      const task = await App.TaskMasterNextTask(sessionId);
      return task as Task | null;
    } else {
      const task = await App.GetNextTask(sessionId);
      return task as Task | null;
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

    tasks.update(t => t.map(task =>
      task.id === taskId ? { ...task, status } : task
    ));
  } catch (e) {
    taskError.set(String(e));
    throw e;
  }
}

// Add a new task (MCP mode with AI)
export async function addTask(sessionId: string, prompt: string, research: boolean = false, priority: string = 'medium') {
  console.log('[tasks.ts] addTask called:', { sessionId, prompt, research, priority, useMCPMode: get(useMCPMode) });
  if (!sessionId || !prompt.trim()) {
    console.log('[tasks.ts] addTask returning early - missing sessionId or prompt');
    return;
  }

  isLoadingTasks.set(true);
  taskError.set(null);

  try {
    if (get(useMCPMode)) {
      console.log('[tasks.ts] Calling App.TaskMasterAddTask (MCP mode)...');
      const result = await App.TaskMasterAddTask(sessionId, prompt, research, priority);
      console.log('[tasks.ts] App.TaskMasterAddTask result:', result);
    } else {
      console.log('[tasks.ts] Calling App.CreateTask (local mode)...');
      // Local mode - create task directly
      const result = await App.CreateTask(sessionId, prompt, '', priority, []);
      console.log('[tasks.ts] App.CreateTask result:', result);
    }
    console.log('[tasks.ts] Reloading tasks...');
    await loadTasks(sessionId);
    console.log('[tasks.ts] Tasks reloaded');
  } catch (e) {
    console.error('[tasks.ts] addTask error:', e);
    taskError.set(String(e));
    throw e;
  } finally {
    isLoadingTasks.set(false);
  }
}

// Add a new task manually (no AI required)
export async function addManualTask(sessionId: string, title: string, description: string = '', details: string = '', priority: string = 'medium') {
  console.log('[tasks.ts] addManualTask called:', { sessionId, title, description, details, priority, useMCPMode: get(useMCPMode) });
  if (!sessionId || !title.trim()) {
    console.log('[tasks.ts] addManualTask returning early - missing sessionId or title');
    return;
  }

  isLoadingTasks.set(true);
  taskError.set(null);

  try {
    if (get(useMCPMode)) {
      console.log('[tasks.ts] Calling App.TaskMasterAddManualTask (MCP mode)...');
      const result = await App.TaskMasterAddManualTask(sessionId, title, description, details, priority);
      console.log('[tasks.ts] App.TaskMasterAddManualTask result:', result);
    } else {
      console.log('[tasks.ts] Calling App.CreateTask (local mode)...');
      // Local mode - create task directly
      const result = await App.CreateTask(sessionId, title, description, priority, []);
      console.log('[tasks.ts] App.CreateTask result:', result);
    }
    console.log('[tasks.ts] Reloading tasks...');
    await loadTasks(sessionId);
    console.log('[tasks.ts] Tasks reloaded');
  } catch (e) {
    console.error('[tasks.ts] addManualTask error:', e);
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
