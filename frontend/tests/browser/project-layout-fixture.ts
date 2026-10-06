import { get } from 'svelte/store';
import { mount, tick } from 'svelte';
import ProjectLayoutFixture from './project-layout-fixture.svelte';
import { activeProjectId } from '../../src/lib/stores/projects';
import { settings } from '../../src/lib/stores/settings';
import { dictationTarget } from '../../src/lib/stores/dictationTarget';
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

const fixtureParams = new URLSearchParams(location.search);
// ?buffer=0: live preview instead of the buffer.
const bufferOn = fixtureParams.get('buffer') !== '0';
let bufferText = '';
const promptsSent: unknown[][] = [];
const fieldInserts: string[] = [];
window.addEventListener('dictation:insertIntoField', (e) => fieldInserts.push((e as CustomEvent<string>).detail));

const backend = new Proxy({
  GetWindowList: async () => [{ Index: 0, Name: 'Layout fixture', Agent: 'claude', Dead: true }],
  GetDictationSettings: async () => ({
    enabled: true, bufferMode: bufferOn, mode: 'streaming', bufferCloseOnSend: true,
  }),
  GetTaskMasterStatus: async () => ({ initialized: false }),
  GetTasks: async () => [],
  GetBufferText: async () => bufferText,
  ClearBuffer: async () => { bufferText = ''; },
  SetBufferText: async (text: string) => { bufferText = text; },
  SendPromptToWindow: async (...args: unknown[]) => { promptsSent.push(args); },
  GetVoiceLevel: async () => 0,
  GetTabWorkingDirectory: async () => '/fixture',
  GetDiffFolder: async () => ({ path: '/fixture', tabDir: '/fixture', custom: false, locked: '' }),
  // The diff controls ask about the TAB's directory, not the session's — a tab
  // can be opened in one of its own. Without this the proxy below answers
  // undefined and the fixture's repository looks like a plain folder, so the
  // diff buttons this file exercises never render.
  TabIsGitRepo: async () => true,
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

// ?target=field: the dictation that starts is a dialog field's, with that
// field holding the caret.
if (fixtureParams.get('target') === 'field') {
  dictationTarget.set('field');
  const field = document.createElement('input');
  field.id = 'dialog-field';
  document.body.prepend(field);
  field.focus();
}

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
  setBufferText: (text: string) => { bufferText = text; },
  promptsSent: () => structuredClone(promptsSent),
  fieldInserts: () => [...fieldInserts],
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
