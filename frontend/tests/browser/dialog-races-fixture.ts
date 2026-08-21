import { mount } from 'svelte';
import { get } from 'svelte/store';
import DialogRacesFixture from './dialog-races-fixture.svelte';
import { activeProjectId, projects } from '../../src/lib/stores/projects';
import { selectedSessionId, sessions } from '../../src/lib/stores/sessions';
import { agents } from '../../src/lib/stores/agents';
import { settings } from '../../src/lib/stores/settings';
import { refreshOpenCount } from '../../src/lib/stores/taskAlerts';

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
const importSessionResolvers: Array<(value: number) => void> = [];
const importSessionCalls: unknown[][] = [];
const sessionFileImportResolvers: Array<(value: number) => void> = [];
const sessionFileImportCalls: unknown[][] = [];
let resolveTrashRestore: ((value: unknown) => void) | null = null;
let recoverySessionLoads = 0;
let resolveUpdate: (() => void) | null = null;
let updateCalls = 0;
let resolveCreateSession: ((value: unknown) => void) | null = null;
const startSessionCalls: string[] = [];
const createGroupCalls: string[] = [];
const forkCalls: unknown[][] = [];
const settingsSaves: unknown[] = [];
let claudeUsageCalls = 0;
let codexUsageCalls = 0;
const claudeUsageResolvers: Array<(value: unknown) => void> = [];
const delayTaskLoads = new URLSearchParams(location.search).get('delayTasks') === '1';
let allTaskCalls = 0;
const allTaskResolvers: Array<(value: unknown[]) => void> = [];
const delayBgAgentLoads = new URLSearchParams(location.search).get('delayBgAgents') === '1';
let bgAgentCalls = 0;
const bgAgentResolvers: Array<(value: unknown[]) => void> = [];
const overviewTasks = [{
  id: 'task-1', title: 'Fixture dashboard task', description: '', details: '',
  status: 'pending', priority: 'medium', projectId: 'project-a',
  projectName: 'Project A', projectPath: '/repo-a', overdue: false,
  dependencies: [], subtasks: [],
}];

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
  GetProjects: async () => [
    { id: 'project-a', name: 'Project A', isLocked: false },
    { id: 'project-b', name: 'Source Project', isLocked: false },
  ],
  GetActiveProjectID: async () => 'project-a',
  GetProjectSessions: async () => [{
    id: 'source-session', name: 'Portable Session', path: '/source', status: 'stopped',
    agent: 'claude', color: '', favorite: false,
  }],
  ImportSessions: (...args: unknown[]) => {
    importSessionCalls.push(args);
    return new Promise<number>((resolve) => { importSessionResolvers.push(resolve); });
  },
  ReadSessionFile: async () => ({
    token: 'opaque-import-token', path: '/display-only/session.json', exportedAt: '2026-08-21T10:00:00Z',
    sessions: [{ name: 'Portable File Session', path: '/portable', agent: 'claude', tabs: 1, pathExists: true }],
  }),
  ImportSessionFile: (...args: unknown[]) => {
    sessionFileImportCalls.push(args);
    return new Promise<number>((resolve) => { sessionFileImportResolvers.push(resolve); });
  },
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
  GetAllTasks: () => {
    allTaskCalls++;
    if (!delayTaskLoads) return Promise.resolve(overviewTasks);
    return new Promise<unknown[]>((resolve) => { allTaskResolvers.push(resolve); });
  },
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
  ListBackgroundAgents: () => {
    bgAgentCalls++;
    const result = [{
      id: 'agent-1', sessionId: '', pid: 42, cwd: '/repo-a', name: 'Background fixture',
      status: 'running', startedAt: Date.now(),
    }];
    if (!delayBgAgentLoads) return Promise.resolve(result);
    return new Promise<unknown[]>((resolve) => { bgAgentResolvers.push(resolve); });
  },
  ForkSession: async (...args: unknown[]) => {
    forkCalls.push(args);
    return { sessionId: 'conversation-fork' };
  },
  SaveSettings: async (value: unknown) => { settingsSaves.push(structuredClone(value)); },
  GetDictationSettings: async () => ({ enabled: false, language: 'en' }),
  GetAvailableLanguages: async () => [],
  GetInputDevices: async () => [],
  GetDictationProblems: async () => [],
  DetectionPatternsVersion: async () => 1,
  GetProjectGitSummaries: async () => [],
  GetClaudeUsage: () => {
    claudeUsageCalls++;
    return new Promise((resolve) => { claudeUsageResolvers.push(resolve); });
  },
  GetCodexUsage: async () => {
    codexUsageCalls++;
    return { available: false };
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
  importSessionCalls: () => structuredClone(importSessionCalls),
  resolveImportSessions: (index: number, count: number) => importSessionResolvers[index]?.(count),
  sessionFileImportCalls: () => structuredClone(sessionFileImportCalls),
  resolveSessionFileImport: (index: number, count: number) => sessionFileImportResolvers[index]?.(count),
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
  settingsSaves: () => structuredClone(settingsSaves),
  claudeUsageCalls: () => claudeUsageCalls,
  codexUsageCalls: () => codexUsageCalls,
  resolveClaudeUsage: (index: number) => claudeUsageResolvers[index]?.({ available: false }),
  allTaskCalls: () => allTaskCalls,
  resolveAllTasks: (index: number) => allTaskResolvers[index]?.(structuredClone(overviewTasks)),
  bgAgentCalls: () => bgAgentCalls,
  resolveBgAgents: (index: number) => bgAgentResolvers[index]?.([{
    id: 'agent-1', sessionId: '', pid: 42, cwd: '/repo-a', name: 'Background fixture',
    status: 'running', startedAt: Date.now(),
  }]),
  triggerOpenTaskRefresh: () => { void refreshOpenCount(); },
};

const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');
const requestedMode = new URLSearchParams(location.search).get('mode');
const mode = requestedMode === 'palette' || requestedMode === 'command' || requestedMode === 'history' || requestedMode === 'quickjump' || requestedMode === 'quickterminal' || requestedMode === 'scheme' || requestedMode === 'import' || requestedMode === 'sessionfile' || requestedMode === 'alltasks' || requestedMode === 'taskbadge' || requestedMode === 'dashboard' || requestedMode === 'recovery' || requestedMode === 'update' || requestedMode === 'settings' || requestedMode === 'newsession' || requestedMode === 'newgroup' || requestedMode === 'bgagents' || requestedMode === 'fork' || requestedMode === 'commandmanager' || requestedMode === 'template'
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
if (mode === 'settings') {
  settings.update((value) => ({
    ...value,
    notifyOnWaiting: true,
    notifyNtfy: true,
    ntfyUrl: 'https://ntfy.sh/old-topic',
  }));
}
if (mode === 'dashboard') {
  activeProjectId.set('project-a');
  projects.set([{ id: 'project-a', name: 'Project A', isLocked: false }]);
  sessions.set([]);
}
mount(DialogRacesFixture, {
  target,
  props: {
    mode,
    onFixtureReady: () => { document.body.dataset.fixtureReady = 'true'; },
  },
});
