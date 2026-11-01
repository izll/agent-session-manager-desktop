import { mount, tick } from 'svelte';
import PasswordPickerDialog from '../../src/lib/components/Dialogs/PasswordPickerDialog.svelte';
import PasswordManagerDialog from '../../src/lib/components/Dialogs/PasswordManagerDialog.svelte';
import { get } from 'svelte/store';
import { sessions, selectedSessionId, selectedWindowIdx } from '../../src/lib/stores/sessions';
import { activeProjectId } from '../../src/lib/stores/projects';

// Both dialogs over a mocked backend. ?agent puts an agent in the tab, ?manager opens
// the manager, ?nokeyring takes the keyring away, ?nosession selects nothing.
// ?custom adds a source of the user's commands, ?slowInsert holds an insertion until
// it is called off.

const params = new URLSearchParams(location.search);
const calls: { method: string; args: unknown[] }[] = [];

let local = [
  { source: 'local', id: 'l1', name: 'Prod DB', username: 'root', group: '' },
  { source: 'local', id: 'l2', name: 'Mail', username: '', group: '' },
];
const vault = [{ source: 'keepassxc', id: 'Work/Vault entry', name: 'Vault entry', username: '', group: 'Work' }];
let keepassLocked = true;
let keepassDb = '/home/me/v.kdbx';
let accounts = params.has('accounts')
  ? [
      { source: 'bitwarden:a1', name: 'Office', available: true, enabled: true, locked: true, detail: 'https://vault-a.example.com', keyFile: '', error: '' },
      { source: 'bitwarden:a2', name: 'Plant', available: true, enabled: true, locked: true, detail: 'https://vault-b.example.com', keyFile: '', error: '' },
    ]
  : [];
let customSources = params.has('custom')
  ? [{ source: 'custom:c1', name: 'My tool', tool: 'mytool', available: true, enabled: true, locked: false, detail: '', keyFile: '', error: '', listCommand: 'mytool ls', getCommand: 'mytool get {name}' }]
  : [];
let releaseInsert: (() => void) | null = null;

// ?hold=Method keeps that call waiting until the test lets it go.
let releaseHeld: () => void = () => {};
const held = new Promise<void>((resolve) => { releaseHeld = resolve; });
const hold = async (method: string) => { if (params.get('hold') === method) await held; };

