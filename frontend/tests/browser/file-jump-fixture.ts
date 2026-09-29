import { mount, tick } from 'svelte';
import FileBrowser from '../../src/lib/components/MainPanel/FileBrowser.svelte';
import { activeProjectId } from '../../src/lib/stores/projects';
import { selectedSessionId, selectedWindowIdx } from '../../src/lib/stores/sessions';
import { requestFileJump, requestFolderJump } from '../../src/lib/stores/fileJump';

// A tree taller than the pane. The tree lists folders before files, so forty
// folders at the root push zz/ below the fold, and forty files in zz/deep/
// push target.txt below it again once zz/ is scrolled into view.
const file = (path: string) => ({ name: path.split('/').pop()!, path, isDir: false, size: 10, modTime: '', unreadable: false });
const dir = (path: string) => ({ name: path.split('/').pop()!, path, isDir: true, size: 0, modTime: '', unreadable: false });
const listings: Record<string, ReturnType<typeof file>[]> = {
  '': [...Array.from({ length: 40 }, (_, i) => dir(`d${String(i).padStart(2, '0')}`)), dir('zz'), file('root.txt')],
  zz: [dir('zz/deep')],
  'zz/deep': [dir('zz/deep/inner'), ...Array.from({ length: 40 }, (_, i) => file(`zz/deep/a${String(i).padStart(2, '0')}.txt`)), file('zz/deep/target.txt')],
  'zz/deep/inner': [file('zz/deep/inner/x.txt')],
};

const backend = new Proxy({
  ListSessionDirectory: async (_session: string, path: string) => ({
    path, absPath: `/repo/${path}`, entries: listings[path] ?? [], truncated: false,
    totalEntries: (listings[path] ?? []).length,
  }),
  ReadSessionDirectoryFile: async (_session: string, path: string) => ({
    path, absPath: `/repo/${path}`, content: `content of ${path}`, size: 10, binary: false, truncated: false,
  }),
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
(window as any).fileJumpFixture = { requestFileJump, requestFolderJump };

activeProjectId.set('project-a');
selectedSessionId.set('s');
selectedWindowIdx.set(0);

const target = document.getElementById('browser');
if (!target) throw new Error('fixture target is missing');
mount(FileBrowser, { target, props: { active: true } });
await tick();
await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
document.body.dataset.fixtureReady = 'true';
