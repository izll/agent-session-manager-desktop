import { mount, tick } from 'svelte';
import FileBrowser from '../../src/lib/components/MainPanel/FileBrowser.svelte';
import Diff from '../../src/lib/components/MainPanel/Diff.svelte';
import { activeProjectId } from '../../src/lib/stores/projects';
import { selectedSessionId, selectedWindowIdx } from '../../src/lib/stores/sessions';

let browserReads = 0;
let diffReads = 0;
let failNextDiffRead = true;

const backend = new Proxy({
  ListSessionDirectory: async () => ({
    path: '',
    absPath: '/canonical/project-a',
    entries: [{ name: 'browser.txt', path: 'browser.txt', isDir: false, size: 22, modTime: '', unreadable: false }],
    truncated: false,
    totalEntries: 1,
  }),
  ReadSessionDirectoryFile: async () => {
    browserReads++;
    return {
      path: 'browser.txt', absPath: '/canonical/project-a/browser.txt',
      content: 'browser content marker', size: 22, binary: false, truncated: false,
    };
  },
  GetTabWorkingDirectory: async () => '/canonical/project-a',
  GetSessionDiffFileList: async () => ([{
    path: 'diff.txt', oldPath: '', status: 'modified', added: 1, removed: 1, binary: false,
  }]),
  GetSessionDiffForFile: async () => {
    diffReads++;
    if (failNextDiffRead) {
      failNextDiffRead = false;
      throw new Error('fixture diff read refused');
    }
    return {
      path: 'diff.txt', oldPath: '', status: 'modified', header: '',
      hunks: [{
        header: '@@ -1 +1 @@', body: '-old line\n+diff content marker',
        index: 0, added: 1, removed: 1, patch: '',
      }],
      added: 1, removed: 1, binary: false,
    };
  },
  GetSettings: async () => null,
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});

(window as any).go = { main: { App: backend, DictationService: backend } };
(window as any).runtime = new Proxy({}, { get: () => (..._args: unknown[]) => () => {} });
(window as any).projectContentFixture = {
  browserReads: () => browserReads,
  diffReads: () => diffReads,
};

activeProjectId.set('project-a');
selectedSessionId.set('same-session');
selectedWindowIdx.set(0);

const browser = document.getElementById('browser');
const diff = document.getElementById('diff');
if (!browser || !diff) throw new Error('fixture targets are missing');
mount(FileBrowser, { target: browser, props: { active: true } });
mount(Diff, { target: diff, props: { active: true, initialMode: 'session' } });
await tick();
await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
document.body.dataset.fixtureReady = 'true';
