import { mount } from 'svelte';
import SessionItem from '../../src/lib/components/Sidebar/SessionItem.svelte';
import { sessions, selectedSessionId, type Session } from '../../src/lib/stores/sessions';
import { registerUnsavedGuard } from '../../src/lib/stores/unsavedChanges';
import { activeProjectId } from '../../src/lib/stores/projects';

const deleted: string[] = [];
let approveDiscard: (() => void) | null = null;
let deferTaskLookup = false;
let resolveTaskLookup: ((value: { title: string }[]) => void) | null = null;

const backend = new Proxy({
  UnfinishedTasksForSession: () => deferTaskLookup
    ? new Promise<{ title: string }[]>((resolve) => { resolveTaskLookup = resolve; })
    : Promise.resolve([{ title: 'unfinished fixture task' }]),
  DeleteSession: async (id: string) => { deleted.push(id); },
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});

(window as any).go = { main: { App: backend } };
(window as any).runtime = new Proxy({}, { get: () => (..._args: unknown[]) => () => {} });

const session: Session = {
  id: 'delete-target', name: 'Delete Target', path: '/fixture', status: 'stopped', agent: 'claude',
  color: '', bgColor: '', fullRowColor: false, groupId: '', autoYes: false, hideStatusLine: false,
  notes: '', favorite: false, resumeSessionId: '', followedWindows: [], tabOrder: [], mainWindowStopped: false,
  extraArgs: '', tabTextColor: '', tabBackgroundColor: '', terminalTheme: '', terminalFontSize: 0,
  hideViewBar: 0, hideStatusBar: 0, mainWindowIndex: 0, lastWindowIndex: 0, isGitRepo: false,
};

sessions.set([session]);
selectedSessionId.set(session.id);
activeProjectId.set('project-a');
registerUnsavedGuard({
  isDirty: () => true,
  requestDiscard: (continuation) => { approveDiscard = continuation; },
});

(window as any).sessionDeleteFixture = {
  deleted: () => [...deleted],
  hasPendingDiscard: () => !!approveDiscard,
  approveDiscard: () => {
    const continuation = approveDiscard;
    approveDiscard = null;
    continuation?.();
  },
  deferTaskLookup: () => { deferTaskLookup = true; },
  taskLookupPending: () => !!resolveTaskLookup,
  switchProject: (id: string) => activeProjectId.set(id),
  resolveTaskLookup: () => {
    const resolve = resolveTaskLookup;
    resolveTaskLookup = null;
    resolve?.([{ title: 'stale project task' }]);
  },
};

const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');
mount(SessionItem, { target, props: { session, index: 0 } });
