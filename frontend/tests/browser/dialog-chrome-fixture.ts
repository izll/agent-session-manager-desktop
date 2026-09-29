import { mount, tick } from 'svelte';
import { activeProjectId, projects } from '../../src/lib/stores/projects';
import { sessions, selectedSessionId, selectedWindowIdx } from '../../src/lib/stores/sessions';
import { agents } from '../../src/lib/stores/agents';
import { settings } from '../../src/lib/stores/settings';

// One page per dialog, picked by ?dialog=…, each opened over a mocked backend
// with enough data that the header, the list and the footer are all on screen.
// Dialogs that live inside a bigger component — the task modals, the tab bar's
// extra-args editor, the task overview's detail — mount that component and
// open the dialog the way a user would.
const HOUR = 60 * 60 * 1000;

// Long enough to overrun any dialog's header: the task dialogs name the task
// they are about, and a task title is a sentence.
const LONG_TITLE = 'Minden metaadat egyszerre keskeny panelen, a határidővel, a prioritással és a függőségekkel együtt, tördelés nélkül';

const session = {
  id: 'session-a', name: 'Main tab', path: '/home/user/projects/api', status: 'stopped',
  agent: 'claude', color: '', bgColor: '', fullRowColor: false, groupId: '',
  autoYes: false, hideStatusLine: false, notes: '', favorite: false,
  resumeSessionId: '', extraArgs: '--main-old', tabTextColor: '',
  tabBackgroundColor: '', terminalTheme: '', terminalFontSize: 0,
  hideViewBar: 0, hideStatusBar: 0, mainWindowIndex: 0, lastWindowIndex: 0,
  isGitRepo: true, tabOrder: [0, 1], mainWindowStopped: true,
  followedWindows: [{
    index: 1, name: 'Second tab', agent: 'claude', stopped: true,
    extra_args: '--second-old', hide_view_bar: 0, hide_status_bar: 0,
  }],
};

const tasks = [
  {
    id: '1', title: 'README és TUI frissítése', description: '', details: '', status: 'pending',
    priority: 'medium', tags: [], dependencies: [], createdAt: '2026-08-14T12:00:00Z',
    updatedAt: '2026-08-14T12:00:00Z', subtasks: [],
  },
  {
    id: '2', title: LONG_TITLE, description: '', details: '', status: 'pending',
    priority: 'high', tags: [], dependencies: ['1'], createdAt: '2026-08-15T12:00:00Z',
    updatedAt: '2026-08-15T12:00:00Z', subtasks: [],
  },
];

const params = new URLSearchParams(location.search);
const mcp = params.get('dialog') === 'taskPRD' || params.get('dialog') === 'taskComplexity';

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
  GetAgents: async () => [{ type: 'claude', name: 'Claude', icon: '', supportsResume: true, supportsAutoYes: true, supportsFork: true }],
  GetAgentsForServer: async () => [{ type: 'claude', name: 'Claude', icon: '', supportsResume: true, supportsAutoYes: true, supportsFork: true }],
  GetSessionTemplates: async () => [],
  GetProjects: async () => [{ id: 'project-a', name: 'Project A', isLocked: false }],
  GetActiveProjectID: async () => 'project-a',
  GetProjectSessions: async () => [],
  GetSessions: async () => [session],
  GetGroups: async () => [],
  GetTrashItems: async () => [],
  GetBackups: async () => [],
  GetTaskBackups: async () => [],
  GetQuickJump: async () => [],
  DiscoverLocalSchemes: async () => [],
  ListOnlineSchemes: async () => [],
  GetChangelog: async () => [{
    version: '1.1.18', date: '2026-09-29', intro: [],
    sections: [{ kind: 'added', title: 'Added', intro: [], items: ['**The favourites section folds.** Press `Enter`.'] }],
  }],
  GetVersion: async () => '1.1.18',
  CheckForUpdate: async () => ({ available: true, currentVersion: '1.0.0', latestVersion: '1.1.0', canAutoInstall: true }),
  ReadSessionFile: async () => ({
    token: 'token', path: '/tmp/session.json', exportedAt: '2026-08-21T10:00:00Z',
    sessions: [{ name: 'Portable', path: '/portable', agent: 'claude', tabs: 1, pathExists: true }],
  }),
  ListCheckpoints: async () => [],
  ListServerDirectory: async () => ({ path: '/home/deploy', entries: [] }),
  GetResumeSessionsOn: async () => [],
  ListGitBranches: async () => ({ branches: [{ name: 'master', current: true }] }),
  GetGitHistory: async () => ({ repository: true, commits: [], hasMore: false, skip: 0 }),
  GetGitCommitFiles: async () => [],
  GetAllTasks: async () => [{
    ...tasks[1], projectId: 'project-a', projectName: 'Project A', projectPath: '/repo-a',
    sessionName: 'Main tab', overdue: false,
  }],
  GetTasks: async () => tasks,
  TaskMasterGetTasks: async () => { if (mcp) return tasks; throw new Error('fixture MCP unavailable'); },
  TaskMasterStatus: async () => ({ initialized: true, running: true, error: null }),
  TaskMasterAnalyzeComplexity: async () => 'Task 2: complexity 8',
  GetExtraArgs: async () => '--main-old',
  GetDictationSettings: async () => ({ enabled: false, bufferMode: false, mode: 'streaming', bufferCloseOnSend: true }),
  GetAvailableLanguages: async () => [],
  GetInputDevices: async () => [],
  GetDictationProblems: async () => [],
  DetectionPatternsVersion: async () => 1,
  GetProjectGitSummaries: async () => [],
  GetClaudeUsage: async () => ({ available: false }),
  GetCodexUsage: async () => ({ available: false }),
}, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});

