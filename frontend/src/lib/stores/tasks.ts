import { writable, derived, get } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';

// Types - matching Task Master format
export type TaskStatus = 'pending' | 'in-progress' | 'done' | 'blocked' | 'deferred';
export type TaskPriority = 'low' | 'medium' | 'high' | 'critical';

export interface Subtask {
  id: string;
  title: string;
  description?: string;
  status: TaskStatus;
  details?: string;
  done?: boolean;
  createdAt?: string;
	dependencies?: string[];
	parentId?: string;
	testStrategy?: string;
	rawJson?: string;
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
  updatedAt?: string;
  /** When the task was ticked off. Absent while it is still open. */
  completedAt?: string;
  /**
   * When the task is due, RFC 3339. Absent when it has no deadline — which is
   * most of them, so this stays optional rather than defaulting to a date.
   */
  dueAt?: string;
  /**
   * The session this task belongs to, if any.
   *
   * Only tasks tied to a session make closing it ask about unfinished work; one
   * belonging to the project as a whole leaves this empty.
   */
  sessionId?: string;
	testStrategy?: string;
	rawJson?: string;
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

export type TaskSortBy =
  | 'priority'
  | 'status'
  | 'created-asc'
  | 'created-desc'
  | 'completed-desc'
  | 'completed-asc';

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
export type TaskProvider = 'mcp' | 'local';
const effectiveProviderBySession = new Map<string, { requestedMCP: boolean; provider: TaskProvider }>();

function providerFor(sessionId: string): TaskProvider {
  const requestedMCP = get(useMCPMode);
  const effective = effectiveProviderBySession.get(sessionId);
  if (effective && effective.requestedMCP === requestedMCP) return effective.provider;
  return requestedMCP ? 'mcp' : 'local';
}

function rememberProvider(sessionId: string, requestedMCP: boolean, provider: TaskProvider) {
  effectiveProviderBySession.set(sessionId, { requestedMCP, provider });
}

function isActiveTasksSession(sessionId: string): boolean {
  return sessionId === activeTasksSessionId;
}

/** Claim the visible list for a new session before any provider probe awaits. */
export function prepareTasksSession(sessionId: string): void {
  activeTasksSessionId = sessionId;
  tasksLoadGeneration++;
  tasks.set([]);
  selectedTaskId.set(null);
  taskError.set(null);
  isLoadingTasks.set(!!sessionId);
}

async function reloadTasksIfActive(sessionId: string): Promise<void> {
  if (isActiveTasksSession(sessionId)) await loadTasks(sessionId);
}

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
      // Implementation details are searched too: they are where the pasted
      // plan or command usually lives, so leaving them out means a task you
      // remember by a line from its notes cannot be found at all.
      filtered = filtered.filter(t =>
        t.title.toLowerCase().includes(lower) ||
        t.description.toLowerCase().includes(lower) ||
        (t.details || '').toLowerCase().includes(lower) ||
        t.tags.some(tag => tag.toLowerCase().includes(lower)) ||
        (t.subtasks || []).some(sub => sub.title.toLowerCase().includes(lower))
      );
    }

    return filtered;
  }
);

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

/**
 * Two finished tasks, most recently completed first.
 *
 * Returns 0 when neither carries a completion time — tasks finished before the
 * field was recorded, or through a path that does not set it — so the caller
 * falls through to its usual ordering rather than shuffling them arbitrarily.
 * One that has a time sorts above one that does not: it is the one we know
 * something about.
 */
function compareCompletion(a: Task, b: Task): number {
  const ca = a.completedAt || '';
  const cb = b.completedAt || '';
  if (ca && cb) return cb.localeCompare(ca);
  if (ca) return -1;
  if (cb) return 1;
  return 0;
}

