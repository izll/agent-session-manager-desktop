import { mount, tick } from 'svelte';
import FileBrowser from '../../src/lib/components/MainPanel/FileBrowser.svelte';
import UndoToast from '../../src/lib/components/common/UndoToast.svelte';
import { activeProjectId } from '../../src/lib/stores/projects';
import { selectedSessionId, selectedWindowIdx } from '../../src/lib/stores/sessions';
import { appError } from '../../src/lib/stores/appErrors';

// A tree whose files can be deleted and put back, as the backend would: the
// listing follows what was deleted, and every call is recorded.
const file = (path: string) => ({ name: path.split('/').pop()!, path, isDir: false, size: 10, modTime: '', unreadable: false });
const dir = (path: string) => ({ name: path.split('/').pop()!, path, isDir: true, size: 0, modTime: '', unreadable: false });
const tree: Record<string, string[]> = { '': ['a.txt', 'b.txt'], docs: ['docs/guide.md'] };
const held = new Map<string, string>();
const calls: string[] = [];
let failNextDelete = '';

const backend = new Proxy({
  ListSessionDirectory: async (_session: string, path: string) => {
    const entries = [...(path === '' ? [dir('docs')] : []), ...(tree[path] ?? []).map(file)];
    return { path, absPath: `/repo/${path}`, entries, truncated: false, totalEntries: entries.length };
  },
  ReadSessionDirectoryFile: async (_session: string, path: string) => ({
    path, absPath: `/repo/${path}`, content: `content of ${path}`, size: 10, binary: false, truncated: false,
  }),
  DeleteSessionFile: async (_session: string, path: string, _window: number, root: string, project: string) => {
    calls.push(`delete ${path} ${root} ${project}`);
    if (failNextDelete) {
      const message = failNextDelete;
      failNextDelete = '';
      throw message;
    }
    const parent = path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : '';
    tree[parent] = tree[parent].filter((p) => p !== path);
    const token = `token-${calls.length}`;
    held.set(token, path);
    return token;
  },
  RestoreDeletedSessionFile: async (token: string) => {
    calls.push(`restore ${token}`);
    const path = held.get(token)!;
    held.delete(token);
    const parent = path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : '';
    tree[parent] = [...tree[parent], path].sort();
  },
  DiscardDeletedSessionFile: async (token: string) => {
    calls.push(`discard ${token}`);
    held.delete(token);
  },
  GetTabWorkingDirectory: async () => '/repo',
  GetSettings: async () => null,
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});

(window as any).go = { main: { App: backend, DictationService: backend } };
(window as any).runtime = new Proxy({}, { get: () => (..._args: unknown[]) => () => {} });
let lastError: string | null = null;
appError.subscribe((message) => { if (message) lastError = message; });
(window as any).fileDeleteFixture = {
  calls: () => [...calls],
  failNextDelete: (message: string) => { failNextDelete = message; },
  lastError: () => lastError,
};

activeProjectId.set('project-a');
selectedSessionId.set('s');
selectedWindowIdx.set(0);

const target = document.getElementById('browser');
const toast = document.getElementById('toast');
if (!target || !toast) throw new Error('fixture target is missing');
mount(FileBrowser, { target, props: { active: true } });
mount(UndoToast, { target: toast });
await tick();
await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
document.body.dataset.fixtureReady = 'true';
