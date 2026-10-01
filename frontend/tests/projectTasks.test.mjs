import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import ts from 'typescript';

// The project's own task list and note: one list per project that belongs to
// no session, shown by the same task panel and notes view as a session's, and
// reachable from the dashboard, the header, a shortcut and the palette.

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const tasksStore = read('../src/lib/stores/tasks.ts');
const projectStore = read('../src/lib/stores/projectTasks.ts');
const projectScope = read('../src/lib/utils/projectScope.ts');
const taskPanel = read('../src/lib/components/MainPanel/TaskPanel.svelte');
const notes = read('../src/lib/components/MainPanel/Notes.svelte');
const app = read('../src/App.svelte');
const palette = read('../src/lib/components/Dialogs/CommandPalette.svelte');
const allTasks = read('../src/lib/components/Dashboard/AllTasks.svelte');
const dashboard = read('../src/lib/components/Dashboard/ProjectDashboard.svelte');
const shortcuts = read('../src/lib/utils/shortcuts.ts');
const alerts = read('../src/lib/stores/taskAlerts.ts');
const backendScope = read('../../project_tasks_api.go');

// Two lists can be on screen at once — a session's in the main panel, the
// project's on the dashboard or in its window — so each needs its own stores.
// Loaded for real: one module, two stores, and neither sees the other's list.
async function loadStoreModule(backend) {
  globalThis.__projectTaskApp = backend;
  const dir = mkdtempSync(join(tmpdir(), 'project-task-store-'));
  const output = join(dir, 'tasks.mjs');
  const transpiled = ts.transpileModule(tasksStore, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
  }).outputText
    .replace("from 'svelte/store'", `from '${import.meta.resolve('svelte/store')}'`)
    .replace("import * as App from '../../../wailsjs/go/main/App';", 'const App = globalThis.__projectTaskApp;')
    .replace("import { main } from '../../../wailsjs/go/models';", 'const main = {};')
    .replace("import { activeProjectId } from './projects';", "const activeProjectId = writable('');");
  writeFileSync(output, transpiled);
  return import(output);
}

test('a project list and a session list are separate stores', async () => {
  const asked = [];
  const store = await loadStoreModule({
    GetTasks: async (scope) => {
      asked.push(['GetTasks', scope]);
      return [{ id: scope, title: `from ${scope}`, description: '', status: 'backlog', priority: 'medium', tags: [], subtasks: [], dependencies: [] }];
    },
    TaskMasterGetTasks: async (scope) => {
      asked.push(['TaskMasterGetTasks', scope]);
      throw new Error('no Task Master here');
    },
  });
  const { get } = await import('svelte/store');
  const project = store.createTaskStore({ localOnly: true });

  await store.loadTasks('session-a');
  await project.loadTasks('@project');

  assert.deepEqual(get(store.tasks).map((task) => task.title), ['from session-a']);
  assert.deepEqual(get(project.tasks).map((task) => task.title), ['from @project']);
  assert.ok(!asked.some(([call, scope]) => call === 'TaskMasterGetTasks' && scope === '@project'),
    'the project list has no directory for Task Master, and must never ask it');
  assert.equal(get(project.effectiveTaskProvider), 'local');

  // A filter set on one list stays on that list.
  project.setTaskFilter({ searchText: 'release' });
  assert.equal(get(store.taskFilter).searchText, '');
});

test('the frontend addresses the project list with the backend scope', () => {
  const front = projectScope.match(/export const PROJECT_TASKS_SCOPE = '([^']+)'/)?.[1];
  const back = backendScope.match(/const ProjectTasksScope = "([^"]+)"/)?.[1];
  assert.ok(front && back, 'a scope constant is missing');
  assert.equal(front, back);
  assert.match(projectStore, /export const projectTaskStore = createTaskStore\(\{ localOnly: true \}\)/);
});