export const sortedFilteredTasks = derived(
  [filteredTasks, taskSortBy],
  ([$filtered, $sortBy]) => {
    return [...$filtered].sort((a, b) => {
      if ($sortBy === 'status') {
        // Sort by status first (done last)
        const sa = statusOrder[a.status] ?? 2;
        const sb = statusOrder[b.status] ?? 2;
        if (sa !== sb) return sa - sb;
        // Finished tasks read as a record of what was done, so they go in the
        // order they were ticked off, most recent first. Priority is the wrong
        // key for them: it says what to do next, and there is no next.
        if (a.status === 'done' && b.status === 'done') {
          const done = compareCompletion(a, b);
          if (done !== 0) return done;
        }
        // Then by priority
        const pa = priorityOrder[a.priority] ?? 3;
        const pb = priorityOrder[b.priority] ?? 3;
        if (pa !== pb) return pa - pb;
        // Then by ID
        const idA = parseFloat(a.id) || 0;
        const idB = parseFloat(b.id) || 0;
        return idA - idB;
      }

      if ($sortBy === 'completed-desc' || $sortBy === 'completed-asc') {
        const ascending = $sortBy === 'completed-asc';
        const ca = a.completedAt || '';
        const cb = b.completedAt || '';
        if (ca && cb) return ascending ? ca.localeCompare(cb) : cb.localeCompare(ca);
        // Unfinished tasks have no completion time. They go last in both
        // directions rather than at whichever end the sort puts empty strings:
        // sorting BY completion is a question about what is done, so the ones
        // that are not belong out of the way.
        if (ca) return -1;
        if (cb) return 1;
        // Among the unfinished, keep the default ordering rather than leaving
        // them in whatever order they arrived.
        const pa = priorityOrder[a.priority] ?? 3;
        const pb = priorityOrder[b.priority] ?? 3;
        if (pa !== pb) return pa - pb;
        return (parseFloat(a.id) || 0) - (parseFloat(b.id) || 0);
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
    await reloadTasksIfActive(sessionId);
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
    throw e;
  } finally {
    if (isActiveTasksSession(sessionId)) isLoadingTasks.set(false);
  }
}

// Parse PRD into tasks
export async function parsePRD(sessionId: string, prdContent: string, numTasks: number = 10) {
  if (!sessionId || !prdContent.trim()) return;

  isLoadingTasks.set(true);
  taskError.set(null);

  try {
    await App.TaskMasterParsePRD(sessionId, prdContent, numTasks);
    await reloadTasksIfActive(sessionId);
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
    throw e;
  } finally {
    if (isActiveTasksSession(sessionId)) isLoadingTasks.set(false);
  }
}

// Preserve createdAt from existing tasks when merging with fresh data
function mergeCreatedAt(newTasks: Task[]): Task[] {
  const existing = get(tasks);
  if (existing.length === 0) return newTasks;
  const createdAtMap = new Map<string, string>();
  // completedAt travels with createdAt for the same reason: Task Master does
  // not always return it, and losing it would drop a finished task out of the
  // order it was ticked off in.
  const completedAtMap = new Map<string, string>();
  for (const t of existing) {
    if (t.createdAt) createdAtMap.set(t.id, t.createdAt);
    if (t.completedAt) completedAtMap.set(t.id, t.completedAt);
  }
  return newTasks.map(t => ({
    ...t,
    createdAt: t.createdAt || createdAtMap.get(t.id),
    completedAt: t.completedAt || completedAtMap.get(t.id),
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

  const requestedMCP = get(useMCPMode);
  try {
    // Try MCP mode first
    if (requestedMCP) {
      try {
        const result = await App.TaskMasterGetTasks(sessionId, '');
        if (generation !== tasksLoadGeneration || sessionId !== activeTasksSessionId) return;
        rememberProvider(sessionId, requestedMCP, 'mcp');
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
    rememberProvider(sessionId, requestedMCP, 'local');
    const converted = (result || []).map(normalizeTask);
    tasks.set(mergeCreatedAt(converted));
  } catch (e) {
    if (generation !== tasksLoadGeneration || sessionId !== activeTasksSessionId) return;
    console.error('Failed to load tasks:', e);
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
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
    if (providerFor(sessionId) === 'mcp') {
      const task = await App.TaskMasterNextTask(sessionId);
      return task ? normalizeTask(task) : null;
    } else {
      const task = await App.GetNextTask(sessionId);
      return task ? normalizeTask(task) : null;
    }
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
    return null;
  }
}

// Set task status
export async function setTaskStatus(sessionId: string, taskId: string, status: TaskStatus, requestedProvider?: TaskProvider): Promise<TaskProvider> {
  if (!sessionId) throw new Error('session is required');

  const provider = requestedProvider ?? providerFor(sessionId);

  try {
    if (provider === 'mcp') {
      await App.TaskMasterSetStatus(sessionId, taskId, status);
    } else {
      await App.MoveTask(sessionId, taskId, status);
    }

    if (sessionId === activeTasksSessionId) {
      tasks.update(t => t.map(task =>
        task.id === taskId ? { ...task, status } : task
      ));
    }
    return provider;
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
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
    if (providerFor(sessionId) === 'mcp') {
      newTask = await App.TaskMasterAddTask(sessionId, prompt, research, priority);
    } else {
      newTask = await App.CreateTask(sessionId, prompt, '', priority, []);
    }
    // Pre-inject createdAt so mergeCreatedAt preserves it across loadTasks
    if (newTask?.id && isActiveTasksSession(sessionId)) {
      const now = new Date().toISOString();
      tasks.update(t => [...t, { ...newTask, createdAt: newTask.createdAt || now } as Task]);
    }
    await reloadTasksIfActive(sessionId);
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
    throw e;
  } finally {
    if (isActiveTasksSession(sessionId)) isLoadingTasks.set(false);
  }
}

// Add a new task manually (no AI required)
export async function addManualTask(sessionId: string, title: string, description: string = '', details: string = '', priority: string = 'medium'): Promise<Task | undefined> {
  if (!sessionId || !title.trim()) return;

  isLoadingTasks.set(true);
  taskError.set(null);

  try {
    let newTask: any;
    if (providerFor(sessionId) === 'mcp') {
      newTask = await App.TaskMasterAddManualTask(sessionId, title, description, details, priority);
    } else {
      newTask = await App.CreateTask(sessionId, title, description, priority, []);
    }
    // Pre-inject createdAt so mergeCreatedAt preserves it across loadTasks
    if (newTask?.id && isActiveTasksSession(sessionId)) {
      const now = new Date().toISOString();
      tasks.update(t => [...t, { ...newTask, createdAt: newTask.createdAt || now } as Task]);
    }
    await reloadTasksIfActive(sessionId);
    return newTask ? normalizeTask(newTask) : undefined;
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
    throw e;
  } finally {
    if (isActiveTasksSession(sessionId)) isLoadingTasks.set(false);
  }
}

/** Restore the provider-neutral snapshot atomically with its original IDs. */
export async function restoreDeletedTask(sessionId: string, snapshot: Task, provider: TaskProvider): Promise<void> {
  if (!sessionId || !snapshot.title.trim()) return;
  if (isActiveTasksSession(sessionId)) {
    isLoadingTasks.set(true);
    taskError.set(null);
  }
  try {
    await App.RestoreDeletedTask(sessionId, provider, new main.DeletedTaskSnapshot(snapshot));
    await reloadTasksIfActive(sessionId);
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
    throw e;
  } finally {
    if (isActiveTasksSession(sessionId)) isLoadingTasks.set(false);
  }
}

export async function restoreDeletedSubtask(
  sessionId: string,
  taskId: string,
  snapshot: Subtask,
  provider: TaskProvider,
): Promise<void> {
  await App.RestoreDeletedSubtask(
    sessionId,
    provider,
    taskId,
    new main.DeletedSubtaskSnapshot(snapshot),
  );
  await reloadTasksIfActive(sessionId);
}

// Update task (MCP mode with AI)
export async function updateTask(sessionId: string, taskId: string, prompt: string, research: boolean = false) {
  if (!sessionId || !taskId) return;

  try {
    if (providerFor(sessionId) === 'mcp') {
      await App.TaskMasterUpdateTask(sessionId, taskId, prompt, research);
    } else {
      await App.UpdateTask(sessionId, taskId, { description: prompt });
    }
    await reloadTasksIfActive(sessionId);
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
    throw e;
  }
}

// Update subtask with implementation notes
export async function updateSubtask(sessionId: string, subtaskId: string, notes: string) {
  if (!sessionId || !subtaskId) return;

  try {
    await App.TaskMasterUpdateSubtask(sessionId, subtaskId, notes);
    await reloadTasksIfActive(sessionId);
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
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
    await reloadTasksIfActive(sessionId);
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
    throw e;
  } finally {
    if (isActiveTasksSession(sessionId)) isLoadingTasks.set(false);
  }
}

// Expand all eligible tasks
export async function expandAllTasks(sessionId: string, research: boolean = true) {
  if (!sessionId) return;

  isLoadingTasks.set(true);
  taskError.set(null);

  try {
    await App.TaskMasterExpandAll(sessionId, research);
    await reloadTasksIfActive(sessionId);
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
    throw e;
  } finally {
    if (isActiveTasksSession(sessionId)) isLoadingTasks.set(false);
  }
}

// Analyze complexity
export async function analyzeComplexity(sessionId: string, research: boolean = true): Promise<string> {
  if (!sessionId) return '';

  isLoadingTasks.set(true);
  taskError.set(null);

  try {
    const result = await App.TaskMasterAnalyzeComplexity(sessionId, research);
    await reloadTasksIfActive(sessionId);
    return result;
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
    throw e;
  } finally {
    if (isActiveTasksSession(sessionId)) isLoadingTasks.set(false);
  }
}

// Remove a task
export async function removeTask(sessionId: string, taskId: string): Promise<TaskProvider> {
  if (!sessionId || !taskId) throw new Error('session and task are required');

  const provider = providerFor(sessionId);

  try {
    if (provider === 'mcp') {
      await App.TaskMasterRemoveTask(sessionId, taskId);
    } else {
      await App.DeleteTask(sessionId, taskId);
    }
    if (isActiveTasksSession(sessionId)) tasks.update(t => t.filter(task => task.id !== taskId));
    if (isActiveTasksSession(sessionId) && get(selectedTaskId) === taskId) {
      selectedTaskId.set(null);
    }
    return provider;
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
    throw e;
  }
}

// Send task to agent
export async function sendTaskToAgent(sessionId: string, taskId: string) {
  if (!sessionId || !taskId) return;

  try {
    if (providerFor(sessionId) === 'mcp') {
      await App.TaskMasterSendToAgent(sessionId, taskId);
    } else {
      await App.SendTaskToAgent(sessionId, taskId);
    }
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
    throw e;
  }
}

// Update task directly (no AI)
/**
 * Save an edited task.
 *
 * dueAt is RFC 3339, or "" to clear the deadline. It is passed only when the
 * caller actually means to change it — the backend keys on the field being
 * present, so an unrelated edit that always sent it would wipe the deadline of
 * every task it touched.
 *
 * Task Master has no deadline field of its own, so in MCP mode the deadline is
 * written through the app's own storage alongside the Task Master update. That
 * keeps the feature working in both modes rather than silently doing nothing in
 * one of them.
 */
export async function updateTaskDirect(sessionId: string, taskId: string, title: string, description: string, details: string, priority: string, dueAt?: string, sessionScoped?: boolean) {
  if (!sessionId || !taskId) return;

  try {
    if (providerFor(sessionId) === 'mcp') {
      // One provider, one atomic replacement. Writing the extra fields through
      // the local update API targeted a separate tasks.json after the MCP file
      // had already changed, leaving a partial edit and a permanent error.
      await App.TaskMasterUpdateTaskDirect(
        sessionId,
        taskId,
        title,
        description,
        details,
        priority,
        dueAt ?? '',
        sessionScoped ? sessionId : '',
      );
    } else {
      // Editing a task is the app's own operation — it has storage for these
      // fields and no reason to ask Task Master. Calling it regardless is what
      // made saving an edit fail with "Task Master is turned off".
      const updates: Record<string, unknown> = { title, description, details, priority };
      if (dueAt !== undefined) updates.dueAt = dueAt;
      // Empty string detaches the task from the session; the backend keys on
      // the field being present, so an edit that never sends it leaves the
      // assignment alone.
      if (sessionScoped !== undefined) updates.sessionId = sessionScoped ? sessionId : '';
      await App.UpdateTask(sessionId, taskId, updates);
    }
    await reloadTasksIfActive(sessionId);
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
    throw e;
  }
}


/**
 * Rewrite one task's subtasks or dependencies through the app's own storage.
 *
 * Task Master exposes an endpoint per operation — add a subtask, remove one,
 * set its status. The local store has no such endpoints, and adding six would
 * be six ways to write the same file. It has UpdateTask, which takes whole
 * fields, so the change is made here on the list already in memory and written
 * back in one call.
 */
async function editTaskLocally(
  sessionId: string,
  taskId: string,
  change: (task: Task) => Partial<Task>,
): Promise<void> {
  // Undo can run after the user has moved to another session. In that case the
  // global store belongs to the new session, so read the requested target
  // instead of accidentally editing a same-id task from the visible list.
  const source = isActiveTasksSession(sessionId)
    ? get(tasks)
    : ((await App.GetTasks(sessionId)) || []).map(normalizeTask);
  const task = source.find((t) => String(t.id) === String(taskId));
  if (!task) throw new Error(`no such task: ${taskId}`);
  await App.UpdateTask(sessionId, String(taskId), change(task) as Record<string, any>);
}

/** The task a subtask id belongs to: Task Master addresses them as "3.1". */
export function parentTaskId(subtaskId: string): string {
  return String(subtaskId).split('.')[0];
}

/** Allocate a stable local subtask id without reusing a surviving sibling. */
function nextLocalSubtaskId(subtasks: Subtask[]): string {
  const occupied = new Set(subtasks.map((subtask) => String(subtask.id)));
  const numeric = new Set<number>();
  let max = 0;
  for (const subtask of subtasks) {
    const text = String(subtask.id);
    if (!/^\d+$/.test(text)) continue;
    const value = Number(text);
    if (!Number.isSafeInteger(value) || value < 0) continue;
    numeric.add(value);
    if (value > max) max = value;
  }

  if (max < Number.MAX_SAFE_INTEGER) return String(max + 1);

  // An outlier at MAX_SAFE_INTEGER cannot be incremented exactly. Fall back to
  // the first free positive integer instead of emitting a rounded duplicate.
  for (let candidate = 1; candidate < Number.MAX_SAFE_INTEGER; candidate++) {
    if (!numeric.has(candidate) && !occupied.has(String(candidate))) return String(candidate);
  }
  throw new Error('no safe local subtask ID is available');
}

// Add subtask to a task
export async function addSubtask(sessionId: string, taskId: string, title: string, description: string = '') {
  if (!sessionId || !taskId || !title.trim()) return;

  try {
    if (providerFor(sessionId) === 'mcp') {
      await App.TaskMasterAddSubtask(sessionId, taskId, title, description);
    } else {
      await editTaskLocally(sessionId, taskId, (task) => ({
        subtasks: [
          ...(task.subtasks || []),
          // Numbered within the task, as Task Master does, so the two stores
          // produce ids of the same shape. Use max+1, not length+1: after a
          // deletion the latter can reuse an ID that still belongs to a sibling.
          { id: nextLocalSubtaskId(task.subtasks || []), title, description, status: 'pending' },
        ],
      }));
    }
    await reloadTasksIfActive(sessionId);
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
    throw e;
  }
}

// Remove a subtask
export async function removeSubtask(sessionId: string, subtaskId: string): Promise<TaskProvider> {
  if (!sessionId || !subtaskId) throw new Error('session and subtask are required');

  const provider = providerFor(sessionId);

  try {
    if (provider === 'mcp') {
      await App.TaskMasterRemoveSubtask(sessionId, subtaskId);
    } else {
      const parent = parentTaskId(subtaskId);
      const child = String(subtaskId).slice(parent.length + 1);
      await editTaskLocally(sessionId, parent, (task) => ({
        subtasks: (task.subtasks || []).filter((sub: any) => String(sub.id) !== child),
      }));
    }
    await reloadTasksIfActive(sessionId);
    return provider;
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
    throw e;
  }
}

// Clear all subtasks from a task
export async function clearSubtasks(sessionId: string, taskId: string) {
  if (!sessionId || !taskId) return;

  try {
    if (providerFor(sessionId) === 'mcp') {
      await App.TaskMasterClearSubtasks(sessionId, taskId);
    } else {
      await editTaskLocally(sessionId, taskId, () => ({ subtasks: [] }));
    }
    await reloadTasksIfActive(sessionId);
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
    throw e;
  }
}

// Set subtask status
export async function setSubtaskStatus(sessionId: string, subtaskId: string, status: TaskStatus, requestedProvider?: TaskProvider): Promise<TaskProvider> {
  if (!sessionId || !subtaskId) throw new Error('session and subtask are required');

  const provider = requestedProvider ?? providerFor(sessionId);

  try {
    if (provider === 'mcp') {
      await App.TaskMasterSetSubtaskStatus(sessionId, subtaskId, status);
    } else {
      const parent = parentTaskId(subtaskId);
      const child = String(subtaskId).slice(parent.length + 1);
      await editTaskLocally(sessionId, parent, (task) => {
        let found = false;
        const subtasks = (task.subtasks || []).map((subtask) => {
          if (String(subtask.id) !== child) return subtask;
          found = true;
          // Both spellings are written deliberately. The local model persists
          // Done, while its normalized frontend shape also exposes Status.
          return { ...subtask, status, done: status === 'done' };
        });
        if (!found) throw new Error(`no such subtask: ${subtaskId}`);
        return { subtasks };
      });
    }
    await reloadTasksIfActive(sessionId);
    return provider;
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
    throw e;
  }
}

// Add dependency to a task
export async function addDependency(sessionId: string, taskId: string, dependsOnId: string, requestedProvider?: TaskProvider): Promise<TaskProvider> {
  if (!sessionId || !taskId || !dependsOnId) throw new Error('session, task and dependency are required');

  const provider = requestedProvider ?? providerFor(sessionId);

  try {
    if (provider === 'mcp') {
      await App.TaskMasterAddDependency(sessionId, taskId, dependsOnId);
    } else {
      await editTaskLocally(sessionId, taskId, (task) => ({
        // Deduplicated: adding the same dependency twice is a no-op, not an
        // error worth interrupting the user for.
        dependencies: Array.from(new Set([...(task.dependencies || []), String(dependsOnId)])),
      }));
    }
    await reloadTasksIfActive(sessionId);
    return provider;
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
    throw e;
  }
}

// Remove dependency from a task
export async function removeDependency(sessionId: string, taskId: string, dependsOnId: string): Promise<TaskProvider> {
  if (!sessionId || !taskId || !dependsOnId) throw new Error('session, task and dependency are required');

  const provider = providerFor(sessionId);

  try {
    if (provider === 'mcp') {
      await App.TaskMasterRemoveDependency(sessionId, taskId, dependsOnId);
    } else {
      await editTaskLocally(sessionId, taskId, (task) => ({
        dependencies: (task.dependencies || []).filter(
          (dep: any) => String(dep) !== String(dependsOnId)),
      }));
    }
    await reloadTasksIfActive(sessionId);
    return provider;
  } catch (e) {
    if (isActiveTasksSession(sessionId)) taskError.set(String(e));
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
