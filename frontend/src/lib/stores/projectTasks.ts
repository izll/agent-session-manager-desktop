import { get, writable } from 'svelte/store';
import * as App from '../../../wailsjs/go/main/App';
import { activeProjectId } from './projects';
import { createTaskStore, sessionTaskStore, type Task } from './tasks';
import { PROJECT_TASKS_SCOPE } from '../utils/projectScope';

/**
 * The project's own task list and note — the ones that belong to no session.
 *
 * The list is the same kind of list a session has, kept by the same backend
 * code beside the project's sessions.json. The task API addresses it with
 * PROJECT_TASKS_SCOPE in place of a session ID.
 */
export { PROJECT_TASKS_SCOPE };

/** The project list as the task panel shows it, separate from a session's. */
export const projectTaskStore = createTaskStore({ localOnly: true });

export type ProjectWorkspaceView = 'tasks' | 'notes';

/** Whether the project tasks window is open, and which half it shows. */
export const projectTasksOpen = writable(false);
export const projectTasksView = writable<ProjectWorkspaceView>('tasks');

export function openProjectTasks(view?: ProjectWorkspaceView) {
  if (view) projectTasksView.set(view);
  projectTasksOpen.set(true);
}

export function closeProjectTasks() {
  projectTasksOpen.set(false);
}

/** Open tasks in the active project's own list, for the header badge. */
export const projectOpenTaskCount = writable(0);
let countGeneration = 0;

/**
 * Recount the active project's open tasks.
 *
 * A failed read leaves the count alone, as the all-tasks badge does: a
 * transient error must not claim there is nothing left to do. A count that
 * arrives after the project changed is dropped, not shown for the new one.
 */
export async function refreshProjectOpenCount(): Promise<void> {
  const generation = ++countGeneration;
  const projectId = get(activeProjectId);
  try {
    const listed = (await App.GetTasks(PROJECT_TASKS_SCOPE)) || [];
    if (generation !== countGeneration || projectId !== get(activeProjectId)) return;
    projectOpenTaskCount.set(listed.filter((task: { status?: string }) => task.status !== 'done').length);
  } catch {
    // Left as it was; see above.
  }
}

/**
 * Keep the count current: at once when the project changes or the panel's
 * list does, and every few minutes for changes made outside this window.
 */
export function watchProjectOpenCount(): () => void {
  const timer = setInterval(() => void refreshProjectOpenCount(), 5 * 60 * 1000);
  // Both subscriptions fire immediately, which is the first count.
  const unsubscribeProject = activeProjectId.subscribe(() => {
    projectOpenTaskCount.set(0);
    void refreshProjectOpenCount();
  });
  const unsubscribeTasks = projectTaskStore.tasks.subscribe(() => void refreshProjectOpenCount());
  const refresh = () => void refreshProjectOpenCount();
  window.addEventListener('tasks:refresh', refresh);
  window.addEventListener(TASK_LISTS_CHANGED, refresh);
  return () => {
    clearInterval(timer);
    unsubscribeProject();
    unsubscribeTasks();
    window.removeEventListener('tasks:refresh', refresh);
    window.removeEventListener(TASK_LISTS_CHANGED, refresh);
  };
}

/**
 * Fired after a task moved between lists, for the views that only read them —
 * the all-tasks overview and the badges.
 *
 * Not 'tasks:refresh': that one makes an open task panel start over, empty
 * and loading, which would close the very dialog the move was made from.
 * The two panels are reloaded in place instead, below.
 */
export const TASK_LISTS_CHANGED = 'tasks:lists-changed';

async function afterMove(sessionId: string): Promise<void> {
  await Promise.all([
    projectTaskStore.reloadTasksIfActive(PROJECT_TASKS_SCOPE),
    sessionTaskStore.reloadTasksIfActive(sessionId),
  ]);
  window.dispatchEvent(new CustomEvent(TASK_LISTS_CHANGED));
}

/**
 * Move a project task into a session's list, optionally onto one of its tabs.
 * Resolves to the task as it now is — its ID may have changed.
 */
export async function moveTaskToSession(taskId: string, sessionId: string, tabId: string): Promise<Task> {
  const moved = await App.MoveTaskToSession(taskId, sessionId, tabId, get(activeProjectId));
  await afterMove(sessionId);
  return moved as unknown as Task;
}

/** Move a session's task into the project's own list. */
export async function moveTaskToProject(sessionId: string, taskId: string): Promise<Task> {
  const moved = await App.MoveTaskToProject(sessionId, taskId, get(activeProjectId));
  await afterMove(sessionId);
  return moved as unknown as Task;
}

/** Type a project task into a session's agent; the task stays where it is. */
export async function sendProjectTaskToAgent(taskId: string, sessionId: string, tabId: string): Promise<void> {
  await App.SendProjectTaskToAgent(taskId, sessionId, tabId, get(activeProjectId));
}
