import { mount } from 'svelte';
import { get } from 'svelte/store';
import TabMoveFixture from './tab-move-fixture.svelte';
import { sessions, selectedSessionId, selectedWindowIdx, type Session } from '../../src/lib/stores/sessions';
import { activeProjectId } from '../../src/lib/stores/projects';
import { appError, appNotice } from '../../src/lib/stores/appErrors';

// Four sessions of one project: the source with one tab besides its own
// window, a running target, a stopped one, and one on a server that a local
// tab cannot join. The backend is a stand-in that records what it was asked
// and answers the way the real one does.

type Fw = { id: string; index: number; name: string; agent: string };

const makeSession = (id: string, name: string, extra: Partial<Session> = {}): Session => ({
  id, name, path: `/fixture/${id}`, status: 'running', agent: 'claude', color: '', bgColor: '',
  fullRowColor: false, groupId: '', autoYes: false, hideStatusLine: false, notes: '',
  favorite: false, resumeSessionId: '', followedWindows: [], tabOrder: [], mainWindowStopped: false,
  extraArgs: '', tabTextColor: '', tabBackgroundColor: '', terminalTheme: '', terminalFontSize: 0,
  hideViewBar: 0, hideStatusBar: 0, mainWindowIndex: 0, lastWindowIndex: 0, isGitRepo: false,
  ...extra,
} as Session);

let state: Session[] = [
  makeSession('src', 'Source', { followedWindows: [{ id: 'tab-w', index: 1, name: 'worker', agent: 'claude' }] as any }),
  makeSession('dst', 'Dest'),
  makeSession('other', 'Other', { status: 'stopped' }),
  makeSession('remote', 'Remote', { status: 'stopped', serverId: 'srv1', serverName: 'srv1' } as any),
];

const calls: Array<{ method: string; args: unknown[] }> = [];
const notices: string[] = [];
const errors: string[] = [];
const refusals = (source: string) => Object.fromEntries(state
  .filter(s => s.id !== source)
  .map(s => [s.id, s.id === 'remote' ? 'error.tabMoveLocalTabToServer' : '']));

const followed = (s: Session) => ((s.followedWindows ?? []) as unknown as Fw[]);

const backend = new Proxy({
  GetSessions: async () => structuredClone(state),
  GetGroups: async () => [],
  GetDictationSettings: async () => ({
    enabled: false, bufferMode: false, mode: 'streaming', bufferCloseOnSend: true,
  }),
  GetWindowList: async (id: string) => {
    const s = state.find(x => x.id === id);
    if (!s || s.status !== 'running') return [];
    return [
      { Index: 0, Name: s.name, Active: true, Followed: true, Agent: 'claude', Dead: false, TextColor: '', BackgroundColor: '' },
      ...followed(s).map(fw => ({ Index: fw.index, Name: fw.name, Active: false, Followed: true,
        Agent: fw.agent, Dead: false, TextColor: '', BackgroundColor: '' })),
    ];
  },
  TabMoveRefusals: async (source: string) => refusals(source),
  SessionMergeRefusals: async (source: string) => refusals(source),
  MoveTabToSession: async (source: string, windowIdx: number, target: string, projectId: string) => {
    calls.push({ method: 'MoveTabToSession', args: [source, windowIdx, target, projectId] });
    const from = state.find(s => s.id === source)!;
    const to = state.find(s => s.id === target)!;
    const tab = followed(from).find(fw => fw.index === windowIdx)!;
    (from as any).followedWindows = followed(from).filter(fw => fw !== tab);
    (to as any).followedWindows = [...followed(to), { ...tab, index: 5 }];
    return { sessionId: target, sessionName: to.name, windowIdx: 5, tabsMoved: 1, tasksMoved: 0 };
  },
  MoveTabToNewSession: async (source: string, windowIdx: number, name: string, projectId: string) => {
    calls.push({ method: 'MoveTabToNewSession', args: [source, windowIdx, name, projectId] });
    const from = state.find(s => s.id === source)!;
    (from as any).followedWindows = followed(from).filter(fw => fw.index !== windowIdx);
    state = [...state, makeSession('split', 'worker')];
    return { sessionId: 'split', sessionName: 'worker', windowIdx: 0, tabsMoved: 1, tasksMoved: 0 };
  },
  MergeSessionInto: async (source: string, target: string, projectId: string) => {
    calls.push({ method: 'MergeSessionInto', args: [source, target, projectId] });
    const from = state.find(s => s.id === source)!;
    const to = state.find(s => s.id === target)!;
    (to as any).followedWindows = [...followed(to), ...followed(from).map(fw => ({ ...fw, index: fw.index + 10 })),
      { id: 'was-main', index: 7, name: from.name, agent: 'claude' }];
    state = state.filter(s => s.id !== source);
    return { sessionId: target, sessionName: to.name, windowIdx: 7, tabsMoved: 2, tasksMoved: 0 };
  },
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});

(window as any).go = { main: { App: backend, DictationService: backend } };
(window as any).runtime = new Proxy({}, { get: () => (..._args: unknown[]) => () => {} });

activeProjectId.set('project-a');
sessions.set(structuredClone(state));
selectedSessionId.set('src');
selectedWindowIdx.set(0);

appNotice.subscribe(message => { if (message) { notices.push(message); appNotice.set(null); } });
appError.subscribe(message => { if (message) { errors.push(message); appError.set(null); } });

(window as any).tabMoveFixture = {
  calls: () => structuredClone(calls),
  notices: () => [...notices],
  errors: () => [...errors],
  selected: () => ({ sessionId: get(selectedSessionId), windowIdx: get(selectedWindowIdx) }),
};

const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');
mount(TabMoveFixture, {
  target,
  props: { onFixtureReady: () => { document.body.dataset.fixtureReady = 'true'; } },
});
