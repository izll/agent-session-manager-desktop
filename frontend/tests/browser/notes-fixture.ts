import { mount } from 'svelte';
import Notes from '../../src/lib/components/MainPanel/Notes.svelte';
import { selectedSessionId, selectedWindowIdx } from '../../src/lib/stores/sessions';
import { afterUnsavedChanges, registerUnsavedGuard } from '../../src/lib/stores/unsavedChanges';
import { activeProjectId, selectProject } from '../../src/lib/stores/projects';

const stored = new Map<string, string>([
  ['project-a\x1fnotes-a:0', 'saved A'],
  ['project-a\x1fnotes-b:0', 'saved B'],
  ['project-b\x1fnotes-b:0', 'saved B in project B'],
]);
let failNextASave = true;
let failedSaves = 0;
let selectedProject = 'project-a';
let secondGuardRegistered = false;
let secondGuardDirty = false;
let secondGuardRevision = 0;
let continueAfterSecondDiscard: (() => void) | null = null;

function noteKey(projectId: string, sessionId: string, windowIdx: number) {
  return `${projectId}\x1f${sessionId}:${windowIdx}`;
}

function enableSecondGuard() {
  if (!secondGuardRegistered) {
    secondGuardRegistered = true;
    registerUnsavedGuard({
      isDirty: () => secondGuardDirty,
      revision: () => secondGuardRevision,
      requestDiscard: (continueAfterDiscard) => {
        continueAfterSecondDiscard = continueAfterDiscard;
        document.body.dataset.secondPrompt = 'true';
      },
    });
  }
  secondGuardDirty = true;
  secondGuardRevision++;
  document.body.dataset.secondPrompt = 'false';
}

function approveSecondGuard() {
  secondGuardDirty = false;
  secondGuardRevision++;
  document.body.dataset.secondPrompt = 'false';
  const continuation = continueAfterSecondDiscard;
  continueAfterSecondDiscard = null;
  continuation?.();
}

const backend = new Proxy({
  GetTabNotes: async (sessionId: string, windowIdx: number) => {
    if (sessionId === 'notes-load-fails') throw new Error('load refused');
    return stored.get(noteKey(selectedProject, sessionId, windowIdx)) ?? '';
  },
  SetTabNotes: async (sessionId: string, windowIdx: number, value: string, expectedProjectId: string) => {
    if (sessionId === 'notes-a' && failNextASave) {
      failNextASave = false;
      failedSaves++;
      throw new Error('save refused');
    }
    stored.set(noteKey(expectedProjectId, sessionId, windowIdx), value);
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
  stored(sessionId: string, projectId = selectedProject) {
    return stored.get(noteKey(projectId, sessionId, 0));
  },
  failedSaves() {
    return failedSaves;
  },
  attemptDestructive() {
    document.body.dataset.destructive = 'false';
    afterUnsavedChanges(() => { document.body.dataset.destructive = 'true'; });
  },
  enableSecondGuard,
  approveSecondGuard,
  switchProject(id: string) {
    void selectProject(id);
  },
  selectedProject() {
    return selectedProject;
  },
  replaceProject(projectId: string, sessionId: string) {
    selectedProject = projectId;
    activeProjectId.set(projectId);
    selectedSessionId.set(sessionId);
    selectedWindowIdx.set(0);
  },
};

activeProjectId.set('project-a');
selectedSessionId.set('notes-a');
selectedWindowIdx.set(0);
const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');
mount(Notes, { target, props: { active: true } });
// Playwright can start eight cold fixture graphs at once. Expose component
// readiness explicitly instead of using the textarea's appearance as an
// accidental proxy for Vite having transformed and evaluated this graph.
document.body.dataset.fixtureReady = 'true';
