import { mount, tick } from 'svelte';
import GroupItem from '../../src/lib/components/Sidebar/GroupItem.svelte';
import { activeProjectId } from '../../src/lib/stores/projects';
import { sessions, groups, type Group, type Session } from '../../src/lib/stores/sessions';

const stopCalls: string[] = [];
const assignCalls: unknown[][] = [];
let resolveFirstStop: (() => void) | null = null;
let rejectSecondStop = false;

const backend = new Proxy({
  StopSession: (id: string) => {
    stopCalls.push(id);
    if (stopCalls.length === 1) return new Promise<void>((resolve) => { resolveFirstStop = resolve; });
    if (stopCalls.length === 2 && rejectSecondStop) return Promise.reject(new Error('fixture stop refused'));
    return Promise.resolve();
  },
  GetSessions: async () => [],
  GetGroups: async () => [],
  AssignToGroup: async (...args: unknown[]) => { assignCalls.push(args); },
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});

(window as any).go = { main: { App: backend } };
(window as any).runtime = new Proxy({}, { get: () => (..._args: unknown[]) => () => {} });
window.confirm = () => true;

const makeSession = (id: string, name: string): Session => ({
  id, name, path: `/fixture/${id}`, status: 'running', agent: 'claude', color: '', bgColor: '',
  fullRowColor: false, groupId: 'group-1', autoYes: false, hideStatusLine: false, notes: '',
  favorite: false, resumeSessionId: '', followedWindows: [], tabOrder: [], mainWindowStopped: false,
  extraArgs: '', tabTextColor: '', tabBackgroundColor: '', terminalTheme: '', terminalFontSize: 0,
  hideViewBar: 0, hideStatusBar: 0, mainWindowIndex: 0, lastWindowIndex: 0, isGitRepo: false,
});
const group: Group = {
  id: 'group-1', name: 'Project A group', collapsed: false, color: '', bgColor: '', fullRowColor: false,
};
const groupSessions = [makeSession('session-1', 'First'), makeSession('session-2', 'Second')];

activeProjectId.set('project-a');
sessions.set(groupSessions);
groups.set([group]);

(window as any).groupProjectFixture = {
  stopCalls: () => [...stopCalls],
  assignCalls: () => structuredClone(assignCalls),
  switchProject: (id: string) => activeProjectId.set(id),
  resolveFirstStop: () => {
    const resolve = resolveFirstStop;
    resolveFirstStop = null;
    resolve?.();
  },
  rejectSecondStop: () => { rejectSecondStop = true; },
  staleSessionDrop: async () => {
    const source = document.querySelector<HTMLElement>('.session-item');
    const target = document.querySelector<HTMLElement>('.group-header');
    if (!source || !target) throw new Error('drag fixture rows are missing');
    const transfer = new DataTransfer();
    source.dispatchEvent(new DragEvent('dragstart', { bubbles: true, dataTransfer: transfer }));
    activeProjectId.set('project-b');
    sessions.set(groupSessions.map((session) => ({ ...session, groupId: '' })));
    await tick();
    target.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: transfer }));
    await tick();
  },
};

const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');
mount(GroupItem, { target, props: { group, sessions: groupSessions, index: 0, groupCount: 1 } });
await tick();
await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
document.body.dataset.fixtureReady = 'true';
