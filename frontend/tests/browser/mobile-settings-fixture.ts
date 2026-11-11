import { mount, tick } from 'svelte';
import { get } from 'svelte/store';
import MobileAccessSection from '../../src/lib/components/Dialogs/MobileAccessSection.svelte';
import { selectedSessionId, selectedWindowIdx } from '../../src/lib/stores/sessions';
import { activeProjectId } from '../../src/lib/stores/projects';

// The mobile access section over a stand-in backend. ?busy makes the port taken,
// ?running starts with the server on, ?devices starts with one phone paired.

const params = new URLSearchParams(location.search);
const calls: { method: string; args: unknown[] }[] = [];
const step = params.get('step') || '';
let running = params.has('running') || step === 'serve' || step === 'phone';
let setupAsks = 0;
let port = 9760;
let devices = params.has('devices')
  ? [{ id: 'd1', name: 'iPhone', createdAt: '2026-11-09T10:00:00Z', lastSeen: '2026-11-09T11:00:00Z' }]
  : [];
// A 1x1 PNG, enough to be an image.
const PNG = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==';

const backend = new Proxy({
  GetMobileStatus: async () => ({ running, port, error: '' }),
  GetMobileSetup: async () => {
    setupAsks++;
    const order = ['install', 'login', 'enable', 'serve', 'phone'];
    const at = order.indexOf(step || 'phone');
    return {
      step: step || 'phone', installed: at > 0, loggedIn: at > 1, serving: at > 3,
      tailnetName: at > 1 ? 'gep.tail1234.ts.net' : '', platform: params.get('platform') || 'linux',
      commands: params.get('platform') === 'darwin'
        ? { install: '', login: '', serve: `/Applications/Tailscale.app/Contents/MacOS/Tailscale serve --bg http://127.0.0.1:${port}` }
        : { install: 'curl -fsSL https://tailscale.com/install.sh | sh', login: 'sudo tailscale up --operator=$USER',
            serve: `sudo tailscale serve --bg http://127.0.0.1:${port}` },
    };
  },
  SetMobileAccess: async (enabled: boolean, wanted: number) => {
    calls.push({ method: 'SetMobileAccess', args: [enabled, wanted] });
    // Like the backend: a refused port leaves what was served as it was.
    if (enabled && params.has('busy')) throw new Error('port already in use: ' + wanted);
    running = enabled;
    port = wanted;
    return { running, port, error: '' };
  },
  CreateMobilePairing: async () => {
    calls.push({ method: 'CreateMobilePairing', args: [] });
    return { code: 'abc', url: `http://127.0.0.1:${port}/#pair=abc`, qrPng: PNG, expiresAt: new Date(Date.now() + 300000).toISOString() };
  },
  GetSessions: async () => [],
  GetGroups: async () => [],
  ListMobileDevices: async () => structuredClone(devices),
  RevokeMobileDevice: async (id: string) => {
    calls.push({ method: 'RevokeMobileDevice', args: [id] });
    devices = devices.filter((device) => device.id !== id);
  },
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});

(window as any).go = { main: { App: backend } };
(window as any).runtime = new Proxy({
  BrowserOpenURL: (url: string) => { calls.push({ method: 'BrowserOpenURL', args: [url] }); },
  ClipboardSetText: async (text: string) => { calls.push({ method: 'ClipboardSetText', args: [text] }); return true; },
} as Record<string, unknown>, { get: (target, key) => (key in target ? target[key as string] : () => () => {}) });
(window as any).mobileFixture = {
  calls: () => structuredClone(calls),
  setupAsks: () => setupAsks,
  selectedWindow: () => get(selectedWindowIdx),
};
activeProjectId.set('p1');
selectedSessionId.set(params.has('nosession') ? null : 's1');
selectedWindowIdx.set(0);

mount(MobileAccessSection, { target: document.getElementById('fixture')! });
await tick();
document.body.dataset.fixtureReady = 'true';