(window as any).go = { main: { App: backend, DictationService: backend } };
(window as any).runtime = new Proxy({}, { get: () => (..._args: unknown[]) => () => {} });

activeProjectId.set('project-a');
projects.set([{ id: 'project-a', name: 'Project A', isLocked: false } as any]);
sessions.set([session as any]);
selectedSessionId.set(session.id);
selectedWindowIdx.set(0);
agents.set([{ type: 'claude', name: 'Claude', icon: '', supportsResume: true, supportsAutoYes: true, supportsFork: true } as any]);
if (mcp) settings.update((value) => ({ ...value, taskMasterEnabled: true }));

const wait = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

async function until<T>(find: () => T | null | undefined, what: string): Promise<T> {
  for (let i = 0; i < 100; i++) {
    const found = find();
    if (found) return found;
    await wait(50);
  }
  throw new Error(`fixture: ${what} never appeared`);
}

function buttonWith(text: RegExp, root: ParentNode = document) {
  return [...root.querySelectorAll('button')].find((b) => text.test(b.textContent || '')) as HTMLButtonElement | undefined;
}

async function rightClick(find: () => Element | null | undefined, what: string) {
  const el = await until(find, what);
  const box = el.getBoundingClientRect();
  el.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: box.left + 5, clientY: box.top + 5, button: 2 }));
  await tick();
}

// The long-titled task, so the dialogs named after it show how a sentence fits.
async function taskMenu(item: RegExp) {
  await rightClick(() => [...document.querySelectorAll('.task-item')].find((row) => row.textContent?.includes('Minden metaadat')), 'task row');
  const menu = await until(() => document.querySelector('.context-menu'), 'task context menu');
  (await until(() => buttonWith(item, menu), `menu item ${item}`)).click();
}

type Entry = {
  load: () => Promise<{ default: any }>;
  props?: Record<string, unknown>;
  open?: () => Promise<void>;
};

const taskPanel = () => import('../../src/lib/components/MainPanel/TaskPanel.svelte');

