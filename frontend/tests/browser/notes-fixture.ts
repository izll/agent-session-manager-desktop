import { mount } from 'svelte';
import Notes from '../../src/lib/components/MainPanel/Notes.svelte';
import { selectedSessionId, selectedWindowIdx } from '../../src/lib/stores/sessions';

const stored = new Map<string, string>([
  ['notes-a:0', 'saved A'],
  ['notes-b:0', 'saved B'],
]);
let failNextASave = true;

const backend = new Proxy({
  GetTabNotes: async (sessionId: string, windowIdx: number) => {
    if (sessionId === 'notes-load-fails') throw new Error('load refused');
    return stored.get(`${sessionId}:${windowIdx}`) ?? '';
  },
  SetTabNotes: async (sessionId: string, windowIdx: number, value: string) => {
    if (sessionId === 'notes-a' && failNextASave) {
      failNextASave = false;
      throw new Error('save refused');
    }
    stored.set(`${sessionId}:${windowIdx}`, value);
  },
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
};

selectedSessionId.set('notes-a');
selectedWindowIdx.set(0);
const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');
mount(Notes, { target, props: { active: true } });
