import '../../src/style.css';
import { mount } from 'svelte';
import Notes from '../../src/lib/components/MainPanel/Notes.svelte';
import SettingsDialog from '../../src/lib/components/Dialogs/SettingsDialog.svelte';
import { selectedSessionId, selectedWindowIdx } from '../../src/lib/stores/sessions';
import { activeProjectId } from '../../src/lib/stores/projects';

// The real Settings dialog and the real notes view side by side, against a
// stand-in backend: a colour picked in the one has to show in the other.
const settingsSaves: Record<string, unknown>[] = [];
const backend = new Proxy({
  GetTabNotePages: async () => [{
    id: 'page-1',
    title: '',
    text: 'Shopping list\n- paper\n- ink\n\nThe notes read like a page now, dark text on a warm background.',
  }],
  SetTabNotePages: async () => undefined,
  GetActiveProjectID: async () => 'project-a',
  GetSessions: async () => [],
  GetGroups: async () => [],
  GetSettings: async () => null,
  GetLockStatus: async () => ({ locked: true, otherInstancePid: 0 }),
  SaveSettings: async (value: Record<string, unknown>) => { settingsSaves.push(structuredClone(value)); },
  DetectedEditor: async () => '',
  GetDictationProblems: async () => [],
  GetDictationSettings: async () => ({}),
  GetAvailableLanguages: async () => [],
  GetInputDevices: async () => [],
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});

(window as any).go = { main: { App: backend, DictationService: backend } };
(window as any).runtime = new Proxy({}, { get: () => (..._args: unknown[]) => () => {} });
(window as any).notesColorsFixture = {
  /** The last settings snapshot written, as the backend would store it. */
  lastSave: () => structuredClone(settingsSaves[settingsSaves.length - 1] ?? null),
  hideSettings: () => {
    const el = document.getElementById('settings');
    if (el) el.style.display = 'none';
  },
};

activeProjectId.set('project-a');
selectedSessionId.set('notes-colors');
selectedWindowIdx.set(0);

const notesTarget = document.getElementById('notes');
const settingsTarget = document.getElementById('settings');
if (!notesTarget || !settingsTarget) throw new Error('fixture targets are missing');
mount(Notes, { target: notesTarget, props: { active: true } });
mount(SettingsDialog, { target: settingsTarget, props: { show: true } });
document.body.dataset.fixtureReady = 'true';
