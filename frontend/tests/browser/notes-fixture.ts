import { mount } from 'svelte';
import Notes from '../../src/lib/components/MainPanel/Notes.svelte';
import { selectedSessionId, selectedWindowIdx } from '../../src/lib/stores/sessions';
import { afterUnsavedChanges } from '../../src/lib/stores/unsavedChanges';
import { selectProject } from '../../src/lib/stores/projects';

const stored = new Map<string, string>([
  ['notes-a:0', 'saved A'],
  ['notes-b:0', 'saved B'],
]);
let failNextASave = true;
let failedSaves = 0;
let selectedProject = '';

const backend = new Proxy({
  GetTabNotes: async (sessionId: string, windowIdx: number) => {
    if (sessionId === 'notes-load-fails') throw new Error('load refused');
    return stored.get(`${sessionId}:${windowIdx}`) ?? '';
  },
  SetTabNotes: async (sessionId: string, windowIdx: number, value: string) => {
    if (sessionId === 'notes-a' && failNextASave) {
      failNextASave = false;
      failedSaves++;
      throw new Error('save refused');
    }
    stored.set(`${sessionId}:${windowIdx}`, value);
  },
  SelectProject: async (id: string) => { selectedProject = id; },
  GetActiveProjectID: async () => selectedProject,
  GetSessions: async () => [],
  GetGroups: async () => [],
  GetSettings: async () => null,
  GetLockStatus: async () => ({ locked: true, otherInstancePid: 0 }),
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});

(window as any).go = { main: { App: backend, DictationService: backend } };
(window as any).runtime = new Proxy({}, { get: () => (..._args: unknown[]) => () => {} });
(window as any).notesFixture = {
  select(sessionId: string) {
    selectedSessionId.set(sessionId);
    selectedWindowIdx.set(0);
  },
  stored(sessionId: string) {
    return stored.get(`${sessionId}:0`);
  },
  failedSaves() {
    return failedSaves;
  },
  attemptDestructive() {
    document.body.dataset.destructive = 'false';
    afterUnsavedChanges(() => { document.body.dataset.destructive = 'true'; });
  },
  switchProject(id: string) {
    void selectProject(id);
  },
  selectedProject() {
    return selectedProject;
  },
};

selectedSessionId.set('notes-a');
selectedWindowIdx.set(0);
const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');
mount(Notes, { target, props: { active: true } });
// Playwright can start eight cold fixture graphs at once. Expose component
// readiness explicitly instead of using the textarea's appearance as an
// accidental proxy for Vite having transformed and evaluated this graph.
document.body.dataset.fixtureReady = 'true';
