import { mount, tick } from 'svelte';
import SessionTree from '../../src/lib/components/Sidebar/SessionTree.svelte';
import { sessions, groups, type Session } from '../../src/lib/stores/sessions';
import { activeProjectId } from '../../src/lib/stores/projects';

const saved: unknown[] = [];
const backend = new Proxy({
  SaveSettings: async (value: unknown) => { saved.push(structuredClone(value)); },
  GetSessions: async () => [],
  GetGroups: async () => [],
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});

(window as any).go = { main: { App: backend } };
(window as any).runtime = new Proxy({}, { get: () => (..._args: unknown[]) => () => {} });

const HOUR = 60 * 60 * 1000;
const DAY = 24 * HOUR;
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

const makeSession = (id: string, name: string, extra: Partial<Session> = {}): Session => ({
  id, name, path: `/fixture/${id}`, status: 'stopped', agent: 'claude', color: '', bgColor: '',
  fullRowColor: false, groupId: '', autoYes: false, hideStatusLine: false, notes: '',
  favorite: false, resumeSessionId: '', followedWindows: [], tabOrder: [], mainWindowStopped: false,
  extraArgs: '', tabTextColor: '', tabBackgroundColor: '', terminalTheme: '', terminalFontSize: 0,
  hideViewBar: 0, hideStatusBar: 0, mainWindowIndex: 0, lastWindowIndex: 0, isGitRepo: false,
  ...extra,
});

activeProjectId.set('project-a');
groups.set([{ id: 'g', name: 'Old group', collapsed: false, color: '', bgColor: '', fullRowColor: false }]);
sessions.set([
  makeSession('run', 'Running api', { status: 'running', updatedAt: ago(2 * HOUR) }),
  makeSession('week', 'Weekly api', { updatedAt: ago(5 * DAY) }),
  makeSession('idle', 'Idle runner', { status: 'running', updatedAt: ago(20 * DAY) }),
  makeSession('grouped', 'Grouped old', { groupId: 'g', updatedAt: ago(60 * DAY) }),
]);

(window as any).sidebarFilterFixture = {
  saved: () => structuredClone(saved),
};

const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');
mount(SessionTree, {
  target,
  props: { onNewSession: () => {}, onNewGroup: () => {}, onCollapse: () => {} },
});
await tick();
await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
document.body.dataset.fixtureReady = 'true';
