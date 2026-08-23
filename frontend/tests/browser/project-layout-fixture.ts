import { get } from 'svelte/store';
import { mount, tick } from 'svelte';
import ProjectLayoutFixture from './project-layout-fixture.svelte';
import { activeProjectId } from '../../src/lib/stores/projects';
import { settings } from '../../src/lib/stores/settings';
import { sessions, selectedSessionId, selectedWindowIdx } from '../../src/lib/stores/sessions';

const fixtureSession = {
  id: 'same-session', name: 'Layout fixture', path: '/fixture', status: 'stopped',
  agent: 'claude', color: '', bgColor: '', fullRowColor: false, groupId: '',
  autoYes: false, hideStatusLine: false, notes: '', favorite: false,
  resumeSessionId: '', extraArgs: '', tabTextColor: '', tabBackgroundColor: '',
  terminalTheme: '', terminalFontSize: 0, hideViewBar: 0, hideStatusBar: 0,
  mainWindowIndex: 0, lastWindowIndex: 0, isGitRepo: true, tabOrder: [0],
  mainWindowStopped: true, followedWindows: [],
};

const backend = new Proxy({
  GetWindowList: async () => [{ Index: 0, Name: 'Layout fixture', Agent: 'claude', Dead: true }],
  GetDictationSettings: async () => ({
    enabled: true, bufferMode: true, mode: 'streaming', bufferCloseOnSend: true,
  }),
  GetTaskMasterStatus: async () => ({ initialized: false }),
  GetTasks: async () => [],
  GetBufferText: async () => '',
  GetVoiceLevel: async () => 0,
  GetTabWorkingDirectory: async () => '/fixture',
  GetGitBranch: async () => ({ isRepo: true, branch: 'main' }),
  SaveSettings: async (...args: unknown[]) => { settingsSaves.push(structuredClone(args)); },
  LogFrontend: async () => undefined,
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});
const settingsSaves: unknown[][] = [];

(window as any).go = { main: { App: backend, DictationService: backend } };
(window as any).runtime = new Proxy({
  EventsOnMultiple: (name: string, callback: (...args: unknown[]) => void) => {
    if (name === 'dictation:state') queueMicrotask(() => callback(true));
    return () => undefined;
  },
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return (..._args: unknown[]) => () => undefined;
  },
});

activeProjectId.set('project-a');
settings.set({
  ...get(settings),
  diffAboveHeight: 180,
  dictationBuffer: { x: 20, y: 30, w: 320, h: 180 },
});
sessions.set([fixtureSession as any]);
selectedSessionId.set(fixtureSession.id);
selectedWindowIdx.set(0);

(window as any).projectLayoutFixture = {
  settingsSaves: () => structuredClone(settingsSaves),
  switchProject: async (projectId = 'project-b', height = 260, x = 110) => {
    // The real switch first publishes defaults, then the replacement project
    // identity and finally its authoritative settings snapshot.
    settings.set({ ...get(settings), diffAboveHeight: undefined, dictationBuffer: null });
    activeProjectId.set(projectId);
    await tick();
    settings.set({
      ...get(settings),
      diffAboveHeight: height,
      dictationBuffer: { x, y: 70, w: 360, h: 210 },
    });
    await tick();
  },
};

const target = document.getElementById('fixture');
if (!target) throw new Error('project layout fixture target is missing');
mount(ProjectLayoutFixture, {
  target,
  props: {
    onFixtureReady: () => { document.body.dataset.fixtureReady = 'true'; },
  },
});
