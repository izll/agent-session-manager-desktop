import { mount, tick } from 'svelte';
import TokenUsage from '../../src/lib/components/Dashboard/TokenUsage.svelte';

// The usage view over a mocked backend. ?empty shows no usage; ?hold leaves the
// scan running until the test finishes it, so the running state can be read.

const params = new URLSearchParams(location.search);

const item = (overrides: Record<string, unknown>) => ({
  machine: 'local', machineName: '', conversation: 'aaaaaaaa-1', agent: 'claude', cwd: '/work/api', lastUnix: 1_790_000_000,
  model: 'claude-opus-5-5', subagent: false, input: 0, output: 0, cacheRead: 0, cacheWrite: 0, responses: 1,
  projectId: '', projectName: '', sessionId: 'api', sessionName: 'API refactor', tabName: '',
  ...overrides,
});

const items = params.has('empty') ? [] : [
  item({ output: 600, cacheRead: 900_000, cacheWrite: 100 }),
  item({ conversation: 'aaaaaaaa-2', tabName: 'review', output: 100, subagent: true }),
  item({ machine: 'srv-1', machineName: 'web server', sessionId: 'site', sessionName: 'Site', projectId: 'p1', projectName: 'Clients', conversation: 'bbbbbbbb-1', model: 'gpt-6', agent: 'codex', output: 200 }),
  item({ sessionId: '', sessionName: '', cwd: '/home/me/scratch', conversation: 'cccccccc-1', output: 100 }),
];

const machines = [
  { id: 'local', name: '', local: true, lastScanUnix: 1_790_000_000, error: '' },
  { id: 'srv-1', name: 'web server', local: false, lastScanUnix: 1_789_000_000, error: 'error.serverNotConnected' },
];

const calls: { method: string; args: unknown[] }[] = [];
const listeners = new Map<string, ((data: unknown) => void)[]>();
const emit = (name: string, data?: unknown) => (listeners.get(name) || []).forEach((cb) => cb(data));

const backend = new Proxy({
  GetTokenUsage: async (days: number) => {
    calls.push({ method: 'GetTokenUsage', args: [days] });
    // A narrower period has less in it, so a period change is visible.
    return { items: days === 1 ? items.slice(0, 1) : items, machines, scanning: false };
  },
  RefreshTokenUsage: async () => {
    calls.push({ method: 'RefreshTokenUsage', args: [] });
    if (!params.has('hold')) emit('tokenUsage:updated');
  },
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
  EventsOff: (name: string) => listeners.delete(name),
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return () => () => {};
  },
});

(window as any).tokenUsageFixture = {
  calls: () => structuredClone(calls),
  emit,
};

mount(TokenUsage, { target: document.getElementById('fixture')! });
await tick();
document.body.dataset.fixtureReady = 'true';
