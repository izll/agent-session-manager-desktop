import { mount, tick } from 'svelte';
import InterruptedWorkDialog from '../../src/lib/components/Dialogs/InterruptedWorkDialog.svelte';
import { activeProjectId } from '../../src/lib/stores/projects';

// The "continue where you left off?" dialog over a mocked backend. The backend
// records what it was asked to reopen and forget; ?fail=<id> makes that
// session's reopen fail, the way a session whose directory is gone does.

const params = new URLSearchParams(location.search);
const failing = params.get('fail') || '';

const calls: { method: string; args: unknown[] }[] = [];
const listeners = new Map<string, ((data: unknown) => void)[]>();
const emit = (name: string, data: unknown) => (listeners.get(name) || []).forEach((cb) => cb(data));

const backend = new Proxy({
  DismissInterruptedWork: async (ids: string[], projectId: string) => {
    calls.push({ method: 'DismissInterruptedWork', args: [ids, projectId] });
  },
  ReopenInterruptedSessions: async (ids: string[], projectId: string) => {
    calls.push({ method: 'ReopenInterruptedSessions', args: [ids, projectId] });
    const results = [];
    for (const id of ids) {
      // Slow enough that the per-row progress is visible between the two.
      await new Promise((resolve) => setTimeout(resolve, 150));
      const result = id === failing
        ? { id, ok: false, error: 'error.notInterrupted' }
        : { id, ok: true };
      emit('interrupted:progress', result);
      results.push(result);
    }
    return results;
  },
  GetSessions: async () => [],
  GetGroups: async () => [],
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});

(window as any).go = { main: { App: backend } };
(window as any).runtime = new Proxy({
  EventsOnMultiple: (name: string, callback: (data: unknown) => void) => {
    listeners.set(name, [...(listeners.get(name) || []), callback]);
    return () => listeners.set(name, (listeners.get(name) || []).filter((cb) => cb !== callback));
  },
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return () => () => {};
  },
});

activeProjectId.set('project-a');

const sessions = [
  // Five tabs, three of them running when the machine went down.
  { id: 'api', name: 'API refactor', path: '/work/api', agent: 'claude', color: '', serverId: '',
    agents: ['claude', 'codex'], reopenTabs: 3, totalTabs: 5 },
  { id: 'docs', name: 'Docs', path: '/work/docs', agent: 'gemini', color: '', serverId: '',
    agents: ['gemini'], reopenTabs: 1, totalTabs: 1 },
  { id: 'ops', name: 'Ops on build-box', path: '/srv/ops', agent: 'terminal', color: '', serverId: 's1',
    agents: ['terminal'], reopenTabs: 2, totalTabs: 2 },
];

const done: unknown[] = [];
const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');
const component = mount(InterruptedWorkDialog, {
  target,
  props: { show: true, sessions, projectName: params.get('project') || '' },
  events: { done: (e: CustomEvent) => done.push(e.detail) },
} as any);

(window as any).interruptedFixture = {
  calls: () => structuredClone(calls),
  done: () => structuredClone(done),
};
void component;

await tick();
document.body.dataset.fixtureReady = 'true';
