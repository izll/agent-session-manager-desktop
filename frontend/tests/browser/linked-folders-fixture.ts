import { mount, tick } from 'svelte';
import LinkedFoldersDialog from '../../src/lib/components/Dialogs/LinkedFoldersDialog.svelte';
import { selectedSessionId, selectedWindowIdx } from '../../src/lib/stores/sessions';
import { activeProjectId } from '../../src/lib/stores/projects';

// The linked folders' window over a stand-in backend. ?linked=/a,/b starts with
// those linked; /work/plain is not a repository, /work/gone no longer exists.

const params = new URLSearchParams(location.search);
const calls: { method: string; args: unknown[] }[] = [];
let linked = (params.get('linked') ?? '').split(',').filter(Boolean);
const folders = () => linked.map((path) => ({
  path, isGitRepo: !path.endsWith('plain'), missing: path.endsWith('gone'),
}));
const entry = (name: string) => ({ name, path: name, isDir: false, size: 10, modTime: '', unreadable: false });

const backend = new Proxy({
  GetTabLinkedFolders: async () => folders(),
  AddTabLinkedFolder: async (_s: string, _w: number, dir: string, project: string) => {
    calls.push({ method: 'AddTabLinkedFolder', args: [dir, project] });
    if (!linked.includes(dir)) linked = [...linked, dir];
    return folders();
  },
  RemoveTabLinkedFolder: async (_s: string, _w: number, dir: string) => {
    calls.push({ method: 'RemoveTabLinkedFolder', args: [dir] });
    linked = linked.filter((d) => d !== dir);
    return folders();
  },
  BrowseDirectory: async () => '/work/lib',
  GetGitHistory: async (_s: string, _branch: string, _skip: number, _w: number, root: string) => {
    calls.push({ method: 'GetGitHistory', args: [root] });
    return { path: root, repository: true, branch: 'main', commits: [
      { hash: 'abc1234def', shortHash: 'abc1234', subject: 'First commit', author: 'Tester', date: '2026-11-13T08:00:00Z', refs: [] },
    ], hasMore: false, skip: 0, unpushed: 0 };
  },
  ListGitBranches: async (_s: string, _w: number, root: string) => {
    calls.push({ method: 'ListGitBranches', args: [root] });
    return { current: 'main', branches: [{ name: 'main', current: true, remote: false }] };
  },
  GetGitCommitFiles: async () => [],
  ReadSessionDirectoryFile: async (_s: string, path: string, _w: number, root: string) => {
    calls.push({ method: 'ReadSessionDirectoryFile', args: [path, root] });
    return { path, absPath: `${root}/${path}`, content: `content of ${path}`, size: 10, binary: false, truncated: false };
  },
  OpenSessionFileForEdit: async (_s: string, path: string, _w: number, root: string) => ({
    path, absPath: `${root}/${path}`, root, text: `content of ${path}`,
    shape: { bom: false, lineEnding: 'lf', trailingNewline: true }, version: 'v1', mode: 0o644, size: 10, editable: true,
  }),
  GetFullDiffFileList: async (_s: string, _w: number, root: string) => {
    calls.push({ method: 'GetFullDiffFileList', args: [root] });
    return [{ path: 'changed.txt', oldPath: '', status: 'modified', added: 1, removed: 1, binary: false }];
  },
  GetFullDiff: async () => ({ content: '', added: 1, removed: 1 }),
  GetDiffHiddenRules: async (_s: string, _w: number, root: string) => ({ repo: root, rules: [] }),
  ListSessionDirectory: async (_s: string, path: string, _w: number, root: string) => {
    calls.push({ method: 'ListSessionDirectory', args: [path, root] });
    // ?slow: the root arrives late, as a big folder's would.
    if (params.has('slow')) await new Promise((resolve) => setTimeout(resolve, 400));
    return { path, absPath: root, entries: [entry('README.md')], truncated: false, totalEntries: 1 };
  },
  GetSettings: async () => null,
  GetSessions: async () => [],
  GetGroups: async () => [],
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});

(window as any).go = { main: { App: backend, DictationService: backend } };
(window as any).runtime = new Proxy({}, { get: () => (..._args: unknown[]) => () => {} });
(window as any).linkedFixture = {
  calls: () => structuredClone(calls),
  switchTab: () => selectedWindowIdx.set(3),
};
activeProjectId.set('p1');
selectedSessionId.set('s1');
selectedWindowIdx.set(0);

const props = { show: true, projectId: 'p1', sessionId: 's1', windowIdx: 0, serverId: params.get('server') ?? '', startPath: '/work' };
mount(LinkedFoldersDialog, { target: document.getElementById('fixture')!, props });
await tick();
document.body.dataset.fixtureReady = 'true';