const backend = new Proxy({
  GetPasswords: async () => { await hold('GetPasswords'); return ({
    entries: [...(keepassLocked ? local : [...local, ...vault]), ...customSources.map((source) => ({ source: source.source, id: 'Work/Tool entry', name: 'Tool entry', username: '', group: 'Work' }))],
    keyringAvailable: !params.has('nokeyring'),
    bitwardenInstalled: params.has('bw'),
    sources: [
      { source: 'local', available: true, enabled: true, locked: false, detail: '', keyFile: '', error: '' },
      { source: 'keepassxc', available: true, enabled: !!keepassDb, locked: keepassLocked, detail: keepassDb, keyFile: '', error: '' },
      ...accounts,
      ...customSources,
    ],
  }); },
  UnlockPasswordSource: async (source: string, master: string) => {
    calls.push({ method: 'UnlockPasswordSource', args: [source, master] });
    await hold('UnlockPasswordSource');
    if (master !== 'right') throw new Error('error.passwordWrongMaster');
    keepassLocked = false;
  },
  LockPasswordSources: async () => { calls.push({ method: 'LockPasswordSources', args: [] }); keepassLocked = true; },
  InsertPassword: async (...args: unknown[]) => {
    calls.push({ method: 'InsertPassword', args });
    await hold('InsertPassword');
    if (params.has('slowInsert')) await new Promise<void>((resolve) => { releaseInsert = resolve; });
    if (params.has('failInsert')) throw new Error('error.passwordToolFailed');
  },
  SaveLocalPassword: async (...args: unknown[]) => {
    calls.push({ method: 'SaveLocalPassword', args });
    const [id, name, username] = args as string[];
    if (!name) throw new Error('error.passwordNameRequired');
    if (id) local = local.map((entry) => entry.id === id ? { ...entry, name, username } : entry);
    else local = [...local, { source: 'local', id: 'new', name, username, group: '' }];
    return id || 'new';
  },
  DeleteLocalPassword: async (id: string) => {
    calls.push({ method: 'DeleteLocalPassword', args: [id] });
    local = local.filter((entry) => entry.id !== id);
  },
  SetKeePassXCDatabase: async (database: string, keyFile: string) => {
    calls.push({ method: 'SetKeePassXCDatabase', args: [database, keyFile] });
    keepassDb = database;
  },
  SaveBitwardenAccount: async (name: string, server: string) => {
    calls.push({ method: 'SaveBitwardenAccount', args: [name, server] });
    await hold('SaveBitwardenAccount');
    if (server === 'bad') throw new Error('error.passwordBadServerUrl');
    accounts = [...accounts, { source: 'bitwarden:new', name, available: true, enabled: true, locked: true, detail: server, keyFile: '', error: '' }];
    return 'new';
  },
  DeleteBitwardenAccount: async (id: string) => {
    calls.push({ method: 'DeleteBitwardenAccount', args: [id] });
    accounts = accounts.filter((account) => account.source !== `bitwarden:${id}`);
  },
  CancelPasswordInsert: async () => { calls.push({ method: 'CancelPasswordInsert', args: [] }); releaseInsert?.(); },
  SaveCustomPasswordSource: async (...args: unknown[]) => {
    calls.push({ method: 'SaveCustomPasswordSource', args });
    const [id, name, listCommand, getCommand] = args as string[];
    if (getCommand.includes("'")) throw new Error('error.passwordBadCommand');
    const saved = { source: `custom:${id || 'new'}`, name, tool: listCommand.split(' ')[0], available: false, enabled: true, locked: false, detail: '', keyFile: '', error: '', listCommand, getCommand };
    customSources = [...customSources.filter((source) => source.source !== saved.source), saved];
    return id || 'new';
  },
  DeleteCustomPasswordSource: async (id: string) => {
    calls.push({ method: 'DeleteCustomPasswordSource', args: [id] });
    customSources = customSources.filter((source) => source.source !== `custom:${id}`);
  },
  OpenBitwardenLogin: async (...args: unknown[]) => {
    calls.push({ method: 'OpenBitwardenLogin', args });
    await hold('OpenBitwardenLogin');
    if (params.has('remoteTab')) throw new Error('error.bitwardenLoginNeedsLocalTab');
    return 7;
  },
  GetInstances: async () => [],
  BrowsePasswordFile: async () => '/picked/file.kdbx',
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});

(window as any).go = { main: { App: backend } };
(window as any).runtime = new Proxy({}, { get: () => () => () => {} });
(window as any).passwordFixture = { calls: () => structuredClone(calls), selectedWindow: () => get(selectedWindowIdx), release: () => releaseHeld() };

activeProjectId.set('p1');
sessions.set([{ id: 's1', agent: params.has('agent') ? 'claude' : 'terminal', followedWindows: [] }] as any);

const target = document.getElementById('fixture')!;
if (params.has('manager')) {
  selectedSessionId.set(params.has('nosession') ? '' : 's1');
  mount(PasswordManagerDialog, {
    target,
    props: {
      show: true,
      sessionId: params.has('nosession') ? '' : 's1',
      windowIdx: 2,
      onDone: () => { (window as any).doneCalled = true; },
    },
  });
} else {
  mount(PasswordPickerDialog, {
    target,
    props: { show: true, sessionId: params.has('nosession') ? '' : 's1', windowIdx: 0, onOpenManager: () => { (window as any).managerAsked = true; } },
  });
}
await tick();
document.body.dataset.fixtureReady = 'true';
