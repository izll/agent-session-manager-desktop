import { mount } from 'svelte';
import { get } from 'svelte/store';
import DialogRacesFixture from './dialog-races-fixture.svelte';
import { activeProjectId } from '../../src/lib/stores/projects';
import { selectedSessionId, sessions } from '../../src/lib/stores/sessions';
import { agents } from '../../src/lib/stores/agents';

let resolveSearch: ((value: unknown[]) => void) | null = null;
let resolveRun: (() => void) | null = null;
const searchCalls: string[] = [];
const runCalls: unknown[][] = [];
const historyResolvers = new Map<string, (value: unknown) => void>();
const historyCalls: string[] = [];
const quickJumpResolvers: Array<(value: unknown[]) => void> = [];
const createTabResolvers: Array<(value: number) => void> = [];
const createTabCalls: unknown[][] = [];
const localSchemeResolvers: Array<(value: unknown[]) => void> = [];
const onlineSchemeResolvers: Array<(value: unknown[]) => void> = [];
let resolveTrashRestore: ((value: unknown) => void) | null = null;
let recoverySessionLoads = 0;
let resolveUpdate: (() => void) | null = null;
let updateCalls = 0;
let resolveCreateSession: ((value: unknown) => void) | null = null;
const startSessionCalls: string[] = [];
const createGroupCalls: string[] = [];
const forkCalls: unknown[][] = [];

const backend = new Proxy({
  GlobalSearch: (query: string) => {
    searchCalls.push(query);
    return new Promise<unknown[]>((resolve) => { resolveSearch = resolve; });
  },
  GetCommands: async () => ({
    groups: [],
    commands: [{
      id: 'command-1', name: 'Fixture command', command: 'echo fixture',
      description: '', groupId: '', placeholders: [],
    }],
  }),
  RunCommand: (...args: unknown[]) => {
    runCalls.push(args);
    return new Promise<void>((resolve) => { resolveRun = resolve; });
  },
  ListGitBranches: async (_sessionId: string, _windowIdx: number, root: string) => ({
    branches: [{ name: root, current: true }],
  }),
  GetGitHistory: (_sessionId: string, _branch: string, _skip: number, _windowIdx: number, root: string) => {
    historyCalls.push(root);
    return new Promise((resolve) => { historyResolvers.set(root, resolve); });
  },
  GetGitCommitFiles: async () => [],
  GetQuickJump: () => new Promise<unknown[]>((resolve) => { quickJumpResolvers.push(resolve); }),
  CreateTab: (...args: unknown[]) => {
    createTabCalls.push(args);
    return new Promise<number>((resolve) => { createTabResolvers.push(resolve); });
  },
  DiscoverLocalSchemes: () => new Promise<unknown[]>((resolve) => { localSchemeResolvers.push(resolve); }),
  ListOnlineSchemes: () => new Promise<unknown[]>((resolve) => { onlineSchemeResolvers.push(resolve); }),
  GetTrashItems: async () => [{
    id: 'trash-1', kind: 'session', name: 'Old project session',
    parentSessionId: '', parentSessionName: '', deletedAt: '2026-08-20T10:00:00Z',
  }],
  GetBackups: async () => [],
  GetTaskBackups: async () => [],
  RestoreTrashItem: () => new Promise<unknown>((resolve) => { resolveTrashRestore = resolve; }),
  CheckForUpdate: async () => ({ available: true, currentVersion: '1.0.0', latestVersion: '1.1.0' }),
  PerformUpdate: () => {
    updateCalls++;
    return new Promise<void>((resolve) => { resolveUpdate = resolve; });
  },
  GetSessions: async () => { recoverySessionLoads++; return []; },
  GetGroups: async () => [],
  GetAllTasks: async () => [{
    id: 'task-1', title: 'Fixture dashboard task', description: '', details: '',
    status: 'pending', priority: 'medium', projectId: 'project-a',
    projectName: 'Project A', projectPath: '/repo-a', overdue: false,
    dependencies: [], subtasks: [],
  }],
  GetAgents: async () => [{
    type: 'claude', name: 'Claude', icon: '', supportsResume: false, supportsAutoYes: true, supportsFork: false,
  }],
  CreateSession: () => new Promise((resolve) => { resolveCreateSession = resolve; }),
  StartSession: async (id: string) => { startSessionCalls.push(id); },
  GetSessionTemplates: async () => [],
  CreateGroup: async (name: string) => {
    createGroupCalls.push(name);
    return { id: 'fixture-group', name, collapsed: false, color: '', bgColor: '', fullRowColor: false };
  },
  ListBackgroundAgents: async () => [{
    id: 'agent-1', sessionId: '', pid: 42, cwd: '/repo-a', name: 'Background fixture',
    status: 'running', startedAt: Date.now(),
  }],
  ForkSession: async (...args: unknown[]) => {
    forkCalls.push(args);
    return { sessionId: 'conversation-fork' };
  },
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});

