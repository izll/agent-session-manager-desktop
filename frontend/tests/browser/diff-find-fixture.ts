import { mount } from 'svelte';
import Diff from '../../src/lib/components/MainPanel/Diff.svelte';
import GitHistoryDialog from '../../src/lib/components/Dialogs/GitHistoryDialog.svelte';
import { selectedSessionId, selectedWindowIdx } from '../../src/lib/stores/sessions';
import { activeProjectId } from '../../src/lib/stores/projects';
import { loadSettings } from '../../src/lib/stores/settings';

/**
 * Find in every diff renderer.
 *
 * ?component=diff|history, ?view=whole|hunks, ?sbs=1 for two columns chosen.
 *
 * The files are chosen for the renderers they end up in: a brand-new and a
 * deleted file are drawn in one column even with two chosen — the case that
 * lost the find bar — and the modified file is drawn in two. Each is long
 * enough that its second match is far below the fold, so the virtualised
 * whole-file view has not rendered that row when the search runs.
 */
const params = new URLSearchParams(location.search);
const component = params.get('component') ?? 'diff';
const whole = params.get('view') !== 'hunks';
const sideBySide = params.get('sbs') === '1';

const LENGTH = 300;
const FAR = 250;

function lines(marker: string) {
  return Array.from({ length: LENGTH }, (_, i) => {
    // Upper case on one of them: the search ignores case.
    if (i === 5) return `${marker}const needle = ${i};`;
    if (i === FAR) return `${marker}return NEEDLE_${i};`;
    return `${marker}line ${i}`;
  }).join('\n') + '\n';
}

function modifiedBody() {
  const out: string[] = [];
  for (let i = 0; i < LENGTH; i++) {
    if (i === 5) out.push('-old needle 5', '+new needle 5');
    else if (i === FAR) out.push('-old value', '+new NEEDLE far');
    else out.push(` line ${i}`);
  }
  return out.join('\n') + '\n';
}

const bodies: Record<string, { status: string; header: string; body: string }> = {
  'added.txt': { status: 'added', header: `@@ -0,0 +1,${LENGTH} @@`, body: lines('+') },
  'deleted.txt': { status: 'deleted', header: `@@ -1,${LENGTH} +0,0 @@`, body: lines('-') },
  'modified.txt': { status: 'modified', header: `@@ -1,${LENGTH} +1,${LENGTH} @@`, body: modifiedBody() },
};

function summaries() {
  return Object.entries(bodies).map(([path, file]) => ({
    path,
    oldPath: '',
    status: file.status,
    added: file.status === 'deleted' ? 0 : 2,
    removed: file.status === 'added' ? 0 : 2,
    binary: false,
  }));
}

function diffFile(path: string) {
  const file = bodies[path];
  return {
    ...summaries().find((s) => s.path === path),
    header: '',
    hunks: [{ header: file.header, body: file.body, index: 0, added: 0, removed: 0, patch: '' }],
  };
}

let stored: Record<string, unknown> = {
  diffSideBySide: sideBySide,
  diffHunksOnly: !whole,
  diffFlatFileList: true,
  // The file under test opens first.
  diffLastFile: { 'p:s:0:session': params.get('file') ?? 'added.txt' },
  shortcuts: {},
};

const backend = new Proxy({
  GetSettings: async () => ({ ...stored }),
  SaveSettings: async (next: Record<string, unknown>) => { stored = { ...next }; },
  GetTabWorkingDirectory: async () => '/repo',
  GetSessionDiffFileList: async () => summaries(),
  GetSessionDiffForFile: async (_s: string, path: string) => diffFile(path),
  GetGitHistory: async () => ({
    repository: true,
    commits: [{ hash: 'abc123', shortHash: 'abc123', subject: 'a commit', author: 'x', date: '' }],
    hasMore: false,
    skip: 1,
    unpushed: 0,
  }),
  ListGitBranches: async () => ({ branches: [] }),
  GetGitCommitFiles: async () => summaries(),
  GetGitCommitDiff: async (_s: string, _h: string, path: string) => diffFile(path),
  GetLockStatus: async () => ({ locked: true, otherInstancePid: 0 }),
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});

(window as any).go = { main: { App: backend, DictationService: backend } };
(window as any).runtime = new Proxy({}, { get: () => (..._args: unknown[]) => () => {} });

activeProjectId.set('p');
selectedSessionId.set('s');
selectedWindowIdx.set(0);

await loadSettings();

const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');
if (component === 'history') {
  const first = params.get('file') ?? 'added.txt';
  // The dialog opens on the first file of the commit.
  const order = [first, ...Object.keys(bodies).filter((p) => p !== first)];
  backend.GetGitCommitFiles = async () => order.map((p) => summaries().find((s) => s.path === p));
  mount(GitHistoryDialog, {
    target,
    props: { show: true, projectId: 'p', sessionId: 's', windowIdx: 0, path: '/repo' },
  });
} else {
  mount(Diff, { target, props: { active: true, initialMode: 'session' } });
}
document.body.dataset.fixtureReady = 'true';
