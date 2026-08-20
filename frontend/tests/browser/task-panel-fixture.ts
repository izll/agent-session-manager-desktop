import { mount } from 'svelte';
import TaskPanel from '../../src/lib/components/MainPanel/TaskPanel.svelte';
import { selectedSessionId } from '../../src/lib/stores/sessions';

const tasks = [
  {
    id: '1', title: 'README és TUI frissítése', description: '', details: '', status: 'pending',
    priority: 'medium', tags: [], dependencies: [], createdAt: '2026-08-14T12:00:00Z',
    updatedAt: '2026-08-14T12:00:00Z', subtasks: [{ id: '1', title: 'ellenőrzés', status: 'pending' }],
  },
  {
    id: '2', title: 'Rendezés javítása', description: '', details: '', status: 'pending',
    priority: 'medium', tags: [], dependencies: ['1'], createdAt: '2026-08-15T12:00:00Z',
    updatedAt: '2026-08-15T12:00:00Z', subtasks: [],
  },
  {
    id: '3', title: 'Commitok nézegetése', description: '', details: '', status: 'in-progress',
    priority: 'medium', tags: [], dependencies: [], createdAt: '2026-08-15T12:00:00Z',
    updatedAt: '2026-08-15T12:00:00Z', subtasks: [], complexity: 8,
  },
  {
    id: '4', title: 'Minden metaadat egyszerre keskeny panelen', description: '', details: '', status: 'blocked',
    priority: 'critical', tags: [], dependencies: ['1', '2'], createdAt: '2026-08-15T12:00:00Z',
    updatedAt: '2026-08-15T12:00:00Z', dueAt: '2026-08-21T12:30:00Z', complexity: 9,
    subtasks: [
      { id: '1', title: 'kész', status: 'done', done: true },
      { id: '2', title: 'hátra van', status: 'pending' },
    ],
  },
];

const backend = new Proxy({ GetTasks: async () => tasks }, {
  get(target, key) {
    if (key in target) return target[key as keyof typeof target];
    return async () => undefined;
  },
});
(window as any).go = { main: { App: backend, DictationService: backend } };
(window as any).runtime = new Proxy({}, {
  get: () => (..._args: unknown[]) => () => {},
});

selectedSessionId.set('layout-session');
const target = document.getElementById('fixture');
if (!target) throw new Error('fixture target is missing');
mount(TaskPanel, { target, props: { active: true } });