(window as any).go = { main: { App: backend, DictationService: backend } };
(window as any).runtime = new Proxy({}, { get: () => (..._args: unknown[]) => () => {} });
agents.set([{ type: 'claude', name: 'Claude', icon: '', supportsResume: false, supportsAutoYes: true, supportsFork: false }]);
(window as any).dialogRacesFixture = {
  resolveSearch: (value: unknown[]) => resolveSearch?.(value),
  resolveRun: () => resolveRun?.(),
  searchCalls: () => structuredClone(searchCalls),
  runCalls: () => structuredClone(runCalls),
  resolveHistory: (path: string, subject: string) => historyResolvers.get(path)?.({
    repository: true,
    commits: [{ hash: `${path}-hash`, subject, author: 'fixture', date: '2026-08-20T10:00:00Z', body: '' }],
    hasMore: false,
    skip: 1,
  }),
  historyCalls: () => structuredClone(historyCalls),
  quickJumpCalls: () => quickJumpResolvers.length,
  resolveQuickJump: (index: number, sessionId: string, label: string) => quickJumpResolvers[index]?.([
    { sessionId, windowIdx: -1, label },
  ]),
  createTabCalls: () => structuredClone(createTabCalls),
  resolveCreateTab: (index: number, windowIdx: number) => createTabResolvers[index]?.(windowIdx),
  localSchemeCalls: () => localSchemeResolvers.length,
  resolveLocalSchemes: (index: number, name: string) => localSchemeResolvers[index]?.([
    { name, source: 'local', colors: {} },
  ]),
  onlineSchemeCalls: () => onlineSchemeResolvers.length,
  resolveOnlineSchemes: (index: number, name: string) => onlineSchemeResolvers[index]?.([
    { name, file: `${name}.json` },
  ]),
  trashRestorePending: () => !!resolveTrashRestore,
  switchRecoveryProject: (projectId: string) => activeProjectId.set(projectId),
  resolveTrashRestore: (sessionId: string) => resolveTrashRestore?.({ sessionId, windowIdx: 0 }),
  recoverySessionLoads: () => recoverySessionLoads,
  selectedSession: () => get(selectedSessionId),
  updateCalls: () => updateCalls,
  resolveUpdate: () => resolveUpdate?.(),
  createSessionPending: () => !!resolveCreateSession,
  resolveCreateSession: () => {
    const resolve = resolveCreateSession;
    resolveCreateSession = null;
    resolve?.({
      id: 'created-in-project-a', name: 'Created A', path: '/repo-a', status: 'stopped', agent: 'claude',
      color: '', bgColor: '', fullRowColor: false, groupId: '', autoYes: false, hideStatusLine: false,
      notes: '', favorite: false, resumeSessionId: '', followedWindows: [], tabOrder: [], mainWindowStopped: false,
      extraArgs: '', tabTextColor: '', tabBackgroundColor: '', terminalTheme: '', terminalFontSize: 0,
      hideViewBar: 0, hideStatusBar: 0, mainWindowIndex: 0, lastWindowIndex: 0, isGitRepo: false,
    });
  },
  startSessionCalls: () => [...startSessionCalls],
  createGroupCalls: () => [...createGroupCalls],
  switchProject: (projectId: string) => activeProjectId.set(projectId),
  forkCalls: () => structuredClone(forkCalls),
};

const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');
const requestedMode = new URLSearchParams(location.search).get('mode');
const mode = requestedMode === 'palette' || requestedMode === 'command' || requestedMode === 'history' || requestedMode === 'quickjump' || requestedMode === 'quickterminal' || requestedMode === 'scheme' || requestedMode === 'alltasks' || requestedMode === 'recovery' || requestedMode === 'update' || requestedMode === 'newsession' || requestedMode === 'newgroup' || requestedMode === 'bgagents' || requestedMode === 'fork' || requestedMode === 'commandmanager' || requestedMode === 'template'
  ? requestedMode : 'global';
if (mode === 'newgroup' || mode === 'bgagents' || mode === 'fork') {
  activeProjectId.set('project-a');
  sessions.set([{
    id: 'session-a', name: 'Session A', path: '/repo-a', status: 'running', agent: 'claude',
    color: '', bgColor: '', fullRowColor: false, groupId: '', autoYes: false,
    hideStatusLine: false, notes: '', favorite: false, resumeSessionId: '', followedWindows: [],
    tabOrder: [], mainWindowStopped: false, extraArgs: '', tabTextColor: '', tabBackgroundColor: '',
    terminalTheme: '', terminalFontSize: 0, hideViewBar: 0, hideStatusBar: 0,
    mainWindowIndex: 0, lastWindowIndex: 0, isGitRepo: false,
  }]);
  selectedSessionId.set('session-a');
}
mount(DialogRacesFixture, {
  target,
  props: {
    mode,
    onFixtureReady: () => { document.body.dataset.fixtureReady = 'true'; },
  },
});
