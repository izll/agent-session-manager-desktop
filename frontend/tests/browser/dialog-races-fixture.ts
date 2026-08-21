import { mount } from 'svelte';
import DialogRacesFixture from './dialog-races-fixture.svelte';

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
  ListGitBranches: async (path: string) => ({ branches: [{ name: path, current: true }] }),
  GetGitHistory: (path: string) => {
    historyCalls.push(path);
    return new Promise((resolve) => { historyResolvers.set(path, resolve); });
  },
  GetGitCommitFiles: async () => [],
  GetQuickJump: () => new Promise<unknown[]>((resolve) => { quickJumpResolvers.push(resolve); }),
  CreateTab: (...args: unknown[]) => {
    createTabCalls.push(args);
    return new Promise<number>((resolve) => { createTabResolvers.push(resolve); });
  },
  DiscoverLocalSchemes: () => new Promise<unknown[]>((resolve) => { localSchemeResolvers.push(resolve); }),
  ListOnlineSchemes: () => new Promise<unknown[]>((resolve) => { onlineSchemeResolvers.push(resolve); }),
  GetSessions: async () => [],
  GetGroups: async () => [],
  GetAllTasks: async () => [{
    id: 'task-1', title: 'Fixture dashboard task', description: '', details: '',
    status: 'pending', priority: 'medium', projectId: 'project-a',
    projectName: 'Project A', projectPath: '/repo-a', overdue: false,
    dependencies: [], subtasks: [],
  }],
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});

(window as any).go = { main: { App: backend, DictationService: backend } };
(window as any).runtime = new Proxy({}, { get: () => (..._args: unknown[]) => () => {} });
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
};

const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');
const requestedMode = new URLSearchParams(location.search).get('mode');
const mode = requestedMode === 'command' || requestedMode === 'history' || requestedMode === 'quickjump' || requestedMode === 'quickterminal' || requestedMode === 'scheme' || requestedMode === 'alltasks'
  ? requestedMode : 'global';
mount(DialogRacesFixture, {
  target,
  props: {
    mode,
    onFixtureReady: () => { document.body.dataset.fixtureReady = 'true'; },
  },
});
