import { get } from 'svelte/store';
import { createFieldDictation } from '../../src/lib/utils/dictationField';
import { dictationTarget } from '../../src/lib/stores/dictationTarget';

// A field controller against an event bus standing in for the backend: what
// it shows while words are heard, where the final words go, and where it
// leaves dictation pointed when it stops.
const listeners = new Map<string, Set<(...args: unknown[]) => void>>();
const emit = (name: string, ...args: unknown[]) => listeners.get(name)?.forEach((cb) => cb(...args));
const targetCalls: string[] = [];
let listening = false;

const backend = new Proxy({
  SetDictationTarget: async (target: string) => { targetCalls.push(target); },
  ToggleDictation: async () => {
    listening = !listening;
    emit('dictation:state', listening);
  },
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});
(window as any).go = { main: { App: backend, DictationService: backend } };
(window as any).runtime = new Proxy({
  EventsOnMultiple: (name: string, callback: (...args: unknown[]) => void) => {
    if (!listeners.has(name)) listeners.set(name, new Set());
    listeners.get(name)!.add(callback);
    return () => listeners.get(name)?.delete(callback);
  },
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return (..._args: unknown[]) => () => undefined;
  },
});

const title = document.getElementById('title') as HTMLInputElement;
// The dialog stays open, so stopping leaves dictation on the field.
const dictation = createFieldDictation(() => title, undefined, () => 'field');

(window as any).dictationFixture = {
  toggle: () => dictation.toggle(),
  emit,
  interim: () => get(dictation.interim),
  listening: () => get(dictation.listening),
  targetCalls: () => [...targetCalls],
  target: () => get(dictationTarget),
};
document.body.dataset.fixtureReady = 'true';
