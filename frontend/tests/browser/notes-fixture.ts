import { mount } from 'svelte';
import Notes from '../../src/lib/components/MainPanel/Notes.svelte';
import { selectedSessionId, selectedWindowIdx } from '../../src/lib/stores/sessions';
import { afterUnsavedChanges, registerUnsavedGuard } from '../../src/lib/stores/unsavedChanges';
import { activeProjectId, selectProject } from '../../src/lib/stores/projects';
import { requestNoteJump, type NoteJump } from '../../src/lib/stores/noteJump';
import { settings } from '../../src/lib/stores/settings';

type Page = { id: string; title: string; text: string };
// As the backend returns notes: one written before pages is a single
// untitled page with the fixed first-page ID.
const legacy = (text: string): Page[] => [{ id: 'page-1', title: '', text }];
const stored = new Map<string, Page[]>([
  ['project-a\x1fnotes-a:0', legacy('saved A')],
  ['project-a\x1fnotes-b:0', legacy('saved B')],
  ['project-b\x1fnotes-b:0', legacy('saved B in project B')],
  ['project-a\x1fnotes-pages:0', [
    { id: 'p-plan', title: 'Plan', text: 'alpha and beta' },
    { id: 'p-risks', title: 'Risks', text: 'first line\nthe hidden needle is here' },
  ]],
]);
const saves: Page[][] = [];
const clone = <T,>(value: T): T => JSON.parse(JSON.stringify(value));
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
  GetTabNotePages: async (sessionId: string, windowIdx: number) => {
    if (sessionId === 'notes-load-fails') throw new Error('load refused');
    return clone(stored.get(noteKey(selectedProject, sessionId, windowIdx)) ?? []);
  },
  SetTabNotePages: async (sessionId: string, windowIdx: number, pages: Page[], expectedProjectId: string) => {
    if (sessionId === 'notes-a' && failNextASave) {
      failNextASave = false;
      failedSaves++;
      throw new Error('save refused');
    }
    saves.push(clone(pages));
    stored.set(noteKey(expectedProjectId, sessionId, windowIdx), clone(pages));
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
  /** The note's text, its pages' texts joined; undefined when never saved. */
  stored(sessionId: string, projectId = selectedProject) {
    return stored.get(noteKey(projectId, sessionId, 0))?.map((p) => p.text).join('\n---\n');
  },
  /** The note's pages as [title, text] pairs. */
  storedPages(sessionId: string, projectId = selectedProject) {
    return stored.get(noteKey(projectId, sessionId, 0))?.map((p) => [p.title, p.text]);
  },
  saveCount() {
    return saves.length;
  },
  jump(jump: NoteJump) {
    requestNoteJump(jump);
  },
  /** Rebind shortcuts as the shortcut editor stores them, without saving. */
  rebind(overrides: Record<string, unknown>) {
    settings.update((current) => ({ ...current, shortcutOverrides: overrides }));
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
