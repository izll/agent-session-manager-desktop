import { mount, tick } from 'svelte';
import SessionTree from '../../src/lib/components/Sidebar/SessionTree.svelte';
import { sessions, groups, type Session } from '../../src/lib/stores/sessions';
import { selectNextSession, selectPrevSession } from '../../src/lib/stores/sidebarOrder';
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

const makeSession = (id: string, name: string, extra: Partial<Session> = {}): Session => ({
  id, name, path: `/fixture/${id}`, status: 'stopped', agent: 'claude', color: '', bgColor: '',
  fullRowColor: false, groupId: '', autoYes: false, hideStatusLine: false, notes: '',
  favorite: false, resumeSessionId: '', followedWindows: [], tabOrder: [], mainWindowStopped: false,
  extraArgs: '', tabTextColor: '', tabBackgroundColor: '', terminalTheme: '', terminalFontSize: 0,
  hideViewBar: 0, hideStatusBar: 0, mainWindowIndex: 0, lastWindowIndex: 0, isGitRepo: false,
  ...extra,
});

activeProjectId.set('project-a');
groups.set([{ id: 'g', name: 'Work group', collapsed: false, color: '', bgColor: '', fullRowColor: false }]);
// ?many=1 puts forty sessions in the group ahead of the grouped favourite, so
// its copy there is far below the fold.
const filler = new URLSearchParams(location.search).get('many') === '1'
  ? Array.from({ length: 40 }, (_, i) => makeSession(`filler-${i}`, `Filler ${i}`, { groupId: 'g' }))
  : [];
sessions.set([
  makeSession('loose', 'Loose fav', { favorite: true }),
  ...filler,
  makeSession('gfav', 'Grouped fav', { favorite: true, groupId: 'g' }),
  makeSession('gplain', 'Grouped plain', { groupId: 'g' }),
  makeSession('plain', 'Plain one'),
]);

(window as any).sidebarFavoritesFixture = {
  saved: () => structuredClone(saved),
  next: () => selectNextSession(),
  prev: () => selectPrevSession(),
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