test('the task panel is reused for the project list, not copied', () => {
  assert.match(taskPanel, /export let project = false;/);
  assert.match(taskPanel, /\} = isProject \? projectTaskStore : sessionTaskStore;/,
    'each scope must act on its own store');
  assert.match(taskPanel, /return isProject \? PROJECT_TASKS_SCOPE : get\(selectedSessionId\);/);
  // What needs a session is not offered for the project list.
  assert.match(taskPanel, /\{#if !isProject\}\s*<div class="tab-filter"/);
  assert.match(taskPanel, /taskMasterUI = !isProject && \$settings\.taskMasterEnabled/);
  assert.match(taskPanel, /const useTaskMaster = !isProject && get\(settings\)\.taskMasterEnabled;/);
  // And the project list gets moving and sending to a chosen session.
  assert.match(taskPanel, /openSessionPicker\('move', menuTask\.id, contextMenuTarget\)/);
  assert.match(taskPanel, /if \(isProject\) \{\s*openSessionPicker\('send', taskId, requestedTarget\);/);
  assert.match(taskPanel, /moveTaskToSession\(pick\.taskId, sessionId, tabId\)/);
  assert.match(taskPanel, /sendProjectTaskToAgent\(pick\.taskId, sessionId, tabId\)/);
  // Moving back out of a session is only for the app's own list.
  assert.match(taskPanel, /\{:else if contextMenuTarget\?\.provider === 'local'\}\s*<button on:click=\{\(\) => handleMoveToProject/);
  // The menu lives at body level, so the window it opens in cannot offset it.
  assert.match(taskPanel, /class="context-menu"\s*use:portal\s*use:menuPosition=/);
});

test('a move reloads both lists in place rather than resetting open panels', () => {
  const afterMove = projectStore.match(/async function afterMove[\s\S]*?\n\}/)?.[0] ?? '';
  assert.match(afterMove, /projectTaskStore\.reloadTasksIfActive\(PROJECT_TASKS_SCOPE\)/);
  assert.match(afterMove, /sessionTaskStore\.reloadTasksIfActive\(sessionId\)/);
  assert.doesNotMatch(afterMove, /tasks:refresh/,
    'tasks:refresh restarts an open panel, which would close the dialog the move came from');
  assert.match(allTasks, /window\.addEventListener\(TASK_LISTS_CHANGED, refresh\)/);
  assert.match(alerts, /projectTaskStore\.tasks\.subscribe/);
});

test('the notes view has a project note, saved in the default project too', () => {
  assert.match(notes, /export let project = false;/);
  assert.match(notes, /isProject \? await App\.GetProjectNotePages\(\) : await App\.GetTabNotePages\(sessionId, windowIdx\)/);
  assert.match(notes, /if \(isProject\) await App\.SetProjectNotePages\(snapshot, projectId\);/);
  // The default project's ID is "": testing it for truth dropped every save.
  const save = notes.match(/async function saveNow[\s\S]*?\n  \}/)?.[0] ?? '';
  assert.doesNotMatch(save, /!projectId/);
});

test('the project list is reachable from the header, a shortcut and the palette', () => {
  const entry = shortcuts.match(/\{\s*id: 'projectTasks\.open',[\s\S]*?\},\n/)?.[0] ?? '';
  assert.match(entry, /defaults: \[\{ key: 'b', ctrl: true, shift: true \}\]/);
  assert.doesNotMatch(entry, /fixed: true/, 'the shortcut must be rebindable');
  assert.match(app, /case 'projectTasks\.open':[\s\S]*?projectTasksOpen\.set\(true\)/);
  assert.match(app, /<ProjectTasksButton \/>/);
  assert.match(app, /<ProjectTasksDialog bind:show=\{\$projectTasksOpen\} \/>/);
  assert.match(app, /\$projectTasksOpen \|\|/, 'the window must count as an open dialog');
  assert.match(app, /stopProjectTaskWatch = watchProjectOpenCount\(\)/);
  assert.match(palette, /id: 'project-tasks'[\s\S]*?openProjectTasks\('tasks'\)/);
  assert.match(palette, /id: 'project-notes'[\s\S]*?openProjectTasks\('notes'\)/);
});

test('no other shortcut claims the default', () => {
  const combo = "{ key: 'b', ctrl: true, shift: true }";
  assert.equal(shortcuts.split(combo).length - 1, 1, 'Ctrl+Shift+B is bound twice');
});

test('the dashboard has the project list as a section of its own', () => {
  assert.match(dashboard, /let dashboardTab: 'overview' \| 'tasks' \| 'statistics'/);
  assert.match(dashboard, /\{:else if dashboardTab === 'tasks'\}[\s\S]*?<ProjectWorkspace/);
  // Never twice on screen: two editors of one note would save over each other.
  assert.match(dashboard, /\{#if \$projectTasksOpen\}[\s\S]*?\{:else\}\s*<ProjectWorkspace/);
});

test('the all-tasks view groups project tasks by project', () => {
  assert.match(allTasks, /task\.projectTask\s*\? `\$\{task\.projectId\}:@project`/);
  assert.match(allTasks, /\$t\('allTasks\.projectGroup', \{ project: projectLabel\(task\.projectName\) \}\)/);
  assert.match(allTasks, /openProjectTasks\('tasks'\)/);
});
