import { mount, tick } from 'svelte';
import FileBrowser from '../../src/lib/components/MainPanel/FileBrowser.svelte';
import { activeProjectId } from '../../src/lib/stores/projects';
import { selectedSessionId, selectedWindowIdx } from '../../src/lib/stores/sessions';
import { requestFileJump } from '../../src/lib/stores/fileJump';

// A repository with a README that uses what READMEs use — headings, a table, a
// task list, an image beside it, links in and out — and a few things a hostile
// one could try.
const file = (path: string) => ({ name: path.split('/').pop()!, path, isDir: false, size: 10, modTime: '', unreadable: false });
const dir = (path: string) => ({ name: path.split('/').pop()!, path, isDir: true, size: 0, modTime: '', unreadable: false });
const listings: Record<string, ReturnType<typeof file>[]> = {
  '': [dir('docs'), file('README.md'), file('notes.txt')],
  docs: [file('docs/guide.md'), file('docs/logo.png')],
};

const readme = [
  '# Fixture Project',
  '',
  'See the [guide](docs/guide.md#setup), the [site](https://example.com/site) and [usage](#usage-1).',
  '',
  '![Logo](docs/logo.png)',
  '![Outside](../../secret.png)',
  '',
  '| Name | Value |',
  '| ---- | ----- |',
  '| a    | 1     |',
  '',
  '- [x] done',
  '- [ ] open',
  '',
  '<img src="x" onerror="window.__pwned = true">',
  '<script>window.__pwned = true</script>',
  '<a href="javascript:window.__pwned = true">evil</a>',
  '',
  '## Usage',
  '',
  'First.',
  '',
  ...Array.from({ length: 60 }, (_, i) => `Line ${i}.\n`),
  '## Usage',
  '',
  'The end.',
].join('\n');

const contents: Record<string, string> = {
  'README.md': readme,
  'docs/guide.md': '# Guide\n\n## Setup\n\nRun it.\n',
  'notes.txt': '# not markdown\n',
};

const openedURLs: string[] = [];
const imageCalls: string[] = [];
// A 1x1 transparent PNG.
const pixel = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=';

const backend = new Proxy({
  ListSessionDirectory: async (_session: string, path: string) => ({
    path, absPath: `/repo/${path}`, entries: listings[path] ?? [], truncated: false,
    totalEntries: (listings[path] ?? []).length,
  }),
  ReadSessionDirectoryFile: async (_session: string, path: string) => ({
    path, absPath: `/repo/${path}`, content: contents[path] ?? `content of ${path}`,
    size: (contents[path] ?? '').length, binary: false, truncated: false,
  }),
  OpenSessionFileForEdit: async (_session: string, path: string) => ({
    path, absPath: `/repo/${path}`, root: '/repo', text: contents[path] ?? '',
    shape: { bom: false, lineEnding: 'lf', trailingNewline: true }, version: 'v1', mode: 0o644,
    size: (contents[path] ?? '').length, editable: true,
  }),
  ReadSessionImage: async (_session: string, path: string) => {
    imageCalls.push(path);
    return pixel;
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
(window as any).runtime = new Proxy({
  BrowserOpenURL: (url: string) => { openedURLs.push(url); },
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return (..._args: unknown[]) => () => {};
  },
});
(window as any).markdownFixture = {
  requestFileJump,
  openedURLs: () => [...openedURLs],
  imageCalls: () => [...imageCalls],
};

try { localStorage.removeItem('asmgr.browser.markdownMode'); } catch { /* fresh profile */ }
activeProjectId.set('project-a');
selectedSessionId.set('s');
selectedWindowIdx.set(0);

const target = document.getElementById('browser');
if (!target) throw new Error('fixture target is missing');
mount(FileBrowser, { target, props: { active: true } });
await tick();
await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
document.body.dataset.fixtureReady = 'true';
