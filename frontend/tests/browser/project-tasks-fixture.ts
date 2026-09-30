import { mount } from 'svelte';
import ProjectTasksFixture from './project-tasks-fixture.svelte';
import { sessions, selectedSessionId } from '../../src/lib/stores/sessions';

// An in-memory backend that keeps the lists the way the Go side does: the
// project's own list under the "@project" scope, and one session's list. Every
// call that changes something is recorded, so a test can check what reached
// the backend and with which project identity.
type FixtureTask = Record<string, any>;

const PROJECT = '@project';
const now = '2026-09-30T10:00:00Z';
let nextId = 1;

function task(id: string, title: string, extra: Partial<FixtureTask> = {}): FixtureTask {
  return {
    id, title, description: '', details: '', status: 'backlog', priority: 'medium', tags: [],
    subtasks: [], dependencies: [], createdAt: now, updatedAt: now, ...extra,
  };
}

const lists = new Map<string, FixtureTask[]>([
  [PROJECT, [
    task('p1', 'Plan the release', { priority: 'high' }),
    task('p2', 'Write the changelog', { subtasks: [{ id: '1', title: 'draft', status: 'backlog', done: false, createdAt: now }] }),
  ]],
  ['session-a', [task('s1', 'Fix the login', { sessionId: 'session-a' })]],
]);
let projectNotes = 'Kickoff on Monday';
const calls: unknown[][] = [];
const clone = <T,>(value: T): T => JSON.parse(JSON.stringify(value));
const list = (scope: string) => {
  if (!lists.has(scope)) lists.set(scope, []);
  return lists.get(scope)!;
};

const backend = new Proxy({
  GetTasks: async (scope: string) => clone(list(scope)),
  CreateTask: async (scope: string, title: string, description: string, priority: string, tags: string[], projectId: string) => {
    calls.push(['CreateTask', scope, title, projectId]);
    const created = task(`t${nextId++}`, title, { description, priority, tags, sessionId: scope === PROJECT ? '' : scope });
    list(scope).push(created);
    return clone(created);
  },
  UpdateTask: async (scope: string, id: string, updates: FixtureTask, projectId: string) => {
    calls.push(['UpdateTask', scope, id, updates, projectId]);
    const found = list(scope).find((candidate) => candidate.id === id);
    if (found) Object.assign(found, updates);
  },
  MoveTask: async (scope: string, id: string, status: string, projectId: string) => {
    calls.push(['MoveTask', scope, id, status, projectId]);
    const found = list(scope).find((candidate) => candidate.id === id);
    if (found) found.status = status;
  },
  MoveTaskToSession: async (id: string, sessionId: string, tabId: string, projectId: string) => {
    calls.push(['MoveTaskToSession', id, sessionId, tabId, projectId]);
    const from = list(PROJECT);
    const index = from.findIndex((candidate) => candidate.id === id);
    const [moved] = from.splice(index, 1);
    Object.assign(moved, { sessionId, tabId, dependencies: [] });
    list(sessionId).push(moved);
    return clone(moved);
  },
  MoveTaskToProject: async (sessionId: string, id: string, projectId: string) => {
    calls.push(['MoveTaskToProject', sessionId, id, projectId]);
    const from = list(sessionId);
    const index = from.findIndex((candidate) => candidate.id === id);
    const [moved] = from.splice(index, 1);
    Object.assign(moved, { sessionId: '', tabId: '', dependencies: [] });
    list(PROJECT).push(moved);
    return clone(moved);
  },
  SendProjectTaskToAgent: async (...args: unknown[]) => { calls.push(['SendProjectTaskToAgent', ...args]); },
  GetProjectNotes: async () => projectNotes,
  SetProjectNotes: async (text: string, projectId: string) => {
    calls.push(['SetProjectNotes', text, projectId]);
    projectNotes = text;
  },
  GetAllTasks: async () => [
    ...list(PROJECT).map((item) => ({ ...clone(item), projectId: '', projectName: '', projectPath: '', projectTask: true, overdue: false })),
    ...list('session-a').map((item) => ({
      ...clone(item), sessionId: 'session-a', sessionName: 'API server', projectId: '', projectName: '',
      projectPath: '/repo/api', overdue: false,
    })),
  ],
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});
(window as any).go = { main: { App: backend, DictationService: backend } };
(window as any).runtime = new Proxy({}, { get: () => (..._args: unknown[]) => () => {} });

sessions.set([{
  id: 'session-a', name: 'API server', path: '/repo/api', status: 'running', agent: 'claude',
  mainWindowIndex: 0, followedWindows: [{ id: 'tab-codex', index: 1, name: 'Codex' }],
} as any]);
selectedSessionId.set('session-a');

(window as any).projectTasksFixture = {
  calls: () => calls,
  list: (scope: string) => clone(list(scope)),
  notes: () => projectNotes,
};

const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');
mount(ProjectTasksFixture, {
  target,
  props: { onFixtureReady: () => { document.body.dataset.fixtureReady = 'true'; } },
});
