import { mount, tick } from 'svelte';
import SettingsDialog from '../../src/lib/components/Dialogs/SettingsDialog.svelte';
import BgAgentsDialog from '../../src/lib/components/Dialogs/BgAgentsDialog.svelte';
import ServerManagerDialog from '../../src/lib/components/Dialogs/ServerManagerDialog.svelte';
import CommandManagerDialog from '../../src/lib/components/Dialogs/CommandManagerDialog.svelte';
import CommandPickerDialog from '../../src/lib/components/Dialogs/CommandPickerDialog.svelte';
import SchemeImportDialog from '../../src/lib/components/Dialogs/SchemeImportDialog.svelte';
import LogDialog from '../../src/lib/components/Dialogs/LogDialog.svelte';

// One page per dialog, picked by ?dialog=…, each opened over a mocked backend
// with enough data that the list and the footer are both on screen.
const HOUR = 60 * 60 * 1000;

const backend = new Proxy({
  ListBackgroundAgents: async () => [
    { id: 'bg-1', sessionId: '', pid: 101, cwd: '/home/user/projects/api', name: 'Refactor auth', status: 'running', startedAt: Date.now() - 2 * HOUR },
    { id: 'bg-2', sessionId: '', pid: 102, cwd: '/home/user/projects/web', name: 'Write docs', status: 'idle', startedAt: Date.now() - 26 * HOUR },
  ],
  GetServers: async () => [
    { id: 's1', name: 'build-box', host: '10.0.0.5', port: 22, user: 'ci', authMethod: 'agent', keyPath: '', jumpHostId: '', extraPath: '', isDefault: true, order: 0, hasPassword: false, displayName: 'build-box' },
    { id: 's2', name: 'staging', host: 'staging.example.com', port: 22, user: 'deploy', authMethod: 'key', keyPath: '~/.ssh/id_ed25519', jumpHostId: '', extraPath: '', isDefault: false, order: 1, hasPassword: false, displayName: 'staging' },
  ],
  KeyringAvailable: async () => true,
  GetCommands: async () => ({
    groups: [{ id: 'g1', name: 'Git', order: 0 }],
    commands: [
      { id: 'c1', name: 'Status', command: 'git status', description: '', groupId: 'g1', sendEnter: true, useCount: 3, placeholders: [] },
      { id: 'c2', name: 'Run tests', command: 'npm test', description: 'The whole suite', groupId: '', sendEnter: true, useCount: 1, placeholders: [] },
    ],
  }),
  GetLogSources: async () => [],
  DetectedEditor: async () => '',
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});

(window as any).go = { main: { App: backend } };
(window as any).runtime = new Proxy({}, { get: () => (..._args: unknown[]) => () => {} });

const dialogs: Record<string, { component: any; props?: Record<string, unknown> }> = {
  settings: { component: SettingsDialog },
  bgAgents: { component: BgAgentsDialog },
  servers: { component: ServerManagerDialog },
  commands: { component: CommandManagerDialog },
  commandPicker: { component: CommandPickerDialog },
  schemeImport: { component: SchemeImportDialog },
  logs: { component: LogDialog },
};

const name = new URLSearchParams(location.search).get('dialog') || 'settings';
const entry = dialogs[name];
if (!entry) throw new Error(`unknown dialog: ${name}`);

const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');
mount(entry.component, { target, props: { show: true, ...(entry.props || {}) } });
await tick();
await new Promise<void>((resolve) => setTimeout(resolve, 400));
document.body.dataset.fixtureReady = 'true';
