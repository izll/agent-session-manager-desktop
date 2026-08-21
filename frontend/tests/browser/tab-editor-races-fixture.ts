import { mount } from 'svelte';
import TabEditorRacesFixture from './tab-editor-races-fixture.svelte';
import { sessions, selectedSessionId, selectedWindowIdx } from '../../src/lib/stores/sessions';

const renameResolvers: Array<() => void> = [];
const extraArgsResolvers: Array<() => void> = [];

const fixtureSession = {
  id: 'session-a', name: 'Main tab', path: '/fixture', status: 'stopped',
  agent: 'claude', color: '', bgColor: '', fullRowColor: false, groupId: '',
  autoYes: false, hideStatusLine: false, notes: '', favorite: false,
  resumeSessionId: '', extraArgs: '--main-old', tabTextColor: '',
  tabBackgroundColor: '', terminalTheme: '', terminalFontSize: 0,
  hideViewBar: 0, hideStatusBar: 0, mainWindowIndex: 0, lastWindowIndex: 0,
  isGitRepo: false, tabOrder: [0, 1], mainWindowStopped: true,
  followedWindows: [{
    index: 1, name: 'Second tab', agent: 'claude', stopped: true,
    extra_args: '--second-old', hide_view_bar: 0, hide_status_bar: 0,
  }],
};

const backend = new Proxy({
  GetExtraArgs: async (_sessionId: string, windowIdx: number) =>
    windowIdx === 0 ? '--main-old' : '--second-old',
  SetExtraArgs: () => new Promise<void>((resolve) => { extraArgsResolvers.push(resolve); }),
  RenameTab: () => new Promise<void>((resolve) => { renameResolvers.push(resolve); }),
  GetSessions: async () => [fixtureSession],
  GetGroups: async () => [],
  GetDictationSettings: async () => ({
    enabled: false, bufferMode: false, mode: 'streaming', bufferCloseOnSend: true,
  }),
  LogFrontend: async () => undefined,
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});

(window as any).go = { main: { App: backend, DictationService: backend } };
(window as any).runtime = new Proxy({}, { get: () => (..._args: unknown[]) => () => {} });

sessions.set([fixtureSession as any]);
selectedSessionId.set(fixtureSession.id);
selectedWindowIdx.set(0);

(window as any).tabEditorRacesFixture = {
  renameCalls: () => renameResolvers.length,
  extraArgsCalls: () => extraArgsResolvers.length,
  resolveRename: (index: number) => renameResolvers[index]?.(),
  resolveExtraArgs: (index: number) => extraArgsResolvers[index]?.(),
};

const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');
mount(TabEditorRacesFixture, {
  target,
  props: {
    onFixtureReady: () => { document.body.dataset.fixtureReady = 'true'; },
  },
});
