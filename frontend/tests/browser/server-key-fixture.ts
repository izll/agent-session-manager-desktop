import { mount, tick } from 'svelte';
import ServerManagerDialog from '../../src/lib/components/Dialogs/ServerManagerDialog.svelte';

// The server form over a mocked backend. The key dialog stays open until the test
// answers it, so typing during it can be tried.

const calls: unknown[][] = [];
let answer: (path: string) => void = () => {};
let refuse: (reason: string) => void = () => {};

const backend = new Proxy({
  GetServers: async () => [],
  BrowseKeyFile: (current: string) => {
    calls.push([current]);
    return new Promise<string>((resolve, reject) => { answer = resolve; refuse = (reason) => reject(new Error(reason)); });
  },
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => [];
  },
});

(window as any).go = { main: { App: backend } };
(window as any).runtime = new Proxy({}, { get: () => () => () => {} });
(window as any).serverKeyFixture = { calls: () => structuredClone(calls), answer: (path: string) => answer(path), refuse: (reason: string) => refuse(reason) };

mount(ServerManagerDialog, { target: document.getElementById('fixture')!, props: { show: true } });
await tick();
document.body.dataset.fixtureReady = 'true';