const dialogs: Record<string, Entry> = {
  settings: { load: () => import('../../src/lib/components/Dialogs/SettingsDialog.svelte') },
  bgAgents: { load: () => import('../../src/lib/components/Dialogs/BgAgentsDialog.svelte') },
  servers: { load: () => import('../../src/lib/components/Dialogs/ServerManagerDialog.svelte') },
  commands: { load: () => import('../../src/lib/components/Dialogs/CommandManagerDialog.svelte') },
  commandPicker: { load: () => import('../../src/lib/components/Dialogs/CommandPickerDialog.svelte'), props: { sessionId: session.id, windowIdx: 0 } },
  schemeImport: { load: () => import('../../src/lib/components/Dialogs/SchemeImportDialog.svelte') },
  logs: { load: () => import('../../src/lib/components/Dialogs/LogDialog.svelte') },
  checkpoints: { load: () => import('../../src/lib/components/Dialogs/CheckpointsDialog.svelte'), props: { projectId: 'project-a', sessionId: session.id, windowIdx: 0, root: session.path } },
  customColor: { load: () => import('../../src/lib/components/Dialogs/CustomColorDialog.svelte'), props: { initial: '#61afef', name: 'Main tab' } },
  customGradient: { load: () => import('../../src/lib/components/Dialogs/CustomGradientDialog.svelte'), props: { name: 'Main tab' } },
  feedback: { load: () => import('../../src/lib/components/Dialogs/FeedbackDialog.svelte') },
  fork: { load: () => import('../../src/lib/components/Dialogs/ForkDialog.svelte') },
  gitHistory: { load: () => import('../../src/lib/components/Dialogs/GitHistoryDialog.svelte'), props: { projectId: 'project-a', sessionId: session.id, windowIdx: 0, path: session.path } },
  help: { load: () => import('../../src/lib/components/Dialogs/HelpDialog.svelte') },
  import: { load: () => import('../../src/lib/components/Dialogs/ImportDialog.svelte') },
  newGroup: { load: () => import('../../src/lib/components/Dialogs/NewGroupDialog.svelte') },
  newSession: { load: () => import('../../src/lib/components/Dialogs/NewSessionDialog.svelte') },
  newTab: { load: () => import('../../src/lib/components/Dialogs/NewTabDialog.svelte'), props: { sessionId: session.id } },
  quickJump: { load: () => import('../../src/lib/components/Dialogs/QuickJumpDialog.svelte') },
  recovery: { load: () => import('../../src/lib/components/Dialogs/RecoveryCenterDialog.svelte') },
  remoteDir: { load: () => import('../../src/lib/components/Dialogs/RemoteDirPicker.svelte'), props: { serverId: 's1', startPath: '/home/deploy' } },
  resumePicker: { load: () => import('../../src/lib/components/Dialogs/ResumeSessionPickerDialog.svelte'), props: { session } },
  saveAsTemplate: { load: () => import('../../src/lib/components/Dialogs/SaveAsTemplateDialog.svelte'), props: { session } },
  sessionColor: { load: () => import('../../src/lib/components/Dialogs/SessionColorDialog.svelte'), props: { session } },
  sessionFile: { load: () => import('../../src/lib/components/Dialogs/SessionFileDialog.svelte') },
  templates: { load: () => import('../../src/lib/components/Dialogs/SessionTemplateDialog.svelte') },
  tabColor: { load: () => import('../../src/lib/components/Dialogs/TabColorDialog.svelte'), props: { sessionId: session.id, tab: { Index: 1, Name: 'Second tab' } } },
  update: { load: () => import('../../src/lib/components/Dialogs/UpdateDialog.svelte') },
  whatsNew: { load: () => import('../../src/lib/components/Dialogs/WhatsNewDialog.svelte') },
  interruptedWork: {
    load: () => import('../../src/lib/components/Dialogs/InterruptedWorkDialog.svelte'),
    props: {
      sessions: [
        { id: 'a', name: 'Main tab', path: '/home/user/projects/api', agent: 'claude', color: '', serverId: '', agents: ['claude'], reopenTabs: 1, totalTabs: 2 },
        { id: 'b', name: LONG_TITLE, path: '/home/user/projects/web', agent: 'codex', color: '', serverId: '', agents: ['codex'], reopenTabs: 1, totalTabs: 1 },
      ],
    },
  },
  taskDetail: {
    load: () => import('../../src/lib/components/Dashboard/AllTasks.svelte'),
    open: async () => {
      (await until(() => buttonWith(/Minden metaadat/), 'task overview row')).click();
    },
  },
  taskAdd: {
    load: taskPanel, props: { active: true },
    open: async () => {
      await until(() => document.querySelector('.task-item'), 'task row');
      (await until(() => buttonWith(/Add Task/), 'Add Task')).click();
    },
  },
  taskEdit: { load: taskPanel, props: { active: true }, open: () => taskMenu(/Edit/) },
  taskSubtask: { load: taskPanel, props: { active: true }, open: () => taskMenu(/Add Subtask/i) },
  taskDependencies: { load: taskPanel, props: { active: true }, open: () => taskMenu(/Dependencies/i) },
  taskPRD: {
    load: taskPanel, props: { active: true },
    open: async () => {
      await until(() => document.querySelector('.task-item'), 'task row');
      (await until(() => buttonWith(/Parse PRD/i), 'Parse PRD')).click();
    },
  },
  taskComplexity: {
    load: taskPanel, props: { active: true },
    open: async () => {
      await until(() => document.querySelector('.task-item'), 'task row');
      (await until(() => buttonWith(/Analy/i), 'Analyze')).click();
    },
  },
  extraArgs: {
    load: () => import('../../src/lib/components/MainPanel/TabBar.svelte'), props: { visible: true },
    open: async () => {
      await rightClick(() => document.querySelector('.tab'), 'a tab');
      (await until(() => buttonWith(/Edit Extra Args/), 'Edit Extra Args')).click();
    },
  },
};

(window as any).dialogChromeFixture = { dialogs: Object.keys(dialogs) };

const name = params.get('dialog') || 'settings';
const entry = dialogs[name];
if (!entry) throw new Error(`unknown dialog: ${name}`);

const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');
const component = (await entry.load()).default;
// A dialog opens through `show`; a host component opens its dialog by hand.
mount(component, { target, props: entry.open ? entry.props : { show: true, ...entry.props } });
await tick();
await entry.open?.();
await until(() => document.querySelector('.dialog-content'), 'the dialog');
await wait(400);
document.body.dataset.fixtureReady = 'true';
